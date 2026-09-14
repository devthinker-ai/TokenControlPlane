package proxy_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devthinker-ai/TokenControlPlane/pkg/auth"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

func TestPhase2_CircuitBreaker(t *testing.T) {
	env := setupEnv(t, 1_000_000, 0)
	var clock atomic.Value
	clock.Store(time.Now().UTC())
	env.breaker.SetClock(func() time.Time { return clock.Load().(time.Time) })

	env.upstream.failWith.Store(500)
	gw := httptest.NewServer(env.gateway)
	t.Cleanup(gw.Close)

	do := func() *http.Response {
		req, _ := http.NewRequest(http.MethodPost, gw.URL+"/mcp/"+serverID, strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+env.keyPlain)
		req.Header.Set("X-Test-Mode", "json")
		req.Header.Set("Accept", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	// 5 consecutive upstream 5xx → OPEN
	for i := 0; i < 5; i++ {
		resp := do()
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 500 {
			t.Fatalf("fail %d: status %d", i+1, resp.StatusCode)
		}
	}
	hitsAfter5 := env.upstream.hits.Load()
	if hitsAfter5 != 5 {
		t.Fatalf("hits after 5 fails: %d", hitsAfter5)
	}

	st := env.breaker.Status(serverID)
	if st.State != "open" && st.State != "half_open" {
		// Immediately after 5th failure should be open (clock not advanced).
		if st.ConsecutiveFailures < 5 {
			t.Fatalf("expected open with ≥5 failures, got %+v", st)
		}
	}

	// Next request: 503 without reaching upstream.
	resp := do()
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d body=%s", resp.StatusCode, body)
	}
	if ra := resp.Header.Get("Retry-After"); ra != "30" {
		t.Fatalf("Retry-After: %q", ra)
	}
	if !strings.Contains(string(body), "circuit open") {
		t.Fatalf("body: %s", body)
	}
	if env.upstream.hits.Load() != hitsAfter5 {
		t.Fatalf("upstream was hit while circuit open: %d → %d", hitsAfter5, env.upstream.hits.Load())
	}

	// Advance past 30s cooldown → HALF_OPEN probe allowed.
	clock.Store(clock.Load().(time.Time).Add(31 * time.Second))
	env.upstream.failWith.Store(0) // probe succeeds

	resp = do()
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("probe want 200, got %d", resp.StatusCode)
	}
	if env.upstream.hits.Load() != hitsAfter5+1 {
		t.Fatalf("probe should hit upstream once")
	}
	if st := env.breaker.Status(serverID); st.State != "closed" {
		t.Fatalf("after successful probe want closed, got %+v", st)
	}

	// Circuit open/close should appear in the activity feed.
	events, err := env.store.ListActivityEvents(context.Background(), store.DefaultAccountID, 20)
	if err != nil {
		t.Fatal(err)
	}
	var sawOpen, sawClosed bool
	for _, e := range events {
		if e.Kind == store.ActivityCircuitOpen {
			sawOpen = true
		}
		if e.Kind == store.ActivityCircuitClosed {
			sawClosed = true
		}
	}
	if !sawOpen {
		t.Fatalf("missing circuit_open event: %+v", events)
	}
	if !sawClosed {
		t.Fatalf("missing circuit_closed event: %+v", events)
	}
}

func TestPhase2_AutoKillAndUnkill(t *testing.T) {
	plain := "tcp_autokill_fresh"
	env := setupEnvOpts(t, setupOpts{
		budget:        1_000_000,
		rpm:           0,
		loopThreshold: 120,
		keyPlain:      plain,
	})
	gw := httptest.NewServer(env.gateway)
	t.Cleanup(gw.Close)

	do := func() (int, string) {
		req, _ := http.NewRequest(http.MethodPost, gw.URL+"/mcp/"+serverID, strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+plain)
		req.Header.Set("X-Test-Mode", "json")
		req.Header.Set("Accept", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp.StatusCode, string(b)
	}

	for i := 0; i < 120; i++ {
		code, body := do()
		if code == http.StatusPaymentRequired && strings.Contains(body, "kill switch") {
			t.Fatalf("auto-killed too early at request %d", i+1)
		}
		if code != 200 {
			t.Fatalf("request %d: status %d body %s", i+1, code, body)
		}
	}

	code, body := do() // 121st
	if code != http.StatusPaymentRequired {
		t.Fatalf("121st: want 402, got %d %s", code, body)
	}
	if strings.TrimSpace(body) != auth.KillSwitchJSON() {
		t.Fatalf("kill body:\n got %s\nwant %s", body, auth.KillSwitchJSON())
	}

	code, _ = do() // still killed
	if code != http.StatusPaymentRequired {
		t.Fatalf("122nd: want 402, got %d", code)
	}

	// unkill?reset=true
	req, _ := http.NewRequest(http.MethodPost, gw.URL+"/admin/keys/"+env.keyID+"/unkill?reset=true", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("unkill: %d", resp.StatusCode)
	}

	code, body = do()
	if code != 200 {
		t.Fatalf("after unkill want 200, got %d %s", code, body)
	}
}

func TestPhase2_Precedence(t *testing.T) {
	t.Run("invalid key returns 401 not 402 even if another key killed", func(t *testing.T) {
		env := setupEnv(t, 1_000_000, 0)
		if err := env.auth.Kill(context.Background(), env.keyID); err != nil {
			t.Fatal(err)
		}
		gw := httptest.NewServer(env.gateway)
		t.Cleanup(gw.Close)

		req, _ := http.NewRequest(http.MethodPost, gw.URL+"/mcp/"+serverID, strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer tcp_totally_wrong")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("want 401, got %d", resp.StatusCode)
		}
	})

	t.Run("disabled server returns 403 not 503", func(t *testing.T) {
		env := setupEnv(t, 1_000_000, 0)
		// Force circuit open via failures, then disable server.
		env.upstream.failWith.Store(500)
		gw := httptest.NewServer(env.gateway)
		t.Cleanup(gw.Close)

		for i := 0; i < 5; i++ {
			req, _ := http.NewRequest(http.MethodPost, gw.URL+"/mcp/"+serverID, strings.NewReader(`{}`))
			req.Header.Set("Authorization", "Bearer "+env.keyPlain)
			resp, _ := http.DefaultClient.Do(req)
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}

		req, _ := http.NewRequest(http.MethodPost, gw.URL+"/admin/servers/"+serverID+"/disable", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("disable: %d", resp.StatusCode)
		}

		req, _ = http.NewRequest(http.MethodPost, gw.URL+"/mcp/"+serverID, strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+env.keyPlain)
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("want 403 (not 503), got %d", resp.StatusCode)
		}
	})
}

