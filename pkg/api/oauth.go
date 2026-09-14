package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/devthinker-ai/TokenControlPlane/pkg/auth/upstream"
	"github.com/devthinker-ai/TokenControlPlane/pkg/session"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

func (h *handlers) connectServer(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")
	srv, err := h.d.Store.GetServer(r.Context(), id)
	if err != nil || srv.AccountID != c.AccountID {
		writeErr(w, http.StatusNotFound, "server not found")
		return
	}
	if srv.AuthType != store.AuthTypeOAuthDevice && srv.AuthType != store.AuthTypeOAuthPKCE {
		writeErr(w, http.StatusBadRequest, "server auth_type is not oauth")
		return
	}
	var body struct {
		Flow string `json:"flow"` // device|pkce
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	flow := body.Flow
	if flow == "" {
		if srv.AuthType == store.AuthTypeOAuthPKCE {
			flow = store.OAuthFlowPKCE
		} else {
			flow = store.OAuthFlowDevice
		}
	}

	var sess upstream.Session
	switch flow {
	case store.OAuthFlowDevice:
		if h.d.DeviceProvider == nil {
			writeErr(w, http.StatusServiceUnavailable, "device provider unavailable")
			return
		}
		sess, err = h.d.DeviceProvider.Begin(r.Context(), srv)
	case store.OAuthFlowPKCE:
		if h.d.PKCEProvider == nil {
			writeErr(w, http.StatusServiceUnavailable, "pkce provider unavailable")
			return
		}
		sess, err = h.d.PKCEProvider.Begin(r.Context(), srv)
	default:
		writeErr(w, http.StatusBadRequest, "flow must be device or pkce")
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	storeSess := store.OAuthSession{
		ID:                           "oas_" + uuid.NewString(),
		AccountID:                    c.AccountID,
		ServerID:                     id,
		Flow:                         sess.Flow,
		DeviceCode:                   sess.DeviceCode,
		VerificationURI:              sess.VerificationURI,
		VerificationCode:             sess.VerificationCode,
		IntervalS:                    sess.Interval,
		ExpiresAt:                    time.Now().UTC().Add(time.Duration(sess.ExpiresIn) * time.Second),
		AuthorizeURL:                 sess.AuthorizeURL,
		State:                        sess.State,
		CodeVerifier:                 sess.CodeVerifier,
		AuthServer:                   sess.AuthServer,
		TokenEndpoint:                sess.TokenEndpoint,
		DeviceAuthEndpoint:           sess.DeviceAuthEndpoint,
		ClientID:                     sess.ClientID,
		Scopes:                       sess.Scopes,
		ProtectedResourceMetadataURL: sess.ProtectedResource,
	}
	if err := h.d.Store.SaveOAuthSession(r.Context(), storeSess); err != nil {
		writeErr(w, http.StatusInternalServerError, "save session failed")
		return
	}

	if flow == store.OAuthFlowDevice {
		go h.pollDeviceUntilDone(storeSess.AccountID, storeSess.ServerID, storeSess.ID)
	}

	out := map[string]any{
		"flow":       sess.Flow,
		"expires_in": sess.ExpiresIn,
		"interval":   sess.Interval,
	}
	if flow == store.OAuthFlowDevice {
		out["device_code"] = sess.DeviceCode
		out["verification_uri"] = sess.VerificationURI
		out["verification_code"] = sess.VerificationCode
	} else {
		out["authorize_url"] = sess.AuthorizeURL
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handlers) getConnectStatus(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")
	srv, err := h.d.Store.GetServer(r.Context(), id)
	if err != nil || srv.AccountID != c.AccountID {
		writeErr(w, http.StatusNotFound, "server not found")
		return
	}
	status, err := h.d.Store.OAuthCredentialStatus(r.Context(), c.AccountID, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "status failed")
		return
	}
	out := map[string]any{"status": status}
	if status == store.OAuthStatusPending {
		sess, err := h.d.Store.GetPendingOAuthSession(r.Context(), c.AccountID, id)
		if err == nil {
			remaining := int(time.Until(sess.ExpiresAt).Seconds())
			if remaining < 0 {
				remaining = 0
			}
			out["flow"] = sess.Flow
			out["verification_uri"] = sess.VerificationURI
			out["verification_code"] = sess.VerificationCode
			out["authorize_url"] = sess.AuthorizeURL
			out["expires_in"] = remaining
			out["interval"] = sess.IntervalS
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handlers) reconnectServer(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")
	srv, err := h.d.Store.GetServer(r.Context(), id)
	if err != nil || srv.AccountID != c.AccountID {
		writeErr(w, http.StatusNotFound, "server not found")
		return
	}
	_ = h.d.Store.DeleteOAuthToken(r.Context(), c.AccountID, id)
	_ = h.d.Store.DeleteOAuthSession(r.Context(), c.AccountID, id)
	h.connectServer(w, r)
}

func (h *handlers) deleteOAuth(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")
	srv, err := h.d.Store.GetServer(r.Context(), id)
	if err != nil || srv.AccountID != c.AccountID {
		writeErr(w, http.StatusNotFound, "server not found")
		return
	}
	_ = h.d.Store.DeleteOAuthToken(r.Context(), c.AccountID, id)
	_ = h.d.Store.DeleteOAuthSession(r.Context(), c.AccountID, id)
	w.WriteHeader(http.StatusNoContent)
}

// OAuthRedirect handles GET /oauth/redirect (public, no JWT) for PKCE callbacks.
func OAuthRedirect(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		state := r.URL.Query().Get("state")
		if code == "" || state == "" {
			http.Error(w, "missing code or state", http.StatusBadRequest)
			return
		}
		sess, err := d.Store.GetOAuthSessionByState(r.Context(), state)
		if err != nil {
			http.Error(w, "unknown or expired oauth state", http.StatusBadRequest)
			return
		}
		if d.PKCEProvider == nil {
			http.Error(w, "pkce unavailable", http.StatusServiceUnavailable)
			return
		}
		ts, err := d.PKCEProvider.CompletePKCEWithStore(r.Context(), sess, code, transport.NewMemoryTokenStore())
		if err != nil {
			http.Error(w, "token exchange failed: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := upstream.PersistToken(r.Context(), d.Store, sess.AccountID, sess.ServerID, ts); err != nil {
			http.Error(w, "persist token failed", http.StatusInternalServerError)
			return
		}
		_ = d.Store.DeleteOAuthSession(r.Context(), sess.AccountID, sess.ServerID)
		srv, _ := d.Store.GetServer(r.Context(), sess.ServerID)
		name := sess.ServerID
		if srv != nil {
			name = srv.Name
		}
		_ = d.Store.InsertActivityEvent(r.Context(), sess.AccountID, store.ActivityOAuthConnected, name,
			"OAuth connected for '"+name+"'", map[string]any{"server_id": sess.ServerID})
		if d.Indexer != nil {
			d.Indexer.IndexServerAsync(sess.ServerID)
		}
		http.Redirect(w, r, "/servers?connected=1", http.StatusFound)
	}
}

func (h *handlers) pollDeviceUntilDone(accountID, serverID, sessionID string) {
	ctx := context.Background()
	if h.d.DeviceProvider == nil {
		return
	}
	for {
		sess, err := h.d.Store.GetPendingOAuthSession(ctx, accountID, serverID)
		if err != nil {
			return
		}
		if sess.ID != sessionID {
			return
		}
		interval := time.Duration(sess.IntervalS) * time.Second
		if interval < time.Second {
			interval = 3 * time.Second
		}
		time.Sleep(interval)

		ts, err := h.d.DeviceProvider.Poll(ctx, sess)
		if errors.Is(err, upstream.ErrAuthorizationPending) {
			continue
		}
		if errors.Is(err, upstream.ErrSlowDown) {
			time.Sleep(5 * time.Second)
			continue
		}
		if errors.Is(err, upstream.ErrExpiredToken) {
			_ = h.d.Store.DeleteOAuthSession(ctx, accountID, serverID)
			return
		}
		if err != nil {
			continue
		}
		if err := upstream.PersistToken(ctx, h.d.Store, accountID, serverID, ts); err != nil {
			return
		}
		_ = h.d.Store.DeleteOAuthSession(ctx, accountID, serverID)
		srv, _ := h.d.Store.GetServer(ctx, serverID)
		name := serverID
		if srv != nil {
			name = srv.Name
		}
		_ = h.d.Store.InsertActivityEvent(ctx, accountID, store.ActivityOAuthConnected, name,
			"OAuth connected for '"+name+"'", map[string]any{"server_id": serverID})
		if h.d.Indexer != nil {
			h.d.Indexer.IndexServerAsync(serverID)
		}
		return
	}
}

type authBody struct {
	Type   string `json:"type"`
	Header string `json:"header"`
	Value  string `json:"value"`
}

// resolveAuthType supports nested auth + flat Phase 8 fields.
func resolveAuthType(authType, flatHeader, flatValue string, nested *authBody) (atype, header, value string) {
	if nested != nil && nested.Type != "" {
		atype = nested.Type
		header = nested.Header
		value = nested.Value
	} else if authType != "" {
		atype = authType
		header = flatHeader
		value = flatValue
	} else if flatValue != "" {
		atype = store.AuthTypeStatic
		header = flatHeader
		value = flatValue
	} else {
		atype = store.AuthTypeNone
	}
	switch atype {
	case "api_key", "key":
		atype = store.AuthTypeStatic
	case "oauth", "oauth_sign_in":
		atype = store.AuthTypeOAuthDevice
	}
	if header == "" {
		header = "Authorization"
	}
	return atype, header, value
}
