package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/devthinker-ai/TokenControlPlane/pkg/session"
	"github.com/devthinker-ai/TokenControlPlane/pkg/update"
)

// versionInfo GET /api/v1/version — running build + license update window + optional latest.
func (h *handlers) versionInfo(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	cur := h.d.Version
	if cur == "" {
		cur = "dev"
	}
	schema := 0
	if h.d.Store != nil {
		schema, _ = h.d.Store.SchemaVersion()
	}

	latest := cur
	available := false
	if os.Getenv("NO_UPDATE_CHECK") != "1" {
		info, err := h.cachedUpdateCheck(cur)
		if err == nil {
			latest = info.Latest
			if latest == "" {
				latest = cur
			}
			available = info.Available
		}
	}

	out := map[string]any{
		"version":          stripVAPI(cur),
		"commit":           h.d.Commit,
		"built":            h.d.Built,
		"schema":           schema,
		"latest":           stripVAPI(latest),
		"update_available": available,
		"update_window":    nil,
	}

	if h.d.Billing != nil && c != nil {
		if win := h.licenseUpdateWindow(r, c.AccountID); win != nil {
			out["update_window"] = win
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handlers) licenseUpdateWindow(r *http.Request, accountID string) map[string]any {
	lic, err := h.d.Store.GetActiveLicense(r.Context(), accountID)
	if err != nil || lic == nil {
		return nil
	}
	now := time.Now().UTC()
	active := !now.After(lic.ExpiresAt)
	daysLeft := int(lic.ExpiresAt.Sub(now).Hours() / 24)
	if !active {
		daysLeft = int(now.Sub(lic.ExpiresAt).Hours() / 24)
		if daysLeft > 0 {
			daysLeft = -daysLeft
		}
	}
	return map[string]any{
		"expires_at":  lic.ExpiresAt.UTC().Format(time.RFC3339),
		"active":      active,
		"is_founders": lic.IsFounders,
		"days_left":   daysLeft,
	}
}

type updateCheckCached struct {
	Current   string
	Latest    string
	Available bool
}

func (h *handlers) cachedUpdateCheck(cur string) (updateCheckCached, error) {
	now := time.Now().UTC()
	const cacheKey = "update_check_json"
	const cacheAtKey = "update_check_at"
	if raw, ok, _ := h.d.Store.GetMeta(cacheAtKey); ok {
		if t, err := time.Parse(time.RFC3339, raw); err == nil && now.Sub(t) < 24*time.Hour {
			if body, ok, _ := h.d.Store.GetMeta(cacheKey); ok {
				var m map[string]any
				if json.Unmarshal([]byte(body), &m) == nil {
					out := updateCheckCached{Current: stripVAPI(cur)}
					if v, ok := m["latest"].(string); ok {
						out.Latest = v
					}
					if v, ok := m["available"].(bool); ok {
						out.Available = v
					}
					if v, ok := m["current"].(string); ok && v != "" {
						out.Current = v
					}
					return out, nil
				}
			}
		}
	}
	info, err := update.Check(update.Config{CurrentVersion: cur, UpdateURL: os.Getenv("UPDATE_URL")})
	if err != nil {
		return updateCheckCached{}, err
	}
	payload := map[string]any{
		"current": info.Current, "latest": info.Latest, "available": info.Available,
		"checked_at": info.CheckedAt.Format(time.RFC3339),
	}
	if b, err := json.Marshal(payload); err == nil {
		_ = h.d.Store.SetMeta(cacheKey, string(b))
		_ = h.d.Store.SetMeta(cacheAtKey, now.Format(time.RFC3339))
	}
	return updateCheckCached{
		Current: info.Current, Latest: info.Latest, Available: info.Available,
	}, nil
}

// applyUpdate POST /api/v1/update — admin server-side self-update (SHA256-verified).
// Never auto-rollbacks: after a forward migration, rollback is a data trap (CLI only).
func (h *handlers) applyUpdate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		File   string `json:"file"`
		SHA256 string `json:"sha256"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}

	before := h.d.Version
	if before == "" {
		before = "dev"
	}
	schema, _ := h.d.Store.SchemaVersion()

	exe := h.d.ExePath
	if exe == "" {
		var err error
		exe, err = os.Executable()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "resolve executable: "+err.Error())
			return
		}
		exe, err = filepath.EvalSymlinks(exe)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "resolve executable: "+err.Error())
			return
		}
	}

	cfg := update.Config{
		CurrentVersion: before,
		UpdateURL:      os.Getenv("UPDATE_URL"),
	}
	if h.d.UpdateHTTP != nil {
		cfg.HTTPClient = h.d.UpdateHTTP
	}

	var applyErr error
	if body.File != "" {
		if body.SHA256 == "" {
			writeErr(w, http.StatusBadRequest, "sha256 required with file")
			return
		}
		applyErr = update.ApplyFile(exe, body.File, body.SHA256)
	} else {
		applyErr = update.DownloadAndApply(cfg, exe)
	}
	if applyErr != nil {
		msg := applyErr.Error()
		code := http.StatusBadGateway
		if strings.Contains(msg, "sha256 mismatch") {
			code = http.StatusBadRequest
		}
		writeErr(w, code, msg)
		return
	}

	restarted, msg := update.MaybeRestartSystemd()
	restart := "manual"
	if restarted {
		restart = "systemd"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "replaced",
		"restart":        restart,
		"message":        msg,
		"version_before": stripVAPI(before),
		"schema":         schema,
		// Forward-only: next boot may apply new migrations. Auto-rollback is never done here.
		"schema_note": "If this release ships migrations, they apply on next boot (forward-only). Rollback stays a deliberate CLI act — auto-rollback after a schema advance is a data trap.",
		"platform":    fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	})
}

// releaseNotes GET /api/v1/releases/{tag} — admin; proxied GitHub body, 24h meta cache.
func (h *handlers) releaseNotes(w http.ResponseWriter, r *http.Request) {
	tag := chi.URLParam(r, "tag")
	if tag == "" {
		writeErr(w, http.StatusBadRequest, "tag required")
		return
	}
	if os.Getenv("NO_UPDATE_CHECK") == "1" {
		writeErr(w, http.StatusServiceUnavailable, "update checks disabled (NO_UPDATE_CHECK=1)")
		return
	}

	cacheKey := "release_notes_" + strings.TrimPrefix(tag, "v")
	cacheAtKey := cacheKey + "_at"
	now := time.Now().UTC()
	if raw, ok, _ := h.d.Store.GetMeta(cacheAtKey); ok {
		if t, err := time.Parse(time.RFC3339, raw); err == nil && now.Sub(t) < 24*time.Hour {
			if body, ok, _ := h.d.Store.GetMeta(cacheKey); ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(body))
				return
			}
		}
	}

	cfg := update.Config{UpdateURL: os.Getenv("UPDATE_URL")}
	if h.d.UpdateHTTP != nil {
		cfg.HTTPClient = h.d.UpdateHTTP
	}
	notes, err := update.FetchRelease(cfg, tag)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	out := map[string]any{
		"tag":            notes.Tag,
		"body_markdown":  notes.Body,
		"published_at":   nil,
	}
	if !notes.PublishedAt.IsZero() {
		out["published_at"] = notes.PublishedAt.UTC().Format(time.RFC3339)
	}
	if b, err := json.Marshal(out); err == nil {
		_ = h.d.Store.SetMeta(cacheKey, string(b))
		_ = h.d.Store.SetMeta(cacheAtKey, now.Format(time.RFC3339))
	}
	writeJSON(w, http.StatusOK, out)
}

func stripVAPI(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}
