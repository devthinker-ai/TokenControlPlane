package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/devthinker-ai/TokenControlPlane/pkg/session"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

func (h *handlers) listLLMProviders(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	list, err := h.d.Store.ListLLMProvidersByAccount(r.Context(), c.AccountID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list failed")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, p := range list {
		mc, _ := h.d.Store.CountLLMModelsByProvider(r.Context(), p.ID)
		out = append(out, map[string]any{
			"id": p.ID, "name": p.Name, "base_url": p.BaseURL, "enabled": p.Enabled,
			"auth_header": p.AuthHeader, "default_model": p.DefaultModel,
			"timeout_seconds": p.TimeoutSeconds, "model_count": mc,
			"last_health_error": p.LastHealthError,
			"created_at":       p.CreatedAt.Format(time.RFC3339Nano),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handlers) createLLMProvider(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	var body struct {
		Name           string `json:"name"`
		BaseURL        string `json:"base_url"`
		AuthHeader     string `json:"auth_header"`
		AuthValue      string `json:"auth_value"`
		DefaultModel   string `json:"default_model"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" || body.BaseURL == "" {
		writeErr(w, http.StatusBadRequest, "name and base_url required")
		return
	}
	id := "llp_" + uuid.NewString()
	p := store.LLMProvider{
		ID: id, AccountID: c.AccountID, Name: body.Name, BaseURL: strings.TrimRight(body.BaseURL, "/"),
		AuthHeader: body.AuthHeader, AuthValue: body.AuthValue, DefaultModel: body.DefaultModel,
		TimeoutSeconds: body.TimeoutSeconds, Enabled: true,
	}
	if err := h.d.Store.CreateLLMProvider(r.Context(), p); err != nil {
		writeErr(w, http.StatusInternalServerError, "create failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": id, "name": p.Name, "base_url": p.BaseURL, "enabled": true,
		"default_model": p.DefaultModel, "timeout_seconds": p.TimeoutSeconds, "model_count": 0,
	})
}

func (h *handlers) patchLLMProvider(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")
	p, err := h.d.Store.GetLLMProvider(r.Context(), id)
	if err != nil || p.AccountID != c.AccountID {
		writeErr(w, http.StatusNotFound, "provider not found")
		return
	}
	var body struct {
		Name           *string `json:"name"`
		BaseURL        *string `json:"base_url"`
		AuthHeader     *string `json:"auth_header"`
		AuthValue      *string `json:"auth_value"`
		DefaultModel   *string `json:"default_model"`
		TimeoutSeconds *int    `json:"timeout_seconds"`
		Enabled        *bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Name != nil {
		p.Name = *body.Name
	}
	if body.BaseURL != nil {
		p.BaseURL = strings.TrimRight(*body.BaseURL, "/")
	}
	if body.AuthHeader != nil {
		p.AuthHeader = *body.AuthHeader
	}
	if body.AuthValue != nil {
		p.AuthValue = *body.AuthValue
	}
	if body.DefaultModel != nil {
		p.DefaultModel = *body.DefaultModel
	}
	if body.TimeoutSeconds != nil {
		p.TimeoutSeconds = *body.TimeoutSeconds
	}
	if body.Enabled != nil {
		p.Enabled = *body.Enabled
	}
	if err := h.d.Store.UpdateLLMProvider(r.Context(), *p); err != nil {
		writeErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	mc, _ := h.d.Store.CountLLMModelsByProvider(r.Context(), id)
	writeJSON(w, http.StatusOK, map[string]any{
		"id": p.ID, "name": p.Name, "base_url": p.BaseURL, "enabled": p.Enabled,
		"default_model": p.DefaultModel, "timeout_seconds": p.TimeoutSeconds, "model_count": mc,
		"last_health_error": p.LastHealthError,
	})
}

func (h *handlers) deleteLLMProvider(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if err := h.d.Store.DeleteLLMProvider(r.Context(), c.AccountID, id); err != nil {
		writeErr(w, http.StatusNotFound, "provider not found")
		return
	}
	if h.d.Policy != nil {
		h.d.Policy.RemoveProvider(id)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) healthLLMProvider(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")
	p, err := h.d.Store.GetLLMProvider(r.Context(), id)
	if err != nil || p.AccountID != c.AccountID {
		writeErr(w, http.StatusNotFound, "provider not found")
		return
	}
	url := strings.TrimRight(p.BaseURL, "/") + "/models"
	client := &http.Client{Timeout: 2 * time.Second}
	start := time.Now()
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, url, nil)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "latency_ms": 0, "error": err.Error()})
		return
	}
	if p.AuthValue != "" {
		req.Header.Set(p.AuthHeader, p.AuthValue)
	}
	resp, err := client.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		_ = h.d.Store.SetLLMProviderHealthError(r.Context(), id, err.Error())
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "latency_ms": latency, "error": err.Error()})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		msg := "upstream status " + http.StatusText(resp.StatusCode)
		_ = h.d.Store.SetLLMProviderHealthError(r.Context(), id, msg)
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "latency_ms": latency, "error": msg})
		return
	}
	_ = h.d.Store.SetLLMProviderHealthError(r.Context(), id, "")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "latency_ms": latency, "error": ""})
}

func (h *handlers) listLLMModels(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	list, err := h.d.Store.ListLLMModelsByAccount(r.Context(), c.AccountID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list failed")
		return
	}
	providers, _ := h.d.Store.ListLLMProvidersByAccount(r.Context(), c.AccountID)
	byID := map[string]store.LLMProvider{}
	for _, p := range providers {
		byID[p.ID] = p
	}
	out := make([]map[string]any, 0, len(list))
	for _, m := range list {
		row := map[string]any{
			"id": m.ID, "name": m.Name, "model": m.Model, "provider_id": m.ProviderID,
			"created_at": m.CreatedAt.Format(time.RFC3339Nano),
		}
		if p, ok := byID[m.ProviderID]; ok {
			row["provider_name"] = p.Name
		}
		if m.FallbackModelID.Valid {
			row["fallback_model_id"] = m.FallbackModelID.String
			if fb, err := h.d.Store.GetLLMModel(r.Context(), m.FallbackModelID.String); err == nil {
				row["fallback_name"] = fb.Name
			}
		} else {
			row["fallback_model_id"] = nil
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handlers) createLLMModel(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	var body struct {
		Name            string  `json:"name"`
		Model           string  `json:"model"`
		ProviderID      string  `json:"provider_id"`
		FallbackModelID *string `json:"fallback_model_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" || body.Model == "" || body.ProviderID == "" {
		writeErr(w, http.StatusBadRequest, "name, model, and provider_id required")
		return
	}
	ok, err := h.d.Store.ProviderBelongsToAccount(r.Context(), c.AccountID, body.ProviderID)
	if err != nil || !ok {
		writeErr(w, http.StatusBadRequest, "unknown provider")
		return
	}
	id := "llm_" + uuid.NewString()
	m := store.LLMModel{
		ID: id, AccountID: c.AccountID, Name: body.Name, Model: body.Model, ProviderID: body.ProviderID,
	}
	if body.FallbackModelID != nil && *body.FallbackModelID != "" {
		if err := h.d.Store.ValidateFallbackChain(r.Context(), c.AccountID, id, *body.FallbackModelID); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		m.FallbackModelID = sql.NullString{String: *body.FallbackModelID, Valid: true}
	}
	if err := h.d.Store.CreateLLMModel(r.Context(), m); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeErr(w, http.StatusConflict, "route alias already exists")
			return
		}
		writeErr(w, http.StatusInternalServerError, "create failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": id, "name": m.Name, "model": m.Model, "provider_id": m.ProviderID,
		"fallback_model_id": nullStr(m.FallbackModelID),
	})
}

func (h *handlers) patchLLMModel(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")
	m, err := h.d.Store.GetLLMModel(r.Context(), id)
	if err != nil || m.AccountID != c.AccountID {
		writeErr(w, http.StatusNotFound, "model not found")
		return
	}
	var body struct {
		Name            *string `json:"name"`
		Model           *string `json:"model"`
		ProviderID      *string `json:"provider_id"`
		FallbackModelID *string `json:"fallback_model_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Name != nil {
		m.Name = *body.Name
	}
	if body.Model != nil {
		m.Model = *body.Model
	}
	if body.ProviderID != nil {
		ok, err := h.d.Store.ProviderBelongsToAccount(r.Context(), c.AccountID, *body.ProviderID)
		if err != nil || !ok {
			writeErr(w, http.StatusBadRequest, "unknown provider")
			return
		}
		m.ProviderID = *body.ProviderID
	}
	if body.FallbackModelID != nil {
		if *body.FallbackModelID == "" {
			m.FallbackModelID = sql.NullString{}
		} else {
			if err := h.d.Store.ValidateFallbackChain(r.Context(), c.AccountID, id, *body.FallbackModelID); err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			m.FallbackModelID = sql.NullString{String: *body.FallbackModelID, Valid: true}
		}
	}
	if err := h.d.Store.UpdateLLMModel(r.Context(), *m); err != nil {
		writeErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": m.ID, "name": m.Name, "model": m.Model, "provider_id": m.ProviderID,
		"fallback_model_id": nullStr(m.FallbackModelID),
	})
}

func (h *handlers) deleteLLMModel(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if err := h.d.Store.DeleteLLMModel(r.Context(), c.AccountID, id); err != nil {
		writeErr(w, http.StatusNotFound, "model not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func nullStr(ns sql.NullString) any {
	if ns.Valid {
		return ns.String
	}
	return nil
}

func (h *handlers) getKeyProviders(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")
	key, err := h.d.Store.GetAPIKey(r.Context(), id)
	if err != nil || key.AccountID != c.AccountID {
		writeErr(w, http.StatusNotFound, "key not found")
		return
	}
	pp, err := h.d.Store.GetKeyProviderPolicy(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load failed")
		return
	}
	period := store.PeriodStartUTC(time.Now().UTC())
	usage, _ := h.d.Store.ListKeyProviderUsage(r.Context(), id, period)
	byID := map[string]store.ProviderUsageRow{}
	for _, u := range usage {
		byID[u.ProviderID] = u
	}
	allowed := make([]map[string]any, 0)
	if pp.Mode == store.ProviderPolicyCustom {
		for _, pid := range pp.Allowed {
			u := byID[pid]
			name := u.Name
			if name == "" {
				if p, err := h.d.Store.GetLLMProvider(r.Context(), pid); err == nil {
					name = p.Name
				}
			}
			allowed = append(allowed, map[string]any{
				"id": pid, "name": name,
				"monthly_budget": u.MonthlyBudget, "tokens_used": u.TokensUsed,
			})
		}
	} else {
		for _, u := range usage {
			if u.MonthlyBudget > 0 {
				allowed = append(allowed, map[string]any{
					"id": u.ProviderID, "name": u.Name,
					"monthly_budget": u.MonthlyBudget, "tokens_used": u.TokensUsed,
				})
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"mode": pp.Mode, "allowed": allowed})
}

func (h *handlers) putKeyProviders(w http.ResponseWriter, r *http.Request) {
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
	if body.Mode != store.ProviderPolicyAll && body.Mode != store.ProviderPolicyCustom {
		writeErr(w, http.StatusBadRequest, "mode must be all or custom")
		return
	}
	if body.Allowed == nil {
		body.Allowed = []string{}
	}
	for _, pid := range body.Allowed {
		ok, err := h.d.Store.ProviderBelongsToAccount(r.Context(), c.AccountID, pid)
		if err != nil || !ok {
			writeErr(w, http.StatusBadRequest, "unknown provider "+pid)
			return
		}
	}
	var budgets []store.KeyProviderBudget
	for pid, b := range body.Budgets {
		if b < 0 {
			writeErr(w, http.StatusBadRequest, "budget must be >= 0")
			return
		}
		ok, err := h.d.Store.ProviderBelongsToAccount(r.Context(), c.AccountID, pid)
		if err != nil || !ok {
			writeErr(w, http.StatusBadRequest, "unknown provider "+pid)
			return
		}
		if b > 0 {
			budgets = append(budgets, store.KeyProviderBudget{ProviderID: pid, MonthlyBudget: b})
		}
	}
	if err := h.d.Store.SetKeyProviderAccess(r.Context(), id, body.Mode, body.Allowed, budgets); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.d.Policy != nil {
		_ = h.d.Policy.InvalidateKey(r.Context(), h.d.Store, id)
	}
	if h.d.Keys != nil {
		_ = h.d.Keys.Reload(r.Context())
	}
	h.getKeyProviders(w, r)
}

func (h *handlers) usageProviders(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	period := store.PeriodStartUTC(time.Now().UTC())
	list, err := h.d.Store.ListProviderUsage(r.Context(), c.AccountID, period)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "usage failed")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, p := range list {
		out = append(out, map[string]any{
			"provider_id": p.ProviderID, "name": p.Name,
			"tokens": p.TokensUsed, "tokens_exact": p.TokensExact, "tokens_estimated": p.TokensEstimated,
			"requests": p.Requests,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handlers) usageKeyProviders(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")
	key, err := h.d.Store.GetAPIKey(r.Context(), id)
	if err != nil || key.AccountID != c.AccountID {
		writeErr(w, http.StatusNotFound, "key not found")
		return
	}
	period := store.PeriodStartUTC(time.Now().UTC())
	list, err := h.d.Store.ListKeyProviderUsage(r.Context(), id, period)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "usage failed")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, p := range list {
		out = append(out, map[string]any{
			"provider_id": p.ProviderID, "name": p.Name,
			"tokens": p.TokensUsed, "tokens_exact": p.TokensExact, "tokens_estimated": p.TokensEstimated,
			"requests": p.Requests, "monthly_budget": p.MonthlyBudget,
		})
	}
	writeJSON(w, http.StatusOK, out)
}
