// Package billing integrates Lemon Squeezy one-time checkout + webhooks.
//
// LS is the merchant of record (VAT/sales tax/refunds). We never compute tax.
// Real API notes (vs early draft): checkouts (not checkout-sessions), signature
// header X-Signature, events order_created / order_refunded (underscores),
// buyer email = attributes.user_email, variant = first_order_item.variant_id.
package billing

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/devthinker-ai/TokenControlPlane/pkg/license"
	"github.com/devthinker-ai/TokenControlPlane/pkg/session"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

const lsAPIBase = "https://api.lemonsqueezy.com/v1"

// Config holds Lemon Squeezy + license-signing env.
type Config struct {
	SecretKey            string // LQ_SECRET_KEY
	WebhookSecret        string // LQ_WEBHOOK_SECRET
	StoreID              string // LQ_STORE_ID
	VariantIDPro  string // LQ_VARIANT_ID_PRO
	VariantIDTeam string // LQ_VARIANT_ID_TEAM
	ReturnURL     string // LQ_RETURN_URL
	LicensePrivPath string // TOKENCONTROLPLANE_LICENSE_PRIVATE
	// FoundersLimit: first N Pro+Team orders get founders pricing/window.
	// 0 = unlimited, -1 = off. Effective value is set in New unless FoundersLimitSet.
	FoundersLimit    int
	FoundersLimitSet bool // when true, use FoundersLimit as-is (tests / callers)
	Logger           *slog.Logger
}

// Service implements checkout, portal, webhook, and license handlers.
type Service struct {
	store   *store.Store
	cfg     Config
	log     *slog.Logger
	client  *http.Client
	privKey *rsa.PrivateKey // nil when path unset / unloadable
}

func New(st *store.Store, cfg Config) *Service {
	if cfg.SecretKey == "" {
		cfg.SecretKey = os.Getenv("LQ_SECRET_KEY")
	}
	if cfg.WebhookSecret == "" {
		cfg.WebhookSecret = os.Getenv("LQ_WEBHOOK_SECRET")
	}
	if cfg.StoreID == "" {
		cfg.StoreID = os.Getenv("LQ_STORE_ID")
	}
	if cfg.VariantIDPro == "" {
		cfg.VariantIDPro = os.Getenv("LQ_VARIANT_ID_PRO")
	}
	if cfg.VariantIDTeam == "" {
		cfg.VariantIDTeam = os.Getenv("LQ_VARIANT_ID_TEAM")
	}
	if cfg.ReturnURL == "" {
		cfg.ReturnURL = os.Getenv("LQ_RETURN_URL")
		if cfg.ReturnURL == "" {
			base := os.Getenv("GATEWAY_URL")
			if base == "" {
				base = "http://localhost:8080"
			}
			cfg.ReturnURL = strings.TrimRight(base, "/") + "/billing?success=1"
		}
	}
	if cfg.LicensePrivPath == "" {
		cfg.LicensePrivPath = os.Getenv("TOKENCONTROLPLANE_LICENSE_PRIVATE")
	}
	cfg.FoundersLimit = resolveFoundersLimit(cfg)
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	svc := &Service{
		store:  st,
		cfg:    cfg,
		log:    log,
		client: &http.Client{Timeout: 10 * time.Second},
	}
	if cfg.LicensePrivPath != "" {
		pemBytes, err := os.ReadFile(cfg.LicensePrivPath)
		if err != nil {
			log.Error("license private key unreadable", "path", cfg.LicensePrivPath, "err", err)
		} else {
			priv, err := license.ParseRSAPrivateKey(pemBytes)
			if err != nil {
				log.Error("license private key parse failed", "err", err)
			} else {
				svc.privKey = priv
			}
		}
	}
	return svc
}

func (s *Service) Enabled() bool {
	return s.cfg.SecretKey != "" && s.cfg.StoreID != "" && s.cfg.VariantIDPro != ""
}

