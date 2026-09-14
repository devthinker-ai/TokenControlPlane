// Package stdio manages local MCP subprocesses (npx/uvx/binaries) and bridges
// them to the gateway's streamable-HTTP client contract.
package stdio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

const (
	StatusRunning  = "running"
	StatusStarting = "starting"
	StatusError    = "error"
	StatusStopped  = "stopped"
)

const (
	defaultMaxInFlight  = 4
	defaultInFlightWait = 30 * time.Second
	defaultInitTimeout  = 10 * time.Second
	defaultMaxStrikes   = 3
	stderrLineCap       = 20
	stderrByteCap       = 8 << 10
)

// CircuitOpener opens the per-server circuit after stdio strikes are exhausted.
type CircuitOpener interface {
	ForceOpen(serverID string)
	Reset(serverID string)
	Allow(serverID string) bool
}

// ActivityEmitter records throttled server_restart events.
type ActivityEmitter interface {
	InsertActivityEvent(ctx context.Context, accountID, kind, actor, summary string, detail any) error
}

// Config tunes ProcessManager behaviour (tests shorten waits).
type Config struct {
	MaxInFlight  int
	InFlightWait time.Duration
	InitTimeout  time.Duration
	MaxStrikes   int
	SandboxRoot  string // empty → ~/.tokencontrolplane/sandbox
}

// Manager owns one stdio MCP client per server_id (shared by indexer + proxy).
type Manager struct {
	store    *store.Store
	breaker  CircuitOpener
	activity ActivityEmitter
	logger   *slog.Logger
	cfg      Config

	mu    sync.Mutex
	procs map[string]*process
}

type process struct {
	mu sync.Mutex

	serverID  string
	accountID string
	name      string
	cfgSnap   spawnConfig

	client     *mcpclient.Client
	initResult *mcp.InitializeResult
	status     string
	pid        int
	startedAt  time.Time
	strikes    int
	spawnCount int
	lastErr    string
	lastExit   int
	stderrBuf  *bytes.Buffer
	inFlight   atomic.Int32
	sem        chan struct{}

	closed bool
}

type spawnConfig struct {
	command      string
	args         []string
	env          []string
	workdir      string
	cwdIsolation bool
}

// Status is the JSON shape for GET /api/v1/servers/{id}/status (stdio).
type Status struct {
	Transport string   `json:"transport"`
	State     string   `json:"state"` // running|starting|error|stopped
	PID       int      `json:"pid,omitempty"`
	UptimeSec int64    `json:"uptime_sec,omitempty"`
	InFlight  int      `json:"in_flight"`
	Strikes   int      `json:"strikes"`
	SpawnCount int     `json:"spawn_count,omitempty"`
	LastError string   `json:"last_error,omitempty"`
	LastExit  int      `json:"last_exit_code,omitempty"`
	Stderr    []string `json:"stderr_tail,omitempty"`
	Circuit   string   `json:"circuit,omitempty"`
}

// NewManager creates an empty process registry.
func NewManager(st *store.Store, breaker CircuitOpener, logger *slog.Logger, cfg Config) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.MaxInFlight <= 0 {
		cfg.MaxInFlight = defaultMaxInFlight
	}
	if cfg.InFlightWait <= 0 {
		cfg.InFlightWait = defaultInFlightWait
	}
	if cfg.InitTimeout <= 0 {
		cfg.InitTimeout = defaultInitTimeout
	}
	if cfg.MaxStrikes <= 0 {
		cfg.MaxStrikes = defaultMaxStrikes
	}
	return &Manager{
		store:   st,
		breaker: breaker,
		logger:  logger,
		cfg:     cfg,
		procs:   map[string]*process{},
	}
}

// SetActivityEmitter wires optional activity logging.
func (m *Manager) SetActivityEmitter(a ActivityEmitter) {
	m.activity = a
}

// CloseAll stops every child (gateway SIGTERM).
func (m *Manager) CloseAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.procs))
	for id := range m.procs {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		_ = m.Stop(id)
	}
}

