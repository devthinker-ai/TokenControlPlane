package proxy_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/devthinker-ai/TokenControlPlane/pkg/auth"
	"github.com/devthinker-ai/TokenControlPlane/pkg/proxy"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

const (
	clientKeyPlain = "tcp_test_key_phase1"
	upstreamKey    = "Bearer upstream-secret-xyz"
	serverID       = "srv-echo"
)

func hashKey(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

type mockUpstream struct {
	sawClientAuth   atomic.Bool
	sawUpstreamAuth atomic.Bool
	gotAuthValue    atomic.Value // string
	gotProto        atomic.Value
	gotSession      atomic.Value
	hits            atomic.Int64
	failWith        atomic.Int32 // if >0, respond with that status

	sseEvery time.Duration
}

func (m *mockUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.hits.Add(1)
	if code := m.failWith.Load(); code > 0 {
		w.WriteHeader(int(code))
		_, _ = w.Write([]byte(`{"error":"upstream boom"}`))
		return
	}
	if v := r.Header.Get("Authorization"); v != "" {
		if v == "Bearer "+clientKeyPlain || strings.Contains(v, "tcp_") {
			m.sawClientAuth.Store(true)
		}
		if v == upstreamKey {
			m.sawUpstreamAuth.Store(true)
		}
		m.gotAuthValue.Store(v)
	}
	if p := r.Header.Get("MCP-Protocol-Version"); p != "" {
		m.gotProto.Store(p)
		w.Header().Set("MCP-Protocol-Version", p)
	}
	if s := r.Header.Get("Mcp-Session-Id"); s != "" {
		m.gotSession.Store(s)
		w.Header().Set("Mcp-Session-Id", s)
	}

	if r.Method == http.MethodPost {
		// Deterministic JSON body echo path for metering tests when Accept is JSON.
		if !strings.Contains(r.Header.Get("Accept"), "text/event-stream") &&
			r.Header.Get("X-Test-Mode") == "json" {
			body, _ := io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"echo":`))
			_, _ = w.Write(body)
			_, _ = w.Write([]byte(`}`))
			return
		}

		// (b) SSE stream: frame every 100ms
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		every := m.sseEvery
		if every == 0 {
			every = 100 * time.Millisecond
		}
		for i := 0; i < 5; i++ {
			_, _ = fmt.Fprintf(w, "data: frame-%d\n\n", i)
			flusher.Flush()
			time.Sleep(every)
		}
		return
	}

	// GET also SSE for completeness
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	if f, ok := w.(http.Flusher); ok {
		_, _ = w.Write([]byte("data: hello\n\n"))
		f.Flush()
	}
}

const adminToken = "test-admin-token"

type testEnv struct {
	store     *store.Store
	auth      *auth.Validator
	meter     *proxy.Meter
	breaker   *proxy.Breaker
	upstream  *mockUpstream
	upstreamS *httptest.Server
	gateway   http.Handler
	keyID     string
	keyPlain  string
}

type setupOpts struct {
	budget        int64
	rpm           int
	loopThreshold int
	keyPlain      string
}

func setupEnv(t *testing.T, budget int64, rpm int) *testEnv {
	t.Helper()
	return setupEnvOpts(t, setupOpts{budget: budget, rpm: rpm, keyPlain: clientKeyPlain})
}

func setupEnvOpts(t *testing.T, opt setupOpts) *testEnv {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	up := &mockUpstream{}
	upServer := httptest.NewServer(up)
	t.Cleanup(upServer.Close)

	srv := store.MCPServer{
		ID:         serverID,
		Name:       "echo",
		BaseURL:    upServer.URL,
		AuthHeader: "Authorization",
		AuthValue:  upstreamKey,
		Enabled:    true,
	}
	if err := st.CreateServer(context.Background(), srv); err != nil {
		t.Fatalf("create server: %v", err)
	}

	plain := opt.keyPlain
	if plain == "" {
		plain = clientKeyPlain
	}
	keyID := uuid.NewString()
	if err := st.CreateAPIKey(context.Background(), store.APIKey{
		ID:            keyID,
		KeyHash:       hashKey(plain),
		Name:          "test",
		MonthlyBudget: opt.budget,
		RateLimitRPM:  opt.rpm,
		Enabled:       true,
	}); err != nil {
		t.Fatalf("create key: %v", err)
	}

	cfg := auth.Config{LoopThreshold: opt.loopThreshold}
	v, err := auth.NewValidator(st, cfg)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	meter := proxy.NewMeter(st, v)
	breaker := proxy.NewBreaker()

	h := proxy.NewRouter(proxy.Deps{
		Store:      st,
		Auth:       v,
		Meter:      meter,
		Indexer:    nil,
		Breaker:    breaker,
		AdminToken: adminToken,
	})

	return &testEnv{
		store:     st,
		auth:      v,
		meter:     meter,
		breaker:   breaker,
		upstream:  up,
		upstreamS: upServer,
		gateway:   h,
		keyID:     keyID,
		keyPlain:  plain,
	}
}

func TestProxy_HeadersStripInjectAndEcho(t *testing.T) {
	env := setupEnv(t, 1_000_000, 0)

	// Use a real client against a gateway test server so streaming works with Flusher.
	gw := httptest.NewServer(env.gateway)
	t.Cleanup(gw.Close)

	payload := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	req, err := http.NewRequest(http.MethodPost, gw.URL+"/mcp/"+serverID, payload)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+clientKeyPlain)
	req.Header.Set("MCP-Protocol-Version", "2024-11-05")
	req.Header.Set("Mcp-Session-Id", "sess-abc-123")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	// (c) client Authorization NOT forwarded; upstream key IS injected
	if env.upstream.sawClientAuth.Load() {
		t.Fatal("client Authorization was forwarded to upstream")
	}
	if !env.upstream.sawUpstreamAuth.Load() {
		t.Fatalf("upstream key not injected; got %v", env.upstream.gotAuthValue.Load())
	}

	// (a) MCP headers echoed back on response
	if got := resp.Header.Get("MCP-Protocol-Version"); got != "2024-11-05" {
		t.Fatalf("protocol header: got %q", got)
	}
	if got := resp.Header.Get("Mcp-Session-Id"); got != "sess-abc-123" {
		t.Fatalf("session header: got %q", got)
	}
	if got := env.upstream.gotProto.Load(); got != "2024-11-05" {
		t.Fatalf("upstream did not see protocol version: %v", got)
	}
	if got := env.upstream.gotSession.Load(); got != "sess-abc-123" {
		t.Fatalf("upstream did not see session id: %v", got)
	}
}

func TestProxy_SSEFrameWithin150ms(t *testing.T) {
	env := setupEnv(t, 1_000_000, 0)
	env.upstream.sseEvery = 100 * time.Millisecond

	gw := httptest.NewServer(env.gateway)
	t.Cleanup(gw.Close)

	req, err := http.NewRequest(http.MethodPost, gw.URL+"/mcp/"+serverID, strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+clientKeyPlain)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content-type: %s", ct)
	}

	reader := bufio.NewReader(resp.Body)
	deadline := time.After(150 * time.Millisecond)
	lineCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				errCh <- err
				return
			}
			if strings.HasPrefix(line, "data:") {
				lineCh <- line
				return
			}
		}
	}()

	select {
	case line := <-lineCh:
		elapsed := time.Since(start)
		t.Logf("first SSE frame in %v: %q", elapsed, strings.TrimSpace(line))
		if elapsed > 150*time.Millisecond {
			t.Fatalf("SSE frame took %v (>150ms) — response buffering?", elapsed)
		}
	case err := <-errCh:
		t.Fatalf("read SSE: %v", err)
	case <-deadline:
		t.Fatal("no SSE frame within 150ms — FlushInterval canary failed")
	}
}

func TestProxy_MeteringExactCounts(t *testing.T) {
	env := setupEnv(t, 1_000_000, 0)

	gw := httptest.NewServer(env.gateway)
	t.Cleanup(gw.Close)

	// Deterministic JSON round-trip (not SSE) for exact byte counts.
	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo"}}`
	req, _ := http.NewRequest(http.MethodPost, gw.URL+"/mcp/"+serverID, strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer "+clientKeyPlain)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-Mode", "json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	respBytes, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d body %s", resp.StatusCode, respBytes)
	}

	if err := env.meter.Flush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}

	usage, err := env.store.GetUsage(context.Background(), env.keyID, store.PeriodStartUTC(time.Now()))
	if err != nil {
		t.Fatalf("get usage: %v", err)
	}

	wantIn := int64(len(reqBody))
	wantOut := int64(len(respBytes))
	wantTokens := (wantIn + wantOut) / 4

	if usage.BytesIn != wantIn {
		t.Fatalf("bytes_in: got %d want %d", usage.BytesIn, wantIn)
	}
	if usage.BytesOut != wantOut {
		t.Fatalf("bytes_out: got %d want %d", usage.BytesOut, wantOut)
	}
	if usage.TokensUsed != wantTokens {
		t.Fatalf("tokens: got %d want %d", usage.TokensUsed, wantTokens)
	}
	if usage.Requests != 1 {
		t.Fatalf("requests: got %d want 1", usage.Requests)
	}
}