func (s *Service) variantForPlan(plan string) string {
	switch strings.ToLower(plan) {
	case store.PlanTeam:
		return s.cfg.VariantIDTeam
	default:
		return s.cfg.VariantIDPro
	}
}

func (s *Service) planForVariant(variantID string) (string, bool) {
	v := strings.TrimSpace(variantID)
	if v == "" {
		return "", false
	}
	if s.cfg.VariantIDTeam != "" && v == s.cfg.VariantIDTeam {
		return store.PlanTeam, true
	}
	if s.cfg.VariantIDPro != "" && v == s.cfg.VariantIDPro {
		return store.PlanPro, true
	}
	return "", false
}

// Checkout creates a Lemon Squeezy hosted checkout (?plan=pro|team, default pro).
func (s *Service) Checkout(w http.ResponseWriter, r *http.Request) {
	if !s.Enabled() {
		writeErr(w, http.StatusServiceUnavailable, "billing not configured — set LQ_SECRET_KEY, LQ_STORE_ID, LQ_VARIANT_ID_PRO")
		return
	}
	claims, ok := session.ClaimsFromContext(r.Context())
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	plan := strings.ToLower(r.URL.Query().Get("plan"))
	if plan == "" {
		var body struct {
			Plan string `json:"plan"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		plan = strings.ToLower(body.Plan)
	}
	if plan == "" {
		plan = store.PlanPro
	}
	switch plan {
	case store.PlanPro, store.PlanTeam:
		// ok
	default:
		writeErr(w, http.StatusServiceUnavailable, "unknown plan")
		return
	}
	variantID := s.variantForPlan(plan)
	if variantID == "" {
		writeErr(w, http.StatusServiceUnavailable, "variant not configured for plan")
		return
	}

	payload := map[string]any{
		"data": map[string]any{
			"type": "checkouts",
			"attributes": map[string]any{
				"product_options": map[string]any{
					"redirect_url":     s.cfg.ReturnURL,
					"enabled_variants": []any{jsonNumber(variantID)},
				},
				"checkout_data": map[string]any{
					"email": claims.Email,
					"custom": map[string]any{
						"account_id": claims.AccountID,
					},
				},
			},
			"relationships": map[string]any{
				"store": map[string]any{
					"data": map[string]any{"type": "stores", "id": s.cfg.StoreID},
				},
				"variant": map[string]any{
					"data": map[string]any{"type": "variants", "id": variantID},
				},
			},
		},
	}

	var out struct {
		Data struct {
			Attributes struct {
				URL string `json:"url"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := s.lsJSON(r.Context(), http.MethodPost, "/checkouts", payload, &out); err != nil {
		s.log.Error("ls checkout", "err", err)
		writeErr(w, http.StatusBadGateway, "checkout failed")
		return
	}
	if out.Data.Attributes.URL == "" {
		writeErr(w, http.StatusBadGateway, "checkout missing url")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": out.Data.Attributes.URL})
}

// Portal returns the LS customer portal URL (order history / re-download).
func (s *Service) Portal(w http.ResponseWriter, r *http.Request) {
	if !s.Enabled() {
		writeErr(w, http.StatusServiceUnavailable, "billing not configured — set LQ_SECRET_KEY, LQ_STORE_ID, LQ_VARIANT_ID_PRO")
		return
	}
	claims, ok := session.ClaimsFromContext(r.Context())
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	acct, err := s.store.GetAccount(r.Context(), claims.AccountID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "account not found")
		return
	}
	if !acct.LSCustomerID.Valid || acct.LSCustomerID.String == "" {
		writeErr(w, http.StatusServiceUnavailable, "no purchases yet — use the buy button first")
		return
	}

	var out struct {
		Data struct {
			Attributes struct {
				URLs struct {
					CustomerPortal string `json:"customer_portal"`
				} `json:"urls"`
			} `json:"attributes"`
		} `json:"data"`
	}
	path := "/customers/" + acct.LSCustomerID.String
	if err := s.lsJSON(r.Context(), http.MethodGet, path, nil, &out); err != nil {
		s.log.Error("ls portal", "err", err)
		writeErr(w, http.StatusBadGateway, "portal failed")
		return
	}
	url := out.Data.Attributes.URLs.CustomerPortal
	if url == "" {
		writeErr(w, http.StatusBadGateway, "portal url missing")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": url})
}

// Webhook verifies X-Signature, enforces idempotency, mints/revokes licenses.
func (s *Service) Webhook(w http.ResponseWriter, r *http.Request) {
	const maxBody = 65536
	payload, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "read body")
		return
	}

	if s.cfg.WebhookSecret != "" {
		sig := r.Header.Get("X-Signature")
		if sig == "" {
			writeErr(w, http.StatusBadRequest, "invalid signature")
			return
		}
		mac := hmac.New(sha256.New, []byte(s.cfg.WebhookSecret))
		_, _ = mac.Write(payload)
		digest := hex.EncodeToString(mac.Sum(nil))
		if subtle.ConstantTimeCompare([]byte(digest), []byte(sig)) != 1 {
			s.log.Warn("ls webhook signature mismatch")
			writeErr(w, http.StatusBadRequest, "invalid signature")
			return
		}
	} else {
		s.log.Warn("LQ_WEBHOOK_SECRET unset — accepting webhook without verify (dev only)")
	}

	eventName := r.Header.Get("X-Event-Name")
	var envelope lsWebhook
	if err := json.Unmarshal(payload, &envelope); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if eventName == "" {
		eventName = envelope.Meta.EventName
	}

	eventID := eventName + ":" + envelope.Data.ID
	if eventID == ":" || envelope.Data.ID == "" {
		// Fallback unique-ish id from body hash when LS omits id (shouldn't happen).
		sum := sha256.Sum256(payload)
		eventID = eventName + ":" + hex.EncodeToString(sum[:8])
	}

	first, err := s.store.MarkWebhookEventProcessed(r.Context(), eventID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "idempotency store failed")
		return
	}
	if !first {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"duplicate":true}`))
		return
	}

	if err := s.handleEvent(r.Context(), eventName, &envelope); err != nil {
		s.log.Error("ls webhook handle", "event", eventName, "err", err)
		// Still 200 for mint failures after needs_manual_key — handleEvent returns nil then.
		writeErr(w, http.StatusInternalServerError, "handle failed")
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
}

type lsWebhook struct {
	Meta struct {
		EventName  string         `json:"event_name"`
		CustomData map[string]any `json:"custom_data"`
	} `json:"meta"`
	Data struct {
		Type       string `json:"type"`
		ID         string `json:"id"`
		Attributes struct {
			CustomerID     int64  `json:"customer_id"`
			UserEmail      string `json:"user_email"`
			Currency       string `json:"currency"`
			Total          int64  `json:"total"`
			Status         string `json:"status"`
			Refunded       bool   `json:"refunded"`
			FirstOrderItem *struct {
				VariantID   int64  `json:"variant_id"`
				ProductID   int64  `json:"product_id"`
				ProductName string `json:"product_name"`
			} `json:"first_order_item"`
		} `json:"attributes"`
	} `json:"data"`
}

func (s *Service) handleEvent(ctx context.Context, eventName string, env *lsWebhook) error {
	switch eventName {
	case "order_created", "order_paid":
		return s.handleOrderPaid(ctx, env)
	case "order_refunded", "refund_created", "order.refunded", "refund.completed":
		return s.handleOrderRefunded(ctx, env)
	default:
		s.log.Info("ls webhook ignored", "event", eventName)
		return nil
	}
}

func (s *Service) handleOrderPaid(ctx context.Context, env *lsWebhook) error {
	attrs := env.Data.Attributes
	if attrs.Status != "" && attrs.Status != "paid" {
		s.log.Info("ls order not paid — ignore", "status", attrs.Status, "order", env.Data.ID)
		return nil
	}
	email := strings.TrimSpace(strings.ToLower(attrs.UserEmail))
	if email == "" {
		return fmt.Errorf("missing user_email")
	}
	variantID := ""
	if attrs.FirstOrderItem != nil {
		variantID = strconv.FormatInt(attrs.FirstOrderItem.VariantID, 10)
	}
	plan, ok := s.planForVariant(variantID)
	if !ok {
		// Tests / misconfig: if no variants configured, default pro when empty config.
		if s.cfg.VariantIDPro == "" && s.cfg.VariantIDTeam == "" {
			plan = store.PlanPro
		} else {
			s.log.Warn("ls unknown variant — ignore", "variant", variantID, "order", env.Data.ID)
			return nil
		}
	}

	accountID := ""
	if env.Meta.CustomData != nil {
		if v, ok := env.Meta.CustomData["account_id"]; ok {
			accountID = fmt.Sprint(v)
		}
	}

	var acct *store.Account
	var err error
	if accountID != "" {
		acct, err = s.store.GetAccount(ctx, accountID)
	}
	if acct == nil {
		acct, err = s.store.CreateAccountIfMissing(ctx, email, plan)
	}
	if err != nil || acct == nil {
		return fmt.Errorf("resolve account: %w", err)
	}

	lsCust := strconv.FormatInt(attrs.CustomerID, 10)
	if lsCust == "0" {
		lsCust = ""
	}
	if err := s.store.UpdateAccountPlan(ctx, acct.ID, plan, strPtr(lsCust)); err != nil {
		return err
	}

	purchasedAt := time.Now().UTC()
	// Founders: first N orders (or unlimited/off via TOKENCONTROLPLANE_FOUNDERS_LIMIT).
	// Refunded founders still count — CountFounders includes revoked rows.
	founders := s.shouldMintFounders(ctx)
	p, ok := PlanFor(plan)
	if !ok {
		p = Ladder["pro"]
	}
	windowMonths := p.WindowFor(founders)
	// Approximate months as 30d — fine for an update window.
	expiresAt := purchasedAt.Add(windowMonthsApprox(windowMonths))

	if s.privKey == nil {
		s.log.Error("order.paid but TOKENCONTROLPLANE_LICENSE_PRIVATE unset — needs_manual_key",
			"account", acct.ID, "order", env.Data.ID)
		_ = s.store.SetAccountNeedsManualKey(ctx, acct.ID, true)
		_ = s.store.InsertActivityEvent(ctx, acct.ID, store.ActivityPlanChanged, plan,
			"License purchased via Lemon Squeezy (awaiting key mint)", map[string]any{
				"plan": plan, "order_id": env.Data.ID, "needs_manual_key": true,
			})
		return nil // 200 — avoid LS retry storm
	}

	caps := license.CapsForPlan(plan)
	tok, err := license.Sign(s.privKey, license.Claims{
		Plan:       plan,
		MaxSeats:   caps.MaxSeats,
		MaxServers: caps.MaxServers,
		MaxKeys:    caps.MaxKeys,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   email,
			IssuedAt:  jwt.NewNumericDate(purchasedAt),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	})
	if err != nil {
		_ = s.store.SetAccountNeedsManualKey(ctx, acct.ID, true)
		return fmt.Errorf("sign license: %w", err)
	}

	// Renewal invariant: each order.paid mints a NEW row with expires_at =
	// purchased_at + window. The window is NOT added onto a prior expiry.
	lic := store.License{
		ID:           "lic_" + uuid.NewString(),
		AccountID:    acct.ID,
		Plan:         plan,
		Key:          tok,
		LQOrderID:    sql.NullString{String: env.Data.ID, Valid: env.Data.ID != ""},
		PurchasedAt:  purchasedAt,
		ExpiresAt:    expiresAt,
		WindowMonths: windowMonths,
		IsFounders:   founders,
	}
	if err := s.store.InsertLicense(ctx, lic); err != nil {
		return err
	}
	_ = s.store.SetAccountNeedsManualKey(ctx, acct.ID, false)
	_ = s.store.InsertActivityEvent(ctx, acct.ID, store.ActivityPlanChanged, plan,
		"License purchased via Lemon Squeezy", map[string]any{
			"plan": plan, "order_id": env.Data.ID, "total": attrs.Total, "currency": attrs.Currency,
			"window_months": windowMonths, "is_founders": founders,
		})
	return nil
}

func (s *Service) handleOrderRefunded(ctx context.Context, env *lsWebhook) error {
	orderID := env.Data.ID
	lic, err := s.store.GetLicenseByOrderID(ctx, orderID)
	if err != nil {
		s.log.Info("ls refund — no license for order", "order", orderID)
		email := strings.TrimSpace(strings.ToLower(env.Data.Attributes.UserEmail))
		if email != "" {
			if acct, err := s.store.GetAccountByEmail(ctx, email); err == nil {
				_ = s.store.UpdateAccountPlan(ctx, acct.ID, store.PlanFree, nil)
				_ = s.store.InsertActivityEvent(ctx, acct.ID, store.ActivityPlanChanged, "free",
					"Plan downgraded after refund", map[string]any{"order_id": orderID})
			}
		}
		return nil
	}
	_ = s.store.RevokeLicense(ctx, lic.ID)
	if err := s.store.UpdateAccountPlan(ctx, lic.AccountID, store.PlanFree, nil); err != nil {
		return err
	}
	_ = s.store.InsertActivityEvent(ctx, lic.AccountID, store.ActivityPlanChanged, "free",
		"License revoked (refund)", map[string]any{"order_id": orderID})
	return nil
}

// MeLicense GET /api/v1/licenses/me
func (s *Service) MeLicense(w http.ResponseWriter, r *http.Request) {
	claims, ok := session.ClaimsFromContext(r.Context())
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	lic, err := s.store.GetActiveLicense(r.Context(), claims.AccountID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "no license")
		return
	}
	acct, _ := s.store.GetAccount(r.Context(), claims.AccountID)
	needs := false
	if acct != nil && acct.NeedsManualKey.Valid {
		needs = acct.NeedsManualKey.Bool
	}
	now := time.Now().UTC()
	expired := now.After(lic.ExpiresAt)
	days := int(lic.ExpiresAt.Sub(now).Hours() / 24)
	if expired {
		days = int(now.Sub(lic.ExpiresAt).Hours() / 24)
		if days > 0 {
			days = -days
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":                 lic.ID,
		"plan":               lic.Plan,
		"key":                lic.Key,
		"purchased_at":       lic.PurchasedAt.UTC().Format(time.RFC3339),
		"expires_at":         lic.ExpiresAt.UTC().Format(time.RFC3339),
		"revoked":            lic.RevokedAt.Valid,
		"needs_manual_key":   needs,
		"expired":            expired,
		"window_months":      lic.WindowMonths,
		"is_founders":        lic.IsFounders,
		"days_to_expiry":     days,
		"ever_works_forever": true, // literal: expiry gates updates only, never function
	})
}

// Pricing GET /api/v1/pricing — public ladder + founders availability (no auth).
func (s *Service) Pricing(w http.ResponseWriter, r *http.Request) {
	count, err := s.store.CountFounders(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "count founders")
		return
	}
	avail, remaining := s.foundersAvailability(count)
	plans := make([]map[string]any, 0, 2)
	for _, key := range []string{"pro", "team"} {
		p := Ladder[key]
		caps := p.CapsFor()
		plans = append(plans, map[string]any{
			"key":             p.Key,
			"label":           p.Label,
			"price":           p.PriceUSD,
			"founders_price":  p.FoundersUSD,
			"window_months":   p.WindowMonths,
			"founders_window": p.FoundersWindow,
			"caps": map[string]any{
				"max_servers":    caps.MaxServers,
				"max_seats":      caps.MaxSeats,
				"max_keys":       caps.MaxKeys,
				"monthly_tokens": caps.MonthlyTokens,
				"tool_policy":    caps.ToolPolicy,
			},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"plans":               plans,
		"founders_limit":      s.cfg.FoundersLimit,
		"founders_remaining":  remaining,
		"founders_available":  avail,
		"founders_deadline":   nil, // order-count based; date cutoff unused in v1
		"ever_works_forever":  true,
		"renewal_note":        RenewalNote,
	})
}

func (s *Service) shouldMintFounders(ctx context.Context) bool {
	limit := s.cfg.FoundersLimit
	if limit < 0 {
		return false
	}
	if limit == 0 {
		return true
	}
	n, err := s.store.CountFounders(ctx)
	if err != nil {
		s.log.Warn("count founders failed — minting standard", "err", err)
		return false
	}
	return n < limit
}

func (s *Service) foundersAvailability(count int) (available bool, remaining int) {
	limit := s.cfg.FoundersLimit
	if limit < 0 {
		return false, 0
	}
	if limit == 0 {
		return true, -1 // unlimited
	}
	remaining = limit - count
	if remaining < 0 {
		remaining = 0
	}
	return remaining > 0, remaining
}

// resolveFoundersLimit applies Config override, then env, then default 100.
func resolveFoundersLimit(cfg Config) int {
	if cfg.FoundersLimitSet {
		return cfg.FoundersLimit
	}
	raw, ok := os.LookupEnv("TOKENCONTROLPLANE_FOUNDERS_LIMIT")
	if !ok || strings.TrimSpace(raw) == "" {
		return DefaultFoundersLimit
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return DefaultFoundersLimit
	}
	return n
}

// windowMonthsApprox treats one month as 30 days — acceptable for update windows.
func windowMonthsApprox(months int) time.Duration {
	if months <= 0 {
		months = 12
	}
	return time.Duration(months) * 30 * 24 * time.Hour
}

// ReissueLicense POST /api/v1/licenses/reissue  body: {"email":"..."} optional subject override
func (s *Service) ReissueLicense(w http.ResponseWriter, r *http.Request) {
	claims, ok := session.ClaimsFromContext(r.Context())
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if s.privKey == nil {
		writeErr(w, http.StatusServiceUnavailable, "license private key not configured")
		return
	}
	var body struct {
		Email string `json:"email"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	email := strings.TrimSpace(body.Email)
	if email == "" {
		email = claims.Email
	}

	lic, err := s.store.GetActiveLicense(r.Context(), claims.AccountID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "no license to reissue")
		return
	}

	caps := license.CapsForPlan(lic.Plan)
	tok, err := license.Sign(s.privKey, license.Claims{
		Plan:       lic.Plan,
		MaxSeats:   caps.MaxSeats,
		MaxServers: caps.MaxServers,
		MaxKeys:    caps.MaxKeys,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   email,
			IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
			ExpiresAt: jwt.NewNumericDate(lic.ExpiresAt),
		},
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "sign failed")
		return
	}
	if err := s.store.UpdateLicenseKey(r.Context(), lic.ID, tok); err != nil {
		writeErr(w, http.StatusInternalServerError, "store failed")
		return
	}
	_ = s.store.SetAccountNeedsManualKey(r.Context(), claims.AccountID, false)
	writeJSON(w, http.StatusOK, map[string]string{"key": tok})
}

func (s *Service) lsJSON(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, lsAPIBase+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.SecretKey)
	req.Header.Set("Accept", "application/vnd.api+json")
	if body != nil {
		req.Header.Set("Content-Type", "application/vnd.api+json")
	}
	res, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		s.log.Error("ls api error", "status", res.StatusCode, "body", string(respBody), "path", path)
		return fmt.Errorf("ls api %d", res.StatusCode)
	}
	if out == nil || len(respBody) == 0 {
		return nil
	}
	return json.Unmarshal(respBody, out)
}

func jsonNumber(id string) any {
	if n, err := strconv.ParseInt(id, 10, 64); err == nil {
		return n
	}
	return id
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(`{"error":"` + msg + `"}`))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