// Stop closes the stdio client for serverID (delete / shutdown).
func (m *Manager) Stop(serverID string) error {
	m.mu.Lock()
	p := m.procs[serverID]
	delete(m.procs, serverID)
	m.mu.Unlock()
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	if p.client != nil {
		_ = p.client.Close()
		p.client = nil
	}
	p.status = StatusStopped
	return nil
}

// Restart stops and clears strikes/circuit, then starts fresh (reindex path).
func (m *Manager) Restart(ctx context.Context, serverID string) error {
	_ = m.Stop(serverID)
	if m.breaker != nil {
		m.breaker.Reset(serverID)
	}
	// Re-create process slot with cleared strikes.
	m.mu.Lock()
	m.procs[serverID] = &process{
		serverID:  serverID,
		status:    StatusStopped,
		sem:       make(chan struct{}, m.cfg.MaxInFlight),
		stderrBuf: &bytes.Buffer{},
	}
	m.mu.Unlock()
	_, err := m.Ensure(ctx, serverID)
	return err
}

// StatusSnapshot returns process status for the dashboard.
func (m *Manager) StatusSnapshot(serverID string) Status {
	m.mu.Lock()
	p := m.procs[serverID]
	m.mu.Unlock()
	st := Status{Transport: "stdio", State: StatusStopped, InFlight: 0}
	if p == nil {
		return st
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	st.State = p.status
	st.PID = p.pid
	st.Strikes = p.strikes
	st.SpawnCount = p.spawnCount
	st.LastError = p.lastErr
	st.LastExit = p.lastExit
	st.InFlight = int(p.inFlight.Load())
	if !p.startedAt.IsZero() && p.status == StatusRunning {
		st.UptimeSec = int64(time.Since(p.startedAt).Seconds())
	}
	if p.stderrBuf != nil {
		st.Stderr = sanitizeStderrTail(p.stderrBuf.String())
	}
	return st
}

// SpawnCount is exposed for tests.
func (m *Manager) SpawnCount(serverID string) int {
	m.mu.Lock()
	p := m.procs[serverID]
	m.mu.Unlock()
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.spawnCount
}

// Ensure lazily starts the stdio process and returns a ready client.
func (m *Manager) Ensure(ctx context.Context, serverID string) (*mcpclient.Client, error) {
	if m.breaker != nil && !m.breaker.Allow(serverID) {
		return nil, fmt.Errorf("circuit open for stdio server")
	}

	srv, err := m.store.GetServer(ctx, serverID)
	if err != nil {
		return nil, err
	}
	if srv.Transport != store.TransportStdio {
		return nil, fmt.Errorf("server %s is not stdio", serverID)
	}

	snap, err := spawnConfigFromServer(srv)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	p, ok := m.procs[serverID]
	if !ok {
		p = &process{
			serverID:  serverID,
			accountID: srv.AccountID,
			name:      srv.Name,
			status:    StatusStopped,
			sem:       make(chan struct{}, m.cfg.MaxInFlight),
			stderrBuf: &bytes.Buffer{},
		}
		m.procs[serverID] = p
	}
	m.mu.Unlock()

	p.mu.Lock()
	defer p.mu.Unlock()
	p.cfgSnap = snap
	p.accountID = srv.AccountID
	p.name = srv.Name

	if p.client != nil && p.status == StatusRunning {
		return p.client, nil
	}
	return m.startLocked(ctx, p)
}

func (m *Manager) startLocked(ctx context.Context, p *process) (*mcpclient.Client, error) {
	if p.closed {
		return nil, fmt.Errorf("process stopped")
	}
	// MaxStrikes failures allowed; attempts = MaxStrikes+1 so missing-cmd tests see spawn_count==4.
	maxAttempts := m.cfg.MaxStrikes + 1
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if p.closed {
			return nil, fmt.Errorf("process stopped")
		}
		p.status = StatusStarting
		p.spawnCount++
		if p.client != nil {
			_ = p.client.Close()
			p.client = nil
		}
		p.stderrBuf.Reset()

		client, pid, err := m.spawn(ctx, p)
		if err != nil {
			lastErr = err
			p.strikes++
			p.lastErr = err.Error()
			p.status = StatusError
			p.pid = 0
			m.emitRestart(p, err)
			continue
		}

		initCtx, cancel := context.WithTimeout(ctx, m.cfg.InitTimeout)
		initRes, err := client.Initialize(initCtx, mcp.InitializeRequest{
			Params: mcp.InitializeParams{
				ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
				ClientInfo: mcp.Implementation{
					Name:    "tokencontrolplane",
					Version: "1.0.0",
				},
			},
		})
		cancel()
		if err != nil {
			_ = client.Close()
			lastErr = err
			p.strikes++
			p.lastErr = fmt.Sprintf("initialize: %v", err)
			p.status = StatusError
			m.emitRestart(p, err)
			continue
		}

		p.client = client
		p.initResult = initRes
		p.pid = pid
		p.startedAt = time.Now().UTC()
		p.status = StatusRunning
		p.lastErr = ""
		// Strikes clear only on explicit Restart (manual reindex) — auto-recover accumulates.
		if m.breaker != nil && p.strikes == 0 {
			m.breaker.Reset(p.serverID)
		}
		return client, nil
	}

	if m.breaker != nil {
		m.breaker.ForceOpen(p.serverID)
	}
	p.status = StatusError
	if lastErr == nil {
		lastErr = fmt.Errorf("too many spawn failures")
	}
	if p.lastErr == "" {
		p.lastErr = lastErr.Error()
	}
	return nil, lastErr
}