func TestProxy_Budget402AndRateLimit429(t *testing.T) {
	t.Run("402 budget exceeded", func(t *testing.T) {
		env := setupEnv(t, 10, 0) // tiny budget
		// Pre-seed usage over budget.
		period := store.PeriodStartUTC(time.Now())
		if err := env.store.IncrUsage(context.Background(), env.keyID, period, 0, 80, 20, 1); err != nil {
			t.Fatal(err)
		}
		if err := env.auth.Reload(context.Background()); err != nil {
			t.Fatal(err)
		}

		gw := httptest.NewServer(env.gateway)
		t.Cleanup(gw.Close)

		req, _ := http.NewRequest(http.MethodPost, gw.URL+"/mcp/"+serverID, strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+clientKeyPlain)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)

		if resp.StatusCode != http.StatusPaymentRequired {
			t.Fatalf("status: got %d want 402", resp.StatusCode)
		}
		got := strings.TrimSpace(string(body))
		want := auth.BudgetExceededJSON()
		if got != want {
			t.Fatalf("body:\n got %s\nwant %s", got, want)
		}
	})

	t.Run("429 rate limited", func(t *testing.T) {
		env := setupEnv(t, 1_000_000, 2) // 2 req/min
		gw := httptest.NewServer(env.gateway)
		t.Cleanup(gw.Close)

		doReq := func() *http.Response {
			req, _ := http.NewRequest(http.MethodPost, gw.URL+"/mcp/"+serverID, strings.NewReader(`{}`))
			req.Header.Set("Authorization", "Bearer "+clientKeyPlain)
			req.Header.Set("X-Test-Mode", "json")
			req.Header.Set("Accept", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			return resp
		}

		for i := 0; i < 2; i++ {
			resp := doReq()
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusTooManyRequests {
				t.Fatalf("request %d unexpectedly 429", i+1)
			}
		}

		resp := doReq()
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("status: got %d want 429 body=%s", resp.StatusCode, body)
		}
		if ra := resp.Header.Get("Retry-After"); ra == "" {
			t.Fatal("missing Retry-After header")
		}
	})
}

