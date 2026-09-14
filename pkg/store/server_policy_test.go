package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

func TestUsageByServerMatchesKeyTotal(t *testing.T) {
	st, err := store.OpenWithOptions(filepath.Join(t.TempDir(), "u.db"), store.OpenOptions{AppVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	acct := "acct_" + uuid.NewString()
	_ = st.CreateAccount(context.Background(), store.Account{ID: acct, Name: "t", Plan: "pro", MaxSeats: 10, MaxServers: 10})
	k1 := "key_" + uuid.NewString()
	_ = st.CreateAPIKey(context.Background(), store.APIKey{ID: k1, AccountID: acct, KeyHash: "h1", Name: "k1", Enabled: true})
	s1 := "srv_" + uuid.NewString()
	s2 := "srv_" + uuid.NewString()
	_ = st.CreateServer(context.Background(), store.MCPServer{ID: s1, AccountID: acct, Name: "A", BaseURL: "https://a", Enabled: true})
	_ = st.CreateServer(context.Background(), store.MCPServer{ID: s2, AccountID: acct, Name: "B", BaseURL: "https://b", Enabled: true})

	period := store.PeriodStartUTC(time.Now().UTC())
	_ = st.IncrUsageWithServer(context.Background(), k1, s1, period, 40, 40, 20, 1)
	_ = st.IncrUsageWithServer(context.Background(), k1, s2, period, 80, 80, 40, 2)

	u, err := st.GetUsage(context.Background(), k1, period)
	if err != nil {
		t.Fatal(err)
	}
	by, err := st.MapUsageByServer(context.Background(), k1, period)
	if err != nil {
		t.Fatal(err)
	}
	var sum int64
	for _, n := range by {
		sum += n
	}
	if sum != u.TokensUsed {
		t.Fatalf("sum by_server=%d usage=%d", sum, u.TokensUsed)
	}
	if by[s1] != 20 || by[s2] != 40 {
		t.Fatalf("by=%v", by)
	}
}

func TestServerScopePolicy(t *testing.T) {
	st, err := store.OpenWithOptions(filepath.Join(t.TempDir(), "s.db"), store.OpenOptions{AppVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	acct := "acct_" + uuid.NewString()
	_ = st.CreateAccount(context.Background(), store.Account{ID: acct, Name: "t", Plan: "pro", MaxSeats: 10, MaxServers: 10})
	kid := "key_" + uuid.NewString()
	_ = st.CreateAPIKey(context.Background(), store.APIKey{ID: kid, AccountID: acct, KeyHash: "h", Name: "k", Enabled: true})
	s1 := "srv_" + uuid.NewString()
	s2 := "srv_" + uuid.NewString()
	_ = st.CreateServer(context.Background(), store.MCPServer{ID: s1, AccountID: acct, Name: "A", BaseURL: "https://a", Enabled: true})
	_ = st.CreateServer(context.Background(), store.MCPServer{ID: s2, AccountID: acct, Name: "B", BaseURL: "https://b", Enabled: true})

	err = st.SetKeyServerAccess(context.Background(), kid, store.ServerPolicyCustom, []string{s1}, []store.KeyServerBudget{
		{ServerID: s1, MonthlyBudget: 100},
	})
	if err != nil {
		t.Fatal(err)
	}
	p, err := st.GetKeyServerPolicy(context.Background(), kid)
	if err != nil || p.Mode != store.ServerPolicyCustom || len(p.Allowed) != 1 || p.Allowed[0] != s1 {
		t.Fatalf("%+v err=%v", p, err)
	}
	budgets, _ := st.ListKeyServerBudgets(context.Background(), kid)
	if len(budgets) != 1 || budgets[0].MonthlyBudget != 100 {
		t.Fatalf("%+v", budgets)
	}
}