func (m *Manager) spawn(ctx context.Context, p *process) (*mcpclient.Client, int, error) {
	cfg := p.cfgSnap
	resolved, err := ResolveCommand(cfg.command)
	if err != nil {
		return nil, 0, err
	}

	workdir := cfg.workdir
	if workdir == "" && cfg.cwdIsolation {
		if m.cfg.SandboxRoot != "" {
			workdir = m.cfg.SandboxRoot + "/" + p.serverID
			if err := os.MkdirAll(workdir, 0o700); err != nil {
				return nil, 0, err
			}
		} else {
			workdir, err = DefaultSandboxDir(p.serverID)
			if err != nil {
				return nil, 0, err
			}
		}
	}

	stderrWriter := &lockedWriter{buf: p.stderrBuf, max: stderrByteCap}

	c, err := mcpclient.NewStdioMCPClientWithOptions(
		resolved,
		cfg.env,
		cfg.args,
		transport.WithCommandStderrWriter(stderrWriter),
		transport.WithCommandFunc(func(ctx context.Context, command string, env []string, args []string) (*exec.Cmd, error) {
			cmd := exec.CommandContext(ctx, command, args...)
			cmd.Env = MergeEnviron(env)
			if workdir != "" {
				cmd.Dir = workdir
			}
			return cmd, nil
		}),
	)
	if err != nil {
		msg := err.Error()
		if strings.Contains(strings.ToLower(msg), "executable file not found") ||
			strings.Contains(strings.ToLower(msg), "no such file") {
			return nil, 0, fmt.Errorf("%s", PathNotFoundMessage(cfg.command))
		}
		return nil, 0, err
	}
	// PID is not exported by mcp-go; leave 0 — status still reports running/uptime.
	return c, 0, nil
}

func spawnConfigFromServer(srv *store.MCPServer) (spawnConfig, error) {
	if strings.TrimSpace(srv.Command) == "" {
		return spawnConfig{}, fmt.Errorf("stdio server requires command")
	}
	args, err := ParseArgsJSON(srv.ArgsJSON)
	if err != nil {
		return spawnConfig{}, err
	}
	env, err := ParseEnvJSON(srv.EnvJSON)
	if err != nil {
		return spawnConfig{}, err
	}
	return spawnConfig{
		command:      srv.Command,
		args:         args,
		env:          env,
		workdir:      srv.Workdir,
		cwdIsolation: srv.CwdIsolation,
	}, nil
}

func (m *Manager) emitRestart(p *process, cause error) {
	if m.activity == nil {
		return
	}
	_ = m.activity.InsertActivityEvent(context.Background(), p.accountID, store.ActivityServerRestart, p.name,
		"Stdio process restart for '"+p.name+"'",
		map[string]any{"server_id": p.serverID, "error": cause.Error(), "strikes": p.strikes})
}

