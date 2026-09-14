package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devthinker-ai/TokenControlPlane/pkg/api"
	"github.com/devthinker-ai/TokenControlPlane/pkg/auth"
	"github.com/devthinker-ai/TokenControlPlane/pkg/billing"
	"github.com/devthinker-ai/TokenControlPlane/pkg/license"
	"github.com/devthinker-ai/TokenControlPlane/pkg/proxy"
	"github.com/devthinker-ai/TokenControlPlane/pkg/session"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

func setupAPI(t *testing.T) (http.Handler, *store.Store) {
	return setupAPICaps(t, license.FreeCaps())
}

func setupAPICaps(t *testing.T, caps license.Caps) (http.Handler, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	keys, err := auth.NewValidator(st, auth.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sess := session.MustManager("test-jwt-secret-phase3-xxxxxxxx")
	bill := billing.New(st, billing.Config{})
	breaker := proxy.NewBreaker()
	h := api.NewRouter(api.Deps{
		Store: st, Sessions: sess, Keys: keys, Billing: bill, Breaker: breaker,
		GatewayURL: "https://gw.test", Caps: caps, Version: "1.0.0",
		Commit: "test", Built: "test",
	})
	return h, st
}

func TestDisableRegister(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	keys, err := auth.NewValidator(st, auth.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sess := session.MustManager("test-jwt-secret-phase3-xxxxxxxx")
	h := api.NewRouter(api.Deps{
		Store: st, Sessions: sess, Keys: keys, GatewayURL: "https://gw.test",
		Caps: license.FreeCaps(), DisableRegister: true,
	})

	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/public-config", nil))
	if resp.Code != http.StatusOK {
		t.Fatalf("public-config: %d", resp.Code)
	}
	var cfg map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["registration_enabled"] != false {
		t.Fatalf("want registration_enabled=false, got %v", cfg["registration_enabled"])
	}

	resp = httptest.NewRecorder()
	h.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/register", bytes.NewBufferString(
		`{"email":"blocked@example.com","password":"password1","account_name":"Nope"}`)))
	if resp.Code != http.StatusForbidden {
		t.Fatalf("register: %d %s", resp.Code, resp.Body.String())
	}
}

func TestRegisterLoginMeUsage(t *testing.T) {
	h, _ := setupAPI(t)

	regBody := `{"email":"a@example.com","password":"password1","account_name":"Acme"}`
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/register", bytes.NewBufferString(regBody)))
	if resp.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", resp.Code, resp.Body.String())
	}
	var reg map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &reg)
	token, _ := reg["token"].(string)
	if token == "" {
		t.Fatal("missing token")
	}

	resp = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBufferString(`{"email":"a@example.com","password":"password1"}`))
	h.ServeHTTP(resp, req)
	if resp.Code != 200 {
		t.Fatalf("login: %d", resp.Code)
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(resp, req)
	if resp.Code != 200 {
		t.Fatalf("me: %d %s", resp.Code, resp.Body.String())
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/usage", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(resp, req)
	if resp.Code != 200 {
		t.Fatalf("usage: %d %s", resp.Code, resp.Body.String())
	}
}

func TestPlanCapServers(t *testing.T) {
	h, _ := setupAPI(t)
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/register", bytes.NewBufferString(
		`{"email":"b@example.com","password":"password1","account_name":"Cap"}`)))
	var reg map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &reg)
	token := reg["token"].(string)

	create := func() int {
		r := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/servers", bytes.NewBufferString(
			`{"name":"s","base_url":"http://127.0.0.1:9"}`))
		req.Header.Set("Authorization", "Bearer "+token)
		h.ServeHTTP(r, req)
		return r.Code
	}
	for i := 0; i < 3; i++ {
		if code := create(); code != http.StatusCreated {
			t.Fatalf("server %d: %d", i+1, code)
		}
	}
	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/servers", bytes.NewBufferString(
		`{"name":"over","base_url":"http://127.0.0.1:9"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(r, req)
	if r.Code != http.StatusPaymentRequired {
		t.Fatalf("want 402, got %d %s", r.Code, r.Body.String())
	}
	body, _ := io.ReadAll(r.Body)
	if !bytes.Contains(body, []byte("LICENSE_KEY")) {
		t.Fatalf("unlicensed body should mention LICENSE_KEY: %s", body)
	}
}

func TestLicensedThreeServerCap(t *testing.T) {
	// Explicit licensed caps (simulates a 3-server self-hosted license).
	caps := license.Caps{
		Plan: license.PlanPro, MaxSeats: 3, MaxServers: 3, MaxKeys: 5,
		MonthlyTokens: license.FreeMonthlyTokens, Licensed: true, Subject: "test-co",
	}
	h, _ := setupAPICaps(t, caps)
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/register", bytes.NewBufferString(
		`{"email":"lic@example.com","password":"password1","account_name":"Lic"}`)))
	var reg map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &reg)
	token := reg["token"].(string)

	for i := 0; i < 3; i++ {
		r := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/servers", bytes.NewBufferString(
			`{"name":"s","base_url":"http://127.0.0.1:9"}`))
		req.Header.Set("Authorization", "Bearer "+token)
		h.ServeHTTP(r, req)
		if r.Code != http.StatusCreated {
			t.Fatalf("server %d: %d %s", i+1, r.Code, r.Body.String())
		}
	}
	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/servers", bytes.NewBufferString(
		`{"name":"over","base_url":"http://127.0.0.1:9"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(r, req)
	if r.Code != http.StatusPaymentRequired {
		t.Fatalf("want 402, got %d", r.Code)
	}
	body, _ := io.ReadAll(r.Body)
	if !bytes.Contains(body, []byte("Upgrade to")) {
		t.Fatalf("licensed body should say Upgrade to: %s", body)
	}
}

