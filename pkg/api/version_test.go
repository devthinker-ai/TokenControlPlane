package api_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/devthinker-ai/TokenControlPlane/pkg/api"
	"github.com/devthinker-ai/TokenControlPlane/pkg/auth"
	"github.com/devthinker-ai/TokenControlPlane/pkg/billing"
	"github.com/devthinker-ai/TokenControlPlane/pkg/license"
	"github.com/devthinker-ai/TokenControlPlane/pkg/proxy"
	"github.com/devthinker-ai/TokenControlPlane/pkg/session"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

func registerToken(t *testing.T, h http.Handler, email string) (token, accountID string) {
	t.Helper()
	resp := httptest.NewRecorder()
	body := fmt.Sprintf(`{"email":%q,"password":"password1","account_name":"Acme"}`, email)
	h.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/register", bytes.NewBufferString(body)))
	if resp.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", resp.Code, resp.Body.String())
	}
	var reg map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &reg)
	tok, _ := reg["token"].(string)
	user, _ := reg["user"].(map[string]any)
	acct, _ := user["account_id"].(string)
	if acct == "" {
		// older shape: account may be nested differently — fetch /me
		r := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/me", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		h.ServeHTTP(r, req)
		var me map[string]any
		_ = json.Unmarshal(r.Body.Bytes(), &me)
		acct, _ = me["account_id"].(string)
	}
	return tok, acct
}

func TestVersionEndpointShapeAndWindow(t *testing.T) {
	h, st := setupAPI(t)
	tok, acct := registerToken(t, h, "ver@example.com")

	// No license → update_window null
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	h.ServeHTTP(resp, req)
	if resp.Code != 200 {
		t.Fatalf("version: %d %s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &body)
	for _, k := range []string{"version", "commit", "built", "schema", "latest", "update_available"} {
		if _, ok := body[k]; !ok {
			t.Fatalf("missing %s: %v", k, body)
		}
	}
	if body["version"] != "1.0.0" {
		t.Fatalf("version=%v", body["version"])
	}
	if body["update_window"] != nil {
		t.Fatalf("expected null window, got %v", body["update_window"])
	}

	// Active license
	now := time.Now().UTC()
	err := st.InsertLicense(t.Context(), store.License{
		ID: "lic_active", AccountID: acct, Plan: "pro", Key: "key",
		PurchasedAt: now.Add(-30 * 24 * time.Hour), ExpiresAt: now.Add(60 * 24 * time.Hour),
		WindowMonths: 12, IsFounders: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/version", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	h.ServeHTTP(resp, req)
	_ = json.Unmarshal(resp.Body.Bytes(), &body)
	win, _ := body["update_window"].(map[string]any)
	if win == nil || win["active"] != true || win["is_founders"] != true {
		t.Fatalf("active window: %v", body["update_window"])
	}

	// Expired license (insert newer purchase that is expired — GetActiveLicense orders by purchased_at DESC)
	err = st.InsertLicense(t.Context(), store.License{
		ID: "lic_exp", AccountID: acct, Plan: "pro", Key: "key2",
		PurchasedAt: now.Add(-10 * 24 * time.Hour), ExpiresAt: now.Add(-5 * 24 * time.Hour),
		WindowMonths: 12,
	})
	if err != nil {
		t.Fatal(err)
	}
	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/version", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	h.ServeHTTP(resp, req)
	_ = json.Unmarshal(resp.Body.Bytes(), &body)
	win, _ = body["update_window"].(map[string]any)
	if win == nil || win["active"] != false {
		t.Fatalf("expired window: %v", body["update_window"])
	}
}

func TestVersionNoBillingNullWindow(t *testing.T) {
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
		Store: st, Sessions: sess, Keys: keys, Billing: nil, Breaker: proxy.NewBreaker(),
		GatewayURL: "https://gw.test", Caps: license.FreeCaps(), Version: "1.0.0",
		Commit: "abc", Built: "now",
	})
	tok, _ := registerToken(t, h, "nobill@example.com")
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	h.ServeHTTP(resp, req)
	if resp.Code != 200 {
		t.Fatalf("%d %s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &body)
	if body["update_window"] != nil {
		t.Fatalf("%v", body)
	}
}

func TestVersionMemberOK(t *testing.T) {
	h, _ := setupAPI(t)
	adminTok, _ := registerToken(t, h, "adm@example.com")

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/invites", bytes.NewBufferString(`{}`))
	req.Header.Set("Authorization", "Bearer "+adminTok)
	h.ServeHTTP(resp, req)
	if resp.Code != 201 && resp.Code != 200 {
		t.Fatalf("invite: %d %s", resp.Code, resp.Body.String())
	}
	var inv map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &inv)
	code, _ := inv["code"].(string)
	if code == "" {
		t.Fatalf("no invite code: %v", inv)
	}

	resp = httptest.NewRecorder()
	h.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/join", bytes.NewBufferString(
		fmt.Sprintf(`{"code":%q,"email":"mem@example.com","password":"password1","name":"Mem"}`, code))))
	if resp.Code != 201 && resp.Code != 200 {
		t.Fatalf("join: %d %s", resp.Code, resp.Body.String())
	}
	var joined map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &joined)
	memTok, _ := joined["token"].(string)

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/version", nil)
	req.Header.Set("Authorization", "Bearer "+memTok)
	h.ServeHTTP(resp, req)
	if resp.Code != 200 {
		t.Fatalf("member version: %d %s", resp.Code, resp.Body.String())
	}
}

