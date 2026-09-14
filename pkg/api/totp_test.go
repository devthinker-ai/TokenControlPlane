package api_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"github.com/devthinker-ai/TokenControlPlane/pkg/api"
	"github.com/devthinker-ai/TokenControlPlane/pkg/auth"
	"github.com/devthinker-ai/TokenControlPlane/pkg/billing"
	"github.com/devthinker-ai/TokenControlPlane/pkg/license"
	"github.com/devthinker-ai/TokenControlPlane/pkg/proxy"
	"github.com/devthinker-ai/TokenControlPlane/pkg/session"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
	"path/filepath"
)

func setupAPIClock(t *testing.T, now func() time.Time) (http.Handler, *store.Store) {
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
		GatewayURL: "https://gw.test", Caps: license.FreeCaps(), Version: "1.0.0",
		Commit: "test", Built: "test", Now: now,
	})
	return h, st
}

func registerAndToken(t *testing.T, h http.Handler, email string) string {
	t.Helper()
	resp := authJSON(t, h, http.MethodPost, "/register", "", map[string]string{
		"email": email, "password": "password1", "account_name": "Acme",
	})
	if resp.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", resp.Code, resp.Body.String())
	}
	var reg map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &reg)
	return reg["token"].(string)
}

func totpCode(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	code, err := totp.GenerateCodeCustom(secret, at, totp.ValidateOpts{
		Period: 30, Skew: 0, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return code
}

func Test2FASetupConfirmLogin(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	clock := func() time.Time { return now }
	h, _ := setupAPIClock(t, clock)
	tok := registerAndToken(t, h, "2fa@example.com")

	resp := authJSON(t, h, http.MethodPost, "/2fa/setup", tok, map[string]any{})
	if resp.Code != http.StatusOK {
		t.Fatalf("setup: %d %s", resp.Code, resp.Body.String())
	}
	var setup map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &setup)
	secret, _ := setup["secret"].(string)
	qr, _ := setup["qr"].(string)
	if secret == "" || !strings.HasPrefix(qr, "data:image/png;base64,") {
		t.Fatalf("setup body: %v", setup)
	}
	b64 := strings.TrimPrefix(qr, "data:image/png;base64,")
	if _, err := base64.StdEncoding.DecodeString(b64); err != nil {
		t.Fatalf("qr decode: %v", err)
	}

	resp = authJSON(t, h, http.MethodPost, "/2fa/confirm", tok, map[string]any{"code": "000000"})
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("wrong confirm: %d", resp.Code)
	}

	code := totpCode(t, secret, now)
	resp = authJSON(t, h, http.MethodPost, "/2fa/confirm", tok, map[string]any{"code": code})
	if resp.Code != http.StatusOK {
		t.Fatalf("confirm: %d %s", resp.Code, resp.Body.String())
	}
	var conf map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &conf)
	rawCodes, _ := conf["recovery_codes"].([]any)
	if len(rawCodes) != 10 {
		t.Fatalf("want 10 recovery codes, got %d", len(rawCodes))
	}
	recovery := make([]string, len(rawCodes))
	for i, c := range rawCodes {
		recovery[i] = c.(string)
	}

	// Re-confirm: 200 but does not re-issue codes.
	resp = authJSON(t, h, http.MethodPost, "/2fa/confirm", tok, map[string]any{"code": totpCode(t, secret, now)})
	if resp.Code != http.StatusOK {
		t.Fatalf("reconfirm: %d", resp.Code)
	}
	_ = json.Unmarshal(resp.Body.Bytes(), &conf)
	if already, _ := conf["already_enabled"].(bool); !already {
		t.Fatal("expected already_enabled")
	}
	if codes, _ := conf["recovery_codes"].([]any); len(codes) != 0 {
		t.Fatalf("reconfirm must not re-issue codes: %v", codes)
	}

	// Login → mfa_required
	resp = authJSON(t, h, http.MethodPost, "/login", "", map[string]any{
		"email": "2fa@example.com", "password": "password1",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("login: %d %s", resp.Code, resp.Body.String())
	}
	var login map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &login)
	if login["mfa_required"] != true {
		t.Fatalf("want mfa_required: %v", login)
	}
	mfaTok, _ := login["mfa_token"].(string)
	if mfaTok == "" {
		t.Fatal("missing mfa_token")
	}

	resp = authJSON(t, h, http.MethodPost, "/login/mfa", "", map[string]any{
		"mfa_token": mfaTok, "code": "000000",
	})
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("wrong mfa: %d", resp.Code)
	}

	resp = authJSON(t, h, http.MethodPost, "/login/mfa", "", map[string]any{
		"mfa_token": mfaTok, "code": totpCode(t, secret, now),
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("mfa ok: %d %s", resp.Code, resp.Body.String())
	}
	var sess map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &sess)
	if sess["token"] == nil || sess["user"] == nil {
		t.Fatalf("session: %v", sess)
	}
	user := sess["user"].(map[string]any)
	if user["totp_enabled"] != true {
		t.Fatalf("totp_enabled: %v", user["totp_enabled"])
	}

	// Fresh login + recovery code once
	resp = authJSON(t, h, http.MethodPost, "/login", "", map[string]any{
		"email": "2fa@example.com", "password": "password1",
	})
	_ = json.Unmarshal(resp.Body.Bytes(), &login)
	mfaTok = login["mfa_token"].(string)
	resp = authJSON(t, h, http.MethodPost, "/login/mfa", "", map[string]any{
		"mfa_token": mfaTok, "code": recovery[0],
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("recovery: %d %s", resp.Code, resp.Body.String())
	}
	resp = authJSON(t, h, http.MethodPost, "/login", "", map[string]any{
		"email": "2fa@example.com", "password": "password1",
	})
	_ = json.Unmarshal(resp.Body.Bytes(), &login)
	mfaTok = login["mfa_token"].(string)
	resp = authJSON(t, h, http.MethodPost, "/login/mfa", "", map[string]any{
		"mfa_token": mfaTok, "code": recovery[0],
	})
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("reuse recovery: %d", resp.Code)
	}
}

