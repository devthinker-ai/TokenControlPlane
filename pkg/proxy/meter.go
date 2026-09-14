package proxy

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/devthinker-ai/TokenControlPlane/pkg/auth"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

// estimatedTokens converts raw byte counts to estimated tokens (bytes/4).
// WHY bytes/4: we never unmarshal JSON streams; this is the metering invariant.
func estimatedTokens(bytesIn, bytesOut int64) int64 {
	return (bytesIn + bytesOut) / 4
}

// countingReader counts bytes read from an underlying reader.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// countingResponseWriter tees writes to the real ResponseWriter and counts bytes.
// It forwards Flush so SSE frames are not buffered by our wrapper.
type countingResponseWriter struct {
	http.ResponseWriter
	n       int64
	wrote   bool
	status  int
	flusher http.Flusher
}

func newCountingResponseWriter(w http.ResponseWriter) *countingResponseWriter {
	cw := &countingResponseWriter{ResponseWriter: w, status: http.StatusOK}
	if f, ok := w.(http.Flusher); ok {
		cw.flusher = f
	}
	return cw
}

func (c *countingResponseWriter) WriteHeader(code int) {
	if !c.wrote {
		c.status = code
		c.wrote = true
		c.ResponseWriter.WriteHeader(code)
	}
}

func (c *countingResponseWriter) Write(p []byte) (int, error) {
	if !c.wrote {
		c.WriteHeader(http.StatusOK)
	}
	n, err := c.ResponseWriter.Write(p)
	c.n += int64(n)
	return n, err
}

func (c *countingResponseWriter) Flush() {
	if c.flusher != nil {
		c.flusher.Flush()
	}
}

// Ensure interfaces are satisfied for ReverseProxy streaming.
var (
	_ http.ResponseWriter = (*countingResponseWriter)(nil)
	_ http.Flusher        = (*countingResponseWriter)(nil)
)

type pendingUsage struct {
	bytesIn          int64
	bytesOut         int64
	requests         int64
	exactTokens      int64 // >0 when LLM usage parsed
	estimatedTokens  int64 // >0 when forced estimate; 0 + exact=0 → compute bytes/4
	useExact         bool  // when true, tokens = exactTokens (+ estimatedTokens if any)
	lastFlush        time.Time
}

// Meter records per-key (and per-server/provider) byte/token usage with throttled SQL flushes
// (at most one write per key+server per second).
type Meter struct {
	store *store.Store
	auth  *auth.Validator

	mu      sync.Mutex
	pending map[string]*pendingUsage // key: keyID\0kind\0id (kind=s|p)

	now        func() time.Time
	flushEvery time.Duration
}

// NewMeter creates a metering accumulator backed by store.
func NewMeter(s *store.Store, v *auth.Validator) *Meter {
	return &Meter{
		store:      s,
		auth:       v,
		pending:    make(map[string]*pendingUsage),
		now:        func() time.Time { return time.Now().UTC() },
		flushEvery: time.Second,
	}
}

func meterKey(keyID, serverID string) string {
	return keyID + "\x00s\x00" + serverID
}

func meterProviderKey(keyID, providerID string) string {
	return keyID + "\x00p\x00" + providerID
}

// Middleware is retained for tests that wrap a handler directly.
// Production uses meteringAccessLog (meter + slog in one pass).
func (m *Meter) Middleware(next http.Handler) http.Handler {
	return meteringAccessLog(m, nil)(next)
}

// Record accumulates usage and flushes to SQL when the per-key throttle allows.
// serverID may be empty (legacy callers); prefer RecordForServer.
func (m *Meter) Record(keyID string, bytesIn, bytesOut, requests int64) {
	m.RecordForServer(keyID, "", bytesIn, bytesOut, requests)
}

// RecordForServer accumulates usage for a key on a specific server (bytes/4 estimate).
func (m *Meter) RecordForServer(keyID, serverID string, bytesIn, bytesOut, requests int64) {
	m.record(meterKey(keyID, serverID), keyID, "s", serverID, bytesIn, bytesOut, requests, 0, false)
}

// RecordForProvider accumulates LLM usage. When exactTokens >= 0 and useExact,
// tokens are recorded as exact; otherwise bytes/4 estimate.
func (m *Meter) RecordForProvider(keyID, providerID string, bytesIn, bytesOut, requests, exactTokens int64, useExact bool) {
	m.record(meterProviderKey(keyID, providerID), keyID, "p", providerID, bytesIn, bytesOut, requests, exactTokens, useExact)
}

