package policy_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/devthinker-ai/TokenControlPlane/pkg/policy"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

func setup(t *testing.T) (*store.Store, *policy.Cache, string, string, string) {
	t.Helper()
	st, err := store.OpenWithOptions(filepath.Join(t.TempDir(), "p.db"), store.OpenOptions{AppVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	acct := "acct_" + uuid.NewString()
	_ = st.CreateAccount(context.Background(), store.Account{ID: acct, Name: "t", Plan: "pro", MaxSeats: 10, MaxServers: 10})
	sid := "srv_" + uuid.NewString()
	_ = st.CreateServer(context.Background(), store.MCPServer{
		ID: sid, AccountID: acct, Name: "s", BaseURL: "https://x", Enabled: true,
	})
	_ = st.ReplaceTools(context.Background(), sid, []store.ToolDef{
		{ID: uuid.NewString(), ServerID: sid, Name: "a", Description: "A", InputSchema: "{}"},
		{ID: uuid.NewString(), ServerID: sid, Name: "b", Description: "B", InputSchema: "{}"},
		{ID: uuid.NewString(), ServerID: sid, Name: "c", Description: "C", InputSchema: "{}"},
	})
	kid := "key_" + uuid.NewString()
	_ = st.CreateAPIKey(context.Background(), store.APIKey{
		ID: kid, AccountID: acct, KeyHash: "h", Name: "k", Enabled: true,
	})
	c := policy.NewCache()
	if err := c.Warm(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	return st, c, acct, sid, kid
}

func TestDisabledToolHiddenFromEveryone(t *testing.T) {
	st, c, _, sid, kid := setup(t)
	_ = st.SetToolEnabled(context.Background(), sid, "b", false)
	_ = c.InvalidateServer(context.Background(), st, sid)
	if c.Allowed(sid, kid, "b") {
		t.Fatal("disabled tool should be denied")
	}
	if !c.Allowed(sid, kid, "a") {
		t.Fatal("enabled tool should be allowed for all-mode key")
	}
}

func TestCustomAllowlist(t *testing.T) {
	st, c, _, sid, kid := setup(t)
	_ = st.SetKeyToolPolicy(context.Background(), kid, store.ToolPolicyCustom, []string{"a"})
	_ = c.InvalidateKey(context.Background(), st, kid)
	if !c.Allowed(sid, kid, "a") {
		t.Fatal("granted tool should be allowed")
	}
	if c.Allowed(sid, kid, "c") {
		t.Fatal("ungranted tool should be denied")
	}
}

func TestCatalogMiss_AllVsCustom(t *testing.T) {
	st, c, _, sid, kid := setup(t)
	// all key: catalog-miss included
	vis := c.FilterToolsList(sid, kid, []string{"a", "brand_new"})
	if _, ok := vis["brand_new"]; !ok {
		t.Fatal("all key should see catalog-miss tool")
	}
	_ = st.SetKeyToolPolicy(context.Background(), kid, store.ToolPolicyCustom, []string{"a"})
	_ = c.InvalidateKey(context.Background(), st, kid)
	vis = c.FilterToolsList(sid, kid, []string{"a", "brand_new"})
	if _, ok := vis["brand_new"]; ok {
		t.Fatal("custom key should exclude catalog-miss")
	}
	if _, ok := vis["a"]; !ok {
		t.Fatal("granted tool missing")
	}
}

func TestReindexPrunesStaleGrants(t *testing.T) {
	st, c, _, sid, kid := setup(t)
	_ = st.SetKeyToolPolicy(context.Background(), kid, store.ToolPolicyCustom, []string{"a", "b"})
	_ = st.ReplaceTools(context.Background(), sid, []store.ToolDef{
		{ID: uuid.NewString(), ServerID: sid, Name: "a", Description: "A", InputSchema: "{}"},
	})
	p, _ := st.GetKeyToolPolicy(context.Background(), kid)
	for _, name := range p.Allowed {
		if name == "b" {
			t.Fatal("stale grant b should be pruned")
		}
	}
	_ = c.Warm(context.Background(), st)
	if c.Allowed(sid, kid, "b") {
		t.Fatal("pruned grant should not allow")
	}
}