func TestTeamTwentySixthServer402(t *testing.T) {
	caps := license.CapsForPlan(license.PlanTeam)
	caps.Licensed = true
	caps.Subject = "team-co"
	h, st := setupAPICaps(t, caps)
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/register", bytes.NewBufferString(
		`{"email":"team26@example.com","password":"password1","account_name":"Team26"}`)))
	var reg map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &reg)
	token := reg["token"].(string)
	acctID := reg["user"].(map[string]any)["account_id"].(string)
	_ = st.UpdateAccountPlan(t.Context(), acctID, store.PlanTeam, nil)

	createServer := func() int {
		r := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/servers", bytes.NewBufferString(
			`{"name":"s","base_url":"http://127.0.0.1:9"}`))
		req.Header.Set("Authorization", "Bearer "+token)
		h.ServeHTTP(r, req)
		return r.Code
	}
	for i := 0; i < 25; i++ {
		if code := createServer(); code != http.StatusCreated {
			t.Fatalf("team server %d: %d", i+1, code)
		}
	}
	if code := createServer(); code != http.StatusPaymentRequired {
		t.Fatalf("26th server want 402 got %d", code)
	}
}

func TestProEleventhServerStill402(t *testing.T) {
	caps := license.CapsForPlan(license.PlanPro)
	caps.Licensed = true
	h, _ := setupAPICaps(t, caps)
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/register", bytes.NewBufferString(
		`{"email":"pro11@example.com","password":"password1","account_name":"Pro11"}`)))
	var reg map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &reg)
	token := reg["token"].(string)

	for i := 0; i < 10; i++ {
		r := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/servers", bytes.NewBufferString(
			`{"name":"s","base_url":"http://127.0.0.1:9"}`))
		req.Header.Set("Authorization", "Bearer "+token)
		h.ServeHTTP(r, req)
		if r.Code != http.StatusCreated {
			t.Fatalf("server %d: %d %s", i+1, r.Code, r.Body.String())
		}
	}
	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/servers", bytes.NewBufferString(
		`{"name":"over","base_url":"http://127.0.0.1:9"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(r, req)
	if r.Code != http.StatusPaymentRequired {
		t.Fatalf("want 402 on 11th, got %d %s", r.Code, r.Body.String())
	}
}

func TestCreateKeyDefaultBudgetByPlan(t *testing.T) {
	// Free (unlicensed) → 5M; Pro licensed → 0.
	hFree, stFree := setupAPI(t)
	resp := httptest.NewRecorder()
	hFree.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/register", bytes.NewBufferString(
		`{"email":"freekey@example.com","password":"password1","account_name":"FreeKey"}`)))
	var reg map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &reg)
	token := reg["token"].(string)
	acctID := reg["user"].(map[string]any)["account_id"].(string)

	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/keys", bytes.NewBufferString(`{"name":"f"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	hFree.ServeHTTP(r, req)
	if r.Code != http.StatusCreated {
		t.Fatalf("free key: %d %s", r.Code, r.Body.String())
	}
	keys, _ := stFree.ListAPIKeysByAccount(t.Context(), acctID)
	if len(keys) != 1 || keys[0].MonthlyBudget != license.FreeMonthlyTokens {
		t.Fatalf("free budget: %+v", keys)
	}

	caps := license.CapsForPlan(license.PlanPro)
	caps.Licensed = true
	hPro, stPro := setupAPICaps(t, caps)
	resp = httptest.NewRecorder()
	hPro.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/register", bytes.NewBufferString(
		`{"email":"prokey@example.com","password":"password1","account_name":"ProKey"}`)))
	_ = json.Unmarshal(resp.Body.Bytes(), &reg)
	token = reg["token"].(string)
	acctID = reg["user"].(map[string]any)["account_id"].(string)

	r = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/keys", bytes.NewBufferString(`{"name":"p"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	hPro.ServeHTTP(r, req)
	if r.Code != http.StatusCreated {
		t.Fatalf("pro key: %d %s", r.Code, r.Body.String())
	}
	keys, _ = stPro.ListAPIKeysByAccount(t.Context(), acctID)
	if len(keys) != 1 || keys[0].MonthlyBudget != 0 {
		t.Fatalf("pro budget want 0: %+v", keys)
	}
}