// ListTools ensures the process is up and returns tools (for indexer).
func (m *Manager) ListTools(ctx context.Context, serverID string) ([]mcp.Tool, error) {
	c, err := m.Ensure(ctx, serverID)
	if err != nil {
		return nil, err
	}
	res, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		m.noteClientError(serverID, err)
		return nil, err
	}
	return res.Tools, nil
}

// InitResult returns the cached initialize result (for bridging client initialize).
func (m *Manager) InitResult(ctx context.Context, serverID string) (*mcp.InitializeResult, error) {
	c, err := m.Ensure(ctx, serverID)
	if err != nil {
		return nil, err
	}
	_ = c
	m.mu.Lock()
	p := m.procs[serverID]
	m.mu.Unlock()
	if p == nil {
		return nil, errors.New("no process")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.initResult, nil
}

// CallTool runs tools/call under the in-flight semaphore.
func (m *Manager) CallTool(ctx context.Context, serverID string, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	c, err := m.Ensure(ctx, serverID)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	p := m.procs[serverID]
	m.mu.Unlock()
	if p == nil {
		return nil, errors.New("no process")
	}

	wait := m.cfg.InFlightWait
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case p.sem <- struct{}{}:
		defer func() { <-p.sem }()
	case <-timer.C:
		return nil, errServerBusy
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	p.inFlight.Add(1)
	defer p.inFlight.Add(-1)

	res, err := c.CallTool(ctx, req)
	if err != nil {
		m.noteClientError(serverID, err)
		return nil, err
	}
	return res, nil
}

// Ping relays a ping.
func (m *Manager) Ping(ctx context.Context, serverID string) error {
	c, err := m.Ensure(ctx, serverID)
	if err != nil {
		return err
	}
	return c.Ping(ctx)
}

var errServerBusy = errors.New("server busy")

// ErrServerBusy is returned when the in-flight tools/call cap is saturated.
func ErrServerBusy() error { return errServerBusy }

func IsBusy(err error) bool { return errors.Is(err, errServerBusy) }

func (m *Manager) noteClientError(serverID string, err error) {
	if err == nil {
		return
	}
	// Transport closed / process death → mark for restart on next Ensure.
	msg := err.Error()
	if !strings.Contains(msg, "closed") && !strings.Contains(msg, "EOF") &&
		!errors.Is(err, transport.ErrTransportClosed) {
		return
	}
	m.mu.Lock()
	p := m.procs[serverID]
	m.mu.Unlock()
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.client != nil {
		_ = p.client.Close()
		p.client = nil
	}
	p.status = StatusError
	p.lastErr = msg
	p.strikes++
	m.emitRestart(p, err)
	if p.strikes >= m.cfg.MaxStrikes && m.breaker != nil {
		m.breaker.ForceOpen(serverID)
	}
}

type lockedWriter struct {
	mu  sync.Mutex
	buf *bytes.Buffer
	max int
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.buf.Len()+len(p) > w.max {
		drop := w.buf.Len() + len(p) - w.max
		if drop > w.buf.Len() {
			w.buf.Reset()
		} else {
			w.buf.Next(drop)
		}
	}
	return w.buf.Write(p)
}

func sanitizeStderrTail(raw string) []string {
	raw = strings.ReplaceAll(raw, "\r", "")
	lines := strings.Split(raw, "\n")
	out := make([]string, 0, stderrLineCap)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Cap line length; strip control chars.
		line = strings.Map(func(r rune) rune {
			if r < 32 && r != '\t' {
				return -1
			}
			return r
		}, line)
		if len(line) > 200 {
			line = line[:200] + "…"
		}
		out = append(out, line)
	}
	if len(out) > stderrLineCap {
		out = out[len(out)-stderrLineCap:]
	}
	return out
}

// EncodeArgsJSON / EncodeEnvJSON helpers for API writes.
func EncodeArgsJSON(args []string) (string, error) {
	if args == nil {
		args = []string{}
	}
	b, err := json.Marshal(args)
	return string(b), err
}

func EncodeEnvJSON(env map[string]string) (string, error) {
	if env == nil {
		env = map[string]string{}
	}
	b, err := json.Marshal(env)
	return string(b), err
}