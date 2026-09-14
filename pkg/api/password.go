package api

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/devthinker-ai/TokenControlPlane/pkg/mail"
	"github.com/devthinker-ai/TokenControlPlane/pkg/session"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

const (
	passwordResetTTL     = time.Hour
	passwordResetMaxRate = 5
	passwordResetWindow  = 10 * time.Minute
	genPasswordLen       = 12
)

func (h *handlers) requestPasswordReset(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	email := strings.TrimSpace(strings.ToLower(body.Email))
	// Always 200 — never leak whether the email is registered.
	defer writeJSON(w, http.StatusOK, map[string]any{"ok": true})

	if email == "" {
		return
	}
	u, err := h.d.Store.GetUserByEmail(r.Context(), email)
	if err != nil {
		return
	}
	cfg, ok := h.smtpEnabled(r.Context(), u.AccountID)
	if !ok {
		return
	}
	n, err := h.d.Store.CountPasswordResetsSince(r.Context(), u.ID, time.Now().UTC().Add(-passwordResetWindow))
	if err != nil || n >= passwordResetMaxRate {
		return
	}
	code, err := generateInviteCode() // same unambiguous alphabet
	if err != nil {
		return
	}
	now := time.Now().UTC()
	pr := store.PasswordReset{
		ID:        "pwr_" + uuid.NewString(),
		AccountID: u.AccountID,
		UserID:    u.ID,
		CodeHash:  store.HashInviteCode(code),
		ExpiresAt: now.Add(passwordResetTTL),
		CreatedAt: now,
	}
	if err := h.d.Store.CreatePasswordReset(r.Context(), pr); err != nil {
		return
	}
	base := strings.TrimRight(h.d.GatewayURL, "/")
	resetURL := base + "/reset-password?token=" + code
	_, storedReset := h.loadTemplatesRaw(r.Context(), u.AccountID)
	html, err := mail.RenderReset(storedReset, mail.ResetData{
		UserName:   u.Name,
		ResetURL:   resetURL,
		ExpiresIn:  "1 hour",
		GatewayURL: base,
	})
	if err != nil {
		return
	}
	_ = (&mail.Sender{Cfg: cfg}).Send(u.Email, "Reset your TokenControlPlane password", html)
	_ = h.d.Store.InsertActivityEvent(r.Context(), u.AccountID, store.ActivityPasswordResetSelf, u.Email,
		"Password reset email requested", map[string]any{"user_id": u.ID})
}

func (h *handlers) confirmPasswordReset(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token           string `json:"token"`
		NewPassword     string `json:"new_password"`
		ConfirmPassword string `json:"confirm_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	body.Token = strings.TrimSpace(body.Token)
	if body.Token == "" || len(body.NewPassword) < 8 {
		writeErr(w, http.StatusBadRequest, "token and password (8+) required")
		return
	}
	if body.NewPassword != body.ConfirmPassword {
		writeErr(w, http.StatusBadRequest, "passwords do not match")
		return
	}
	pr, err := h.d.Store.GetPasswordResetByHash(r.Context(), store.HashInviteCode(body.Token))
	if err != nil || pr.ConsumedAt.Valid || time.Now().UTC().After(pr.ExpiresAt) {
		writeErr(w, http.StatusBadRequest, "link invalid or expired")
		return
	}
	hash, err := session.HashPassword(body.NewPassword)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "hash failed")
		return
	}
	if err := h.d.Store.SetUserPassword(r.Context(), pr.UserID, hash); err != nil {
		writeErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	if err := h.d.Store.ConsumePasswordReset(r.Context(), pr.ID); err != nil {
		writeErr(w, http.StatusBadRequest, "link invalid or expired")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *handlers) changeMyPassword(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	var body struct {
		Current string `json:"current"`
		New     string `json:"new"`
		Confirm string `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if len(body.New) < 8 {
		writeErr(w, http.StatusBadRequest, "password (8+) required")
		return
	}
	if body.New != body.Confirm {
		writeErr(w, http.StatusBadRequest, "passwords do not match")
		return
	}
	u, err := h.d.Store.GetUser(r.Context(), c.UserID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	if !session.CheckPassword(u.PasswordHash, body.Current) {
		writeErr(w, http.StatusUnauthorized, "current password incorrect")
		return
	}
	hash, err := session.HashPassword(body.New)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "hash failed")
		return
	}
	if err := h.d.Store.SetUserPassword(r.Context(), u.ID, hash); err != nil {
		writeErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *handlers) adminResetPassword(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")
	var body struct {
		NewPassword string `json:"new_password"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body) // empty body ok

	target, err := h.d.Store.GetUser(r.Context(), id)
	if err != nil || target.AccountID != c.AccountID {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}

	generated := false
	pwd := body.NewPassword
	if pwd == "" {
		var err error
		pwd, err = generateStrongPassword(genPasswordLen)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "codegen failed")
			return
		}
		generated = true
	} else if len(pwd) < 8 {
		writeErr(w, http.StatusBadRequest, "password (8+) required")
		return
	}

	hash, err := session.HashPassword(pwd)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "hash failed")
		return
	}
	if err := h.d.Store.SetUserPassword(r.Context(), target.ID, hash); err != nil {
		writeErr(w, http.StatusInternalServerError, "update failed")
		return
	}

	emailSent := false
	if cfg, ok := h.smtpEnabled(r.Context(), c.AccountID); ok {
		base := strings.TrimRight(h.d.GatewayURL, "/")
		subj := "Your TokenControlPlane password was reset"
		var bodyHTML string
		if generated {
			bodyHTML = `<p style="font-family:sans-serif;">An admin reset your password. Sign in at <a href="` +
				base + `/login">` + base + `/login</a> with the temporary password they share with you.</p>`
		} else {
			bodyHTML = `<p style="font-family:sans-serif;">An admin set a new password for your TokenControlPlane account. If you did not expect this, contact your admin.</p>`
		}
		if err := (&mail.Sender{Cfg: cfg}).Send(target.Email, subj, bodyHTML); err == nil {
			emailSent = true
		}
	}

	_ = h.d.Store.InsertActivityEvent(r.Context(), c.AccountID, store.ActivityPasswordResetAdmin, target.Name,
		"Admin reset password for "+target.Name, map[string]any{"user_id": target.ID, "email_sent": emailSent})

	resp := map[string]any{"ok": true, "email_sent": emailSent}
	// Documented v1: echo generated password once so admin can copy it.
	if generated {
		resp["new_password"] = pwd
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *handlers) loadTemplatesRaw(ctx context.Context, accountID string) (invite, reset string) {
	if v, ok, _ := h.d.Store.GetAccountSetting(ctx, accountID, store.SettingTplInvite); ok {
		invite = v
	}
	if v, ok, _ := h.d.Store.GetAccountSetting(ctx, accountID, store.SettingTplReset); ok {
		reset = v
	}
	return invite, reset
}

func generateStrongPassword(n int) (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz23456789!@#$%"
	b := make([]byte, n)
	max := big.NewInt(int64(len(alphabet)))
	for i := range b {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b[i] = alphabet[v.Int64()]
	}
	return string(b), nil
}
