package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/devthinker-ai/TokenControlPlane/pkg/session"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

// 2FA endpoints (dashboard humans only — never machine keys).
// Setup persists secret with enabled=0 pending confirm; confirm enables + issues
// recovery codes once. Re-confirm while already enabled returns 200 without
// re-issuing codes (invalidates nothing). DELETE is idempotent 204.

func (h *handlers) setup2FA(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	secret, err := session.GenerateSecret()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "secret generation failed")
		return
	}
	// Persist pending enrollment (enabled=0, empty recovery). Confirm flips enabled.
	// Secret is base32 (not an auth credential by itself) — never log it.
	if err := h.d.Store.UpsertTOTP(r.Context(), c.UserID, secret, false, nil); err != nil {
		writeErr(w, http.StatusInternalServerError, "setup failed")
		return
	}
	uri := session.ProvisioningURI(secret, c.Email, "")
	qr, err := session.QRDataURL(uri)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "qr failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"secret":           secret,
		"provisioning_uri": uri,
		"qr":               qr,
	})
}

func (h *handlers) confirm2FA(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	t, err := h.d.Store.GetTOTP(r.Context(), c.UserID)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid code")
		return
	}
	// Already enrolled: accept a valid code but do not re-issue recovery codes.
	if t.Enabled {
		ok, _ := session.Verify(t.Secret, body.Code, h.clock())
		if !ok {
			writeErr(w, http.StatusUnauthorized, "invalid code")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"recovery_codes":  []string{},
			"already_enabled": true,
		})
		return
	}
	ok, _ := session.Verify(t.Secret, body.Code, h.clock())
	if !ok {
		writeErr(w, http.StatusUnauthorized, "invalid code")
		return
	}
	plain, err := session.GenerateRecoveryCodes()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "recovery generation failed")
		return
	}
	hashes := make([]string, len(plain))
	for i, code := range plain {
		hashes[i] = session.HashRecoveryCode(code)
	}
	if err := h.d.Store.SetTOTPConfirmed(r.Context(), c.UserID, hashes); err != nil {
		writeErr(w, http.StatusInternalServerError, "confirm failed")
		return
	}
	_ = h.d.Store.InsertActivityEvent(r.Context(), c.AccountID, store.Activity2FAEnabled, c.Email,
		"2FA enabled", map[string]any{"user_id": c.UserID})
	writeJSON(w, http.StatusOK, map[string]any{"recovery_codes": plain})
}

func (h *handlers) delete2FA(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	enabled, _ := h.d.Store.TOTPEnabled(r.Context(), c.UserID)
	_ = h.d.Store.DeleteTOTP(r.Context(), c.UserID)
	if enabled {
		_ = h.d.Store.InsertActivityEvent(r.Context(), c.AccountID, store.Activity2FARemoved, c.Email,
			"2FA removed", map[string]any{"user_id": c.UserID})
	}
	// Idempotent: enrolled or not → 204 (disabled is not an error state).
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) get2FA(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	enabled, confirmedAt, remaining := h.totpFields(r, c.UserID)
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":            enabled,
		"confirmed_at":       confirmedAt,
		"recovery_remaining": remaining,
	})
}

func (h *handlers) loginMFA(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MFAToken string `json:"mfa_token"`
		Code     string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	now := h.clock()
	claims, err := h.d.Sessions.ParseMFA(body.MFAToken, now)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "mfa token expired or already used")
		return
	}
	if h.mfa.IsUsed(claims.ID) {
		writeErr(w, http.StatusUnauthorized, "mfa token expired or already used")
		return
	}
	exp := claims.ExpiresAt.Time
	allowed, _ := h.mfa.CheckAndCountAttempt(claims.ID, exp)
	if !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(session.MFARetryAfterSec))
		writeErr(w, http.StatusTooManyRequests, "too many attempts")
		return
	}

	u, err := h.d.Store.GetUser(r.Context(), claims.UserID)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "mfa token expired or already used")
		return
	}
	t, err := h.d.Store.GetTOTP(r.Context(), u.ID)
	if err != nil || !t.Enabled {
		writeErr(w, http.StatusUnauthorized, "invalid code")
		return
	}

	codeOK := false
	if session.LooksLikeRecoveryCode(body.Code) {
		consumed, cerr := h.d.Store.ConsumeRecoveryCode(r.Context(), u.ID, session.HashRecoveryCode(body.Code))
		if cerr != nil {
			writeErr(w, http.StatusInternalServerError, "recovery failed")
			return
		}
		codeOK = consumed
	} else {
		ok, _ := session.Verify(t.Secret, body.Code, now)
		codeOK = ok
	}
	if !codeOK {
		writeErr(w, http.StatusUnauthorized, "invalid code")
		return
	}
	if !h.mfa.MarkUsed(claims.ID, exp) {
		writeErr(w, http.StatusUnauthorized, "mfa token expired or already used")
		return
	}

	acct, err := h.d.Store.GetAccount(r.Context(), u.AccountID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "account missing")
		return
	}
	token, err := h.d.Sessions.Issue(u.ID, u.AccountID, u.Email, u.Role)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "token issue failed")
		return
	}
	seatsUsed, _ := h.d.Store.CountUsers(r.Context(), u.AccountID)
	writeJSON(w, http.StatusOK, map[string]any{
		"token": token,
		"user":  h.userDTO(r, u.ID, u.AccountID, u.Email, u.Name, u.Role, acct.Plan, acct.MaxSeats, acct.MaxServers, seatsUsed),
	})
}

func (h *handlers) clock() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now().UTC()
}

func (h *handlers) totpFields(r *http.Request, userID string) (enabled bool, confirmedAt any, recoveryRemaining int) {
	t, err := h.d.Store.GetTOTP(r.Context(), userID)
	if err == sql.ErrNoRows || err != nil {
		return false, nil, 0
	}
	if !t.Enabled {
		return false, nil, 0
	}
	var conf any
	if t.ConfirmedAt.Valid {
		conf = t.ConfirmedAt.Time.UTC().Format(time.RFC3339)
	}
	return true, conf, len(t.RecoveryCodes)
}
