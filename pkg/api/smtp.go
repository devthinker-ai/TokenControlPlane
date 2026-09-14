package api

import (
	"context"
	"encoding/json"
	"net/http"
	netmail "net/mail"
	"strings"

	"github.com/devthinker-ai/TokenControlPlane/pkg/mail"
	"github.com/devthinker-ai/TokenControlPlane/pkg/session"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

// smtpDTO is the API shape — never includes the password secret.
type smtpDTO struct {
	Enabled     bool   `json:"enabled"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	From        string `json:"from"`
	FromName    string `json:"from_name"`
	TLSMode     string `json:"tls_mode"`
	HasPassword bool   `json:"has_password"`
}

func (h *handlers) getSMTP(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	cfg, err := h.loadSMTPConfig(r.Context(), c.AccountID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load failed")
		return
	}
	writeJSON(w, http.StatusOK, smtpToDTO(cfg))
}

func (h *handlers) putSMTP(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	var body struct {
		Enabled  *bool  `json:"enabled"`
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Password string `json:"password"` // empty = keep existing
		From     string `json:"from"`
		FromName string `json:"from_name"`
		TLSMode  string `json:"tls_mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	existing, err := h.loadSMTPConfig(r.Context(), c.AccountID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load failed")
		return
	}
	cfg := existing
	if body.Enabled != nil {
		cfg.Enabled = *body.Enabled
	}
	cfg.Host = strings.TrimSpace(body.Host)
	if body.Port > 0 {
		cfg.Port = body.Port
	}
	cfg.Username = strings.TrimSpace(body.Username)
	cfg.From = strings.TrimSpace(body.From)
	cfg.FromName = strings.TrimSpace(body.FromName)
	mode := strings.ToLower(strings.TrimSpace(body.TLSMode))
	if mode == "" {
		mode = mail.TLSModeStartTLS
	}
	if mode != mail.TLSModeStartTLS && mode != mail.TLSModeTLS && mode != mail.TLSModePlain {
		writeErr(w, http.StatusBadRequest, "tls_mode must be starttls, tls, or plain")
		return
	}
	cfg.TLSMode = mode
	// WHY: password is write-only — empty string means "keep stored secret".
	if body.Password != "" {
		cfg.Password = body.Password
	}
	if cfg.Port <= 0 {
		switch cfg.TLSMode {
		case mail.TLSModeTLS:
			cfg.Port = 465
		case mail.TLSModePlain:
			cfg.Port = 25
		default:
			cfg.Port = 587
		}
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "encode failed")
		return
	}
	if err := h.d.Store.SetAccountSetting(r.Context(), c.AccountID, store.SettingSMTPConfig, string(raw)); err != nil {
		writeErr(w, http.StatusInternalServerError, "save failed")
		return
	}
	writeJSON(w, http.StatusOK, smtpToDTO(cfg))
}

func (h *handlers) testSMTP(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	var body struct {
		To string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	to := strings.TrimSpace(body.To)
	if _, err := parseMailAddress(to); err != nil {
		writeErr(w, http.StatusBadRequest, "valid to address required")
		return
	}
	cfg, err := h.loadSMTPConfig(r.Context(), c.AccountID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load failed")
		return
	}
	if !cfg.Enabled || cfg.Host == "" {
		writeErr(w, http.StatusConflict, "SMTP not configured")
		return
	}
	sender := &mail.Sender{Cfg: cfg}
	html := `<p style="font-family:sans-serif;">TokenControlPlane SMTP test — if you received this, outgoing mail works.</p>`
	if err := sender.Send(to, "TokenControlPlane — SMTP test", html); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = h.d.Store.InsertActivityEvent(r.Context(), c.AccountID, store.ActivitySMTPTestSent, to,
		"SMTP test email sent", map[string]any{"to": to})
	writeJSON(w, http.StatusOK, map[string]any{"sent": true})
}

func (h *handlers) getTemplates(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	invite, reset := h.loadTemplates(r.Context(), c.AccountID)
	writeJSON(w, http.StatusOK, map[string]string{
		"invite": invite,
		"reset":  reset,
	})
}

func (h *handlers) putTemplates(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	var body struct {
		Invite string `json:"invite"`
		Reset  string `json:"reset"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	// Surface parse errors on save so admins fix typos before send-time fallback.
	if strings.TrimSpace(body.Invite) != "" {
		if err := mail.ValidateTemplate("invite", body.Invite); err != nil {
			writeErr(w, http.StatusBadRequest, "invite template: "+err.Error())
			return
		}
	}
	if strings.TrimSpace(body.Reset) != "" {
		if err := mail.ValidateTemplate("reset", body.Reset); err != nil {
			writeErr(w, http.StatusBadRequest, "reset template: "+err.Error())
			return
		}
	}
	if err := h.d.Store.SetAccountSetting(r.Context(), c.AccountID, store.SettingTplInvite, body.Invite); err != nil {
		writeErr(w, http.StatusInternalServerError, "save failed")
		return
	}
	if err := h.d.Store.SetAccountSetting(r.Context(), c.AccountID, store.SettingTplReset, body.Reset); err != nil {
		writeErr(w, http.StatusInternalServerError, "save failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"invite": body.Invite,
		"reset":  body.Reset,
	})
}

func smtpToDTO(cfg mail.Config) smtpDTO {
	port := cfg.Port
	if port == 0 {
		port = 587
	}
	mode := cfg.TLSMode
	if mode == "" {
		mode = mail.TLSModeStartTLS
	}
	return smtpDTO{
		Enabled:     cfg.Enabled,
		Host:        cfg.Host,
		Port:        port,
		Username:    cfg.Username,
		From:        cfg.From,
		FromName:    cfg.FromName,
		TLSMode:     mode,
		HasPassword: cfg.Password != "",
	}
}

func (h *handlers) loadSMTPConfig(ctx context.Context, accountID string) (mail.Config, error) {
	raw, ok, err := h.d.Store.GetAccountSetting(ctx, accountID, store.SettingSMTPConfig)
	if err != nil {
		return mail.Config{}, err
	}
	if !ok || raw == "" {
		return mail.Config{TLSMode: mail.TLSModeStartTLS, Port: 587}, nil
	}
	var cfg mail.Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return mail.Config{}, err
	}
	if cfg.TLSMode == "" {
		cfg.TLSMode = mail.TLSModeStartTLS
	}
	return cfg, nil
}

func (h *handlers) loadTemplates(ctx context.Context, accountID string) (invite, reset string) {
	if v, ok, _ := h.d.Store.GetAccountSetting(ctx, accountID, store.SettingTplInvite); ok {
		invite = v
	}
	if invite == "" {
		invite = mail.DefaultInviteTemplate
	}
	if v, ok, _ := h.d.Store.GetAccountSetting(ctx, accountID, store.SettingTplReset); ok {
		reset = v
	}
	if reset == "" {
		reset = mail.DefaultResetTemplate
	}
	return invite, reset
}

func (h *handlers) smtpEnabled(ctx context.Context, accountID string) (mail.Config, bool) {
	cfg, err := h.loadSMTPConfig(ctx, accountID)
	if err != nil || !cfg.Enabled || cfg.Host == "" {
		return cfg, false
	}
	return cfg, true
}

// parseMailAddress wraps net/mail for invite/reset validation.
func parseMailAddress(s string) (*netmail.Address, error) {
	return netmail.ParseAddress(s)
}
