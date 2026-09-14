package billing_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/devthinker-ai/TokenControlPlane/pkg/billing"
	"github.com/devthinker-ai/TokenControlPlane/pkg/license"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

func TestWebhookTeamMint(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "team.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	privPath := filepath.Join(t.TempDir(), "license.pem")
	if err := os.WriteFile(privPath, pemPrivate(priv), 0o600); err != nil {
		t.Fatal(err)
	}

	svc := billing.New(st, billing.Config{
		WebhookSecret:    "",
		VariantIDPro:     "111",
		VariantIDTeam:    "222",
		LicensePrivPath:  privPath,
		FoundersLimit:    -1, // standard 12mo
		FoundersLimitSet: true,
	})

	email := "team@example.com"
	acctID := "acct_" + uuid.NewString()
	if err := st.CreateAccount(t.Context(), store.Account{
		ID: acctID, Name: "T", Plan: store.PlanFree, MaxSeats: 3, MaxServers: 3,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateUser(t.Context(), store.User{
		ID: "usr_" + uuid.NewString(), AccountID: acctID, Email: email,
		PasswordHash: "x", Name: "Buyer", Role: "admin",
	}); err != nil {
		t.Fatal(err)
	}

	payload, _ := json.Marshal(map[string]any{
		"meta": map[string]any{
			"event_name":  "order_paid",
			"custom_data": map[string]any{"account_id": acctID},
		},
		"data": map[string]any{
			"type": "orders",
			"id":   "ord_team_1",
			"attributes": map[string]any{
				"customer_id": 99,
				"user_email":  email,
				"currency":    "USD",
				"total":       59900,
				"status":      "paid",
				"first_order_item": map[string]any{
					"variant_id": 222,
					"product_id": 2,
				},
			},
		},
	})
	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(payload))
	req.Header.Set("X-Event-Name", "order_paid")
	svc.Webhook(r, req)
	if r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
	acct, err := st.GetAccount(t.Context(), acctID)
	if err != nil {
		t.Fatal(err)
	}
	if acct.Plan != store.PlanTeam {
		t.Fatalf("plan=%s", acct.Plan)
	}
	lic, err := st.GetActiveLicense(t.Context(), acctID)
	if err != nil {
		t.Fatal(err)
	}
	if lic.Plan != store.PlanTeam || lic.WindowMonths != 12 || lic.IsFounders {
		t.Fatalf("lic=%+v", lic)
	}
	claims, err := license.ValidateWithKey(lic.Key, &priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Plan != store.PlanTeam || claims.MaxServers != 25 {
		t.Fatalf("claims=%+v", claims)
	}
}

func TestWebhookIdempotencyAndMint(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	privPath := filepath.Join(t.TempDir(), "license.pem")
	if err := os.WriteFile(privPath, pemPrivate(priv), 0o600); err != nil {
		t.Fatal(err)
	}

	svc := billing.New(st, billing.Config{
		WebhookSecret:   "", // parse without verify
		VariantIDPro:    "111",
		LicensePrivPath: privPath,
	})

	email := "buyer@example.com"
	acctID := "acct_" + uuid.NewString()
	if err := st.CreateAccount(t.Context(), store.Account{
		ID: acctID, Name: "T", Plan: store.PlanFree, MaxSeats: 3, MaxServers: 3,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateUser(t.Context(), store.User{
		ID: "usr_" + uuid.NewString(), AccountID: acctID, Email: email,
		PasswordHash: "x", Name: "Buyer", Role: "admin",
	}); err != nil {
		t.Fatal(err)
	}

	orderID := "99"
	payload, _ := json.Marshal(map[string]any{
		"meta": map[string]any{
			"event_name":  "order_created",
			"custom_data": map[string]any{"account_id": acctID},
		},
		"data": map[string]any{
			"type": "orders",
			"id":   orderID,
			"attributes": map[string]any{
				"customer_id": 42,
				"user_email":  email,
				"currency":    "USD",
				"total":       24900,
				"status":      "paid",
				"first_order_item": map[string]any{
					"variant_id": 111,
					"product_id": 1,
				},
			},
		},
	})

	do := func() *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(payload))
		req.Header.Set("X-Event-Name", "order_created")
		svc.Webhook(r, req)
		return r
	}

	r1 := do()
	if r1.Code != 200 {
		t.Fatalf("first: %d %s", r1.Code, r1.Body.String())
	}
	acct, err := st.GetAccount(t.Context(), acctID)
	if err != nil {
		t.Fatal(err)
	}
	if acct.Plan != store.PlanPro {
		t.Fatalf("plan: %s", acct.Plan)
	}
	lic, err := st.GetActiveLicense(t.Context(), acctID)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := license.ValidateWithKey(lic.Key, &priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Plan != store.PlanPro || claims.Subject != email {
		t.Fatalf("claims=%+v", claims)
	}
	if !lic.IsFounders || lic.WindowMonths != 24 {
		t.Fatalf("founders mint: founders=%v window=%d (default limit 100)", lic.IsFounders, lic.WindowMonths)
	}
	// Paste for commit notes: header.payload.sig — exp is update window end.
	t.Logf("minted license JWT (test key):\n%s\nexp claim unix=%d (%s)", lic.Key, claims.ExpiresAt.Unix(), claims.ExpiresAt.Time.UTC().Format(time.RFC3339))

	r2 := do()
	if r2.Code != 200 {
		t.Fatalf("replay: %d %s", r2.Code, r2.Body.String())
	}
	if !bytes.Contains(r2.Body.Bytes(), []byte(`"duplicate":true`)) {
		t.Fatalf("expected duplicate ack: %s", r2.Body.String())
	}
}

func TestWebhookBadSignature(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "sig.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	svc := billing.New(st, billing.Config{WebhookSecret: "whsec_test"})
	body := []byte(`{"meta":{"event_name":"order_created"},"data":{"id":"1","attributes":{}}}`)
	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Signature", "deadbeef")
	req.Header.Set("X-Event-Name", "order_created")
	svc.Webhook(r, req)
	if r.Code != 400 {
		t.Fatalf("code=%d body=%s", r.Code, r.Body.String())
	}
}

func TestWebhookGoodSignature(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "ok.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	secret := "whsec_test"
	svc := billing.New(st, billing.Config{WebhookSecret: secret})
	body := []byte(`{"meta":{"event_name":"subscription_created"},"data":{"id":"1","type":"subscriptions","attributes":{}}}`)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))

	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Signature", sig)
	req.Header.Set("X-Event-Name", "subscription_created")
	svc.Webhook(r, req)
	if r.Code != 200 {
		t.Fatalf("code=%d body=%s", r.Code, r.Body.String())
	}
}

func TestRefundRevokesLicense(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "ref.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	privPath := filepath.Join(t.TempDir(), "license.pem")
	if err := os.WriteFile(privPath, pemPrivate(priv), 0o600); err != nil {
		t.Fatal(err)
	}

	svc := billing.New(st, billing.Config{
		VariantIDPro:    "111",
		LicensePrivPath: privPath,
	})

	email := "refund@example.com"
	acctID := "acct_" + uuid.NewString()
	_ = st.CreateAccount(t.Context(), store.Account{ID: acctID, Name: "T", Plan: store.PlanPro, MaxSeats: 10, MaxServers: 10})
	_ = st.CreateUser(t.Context(), store.User{
		ID: "usr_" + uuid.NewString(), AccountID: acctID, Email: email, PasswordHash: "x", Name: "R", Role: "admin",
	})

	tok, err := license.Sign(priv, license.Claims{
		Plan: store.PlanPro, MaxSeats: 10, MaxServers: 10, MaxKeys: 20,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   email,
			ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(30 * 24 * time.Hour)),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	orderID := "ord_refund"
	_ = st.InsertLicense(t.Context(), store.License{
		ID: "lic_1", AccountID: acctID, Plan: store.PlanPro, Key: tok,
		LQOrderID:   sql.NullString{String: orderID, Valid: true},
		PurchasedAt: time.Now().UTC(),
		ExpiresAt:   time.Now().UTC().Add(30 * 24 * time.Hour),
	})

	payload, _ := json.Marshal(map[string]any{
		"meta": map[string]any{"event_name": "order_refunded"},
		"data": map[string]any{
			"type": "orders", "id": orderID,
			"attributes": map[string]any{"user_email": email, "customer_id": 1},
		},
	})
	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(payload))
	req.Header.Set("X-Event-Name", "order_refunded")
	svc.Webhook(r, req)
	if r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
	acct, _ := st.GetAccount(t.Context(), acctID)
	if acct.Plan != store.PlanFree {
		t.Fatalf("plan=%s", acct.Plan)
	}
	_, err = st.GetActiveLicense(t.Context(), acctID)
	if err == nil {
		t.Fatal("expected no active license after revoke")
	}
}

func TestSignValidateRoundTrip(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := license.Sign(priv, license.Claims{
		Plan: store.PlanPro, MaxSeats: 10, MaxServers: 10, MaxKeys: 20,
		RegisteredClaims: jwt.RegisteredClaims{Subject: "a@b.c"},
	})
	if err != nil {
		t.Fatal(err)
	}
	c, err := license.ValidateWithKey(tok, &priv.PublicKey)
	if err != nil || c.Plan != store.PlanPro {
		t.Fatalf("err=%v claims=%+v", err, c)
	}
}

func pemPrivate(priv *rsa.PrivateKey) []byte {
	return pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(priv),
	})
}

