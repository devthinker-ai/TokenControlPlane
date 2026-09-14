package billing_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/devthinker-ai/TokenControlPlane/pkg/billing"
	"github.com/devthinker-ai/TokenControlPlane/pkg/license"
	"github.com/devthinker-ai/TokenControlPlane/pkg/session"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

func TestPriceForWindowFor(t *testing.T) {
	pro := billing.Ladder["pro"]
	if pro.PriceFor(false) != 249 || pro.PriceFor(true) != 179 {
		t.Fatalf("pro prices: std=%d founders=%d", pro.PriceFor(false), pro.PriceFor(true))
	}
	if pro.WindowFor(false) != 12 || pro.WindowFor(true) != 24 {
		t.Fatalf("pro windows: std=%d founders=%d", pro.WindowFor(false), pro.WindowFor(true))
	}
	team := billing.Ladder["team"]
	if team.PriceFor(false) != 599 || team.PriceFor(true) != 449 {
		t.Fatalf("team prices")
	}
	if team.WindowFor(false) != 12 || team.WindowFor(true) != 24 {
		t.Fatalf("team windows")
	}
	// Caps must match license.CapsForPlan — no fork.
	for _, key := range []string{"pro", "team"} {
		p := billing.Ladder[key]
		got := p.CapsFor()
		want := license.CapsForPlan(key)
		if got.MaxServers != want.MaxServers || got.MaxSeats != want.MaxSeats ||
			got.MaxKeys != want.MaxKeys || got.MonthlyTokens != want.MonthlyTokens ||
			got.ToolPolicy != want.ToolPolicy {
			t.Fatalf("%s caps forked: got=%+v want=%+v", key, got, want)
		}
	}
}

func TestFoundersLimitBoundary(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "fl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	privPath := writeTempPriv(t)
	svc := billing.New(st, billing.Config{
		VariantIDPro:     "111",
		LicensePrivPath:  privPath,
		FoundersLimit:    1,
		FoundersLimitSet: true,
	})

	// First order → founders 24mo
	acct1 := mintPaidOrder(t, st, svc, "a1@ex.com", "ord_f1")
	lic1, err := st.GetActiveLicense(t.Context(), acct1)
	if err != nil {
		t.Fatal(err)
	}
	if !lic1.IsFounders || lic1.WindowMonths != 24 {
		t.Fatalf("first: founders=%v window=%d", lic1.IsFounders, lic1.WindowMonths)
	}

	// Second order (slot full) → standard 12mo
	acct2 := mintPaidOrder(t, st, svc, "a2@ex.com", "ord_f2")
	lic2, err := st.GetActiveLicense(t.Context(), acct2)
	if err != nil {
		t.Fatal(err)
	}
	if lic2.IsFounders || lic2.WindowMonths != 12 {
		t.Fatalf("second: founders=%v window=%d", lic2.IsFounders, lic2.WindowMonths)
	}
}

func TestRenewalFreshWindowNotStacked(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "ren.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	privPath := writeTempPriv(t)
	svc := billing.New(st, billing.Config{
		VariantIDPro:     "111",
		LicensePrivPath:  privPath,
		FoundersLimit:    -1, // off — both standard 12mo
		FoundersLimitSet: true,
	})

	email := "renew@ex.com"
	acctID := mintPaidOrder(t, st, svc, email, "ord_r1")
	lic1, _ := st.GetActiveLicense(t.Context(), acctID)
	firstExpiry := lic1.ExpiresAt

	// Second purchase for same account — new window from *new* purchase, not stacked.
	time.Sleep(20 * time.Millisecond)
	_ = mintPaidOrderExisting(t, st, svc, email, acctID, "ord_r2")
	lic2, err := st.GetActiveLicense(t.Context(), acctID)
	if err != nil {
		t.Fatal(err)
	}
	if !lic2.PurchasedAt.After(lic1.PurchasedAt) {
		t.Fatal("second purchase_at should be later")
	}
	// expires = purchased + ~12*30d — NOT firstExpiry + window
	delta := lic2.ExpiresAt.Sub(lic2.PurchasedAt)
	want := 12 * 30 * 24 * time.Hour
	if delta < want-time.Minute || delta > want+time.Minute {
		t.Fatalf("window from new purchase: got %v want ~%v", delta, want)
	}
	if lic2.ExpiresAt.Equal(firstExpiry.Add(want)) {
		t.Fatal("must not stack onto first expiry")
	}
}