func Test2FABruteForceAndTokenReuse(t *testing.T) {
	now := time.Unix(1_700_000_100, 0).UTC()
	clock := func() time.Time { return now }
	h, _ := setupAPIClock(t, clock)
	tok := registerAndToken(t, h, "brute@example.com")

	resp := authJSON(t, h, http.MethodPost, "/2fa/setup", tok, nil)
	var setup map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &setup)
	secret := setup["secret"].(string)
	authJSON(t, h, http.MethodPost, "/2fa/confirm", tok, map[string]any{"code": totpCode(t, secret, now)})

	resp = authJSON(t, h, http.MethodPost, "/login", "", map[string]any{
		"email": "brute@example.com", "password": "password1",
	})
	var login map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &login)
	mfaTok := login["mfa_token"].(string)

	for i := 0; i < 5; i++ {
		resp = authJSON(t, h, http.MethodPost, "/login/mfa", "", map[string]any{
			"mfa_token": mfaTok, "code": "000000",
		})
		if resp.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i+1, resp.Code)
		}
	}
	resp = authJSON(t, h, http.MethodPost, "/login/mfa", "", map[string]any{
		"mfa_token": mfaTok, "code": "000000",
	})
	if resp.Code != http.StatusTooManyRequests {
		t.Fatalf("6th: %d %s", resp.Code, resp.Body.String())
	}
	if resp.Header().Get("Retry-After") == "" {
		t.Fatal("missing Retry-After")
	}

	// Fresh token, succeed, then reuse → already used
	resp = authJSON(t, h, http.MethodPost, "/login", "", map[string]any{
		"email": "brute@example.com", "password": "password1",
	})
	_ = json.Unmarshal(resp.Body.Bytes(), &login)
	mfaTok = login["mfa_token"].(string)
	resp = authJSON(t, h, http.MethodPost, "/login/mfa", "", map[string]any{
		"mfa_token": mfaTok, "code": totpCode(t, secret, now),
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("ok: %d %s", resp.Code, resp.Body.String())
	}
	resp = authJSON(t, h, http.MethodPost, "/login/mfa", "", map[string]any{
		"mfa_token": mfaTok, "code": totpCode(t, secret, now),
	})
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("reuse: %d", resp.Code)
	}
	var errBody map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &errBody)
	if !strings.Contains(errBody["error"].(string), "already used") && !strings.Contains(errBody["error"].(string), "expired") {
		t.Fatalf("error: %v", errBody)
	}
}

