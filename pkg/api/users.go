package api

import (
	"context"
	"crypto/rand"
	"database/sql"
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

// Invite alphabet: no 0/O/1/I — unambiguous when shared verbally / QR.
const inviteAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789abcdefghjkmnpqrstuvwxyz"

const (
	inviteTTL       = 7 * 24 * time.Hour
	maxPendingInvites = 5
	inviteCodeLen   = 16
)

func (h *handlers) listUsers(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	users, err := h.d.Store.ListUsersByAccount(r.Context(), c.AccountID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list failed")
		return
	}
	acct, err := h.d.Store.GetAccount(r.Context(), c.AccountID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "account not found")
		return
	}
	caps := h.effectiveCaps(acct)
	out := make([]map[string]any, 0, len(users))
	for _, u := range users {
		totpOn, _ := h.d.Store.TOTPEnabled(r.Context(), u.ID)
		out = append(out, map[string]any{
			"id": u.ID, "email": u.Email, "name": u.Name, "role": u.Role,
			"created_at":   u.CreatedAt.UTC().Format(time.RFC3339),
			"totp_enabled": totpOn,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"users":      out,
		"seats_used": len(users),
		"seats_max":  caps.MaxSeats,
	})
}

func (h *handlers) createInvite(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	code, inv, errMsg, codeStatus := h.newInvite(r.Context(), c.AccountID, c.UserID)
	if errMsg != "" {
		writeErr(w, codeStatus, errMsg)
		return
	}
	writeJSON(w, http.StatusCreated, inviteResponse(code, inv, h.d.GatewayURL))
}

// newInvite creates a pending invite; shared by link and email paths.
func (h *handlers) newInvite(ctx context.Context, accountID, createdBy string) (code string, inv store.Invite, errMsg string, status int) {
	pending, err := h.d.Store.CountPendingInvites(ctx, accountID)
	if err != nil {
		return "", store.Invite{}, "count failed", http.StatusInternalServerError
	}
	if pending >= maxPendingInvites {
		return "", store.Invite{}, "too many pending invites (max 5)", http.StatusConflict
	}
	code, err = generateInviteCode()
	if err != nil {
		return "", store.Invite{}, "codegen failed", http.StatusInternalServerError
	}
	now := time.Now().UTC()
	inv = store.Invite{
		ID:        "inv_" + uuid.NewString(),
		AccountID: accountID,
		CodeHash:  store.HashInviteCode(code),
		Role:      "member", // v1: member-only invites
		CreatedBy: createdBy,
		ExpiresAt: now.Add(inviteTTL),
		CreatedAt: now,
	}
	if err := h.d.Store.InsertInvite(ctx, inv); err != nil {
		return "", store.Invite{}, "create failed", http.StatusInternalServerError
	}
	return code, inv, "", 0
}

func inviteResponse(code string, inv store.Invite, gatewayURL string) map[string]any {
	path := "/register?invite=" + code
	resp := map[string]any{
		"code":       code,
		"path":       path,
		"expires_at": inv.ExpiresAt.Format(time.RFC3339),
	}
	if gatewayURL != "" {
		resp["url"] = strings.TrimRight(gatewayURL, "/") + path
	}
	return resp
}

func (h *handlers) createEmailInvite(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	var body struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	email := strings.TrimSpace(strings.ToLower(body.Email))
	if _, err := parseMailAddress(email); err != nil {
		writeErr(w, http.StatusBadRequest, "valid email required")
		return
	}
	cfg, ok := h.smtpEnabled(r.Context(), c.AccountID)
	if !ok {
		writeErr(w, http.StatusConflict, "SMTP not configured")
		return
	}

	code, inv, errMsg, codeStatus := h.newInvite(r.Context(), c.AccountID, c.UserID)
	if errMsg != "" {
		writeErr(w, codeStatus, errMsg)
		return
	}
	invResp := inviteResponse(code, inv, h.d.GatewayURL)
	inviteURL, _ := invResp["url"].(string)
	if inviteURL == "" {
		inviteURL = strings.TrimRight(h.d.GatewayURL, "/") + invResp["path"].(string)
	}

	acct, _ := h.d.Store.GetAccount(r.Context(), c.AccountID)
	inviter, _ := h.d.Store.GetUser(r.Context(), c.UserID)
	accountName := "your team"
	if acct != nil {
		accountName = acct.Name
	}
	inviterName := "An admin"
	if inviter != nil {
		inviterName = inviter.Name
	}
	storedInvite, _ := h.loadTemplatesRaw(r.Context(), c.AccountID)
	html, err := mail.RenderInvite(storedInvite, mail.InviteData{
		AccountName: accountName,
		InviterName: inviterName,
		InviteURL:   inviteURL,
		ExpiresAt:   inv.ExpiresAt.UTC().Format(time.RFC1123),
		GatewayURL:  strings.TrimRight(h.d.GatewayURL, "/"),
	})
	emailSent := false
	emailErr := ""
	if err != nil {
		emailErr = err.Error()
	} else if sendErr := (&mail.Sender{Cfg: cfg}).Send(email, "You're invited to "+accountName, html); sendErr != nil {
		emailErr = sendErr.Error()
	} else {
		emailSent = true
	}

	_ = h.d.Store.InsertEmailInvite(r.Context(), store.EmailInvite{
		ID:        "einv_" + uuid.NewString(),
		AccountID: c.AccountID,
		Email:     email,
		InviteID:  inv.ID,
		CreatedBy: c.UserID,
	})
	_ = h.d.Store.InsertActivityEvent(r.Context(), c.AccountID, store.ActivityInviteEmailSent, email,
		"Invite email sent to "+email, map[string]any{
			"invite_id": inv.ID, "email_sent": emailSent, "email_error": emailErr,
		})

	writeJSON(w, http.StatusCreated, map[string]any{
		"invite":      invResp,
		"email_sent":  emailSent,
		"email_error": emailErr,
	})
}

func (h *handlers) listEmailInvites(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	list, err := h.d.Store.ListEmailInvites(r.Context(), c.AccountID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list failed")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, ei := range list {
		out = append(out, map[string]any{
			"id":         ei.ID,
			"email":      ei.Email,
			"invite_id":  ei.InviteID,
			"sent_at":    ei.SentAt.UTC().Format(time.RFC3339),
			"created_by": ei.CreatedBy,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handlers) listInvites(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	list, err := h.d.Store.ListPendingInvites(r.Context(), c.AccountID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list failed")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, inv := range list {
		out = append(out, map[string]any{
			"id":              inv.ID,
			"created_by_name": inv.CreatedByName,
			"role":            inv.Role,
			"expires_at":      inv.ExpiresAt.UTC().Format(time.RFC3339),
			"created_at":      inv.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handlers) deleteInvite(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if err := h.d.Store.DeleteInvite(r.Context(), c.AccountID, id); err != nil {
		writeErr(w, http.StatusNotFound, "invite not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// publicInviteLookup GET /invites/public/{code} — auth-free; returns only account_name + role.
func (h *handlers) publicInviteLookup(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	inv, err := h.d.Store.GetInviteByCodeHash(r.Context(), store.HashInviteCode(code))
	if err != nil || !inviteJoinable(inv) {
		writeErr(w, http.StatusNotFound, "invalid or expired invite")
		return
	}
	acct, err := h.d.Store.GetAccount(r.Context(), inv.AccountID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "invalid or expired invite")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"account_name": acct.Name,
		"role":         inv.Role,
	})
}

func (h *handlers) join(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code     string `json:"code"`
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	body.Email = strings.TrimSpace(strings.ToLower(body.Email))
	body.Code = strings.TrimSpace(body.Code)
	if body.Code == "" || body.Email == "" || len(body.Password) < 8 {
		writeErr(w, http.StatusBadRequest, "code, email, and password (8+) required")
		return
	}
	if body.Name == "" {
		body.Name = body.Email
	}

	inv, err := h.d.Store.GetInviteByCodeHash(r.Context(), store.HashInviteCode(body.Code))
	if err != nil || !inviteJoinable(inv) {
		// Same message for missing/used/expired — no oracle.
		writeErr(w, http.StatusNotFound, "invalid or expired invite")
		return
	}

	acct, err := h.d.Store.GetAccount(r.Context(), inv.AccountID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "invalid or expired invite")
		return
	}
	caps := h.effectiveCaps(acct)
	used, err := h.d.Store.CountUsers(r.Context(), inv.AccountID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "count failed")
		return
	}
	if used >= caps.MaxSeats {
		writeErr(w, http.StatusConflict, "account is at its seat limit")
		return
	}

	hash, err := session.HashPassword(body.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "hash failed")
		return
	}
	userID := "usr_" + uuid.NewString()
	role := inv.Role
	if role == "" {
		role = "member"
	}
	if err := h.d.Store.CreateUser(r.Context(), store.User{
		ID: userID, AccountID: inv.AccountID, Email: body.Email,
		PasswordHash: hash, Name: body.Name, Role: role,
	}); err != nil {
		writeErr(w, http.StatusConflict, "email already registered")
		return
	}
	if err := h.d.Store.MarkInviteUsed(r.Context(), inv.ID, userID); err != nil {
		writeErr(w, http.StatusNotFound, "invalid or expired invite")
		return
	}
	token, err := h.d.Sessions.Issue(userID, inv.AccountID, body.Email, role)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "token issue failed")
		return
	}
	seatsUsed, _ := h.d.Store.CountUsers(r.Context(), inv.AccountID)
	writeJSON(w, http.StatusCreated, map[string]any{
		"token": token,
		"user":  h.userDTO(r, userID, inv.AccountID, body.Email, body.Name, role, acct.Plan, caps.MaxSeats, caps.MaxServers, seatsUsed),
	})
}

func (h *handlers) patchUser(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")
	var body struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	role := strings.ToLower(strings.TrimSpace(body.Role))
	if role != "admin" && role != "member" {
		writeErr(w, http.StatusBadRequest, "role must be admin or member")
		return
	}
	target, err := h.d.Store.GetUser(r.Context(), id)
	if err != nil || target.AccountID != c.AccountID {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	if target.Role == "admin" && role == "member" {
		n, _ := h.d.Store.CountAdmins(r.Context(), c.AccountID)
		if n <= 1 {
			writeErr(w, http.StatusConflict, "cannot demote the last admin")
			return
		}
	}
	if err := h.d.Store.UpdateUserRole(r.Context(), c.AccountID, id, role); err != nil {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": id, "role": role,
	})
}

func (h *handlers) deleteUser(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if id == c.UserID {
		writeErr(w, http.StatusConflict, "cannot delete yourself")
		return
	}
	target, err := h.d.Store.GetUser(r.Context(), id)
	if err != nil || target.AccountID != c.AccountID {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	if target.Role == "admin" {
		n, _ := h.d.Store.CountAdmins(r.Context(), c.AccountID)
		if n <= 1 {
			writeErr(w, http.StatusConflict, "cannot remove the last admin")
			return
		}
	}
	if err := h.d.Store.DeleteUser(r.Context(), c.AccountID, id); err != nil {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func inviteJoinable(inv *store.Invite) bool {
	if inv == nil {
		return false
	}
	if inv.UsedAt.Valid {
		return false
	}
	return time.Now().UTC().Before(inv.ExpiresAt)
}

func generateInviteCode() (string, error) {
	b := make([]byte, inviteCodeLen)
	max := big.NewInt(int64(len(inviteAlphabet)))
	for i := range b {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b[i] = inviteAlphabet[n.Int64()]
	}
	return string(b), nil
}

// canWriteKey: admin any key; member only keys they own.
// Kill is intentionally wider (any teammate) — see killKey docstring.
func (h *handlers) canWriteKey(c *session.Claims, k *store.APIKey) bool {
	if c.Role == "admin" {
		return true
	}
	return k.OwnerID.Valid && k.OwnerID.String == c.UserID
}

func ownerDTO(ctx context.Context, st *store.Store, ownerID sql.NullString) any {
	if !ownerID.Valid || ownerID.String == "" {
		return nil
	}
	u, err := st.GetUser(ctx, ownerID.String)
	if err != nil {
		return map[string]any{"id": ownerID.String, "name": ""}
	}
	return map[string]any{"id": u.ID, "name": u.Name}
}