func TestProxy_Unauthorized401(t *testing.T) {
	env := setupEnv(t, 1000, 0)
	gw := httptest.NewServer(env.gateway)
	t.Cleanup(gw.Close)

	req, _ := http.NewRequest(http.MethodPost, gw.URL+"/mcp/"+serverID, strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer tcp_wrong")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestProxy_DisabledServer403(t *testing.T) {
	env := setupEnv(t, 1000, 0)
	_, err := env.store.DB().Exec(`UPDATE mcp_servers SET enabled = 0 WHERE id = ?`, serverID)
	if err != nil {
		t.Fatal(err)
	}
	gw := httptest.NewServer(env.gateway)
	t.Cleanup(gw.Close)

	req, _ := http.NewRequest(http.MethodPost, gw.URL+"/mcp/"+serverID, strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+clientKeyPlain)
	req.Header.Set("X-Test-Mode", "json")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("got %d want 403", resp.StatusCode)
	}
}

func TestPeriodStartUTC(t *testing.T) {
	tests := []struct {
		in   time.Time
		want time.Time
	}{
		{
			in:   time.Date(2026, 9, 5, 14, 30, 0, 0, time.UTC),
			want: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			in:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			want: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	for _, tt := range tests {
		got := store.PeriodStartUTC(tt.in)
		if !got.Equal(tt.want) {
			t.Fatalf("got %v want %v", got, tt.want)
		}
	}
}

func TestBudgetBodyExactJSON(t *testing.T) {
	var m map[string]string
	if err := json.Unmarshal([]byte(auth.BudgetExceededJSON()), &m); err != nil {
		t.Fatal(err)
	}
	if m["error"] != "Budget limit exceeded. Access suspended by TokenControlPlane." {
		t.Fatalf("unexpected: %#v", m)
	}
}