func writeTempPriv(t *testing.T) string {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	privPath := filepath.Join(t.TempDir(), "license.pem")
	if err := os.WriteFile(privPath, pemPrivate(priv), 0o600); err != nil {
		t.Fatal(err)
	}
	return privPath
}

func mintPaidOrder(t *testing.T, st *store.Store, svc *billing.Service, email, orderID string) string {
	t.Helper()
	acctID := "acct_" + uuid.NewString()
	if err := st.CreateAccount(t.Context(), store.Account{
		ID: acctID, Name: "T", Plan: store.PlanFree, MaxSeats: 3, MaxServers: 3,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateUser(t.Context(), store.User{
		ID: "usr_" + uuid.NewString(), AccountID: acctID, Email: email,
		PasswordHash: "x", Name: "Buyer", Role: "admin",
	}); err != nil {
		t.Fatal(err)
	}
	return mintPaidOrderExisting(t, st, svc, email, acctID, orderID)
}

func mintPaidOrderExisting(t *testing.T, st *store.Store, svc *billing.Service, email, acctID, orderID string) string {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"meta": map[string]any{
			"event_name":  "order_created",
			"custom_data": map[string]any{"account_id": acctID},
		},
		"data": map[string]any{
			"type": "orders",
			"id":   orderID,
			"attributes": map[string]any{
				"customer_id": 42,
				"user_email":  email,
				"currency":    "USD",
				"total":       24900,
				"status":      "paid",
				"first_order_item": map[string]any{
					"variant_id": 111,
					"product_id": 1,
				},
			},
		},
	})
	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(payload))
	req.Header.Set("X-Event-Name", "order_created")
	svc.Webhook(r, req)
	if r.Code != 200 {
		t.Fatalf("mint %s: %d %s", orderID, r.Code, r.Body.String())
	}
	return acctID
}