func (m *Meter) record(id, keyID, kind, targetID string, bytesIn, bytesOut, requests, exactTokens int64, useExact bool) {
	m.mu.Lock()
	p, ok := m.pending[id]
	if !ok {
		p = &pendingUsage{}
		m.pending[id] = p
	}
	p.bytesIn += bytesIn
	p.bytesOut += bytesOut
	p.requests += requests
	if useExact {
		p.useExact = true
		p.exactTokens += exactTokens
	}
	now := m.now()
	shouldFlush := p.lastFlush.IsZero() || now.Sub(p.lastFlush) >= m.flushEvery
	var flush *pendingUsage
	if shouldFlush {
		flush = &pendingUsage{
			bytesIn: p.bytesIn, bytesOut: p.bytesOut, requests: p.requests,
			exactTokens: p.exactTokens, useExact: p.useExact,
		}
		p.bytesIn, p.bytesOut, p.requests = 0, 0, 0
		p.exactTokens, p.useExact = 0, false
		p.lastFlush = now
	}
	m.mu.Unlock()

	if flush != nil {
		if err := m.flushOne(keyID, kind, targetID, flush); err != nil {
			m.mu.Lock()
			p := m.pending[id]
			if p == nil {
				p = &pendingUsage{}
				m.pending[id] = p
			}
			p.bytesIn += flush.bytesIn
			p.bytesOut += flush.bytesOut
			p.requests += flush.requests
			if flush.useExact {
				p.useExact = true
				p.exactTokens += flush.exactTokens
			}
			p.lastFlush = time.Time{}
			m.mu.Unlock()
		}
	}
}

// Flush forces all pending usage to SQLite (tests / shutdown).
func (m *Meter) Flush(ctx context.Context) error {
	m.mu.Lock()
	type snap struct {
		keyID, kind, targetID string
		p                     *pendingUsage
	}
	var list []snap
	for id, p := range m.pending {
		if p.bytesIn == 0 && p.bytesOut == 0 && p.requests == 0 && p.exactTokens == 0 {
			continue
		}
		keyID, kind, targetID, _ := splitMeterKey(id)
		list = append(list, snap{keyID, kind, targetID, &pendingUsage{
			bytesIn: p.bytesIn, bytesOut: p.bytesOut, requests: p.requests,
			exactTokens: p.exactTokens, useExact: p.useExact,
		}})
		p.bytesIn, p.bytesOut, p.requests = 0, 0, 0
		p.exactTokens, p.useExact = 0, false
		p.lastFlush = m.now()
	}
	m.mu.Unlock()

	for _, s := range list {
		if err := m.flushOne(s.keyID, s.kind, s.targetID, s.p); err != nil {
			return err
		}
	}
	return nil
}

func splitMeterKey(id string) (keyID, kind, targetID string, ok bool) {
	// format: keyID \0 kind \0 targetID
	parts := make([]string, 0, 3)
	start := 0
	for i := 0; i < len(id); i++ {
		if id[i] == 0 {
			parts = append(parts, id[start:i])
			start = i + 1
		}
	}
	parts = append(parts, id[start:])
	if len(parts) == 3 {
		return parts[0], parts[1], parts[2], true
	}
	// legacy: keyID \0 serverID
	if len(parts) == 2 {
		return parts[0], "s", parts[1], true
	}
	return id, "s", "", false
}

func (m *Meter) flushOne(keyID, kind, targetID string, p *pendingUsage) error {
	var tokens, exact, estimated int64
	if p.useExact && p.exactTokens > 0 {
		tokens = p.exactTokens
		exact = p.exactTokens
	} else {
		tokens = estimatedTokens(p.bytesIn, p.bytesOut)
		estimated = tokens
	}
	period := store.PeriodStartUTC(m.now())
	var err error
	if kind == "p" {
		err = m.store.IncrUsageWithProvider(context.Background(), keyID, targetID, period,
			p.bytesIn, p.bytesOut, tokens, exact, estimated, p.requests)
		if err == nil && m.auth != nil {
			m.auth.AddProviderTokensUsed(keyID, targetID, tokens)
		}
	} else {
		err = m.store.IncrUsageWithServer(context.Background(), keyID, targetID, period,
			p.bytesIn, p.bytesOut, tokens, p.requests)
		if err == nil && m.auth != nil {
			m.auth.AddServerTokensUsed(keyID, targetID, tokens)
		}
	}
	if err == nil {
		if acctID, aerr := m.store.AccountIDForKey(context.Background(), keyID); aerr == nil && acctID != "" {
			_ = m.store.IncrDailyUsage(context.Background(), acctID, m.now(), tokens, p.requests)
		}
	}
	return err
}