func TestCreateKeySnippets(t *testing.T) {
	h, _ := setupAPI(t)
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/register", bytes.NewBufferString(
		`{"email":"c@example.com","password":"password1","account_name":"Keys"}`)))
	var reg map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &reg)
	token := reg["token"].(string)

	req := httptest.NewRequest(http.MethodPost, "/servers", bytes.NewBufferString(
		`{"name":"demo","base_url":"http://127.0.0.1:9"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	r := httptest.NewRecorder()
	h.ServeHTTP(r, req)

	req = httptest.NewRequest(http.MethodPost, "/keys", bytes.NewBufferString(`{"name":"laptop"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	r = httptest.NewRecorder()
	h.ServeHTTP(r, req)
	if r.Code != http.StatusCreated {
		t.Fatalf("create key: %d %s", r.Code, r.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(r.Body.Bytes(), &out)
	key, _ := out["key"].(string)
	if key == "" || !strings.HasPrefix(key, "tcp_") {
		t.Fatalf("key: %v", out["key"])
	}
	snips := out["snippets"].(map[string]any)
	for _, k := range []string{"claude_desktop", "cursor", "windsurf"} {
		raw, _ := snips[k].(string)
		if raw == "" {
			t.Fatalf("missing snippet %s", k)
		}
		var root map[string]any
		if err := json.Unmarshal([]byte(raw), &root); err != nil {
			t.Fatalf("%s invalid json: %v", k, err)
		}
	}
}

func TestOnboardingAndActivity(t *testing.T) {
	h, st := setupAPI(t)
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/register", bytes.NewBufferString(
		`{"email":"onb@example.com","password":"password1","account_name":"Onb"}`)))
	var reg map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &reg)
	token := reg["token"].(string)
	auth := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		h.ServeHTTP(r, req)
		return r
	}

	r := auth(http.MethodGet, "/onboarding", "")
	if r.Code != 200 {
		t.Fatalf("onboarding: %d %s", r.Code, r.Body.String())
	}
	var ob map[string]any
	_ = json.Unmarshal(r.Body.Bytes(), &ob)
	if ob["has_servers"] != false || ob["onboarded"] != false {
		t.Fatalf("onboarding=%v", ob)
	}

	r = auth(http.MethodPost, "/keys", `{"name":"loop"}`)
	if r.Code != http.StatusCreated {
		t.Fatalf("key: %d", r.Code)
	}
	r = auth(http.MethodGet, "/activity?limit=10", "")
	if r.Code != 200 {
		t.Fatalf("activity: %d %s", r.Code, r.Body.String())
	}
	var events []map[string]any
	_ = json.Unmarshal(r.Body.Bytes(), &events)
	if len(events) < 1 || events[0]["kind"] != "key_created" {
		t.Fatalf("events=%v", events)
	}

	acctID := reg["user"].(map[string]any)["account_id"].(string)
	_ = st.InsertActivityEvent(t.Context(), acctID, store.ActivityKeyKilledAuto, "loop",
		"Key 'loop' was auto-killed: 121 req in 60s", map[string]any{"requests": 121})
	r = auth(http.MethodGet, "/activity?limit=5", "")
	_ = json.Unmarshal(r.Body.Bytes(), &events)
	found := false
	for _, e := range events {
		if e["kind"] == "key_killed_auto" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing auto-kill event: %v", events)
	}

	r = auth(http.MethodPost, "/onboarding/complete", "")
	if r.Code != 200 {
		t.Fatalf("complete: %d", r.Code)
	}
	r = auth(http.MethodGet, "/onboarding", "")
	_ = json.Unmarshal(r.Body.Bytes(), &ob)
	if ob["onboarded"] != true {
		t.Fatalf("expected onboarded: %v", ob)
	}
}

func TestUpdateCheckNoUpdateCheck(t *testing.T) {
	t.Setenv("NO_UPDATE_CHECK", "1")
	h, _ := setupAPI(t)
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/register", bytes.NewBufferString(
		`{"email":"upd@example.com","password":"password1","account_name":"Upd"}`)))
	var reg map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &reg)
	token := reg["token"].(string)

	resp = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/update-check", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(resp, req)
	if resp.Code != 200 {
		t.Fatalf("update-check: %d %s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &body)
	if body["available"] != false || body["current"] != "1.0.0" {
		t.Fatalf("%v", body)
	}
}