func TestApplyUpdateFileAndSHAMismatch(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "tokencontrolplane")
	if err := os.WriteFile(exe, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	neu := filepath.Join(dir, "newbin")
	payload := []byte("new-binary-contents")
	if err := os.WriteFile(neu, payload, 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	hexSum := hex.EncodeToString(sum[:])

	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	keys, _ := auth.NewValidator(st, auth.Config{})
	sess := session.MustManager("test-jwt-secret-phase3-xxxxxxxx")
	bill := billing.New(st, billing.Config{})
	h := api.NewRouter(api.Deps{
		Store: st, Sessions: sess, Keys: keys, Billing: bill, Breaker: proxy.NewBreaker(),
		GatewayURL: "https://gw.test", Caps: license.FreeCaps(), Version: "1.0.0",
		ExePath: exe,
	})
	tok, _ := registerToken(t, h, "upd@example.com")

	// SHA mismatch — binary unchanged
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/update", bytes.NewBufferString(
		fmt.Sprintf(`{"file":%q,"sha256":"deadbeef"}`, neu)))
	req.Header.Set("Authorization", "Bearer "+tok)
	h.ServeHTTP(resp, req)
	if resp.Code != 400 {
		t.Fatalf("mismatch: %d %s", resp.Code, resp.Body.String())
	}
	if got, _ := os.ReadFile(exe); string(got) != "old-binary" {
		t.Fatal("binary should be unchanged")
	}

	// Success
	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/update", bytes.NewBufferString(
		fmt.Sprintf(`{"file":%q,"sha256":%q}`, neu, hexSum)))
	req.Header.Set("Authorization", "Bearer "+tok)
	h.ServeHTTP(resp, req)
	if resp.Code != 200 {
		t.Fatalf("update: %d %s", resp.Code, resp.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &out)
	if out["status"] != "replaced" || out["restart"] != "manual" {
		t.Fatalf("%v", out)
	}
	if got, _ := os.ReadFile(exe); string(got) != string(payload) {
		t.Fatalf("got %q", got)
	}
	if _, err := os.Stat(exe + ".prev"); err != nil {
		t.Fatal("expected .prev")
	}
}

func TestApplyUpdateRemoteDownload(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "tokencontrolplane")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	payload := []byte("remote-new-binary")
	sum := sha256.Sum256(payload)
	hexSum := hex.EncodeToString(sum[:])
	assetName := fmt.Sprintf("tokencontrolplane_%s_%s", runtime.GOOS, runtime.GOARCH)

	var assetHits, latestHits int
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/"+assetName) || r.URL.Path == "/"+assetName:
			assetHits++
			_, _ = w.Write(payload)
		case strings.HasSuffix(r.URL.Path, "/SHA256SUMS") || r.URL.Path == "/SHA256SUMS":
			_, _ = fmt.Fprintf(w, "%s  %s\n", hexSum, assetName)
		default:
			latestHits++
			base := "http://" + r.Host
			_, _ = fmt.Fprintf(w, `{"tag_name":"v1.1.0","assets":[
				{"name":%q,"browser_download_url":%q},
				{"name":"SHA256SUMS","browser_download_url":%q}
			]}`, assetName, base+"/"+assetName, base+"/SHA256SUMS")
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Setenv("UPDATE_URL", srv.URL)

	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	keys, _ := auth.NewValidator(st, auth.Config{})
	sess := session.MustManager("test-jwt-secret-phase3-xxxxxxxx")
	bill := billing.New(st, billing.Config{})
	h := api.NewRouter(api.Deps{
		Store: st, Sessions: sess, Keys: keys, Billing: bill, Breaker: proxy.NewBreaker(),
		GatewayURL: "https://gw.test", Caps: license.FreeCaps(), Version: "1.0.0",
		ExePath: exe, UpdateHTTP: srv.Client(),
	})
	tok, _ := registerToken(t, h, "remote@example.com")

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/update", bytes.NewBufferString(`{}`))
	req.Header.Set("Authorization", "Bearer "+tok)
	h.ServeHTTP(resp, req)
	if resp.Code != 200 {
		t.Fatalf("remote update: %d %s", resp.Code, resp.Body.String())
	}
	if latestHits < 1 || assetHits < 1 {
		t.Fatalf("hits latest=%d asset=%d", latestHits, assetHits)
	}
	if got, _ := os.ReadFile(exe); string(got) != string(payload) {
		t.Fatalf("got %q", got)
	}
}

func TestReleaseNotesCache(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if !strings.Contains(r.URL.Path, "/tags/v1.2.0") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.0","body":"## Hello\n- note","published_at":"2026-03-01T00:00:00Z"}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("UPDATE_URL", srv.URL+"/releases/latest")

	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	keys, _ := auth.NewValidator(st, auth.Config{})
	sess := session.MustManager("test-jwt-secret-phase3-xxxxxxxx")
	bill := billing.New(st, billing.Config{})
	h := api.NewRouter(api.Deps{
		Store: st, Sessions: sess, Keys: keys, Billing: bill, Breaker: proxy.NewBreaker(),
		GatewayURL: "https://gw.test", Caps: license.FreeCaps(), Version: "1.0.0",
		UpdateHTTP: srv.Client(),
	})
	tok, _ := registerToken(t, h, "notes@example.com")

	get := func() map[string]any {
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/releases/v1.2.0", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		h.ServeHTTP(resp, req)
		if resp.Code != 200 {
			t.Fatalf("releases: %d %s", resp.Code, resp.Body.String())
		}
		var body map[string]any
		_ = json.Unmarshal(resp.Body.Bytes(), &body)
		return body
	}
	b1 := get()
	if b1["tag"] != "v1.2.0" || !strings.Contains(fmt.Sprint(b1["body_markdown"]), "Hello") {
		t.Fatalf("%v", b1)
	}
	_ = get()
	if hits != 1 {
		t.Fatalf("expected 1 upstream hit (24h cache), got %d", hits)
	}
}
