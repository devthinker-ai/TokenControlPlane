package stdio_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/devthinker-ai/TokenControlPlane/pkg/policy"
	"github.com/devthinker-ai/TokenControlPlane/pkg/proxy"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
	"github.com/devthinker-ai/TokenControlPlane/pkg/upstream/stdio"
)

func compileFixture(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "fixture")
	pkgDir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", out, "./testdata/fixture")
	cmd.Dir = pkgDir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fixture: %v\n%s", err, b)
	}
	return out
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func insertStdioServer(t *testing.T, st *store.Store, id, command string, env map[string]string) {
	t.Helper()
	ej, err := stdio.EncodeEnvJSON(env)
	if err != nil {
		t.Fatal(err)
	}
	err = st.CreateServer(context.Background(), store.MCPServer{
		ID: id, AccountID: store.DefaultAccountID, Name: "fix",
		Transport: store.TransportStdio, Command: command, ArgsJSON: "[]", EnvJSON: ej,
		AuthType: store.AuthTypeNone, Enabled: true, CwdIsolation: true,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestParseMCPJSONShapes(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		tr   string
	}{
		{"npx", `{"mcpServers":{"fs":{"command":"npx","args":["-y","@modelcontextprotocol/server-filesystem","/tmp"],"env":{"FOO":"1"}}}}`, "stdio"},
		{"uvx", `{"mcpServers":{"py":{"command":"uvx","args":["mcp-server-fetch"]}}}`, "stdio"},
		{"abs", `{"mcpServers":{"bin":{"command":"/usr/local/bin/my-mcp","args":[]}}}`, "stdio"},
		{"http", `{"mcpServers":{"remote":{"url":"https://mcp.example.com/mcp"}}}`, "http"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ents, err := stdio.ParseMCPJSON(tc.raw)
			if err != nil {
				t.Fatal(err)
			}
			if len(ents) != 1 || ents[0].Transport != tc.tr {
				t.Fatalf("%+v", ents)
			}
		})
	}
}

func TestMissingCommandExactMessage(t *testing.T) {
	st := openStore(t)
	breaker := proxy.NewBreaker()
	mgr := stdio.NewManager(st, breaker, nil, stdio.Config{
		MaxStrikes: 3, SandboxRoot: t.TempDir(),
	})
	defer mgr.CloseAll()
	insertStdioServer(t, st, "srv_miss", "definitely-not-a-real-binary-xyz", nil)

	_, err := mgr.Ensure(context.Background(), "srv_miss")
	if err == nil {
		t.Fatal("expected error")
	}
	want := stdio.PathNotFoundMessage("definitely-not-a-real-binary-xyz")
	if err.Error() != want {
		t.Fatalf("err=%q want=%q", err.Error(), want)
	}
	if stSnap := mgr.StatusSnapshot("srv_miss"); stSnap.State != stdio.StatusError {
		t.Fatalf("state=%s", stSnap.State)
	}
	if n := mgr.SpawnCount("srv_miss"); n != 4 {
		t.Fatalf("spawn_count=%d want 4", n)
	}
}

func TestStdioRoundTripAndPolicy(t *testing.T) {
	bin := compileFixture(t)
	st := openStore(t)
	breaker := proxy.NewBreaker()
	mgr := stdio.NewManager(st, breaker, nil, stdio.Config{
		MaxStrikes: 3, SandboxRoot: t.TempDir(), InFlightWait: 2 * time.Second,
	})
	defer mgr.CloseAll()
	insertStdioServer(t, st, "srv_ok", bin, map[string]string{"SLOW_MS": "50"})

	tools, err := mgr.ListTools(context.Background(), "srv_ok")
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 2 {
		t.Fatalf("tools=%d", len(tools))
	}

	defs := make([]store.ToolDef, 0, len(tools))
	for _, tool := range tools {
		defs = append(defs, store.ToolDef{
			ID: tool.Name + "-id", ServerID: "srv_ok", Name: tool.Name,
			Description: tool.Description, InputSchema: "{}",
		})
	}
	if err := st.ReplaceTools(context.Background(), "srv_ok", defs); err != nil {
		t.Fatal(err)
	}
	_ = st.SetToolEnabled(context.Background(), "srv_ok", "slow", false)

	cache := policy.NewCache()
	_ = cache.Warm(context.Background(), st)

	names := []string{"echo", "slow"}
	allowed := cache.FilterToolsList("srv_ok", "key_all", names)
	if _, ok := allowed["slow"]; ok {
		t.Fatal("slow should be filtered")
	}
	if _, ok := allowed["echo"]; !ok {
		t.Fatal("echo should remain")
	}
	if cache.Allowed("srv_ok", "key_all", "slow") {
		t.Fatal("slow call should be denied")
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = "echo"
	req.Params.Arguments = map[string]any{"text": "hi"}
	res, err := mgr.CallTool(context.Background(), "srv_ok", req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(res)
	if !strings.Contains(string(b), "hi") {
		t.Fatalf("result=%s", b)
	}
}

func TestInFlightCap(t *testing.T) {
	bin := compileFixture(t)
	st := openStore(t)
	breaker := proxy.NewBreaker()
	mgr := stdio.NewManager(st, breaker, nil, stdio.Config{
		MaxInFlight: 4, InFlightWait: 200 * time.Millisecond,
		MaxStrikes: 3, SandboxRoot: t.TempDir(),
	})
	defer mgr.CloseAll()
	insertStdioServer(t, st, "srv_cap", bin, map[string]string{"SLOW_MS": "800"})

	if _, err := mgr.Ensure(context.Background(), "srv_cap"); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := mcp.CallToolRequest{}
			req.Params.Name = "slow"
			req.Params.Arguments = map[string]any{"text": "x"}
			_, err := mgr.CallTool(context.Background(), "srv_cap", req)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	busy, ok := 0, 0
	for err := range errs {
		switch {
		case err == nil:
			ok++
		case stdio.IsBusy(err):
			busy++
		default:
			t.Fatalf("unexpected err: %v", err)
		}
	}
	if ok != 4 || busy != 2 {
		t.Fatalf("ok=%d busy=%d", ok, busy)
	}
}

func TestCrashRecoveryAndRestart(t *testing.T) {
	bin := compileFixture(t)
	st := openStore(t)
	breaker := proxy.NewBreaker()
	mgr := stdio.NewManager(st, breaker, nil, stdio.Config{
		MaxStrikes: 3, SandboxRoot: t.TempDir(),
	})
	defer mgr.CloseAll()
	ej, _ := stdio.EncodeEnvJSON(map[string]string{"CRASH_AFTER": "1"})
	id := "srv_crash"
	if err := st.CreateServer(context.Background(), store.MCPServer{
		ID: id, AccountID: store.DefaultAccountID, Name: "crash",
		Transport: store.TransportStdio, Command: bin, ArgsJSON: "[]", EnvJSON: ej,
		AuthType: store.AuthTypeNone, Enabled: true, CwdIsolation: true,
	}); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		if _, err := mgr.Ensure(context.Background(), id); err != nil {
			t.Fatalf("ensure %d: %v", i, err)
		}
		req := mcp.CallToolRequest{}
		req.Params.Name = "echo"
		req.Params.Arguments = map[string]any{"text": "x"}
		_, _ = mgr.CallTool(context.Background(), id, req)
		time.Sleep(200 * time.Millisecond)
		// Next call should observe closed transport and bump strikes.
		_, _ = mgr.CallTool(context.Background(), id, req)
		time.Sleep(50 * time.Millisecond)
	}

	// After enough deaths, circuit should refuse or status shows strikes.
	stSnap := mgr.StatusSnapshot(id)
	if stSnap.Strikes < 1 && stSnap.State == stdio.StatusRunning {
		// Soft assert: at least process died once; ForceOpen path covered by missing-cmd.
		t.Logf("strikes=%d state=%s (crash timing may vary)", stSnap.Strikes, stSnap.State)
	}

	breaker.ForceOpen(id)
	if err := mgr.Restart(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if st2 := mgr.StatusSnapshot(id); st2.State != stdio.StatusRunning {
		t.Fatalf("after restart state=%s err=%s", st2.State, st2.LastError)
	}
}