func TestPhase2_PeriodRollover(t *testing.T) {
	env := setupEnv(t, 1_000_000, 0)
	lastMonth := store.PeriodStartUTC(time.Now().UTC().AddDate(0, -1, 0))
	if err := env.store.IncrUsage(context.Background(), env.keyID, lastMonth, 100, 100, 50, 3); err != nil {
		t.Fatal(err)
	}
	before, err := env.store.GetUsage(context.Background(), env.keyID, lastMonth)
	if err != nil {
		t.Fatal(err)
	}

	gw := httptest.NewServer(env.gateway)
	t.Cleanup(gw.Close)
	req, _ := http.NewRequest(http.MethodPost, gw.URL+"/mcp/"+serverID, strings.NewReader(`{"n":1}`))
	req.Header.Set("Authorization", "Bearer "+env.keyPlain)
	req.Header.Set("X-Test-Mode", "json")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	_ = env.meter.Flush(context.Background())

	after, err := env.store.GetUsage(context.Background(), env.keyID, lastMonth)
	if err != nil {
		t.Fatal(err)
	}
	if after.TokensUsed != before.TokensUsed || after.Requests != before.Requests {
		t.Fatalf("last month mutated: before=%+v after=%+v", before, after)
	}

	cur, err := env.store.GetUsage(context.Background(), env.keyID, store.PeriodStartUTC(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if cur.Requests < 1 {
		t.Fatalf("current period not incremented: %+v", cur)
	}
}

func TestPhase2_AdminKillEnableImmediate(t *testing.T) {
	env := setupEnv(t, 1_000_000, 0)
	gw := httptest.NewServer(env.gateway)
	t.Cleanup(gw.Close)

	proxyOK := func() int {
		req, _ := http.NewRequest(http.MethodPost, gw.URL+"/mcp/"+serverID, strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+env.keyPlain)
		req.Header.Set("X-Test-Mode", "json")
		req.Header.Set("Accept", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		code := resp.StatusCode
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return code
	}
	admin := func(method, path string) int {
		req, _ := http.NewRequest(method, gw.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return resp.StatusCode
	}

	if code := proxyOK(); code != 200 {
		t.Fatalf("warmup: %d", code)
	}
	if code := admin(http.MethodPost, "/admin/keys/"+env.keyID+"/kill"); code != 200 {
		t.Fatalf("kill: %d", code)
	}
	if code := proxyOK(); code != http.StatusPaymentRequired {
		t.Fatalf("after kill want 402, got %d", code)
	}
	if code := admin(http.MethodPost, "/admin/keys/"+env.keyID+"/unkill"); code != 200 {
		t.Fatalf("unkill: %d", code)
	}
	if code := proxyOK(); code != 200 {
		t.Fatalf("after unkill want 200, got %d", code)
	}

	if code := admin(http.MethodPost, "/admin/servers/"+serverID+"/disable"); code != 200 {
		t.Fatalf("disable: %d", code)
	}
	if code := proxyOK(); code != http.StatusForbidden {
		t.Fatalf("after disable want 403, got %d", code)
	}
	if code := admin(http.MethodPost, "/admin/servers/"+serverID+"/enable"); code != 200 {
		t.Fatalf("enable: %d", code)
	}
	if code := proxyOK(); code != 200 {
		t.Fatalf("after enable want 200, got %d", code)
	}
}

func TestPhase2_HealthReadyUsage(t *testing.T) {
	env := setupEnv(t, 1000, 0)
	gw := httptest.NewServer(env.gateway)
	t.Cleanup(gw.Close)

	resp, err := http.Get(gw.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"status":"ok"`) {
		t.Fatalf("healthz: %d %s", resp.StatusCode, body)
	}

	resp, err = http.Get(gw.URL + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), "ready") {
		t.Fatalf("readyz: %d %s", resp.StatusCode, body)
	}

	req, _ := http.NewRequest(http.MethodGet, gw.URL+"/admin/usage", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("usage: %d %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), env.keyID) {
		t.Fatalf("usage missing key: %s", body)
	}
}

func TestPhase2_AdminAuthRequired(t *testing.T) {
	env := setupEnv(t, 1000, 0)
	gw := httptest.NewServer(env.gateway)
	t.Cleanup(gw.Close)

	resp, err := http.Get(gw.URL + "/admin/usage")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", resp.StatusCode)
	}
}