func TestFoundersRefundKeepsSlot(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "fr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	privPath := writeTempPriv(t)
	svc := billing.New(st, billing.Config{
		VariantIDPro:     "111",
		LicensePrivPath:  privPath,
		FoundersLimit:    1,
		FoundersLimitSet: true,
	})

	acctID := mintPaidOrder(t, st, svc, "found@ex.com", "ord_keep")
	lic, _ := st.GetActiveLicense(t.Context(), acctID)
	if !lic.IsFounders {
		t.Fatal("expected founders")
	}
	nBefore, _ := st.CountFounders(t.Context())
	_ = st.RevokeLicense(t.Context(), lic.ID)
	nAfter, _ := st.CountFounders(t.Context())
	if nBefore != 1 || nAfter != 1 {
		t.Fatalf("founders count must stay consumed after refund: before=%d after=%d", nBefore, nAfter)
	}
	// Next buyer gets standard
	acct2 := mintPaidOrder(t, st, svc, "next@ex.com", "ord_next")
	lic2, _ := st.GetActiveLicense(t.Context(), acct2)
	if lic2.IsFounders {
		t.Fatal("refunded founders slot must not reopen")
	}
}

func TestPricingEndpoint(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "pr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	svc := billing.New(st, billing.Config{
		FoundersLimit:    100,
		FoundersLimitSet: true,
	})
	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/pricing", nil)
	svc.Pricing(r, req)
	if r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
	var out struct {
		Plans []struct {
			Key   string `json:"key"`
			Price int    `json:"price"`
			Caps  struct {
				MaxServers int   `json:"max_servers"`
				Tokens     int64 `json:"monthly_tokens"`
				Unlimited  bool  `json:"unlimited"`
			} `json:"caps"`
		} `json:"plans"`
		FoundersAvailable bool `json:"founders_available"`
		FoundersLimit     int  `json:"founders_limit"`
		EverWorksForever  bool `json:"ever_works_forever"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.FoundersAvailable || out.FoundersLimit != 100 || !out.EverWorksForever {
		t.Fatalf("%+v", out)
	}
	if len(out.Plans) != 2 {
		t.Fatalf("plans=%d", len(out.Plans))
	}
	for _, p := range out.Plans {
		want := license.CapsForPlan(p.Key)
		if p.Caps.MaxServers != want.MaxServers || p.Caps.Tokens != want.MonthlyTokens {
			t.Fatalf("%s caps mismatch got=%+v want tokens=%d",
				p.Key, p.Caps, want.MonthlyTokens)
		}
		if p.Key == "pro" || p.Key == "team" {
			if p.Caps.Tokens != 0 {
				t.Fatalf("%s should have unlimited tokens (0): %+v", p.Key, p.Caps)
			}
		}
	}
}

func TestMeLicenseEverWorksForever(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "me.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	privPath := writeTempPriv(t)
	svc := billing.New(st, billing.Config{
		VariantIDPro:     "111",
		LicensePrivPath:  privPath,
		FoundersLimit:    -1,
		FoundersLimitSet: true,
	})
	acctID := mintPaidOrder(t, st, svc, "me@ex.com", "ord_me")
	lic, _ := st.GetActiveLicense(t.Context(), acctID)
	_, err = st.DB().Exec(`UPDATE licenses SET expires_at = ? WHERE id = ?`,
		time.Now().UTC().Add(-48*time.Hour).Format(time.RFC3339Nano), lic.ID)
	if err != nil {
		t.Fatal(err)
	}

	sess := session.MustManager("test-jwt-secret-phase12-xxxxxxx")
	tok, err := sess.Issue("usr_me", acctID, "me@ex.com", "admin")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/licenses/me", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	req = req.WithContext(req.Context())
	// Attach claims via middleware
	sess.Middleware(http.HandlerFunc(svc.MeLicense)).ServeHTTP(r, req)
	if r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(r.Body.Bytes(), &out)
	if out["ever_works_forever"] != true {
		t.Fatalf("ever_works_forever missing: %v", out)
	}
	if out["expired"] != true {
		t.Fatalf("expected expired: %v", out)
	}
	// Function never dies: Resolve fail-opens on bad/expired keys.
	caps := license.Resolve(nil, "not.a.valid.key")
	if caps.Licensed || caps.Plan != license.PlanFree {
		t.Fatalf("Resolve must fail open: %+v", caps)
	}
}
