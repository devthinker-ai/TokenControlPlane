package proxy_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/devthinker-ai/TokenControlPlane/pkg/auth"
	"github.com/devthinker-ai/TokenControlPlane/pkg/policy"
	"github.com/devthinker-ai/TokenControlPlane/pkg/proxy"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

type policyUpstream struct {
	hits atomic.Int64
	last atomic.Value // []byte
}

func (u *policyUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	u.hits.Add(1)
	body, _ := io.ReadAll(r.Body)
	u.last.Store(body)
	var msg map[string]any
	_ = json.Unmarshal(body, &msg)
	method, _ := msg["method"].(string)
	w.Header().Set("Content-Type", "application/json")
	if method == "tools/list" {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"a"},{"name":"b"},{"name":"c"}]}}`))
		return
	}
	_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`))
}

func TestToolPolicy_DeniedCallNoUpstream(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	up := &policyUpstream{}
	upS := httptest.NewServer(up)
	t.Cleanup(upS.Close)

	sid := "srv_pol"
	_ = st.CreateServer(context.Background(), store.MCPServer{
		ID: sid, Name: "p", BaseURL: upS.URL, Enabled: true, AuthValue: "Bearer x",
	})
	_ = st.ReplaceTools(context.Background(), sid, []store.ToolDef{
		{ID: uuid.NewString(), ServerID: sid, Name: "a", InputSchema: "{}"},
		{ID: uuid.NewString(), ServerID: sid, Name: "b", InputSchema: "{}"},
	})
	_ = st.SetToolEnabled(context.Background(), sid, "b", false)

	plain := "tcp_pol_test"
	kid := uuid.NewString()
	_ = st.CreateAPIKey(context.Background(), store.APIKey{
		ID: kid, KeyHash: hashKey(plain), Name: "k", Enabled: true, MonthlyBudget: 1_000_000,
	})

	cache := policy.NewCache()
	_ = cache.Warm(context.Background(), st)
	v, _ := auth.NewValidator(st, auth.Config{})
	meter := proxy.NewMeter(st, v)
	h := proxy.NewRouter(proxy.Deps{
		Store: st, Auth: v, Meter: meter, Breaker: proxy.NewBreaker(),
		AdminToken: adminToken, Policy: cache,
	})
	gw := httptest.NewServer(h)
	t.Cleanup(gw.Close)

	body := `{"jsonrpc":"2.0","id":42,"method":"tools/call","params":{"name":"b"}}`
	req, _ := http.NewRequest(http.MethodPost, gw.URL+"/mcp/"+sid, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+plain)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if up.hits.Load() != 0 {
		t.Fatalf("upstream hits=%d want 0", up.hits.Load())
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("body=%s err=%v", raw, err)
	}
	errObj, _ := out["error"].(map[string]any)
	if errObj == nil {
		t.Fatalf("want error, got %s", raw)
	}
	msg, _ := errObj["message"].(string)
	if !strings.Contains(msg, "tool 'b' is not available") {
		t.Fatalf("message=%q", msg)
	}
	if code, _ := errObj["code"].(float64); code != -32602 {
		t.Fatalf("code=%v", code)
	}
}

func TestToolPolicy_ListFilterJSON(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	up := &policyUpstream{}
	upS := httptest.NewServer(up)
	t.Cleanup(upS.Close)

	sid := "srv_list"
	_ = st.CreateServer(context.Background(), store.MCPServer{
		ID: sid, Name: "p", BaseURL: upS.URL, Enabled: true,
	})
	_ = st.ReplaceTools(context.Background(), sid, []store.ToolDef{
		{ID: uuid.NewString(), ServerID: sid, Name: "a", InputSchema: "{}"},
		{ID: uuid.NewString(), ServerID: sid, Name: "b", InputSchema: "{}"},
		{ID: uuid.NewString(), ServerID: sid, Name: "c", InputSchema: "{}"},
	})
	_ = st.SetToolEnabled(context.Background(), sid, "b", false)

	plain := "tcp_list_test"
	kid := uuid.NewString()
	_ = st.CreateAPIKey(context.Background(), store.APIKey{
		ID: kid, KeyHash: hashKey(plain), Name: "k", Enabled: true, MonthlyBudget: 1_000_000,
	})

	cache := policy.NewCache()
	_ = cache.Warm(context.Background(), st)
	v, _ := auth.NewValidator(st, auth.Config{})
	h := proxy.NewRouter(proxy.Deps{
		Store: st, Auth: v, Meter: proxy.NewMeter(st, v), Breaker: proxy.NewBreaker(),
		AdminToken: adminToken, Policy: cache,
	})
	gw := httptest.NewServer(h)
	t.Cleanup(gw.Close)

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	req, _ := http.NewRequest(http.MethodPost, gw.URL+"/mcp/"+sid, bytes.NewReader([]byte(body)))
	req.Header.Set("Authorization", "Bearer "+plain)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s: %v", raw, err)
	}
	names := map[string]bool{}
	for _, tool := range out.Result.Tools {
		names[tool.Name] = true
	}
	if names["b"] {
		t.Fatal("disabled tool b should be filtered")
	}
	if !names["a"] || !names["c"] {
		t.Fatalf("want a,c got %v", names)
	}
	if resp.Header.Get("Content-Length") == "" {
		t.Fatal("Content-Length should be set")
	}
}

func TestToolPolicy_BatchMixed(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	up := &policyUpstream{}
	upS := httptest.NewServer(up)
	t.Cleanup(upS.Close)

	sid := "srv_batch"
	_ = st.CreateServer(context.Background(), store.MCPServer{
		ID: sid, Name: "p", BaseURL: upS.URL, Enabled: true,
	})
	_ = st.ReplaceTools(context.Background(), sid, []store.ToolDef{
		{ID: uuid.NewString(), ServerID: sid, Name: "a", InputSchema: "{}"},
		{ID: uuid.NewString(), ServerID: sid, Name: "b", InputSchema: "{}"},
		{ID: uuid.NewString(), ServerID: sid, Name: "c", InputSchema: "{}"},
	})
	_ = st.SetToolEnabled(context.Background(), sid, "b", false)

	plain := "tcp_batch_test"
	kid := uuid.NewString()
	_ = st.CreateAPIKey(context.Background(), store.APIKey{
		ID: kid, KeyHash: hashKey(plain), Name: "k", Enabled: true, MonthlyBudget: 1_000_000,
	})

	cache := policy.NewCache()
	_ = cache.Warm(context.Background(), st)
	v, _ := auth.NewValidator(st, auth.Config{})
	h := proxy.NewRouter(proxy.Deps{
		Store: st, Auth: v, Meter: proxy.NewMeter(st, v), Breaker: proxy.NewBreaker(),
		AdminToken: adminToken, Policy: cache,
	})
	gw := httptest.NewServer(h)
	t.Cleanup(gw.Close)

	body := `[
	  {"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"a"}},
	  {"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"b"}},
	  {"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"c"}}
	]`
	req, _ := http.NewRequest(http.MethodPost, gw.URL+"/mcp/"+sid, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+plain)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var arr []map[string]any
	if err := json.Unmarshal(raw, &arr); err != nil {
		t.Fatalf("%s: %v", raw, err)
	}
	if len(arr) != 3 {
		t.Fatalf("len=%d", len(arr))
	}
	// middle item denied
	if arr[1]["error"] == nil {
		t.Fatalf("want error on id 2: %s", raw)
	}
	// upstream should have been hit for the allowed batch (once)
	if up.hits.Load() != 1 {
		t.Fatalf("upstream hits=%d want 1", up.hits.Load())
	}
}