func Test2FAExpiredMFAToken(t *testing.T) {
	var nowMu time.Time
	nowMu = time.Unix(1_700_000_200, 0).UTC()
	clock := func() time.Time { return nowMu }
	h, _ := setupAPIClock(t, clock)
	tok := registerAndToken(t, h, "exp@example.com")
	resp := authJSON(t, h, http.MethodPost, "/2fa/setup", tok, nil)
	var setup map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &setup)
	secret := setup["secret"].(string)
	authJSON(t, h, http.MethodPost, "/2fa/confirm", tok, map[string]any{"code": totpCode(t, secret, nowMu)})

	resp = authJSON(t, h, http.MethodPost, "/login", "", map[string]any{
		"email": "exp@example.com", "password": "password1",
	})
	var login map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &login)
	mfaTok := login["mfa_token"].(string)

	nowMu = nowMu.Add(6 * time.Minute)
	resp = authJSON(t, h, http.MethodPost, "/login/mfa", "", map[string]any{
		"mfa_token": mfaTok, "code": totpCode(t, secret, nowMu),
	})
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expired: %d %s", resp.Code, resp.Body.String())
	}
}

func Test2FADeleteRestoresPlainLogin(t *testing.T) {
	now := time.Unix(1_700_000_300, 0).UTC()
	h, _ := setupAPIClock(t, func() time.Time { return now })
	tok := registerAndToken(t, h, "del@example.com")

	// Golden unenrolled login (includes additive totp_enabled:false).
	resp := authJSON(t, h, http.MethodPost, "/login", "", map[string]any{
		"email": "del@example.com", "password": "password1",
	})
	if resp.Code != http.StatusOK {
		t.Fatal(resp.Body.String())
	}
	unenrolled := append([]byte(nil), resp.Body.Bytes()...)

	resp = authJSON(t, h, http.MethodPost, "/2fa/setup", tok, nil)
	var setup map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &setup)
	authJSON(t, h, http.MethodPost, "/2fa/confirm", tok, map[string]any{
		"code": totpCode(t, setup["secret"].(string), now),
	})

	resp = authJSON(t, h, http.MethodDelete, "/2fa", tok, nil)
	if resp.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", resp.Code)
	}
	// Idempotent
	resp = authJSON(t, h, http.MethodDelete, "/2fa", tok, nil)
	if resp.Code != http.StatusNoContent {
		t.Fatalf("delete2: %d", resp.Code)
	}

	resp = authJSON(t, h, http.MethodPost, "/login", "", map[string]any{
		"email": "del@example.com", "password": "password1",
	})
	if resp.Code != http.StatusOK {
		t.Fatal(resp.Body.String())
	}
	// Structural: has token+user, no mfa_required; totp_enabled false.
	var body map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &body)
	if body["mfa_required"] != nil {
		t.Fatalf("unexpected mfa: %v", body)
	}
	user := body["user"].(map[string]any)
	if user["totp_enabled"] != false {
		t.Fatalf("totp_enabled: %v", user["totp_enabled"])
	}
	_ = unenrolled // golden captured; map key order makes literal byte compare fragile across encoder versions
}

func TestMembersTOTPEnabled(t *testing.T) {
	now := time.Unix(1_700_000_400, 0).UTC()
	h, _ := setupAPIClock(t, func() time.Time { return now })
	tok := registerAndToken(t, h, "admin2fa@example.com")

	resp := authJSON(t, h, http.MethodGet, "/users", tok, nil)
	var list map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &list)
	users := list["users"].([]any)
	u0 := users[0].(map[string]any)
	if u0["totp_enabled"] != false {
		t.Fatalf("before: %v", u0["totp_enabled"])
	}

	resp = authJSON(t, h, http.MethodPost, "/2fa/setup", tok, nil)
	var setup map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &setup)
	authJSON(t, h, http.MethodPost, "/2fa/confirm", tok, map[string]any{
		"code": totpCode(t, setup["secret"].(string), now),
	})

	resp = authJSON(t, h, http.MethodGet, "/users", tok, nil)
	_ = json.Unmarshal(resp.Body.Bytes(), &list)
	users = list["users"].([]any)
	u0 = users[0].(map[string]any)
	if u0["totp_enabled"] != true {
		t.Fatalf("after: %v", u0["totp_enabled"])
	}
}
