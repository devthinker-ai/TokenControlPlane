package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/devthinker-ai/TokenControlPlane/pkg/session"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

func (h *handlers) getKeyServers(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")
	key, err := h.d.Store.GetAPIKey(r.Context(), id)
	if err != nil || key.AccountID != c.AccountID {
		writeErr(w, http.StatusNotFound, "key not found")
		return
	}
	sp, err := h.d.Store.GetKeyServerPolicy(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load failed")
		return
	}
	period := store.PeriodStartUTC(time.Now().UTC())
	usage, _ := h.d.Store.ListKeyServerUsage(r.Context(), id, period)
	byID := map[string]store.ServerUsageRow{}
	for _, u := range usage {
		byID[u.ServerID] = u
	}
	allowed := make([]map[string]any, 0, len(sp.Allowed))
	if sp.Mode == store.ServerPolicyCustom {
		for _, sid := range sp.Allowed {
			u := byID[sid]
			name := u.Name
			if name == "" {
				if srv, err := h.d.Store.GetServer(r.Context(), sid); err == nil {
					name = srv.Name
				}
			}
			allowed = append(allowed, map[string]any{
				"id": sid, "name": name,
				"monthly_budget": u.MonthlyBudget, "tokens_used": u.TokensUsed,
			})
		}
	} else {
		// all mode: still return budgets that are set
		for _, u := range usage {
			if u.MonthlyBudget > 0 {
				allowed = append(allowed, map[string]any{
					"id": u.ServerID, "name": u.Name,
					"monthly_budget": u.MonthlyBudget, "tokens_used": u.TokensUsed,
				})
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"mode": sp.Mode, "allowed": allowed,
	})
}

func (h *handlers) putKeyServers(w http.ResponseWriter, r *http.Request) {
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
		Mode    string           `json:"mode"`
		Allowed []string         `json:"allowed"`
		Budgets map[string]int64 `json:"budgets"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Mode != store.ServerPolicyAll && body.Mode != store.ServerPolicyCustom {
		writeErr(w, http.StatusBadRequest, "mode must be all or custom")
		return
	}
	if body.Allowed == nil {
		body.Allowed = []string{}
	}
	for _, sid := range body.Allowed {
		ok, err := h.d.Store.ServerBelongsToAccount(r.Context(), c.AccountID, sid)
		if err != nil || !ok {
			writeErr(w, http.StatusBadRequest, "unknown server "+sid)
			return
		}
	}
	var budgets []store.KeyServerBudget
	for sid, b := range body.Budgets {
		if b < 0 {
			writeErr(w, http.StatusBadRequest, "budget must be >= 0")
			return
		}
		ok, err := h.d.Store.ServerBelongsToAccount(r.Context(), c.AccountID, sid)
		if err != nil || !ok {
			writeErr(w, http.StatusBadRequest, "unknown server "+sid)
			return
		}
		if b > 0 {
			budgets = append(budgets, store.KeyServerBudget{ServerID: sid, MonthlyBudget: b})
		}
	}
	if err := h.d.Store.SetKeyServerAccess(r.Context(), id, body.Mode, body.Allowed, budgets); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.d.Policy != nil {
		_ = h.d.Policy.InvalidateKey(r.Context(), h.d.Store, id)
	}
	if h.d.Keys != nil {
		_ = h.d.Keys.Reload(r.Context())
	}
	h.getKeyServers(w, r)
}

func (h *handlers) usageServers(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	period := store.PeriodStartUTC(time.Now().UTC())
	list, err := h.d.Store.ListServerUsage(r.Context(), c.AccountID, period)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "usage failed")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, s := range list {
		byKey := make([]map[string]any, 0, len(s.ByKey))
		for _, k := range s.ByKey {
			byKey = append(byKey, map[string]any{
				"key_id": k.KeyID, "name": k.Name, "tokens": k.TokensUsed, "requests": k.Requests,
			})
		}
		out = append(out, map[string]any{
			"server_id": s.ServerID, "name": s.Name,
			"tokens": s.TokensUsed, "requests": s.Requests, "by_key": byKey,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handlers) usageKeyServers(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")
	key, err := h.d.Store.GetAPIKey(r.Context(), id)
	if err != nil || key.AccountID != c.AccountID {
		writeErr(w, http.StatusNotFound, "key not found")
		return
	}
	period := store.PeriodStartUTC(time.Now().UTC())
	list, err := h.d.Store.ListKeyServerUsage(r.Context(), id, period)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "usage failed")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, s := range list {
		out = append(out, map[string]any{
			"server_id": s.ServerID, "name": s.Name,
			"tokens": s.TokensUsed, "requests": s.Requests,
			"monthly_budget": s.MonthlyBudget,
		})
	}
	writeJSON(w, http.StatusOK, out)
}
