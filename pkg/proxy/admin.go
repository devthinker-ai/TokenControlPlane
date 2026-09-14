package proxy

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/devthinker-ai/TokenControlPlane/pkg/auth"
	"github.com/devthinker-ai/TokenControlPlane/pkg/catalog"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

// AdminAuth requires Authorization: Bearer <ADMIN_TOKEN> via constant-time compare.
func AdminAuth(token string) func(http.Handler) http.Handler {
	want := []byte(token)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token == "" {
				auth.WriteErrorJSON(w, http.StatusUnauthorized, "admin token not configured")
				return
			}
			got := bearerAdmin(r.Header.Get("Authorization"))
			if subtle.ConstantTimeCompare([]byte(got), want) != 1 {
				auth.WriteErrorJSON(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func bearerAdmin(h string) string {
	const p = "Bearer "
	if len(h) < len(p) || h[:len(p)] != p {
		return ""
	}
	return h[len(p):]
}

type adminAPI struct {
	store   *store.Store
	auth    *auth.Validator
	breaker *Breaker
	indexer *catalog.Indexer
}

func (a *adminAPI) serverStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := a.store.GetServer(r.Context(), id); err != nil {
		auth.WriteErrorJSON(w, http.StatusNotFound, "server not found")
		return
	}
	st := a.breaker.Status(id)
	writeJSON(w, http.StatusOK, st)
}

func (a *adminAPI) killKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.auth.Kill(r.Context(), id); err != nil {
		if err == sql.ErrNoRows {
			auth.WriteErrorJSON(w, http.StatusNotFound, "key not found")
			return
		}
		auth.WriteErrorJSON(w, http.StatusInternalServerError, "kill failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "killed", "key_id": id})
}

func (a *adminAPI) unkillKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	reset := r.URL.Query().Get("reset") == "true"
	if err := a.auth.Unkill(r.Context(), id, reset); err != nil {
		if err == sql.ErrNoRows {
			auth.WriteErrorJSON(w, http.StatusNotFound, "key not found")
			return
		}
		auth.WriteErrorJSON(w, http.StatusInternalServerError, "unkill failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "unkilled", "key_id": id, "reset": reset})
}

func (a *adminAPI) disableServer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.store.SetServerEnabled(r.Context(), id, false); err != nil {
		if err == sql.ErrNoRows {
			auth.WriteErrorJSON(w, http.StatusNotFound, "server not found")
			return
		}
		auth.WriteErrorJSON(w, http.StatusInternalServerError, "disable failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "disabled", "server_id": id})
}

func (a *adminAPI) enableServer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.store.SetServerEnabled(r.Context(), id, true); err != nil {
		if err == sql.ErrNoRows {
			auth.WriteErrorJSON(w, http.StatusNotFound, "server not found")
			return
		}
		auth.WriteErrorJSON(w, http.StatusInternalServerError, "enable failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "enabled", "server_id": id})
}

func (a *adminAPI) usage(w http.ResponseWriter, r *http.Request) {
	period := store.PeriodStartUTC(time.Now().UTC())
	rows, err := a.store.ListUsageSummaries(r.Context(), period)
	if err != nil {
		auth.WriteErrorJSON(w, http.StatusInternalServerError, "usage query failed")
		return
	}
	type row struct {
		KeyID         string  `json:"key_id"`
		Name          string  `json:"name"`
		TokensUsed    int64   `json:"tokens_used"`
		MonthlyBudget int64   `json:"monthly_budget"`
		Requests      int64   `json:"requests"`
		KilledAt      *string `json:"killed_at"`
		LastUsedAt    *string `json:"last_used_at"`
	}
	out := make([]row, 0, len(rows))
	for _, u := range rows {
		item := row{
			KeyID:         u.KeyID,
			Name:          u.Name,
			TokensUsed:    u.TokensUsed,
			MonthlyBudget: u.MonthlyBudget,
			Requests:      u.Requests,
		}
		if u.KilledAt.Valid {
			s := u.KilledAt.Time.UTC().Format(time.RFC3339Nano)
			item.KilledAt = &s
		}
		if u.LastUsedAt.Valid {
			s := u.LastUsedAt.Time.UTC().Format(time.RFC3339Nano)
			item.LastUsedAt = &s
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *adminAPI) reindex(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "server_id")
	if serverID == "" {
		serverID = chi.URLParam(r, "id")
	}
	if a.indexer == nil {
		auth.WriteErrorJSON(w, http.StatusServiceUnavailable, "indexer unavailable")
		return
	}
	go a.indexer.IndexServer(serverID)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "reindex started"})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
