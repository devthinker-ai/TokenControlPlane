package proxy

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/devthinker-ai/TokenControlPlane/pkg/auth"
	"github.com/devthinker-ai/TokenControlPlane/pkg/auth/upstream"
	"github.com/devthinker-ai/TokenControlPlane/pkg/catalog"
	"github.com/devthinker-ai/TokenControlPlane/pkg/policy"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
	"github.com/devthinker-ai/TokenControlPlane/pkg/upstream/stdio"
)

// Deps wires packages into the HTTP router.
type Deps struct {
	Store      *store.Store
	Auth       *auth.Validator
	Meter      *Meter
	Indexer    *catalog.Indexer
	Breaker    *Breaker
	AdminToken string
	Logger     *slog.Logger
	// API is the /api/v1 handler (JWT dashboard). Built in main to avoid import cycles.
	API http.Handler
	// Webhook is Lemon Squeezy billing webhook (no JWT). Optional.
	Webhook http.HandlerFunc
	// SPA serves the embedded dashboard for non-API routes. Optional (tests omit it).
	SPA http.Handler
	// Version metadata for /healthz (optional).
	Version string
	Commit  string
	Built   string
	// TokenGuard is shared with indexer for upstream OAuth injection.
	TokenGuard *upstream.TokenGuard
	// OAuthRedirect is the public PKCE callback (GET /oauth/redirect).
	OAuthRedirect http.HandlerFunc
	// Policy is the in-memory tool visibility cache (11a).
	Policy *policy.Cache
	// Stdio manages local MCP subprocesses (11b).
	Stdio *stdio.Manager
}

// NewRouter builds the chi router for the gateway.
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	if d.Breaker == nil {
		d.Breaker = NewBreaker()
	}
	proxyHandler := NewHandler(d.Store, d.Breaker)
	if d.TokenGuard != nil {
		proxyHandler.SetTokenGuard(d.TokenGuard)
	}
	if d.Policy != nil {
		proxyHandler.SetPolicyCache(d.Policy)
	}
	if d.Stdio != nil {
		proxyHandler.SetStdioManager(d.Stdio)
	}
	admin := &adminAPI{
		store:   d.Store,
		auth:    d.Auth,
		breaker: d.Breaker,
		indexer: d.Indexer,
	}

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		out := map[string]any{"status": "ok"}
		if d.Version != "" {
			out["version"] = d.Version
			out["commit"] = d.Commit
			out["built"] = d.Built
		}
		if d.Store != nil {
			if schema, err := d.Store.SchemaVersion(); err == nil {
				out["schema"] = schema
			}
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Get("/version", func(w http.ResponseWriter, _ *http.Request) {
		schema := 0
		if d.Store != nil {
			schema, _ = d.Store.SchemaVersion()
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"version": d.Version, "commit": d.Commit, "built": d.Built, "schema": schema,
		})
	})
	r.Get("/readyz", func(w http.ResponseWriter, req *http.Request) {
		if err := d.Store.Ping(req.Context()); err != nil {
			auth.WriteErrorJSON(w, http.StatusServiceUnavailable, "db not ready")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

	r.Route("/mcp/{server_id}", func(r chi.Router) {
		r.Use(d.Auth.Middleware)
		r.Use(meteringAccessLog(d.Meter, d.Logger))
		r.HandleFunc("/", proxyHandler.ServeHTTP)
		r.HandleFunc("/*", proxyHandler.ServeHTTP)
	})

	// LLM chat completions — same key auth + budgets; metering happens inside LLMHandler.
	llmHandler := NewLLMHandler(d.Store, d.Meter, d.Policy)
	r.Route("/v1", func(r chi.Router) {
		r.Use(d.Auth.Middleware)
		r.Post("/chat/completions", llmHandler.ServeHTTP)
	})

	// Design-partner admin token gate (kept during JWT migration).
	r.Route("/admin", func(r chi.Router) {
		r.Use(AdminAuth(d.AdminToken))
		r.Get("/usage", admin.usage)
		r.Get("/servers/{id}/status", admin.serverStatus)
		r.Post("/servers/{id}/disable", admin.disableServer)
		r.Post("/servers/{id}/enable", admin.enableServer)
		r.Post("/servers/{id}/reindex", admin.reindex)
		r.Post("/keys/{id}/kill", admin.killKey)
		r.Post("/keys/{id}/unkill", admin.unkillKey)
		r.Post("/{server_id}/reindex", admin.reindex)
	})

	if d.API != nil {
		r.Mount("/api/v1", d.API)
	}
	if d.Webhook != nil {
		r.Post("/api/v1/billing/webhook", d.Webhook)
	}
	// PKCE callback before SPA catch-all (public, no JWT).
	if d.OAuthRedirect != nil {
		r.Get("/oauth/redirect", d.OAuthRedirect)
	}

	// SPA last: /api, /mcp, /admin, /healthz, /oauth already matched above.
	// NotFound covers client-side routes; Method Get/Head /* serves assets at /.
	if d.SPA != nil {
		r.NotFound(d.SPA.ServeHTTP)
		r.Get("/", d.SPA.ServeHTTP)
		r.Get("/*", d.SPA.ServeHTTP)
	}

	return r
}
