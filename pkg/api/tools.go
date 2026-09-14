package api

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/devthinker-ai/TokenControlPlane/pkg/session"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

func (h *handlers) requireToolPolicy(w http.ResponseWriter, r *http.Request) bool {
	c, _ := session.ClaimsFromContext(r.Context())
	acct, err := h.d.Store.GetAccount(r.Context(), c.AccountID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "account not found")
		return false
	}
	caps := h.effectiveCaps(acct)
	if caps.ToolPolicy {
		return true
	}
	if h.d.Caps.Licensed {
		writeErr(w, http.StatusPaymentRequired, "Tool control is a Pro feature. Upgrade your plan.")
	} else {
		writeErr(w, http.StatusPaymentRequired,
			"Tool control is a Pro feature. Set TOKENCONTROLPLANE_LICENSE_KEY for the licensed build.")
	}
	return false
}

func (h *handlers) listServerTools(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")
	srv, err := h.d.Store.GetServer(r.Context(), id)
	if err != nil || srv.AccountID != c.AccountID {
		writeErr(w, http.StatusNotFound, "server not found")
		return
	}
	tools, err := h.d.Store.ListToolsWithStats(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list failed")
		return
	}
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		out = append(out, map[string]any{
			"name": t.Name, "description": t.Description, "input_schema": json.RawMessage(t.InputSchema),
			"enabled": t.Enabled, "calls_30d": t.Calls30d,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handlers) patchServerTool(w http.ResponseWriter, r *http.Request) {
	if !h.requireToolPolicy(w, r) {
		return
	}
	c, _ := session.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")
	srv, err := h.d.Store.GetServer(r.Context(), id)
	if err != nil || srv.AccountID != c.AccountID {
		writeErr(w, http.StatusNotFound, "server not found")
		return
	}
	var body struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeErr(w, http.StatusBadRequest, "name required")
		return
	}
	if err := h.d.Store.SetToolEnabled(r.Context(), id, body.Name, body.Enabled); err != nil {
		if err == sql.ErrNoRows {
			writeErr(w, http.StatusNotFound, "tool not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	if h.d.Policy != nil {
		_ = h.d.Policy.InvalidateServer(r.Context(), h.d.Store, id)
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": body.Name, "enabled": body.Enabled})
}

func (h *handlers) bulkServerTools(w http.ResponseWriter, r *http.Request) {
	if !h.requireToolPolicy(w, r) {
		return
	}
	c, _ := session.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")
	srv, err := h.d.Store.GetServer(r.Context(), id)
	if err != nil || srv.AccountID != c.AccountID {
		writeErr(w, http.StatusNotFound, "server not found")
		return
	}
	var body struct {
		Enabled bool     `json:"enabled"`
		Scope   string   `json:"scope"` // all|selected
		Names   []string `json:"names"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	var names []string
	if body.Scope == "selected" {
		names = body.Names
	}
	n, err := h.d.Store.SetToolsEnabledBulk(r.Context(), id, body.Enabled, names)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	if h.d.Policy != nil {
		_ = h.d.Policy.InvalidateServer(r.Context(), h.d.Store, id)
	}
	writeJSON(w, http.StatusOK, map[string]any{"updated": n, "enabled": body.Enabled})
}

func (h *handlers) getKeyTools(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")
	key, err := h.d.Store.GetAPIKey(r.Context(), id)
	if err != nil || key.AccountID != c.AccountID {
		writeErr(w, http.StatusNotFound, "key not found")
		return
	}
	p, err := h.d.Store.GetKeyToolPolicy(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mode": p.Mode, "allowed": p.Allowed})
}

func (h *handlers) putKeyTools(w http.ResponseWriter, r *http.Request) {
	if !h.requireToolPolicy(w, r) {
		return
	}
	c, _ := session.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")
	key, err := h.d.Store.GetAPIKey(r.Context(), id)
	if err != nil || key.AccountID != c.AccountID {
		writeErr(w, http.StatusNotFound, "key not found")
		return
	}
	if !h.canWriteKey(c, key) {
		writeErr(w, http.StatusForbidden, "not your key")
		return
	}
	var body struct {
		Mode    string   `json:"mode"`
		Allowed []string `json:"allowed"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Mode != store.ToolPolicyAll && body.Mode != store.ToolPolicyCustom {
		writeErr(w, http.StatusBadRequest, "mode must be all or custom")
		return
	}
	if body.Mode == store.ToolPolicyCustom && len(body.Allowed) > 0 {
		known, err := h.d.Store.ListToolNamesInAccount(r.Context(), c.AccountID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "catalog load failed")
			return
		}
		for _, name := range body.Allowed {
			if _, ok := known[name]; !ok {
				writeErr(w, http.StatusBadRequest, "unknown tool: "+name)
				return
			}
		}
	}
	if err := h.d.Store.SetKeyToolPolicy(r.Context(), id, body.Mode, body.Allowed); err != nil {
		writeErr(w, http.StatusInternalServerError, "save failed")
		return
	}
	if h.d.Policy != nil {
		_ = h.d.Policy.InvalidateKey(r.Context(), h.d.Store, id)
	}
	writeJSON(w, http.StatusOK, map[string]any{"mode": body.Mode, "allowed": body.Allowed})
}
