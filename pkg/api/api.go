// Package api implements /api/v1 dashboard endpoints (JWT user sessions).
package api

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/devthinker-ai/TokenControlPlane/pkg/auth"
	"github.com/devthinker-ai/TokenControlPlane/pkg/auth/upstream"
	"github.com/devthinker-ai/TokenControlPlane/pkg/billing"
	"github.com/devthinker-ai/TokenControlPlane/pkg/catalog"
	"github.com/devthinker-ai/TokenControlPlane/pkg/license"
	"github.com/devthinker-ai/TokenControlPlane/pkg/policy"
	"github.com/devthinker-ai/TokenControlPlane/pkg/session"
	"github.com/devthinker-ai/TokenControlPlane/pkg/snippets"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
	"github.com/devthinker-ai/TokenControlPlane/pkg/upstream/stdio"
)

// CircuitReporter abstracts circuit status (implemented by *proxy.Breaker.StatusMap).
type CircuitReporter interface {
	StatusMap(serverID string) map[string]any
}

// Deps wires API handlers.
type Deps struct {
	Store          *store.Store
	Sessions       *session.Manager
	Keys           *auth.Validator
	Indexer        *catalog.Indexer
	Breaker        CircuitReporter
	Billing        *billing.Service
	GatewayURL     string // public URL for snippets + PKCE redirect
	Caps           license.Caps
	Version        string
	Commit         string
	Built          string
	ExePath        string       // override for tests; empty → os.Executable()
	UpdateHTTP     *http.Client // override GitHub client for tests
	TokenGuard     *upstream.TokenGuard
	DeviceProvider *upstream.DeviceProvider
	PKCEProvider   *upstream.PKCEProvider
	Policy         *policy.Cache
	Stdio          *stdio.Manager
	// DisableRegister blocks POST /register (new free accounts). Invite join (/join) stays open.
	// Set via DISABLE_REGISTER=1 — useful for locked demos.
	DisableRegister bool
	// Now optional clock for MFA/TOTP tests (nil → time.Now.UTC).
	Now func() time.Time
}

// NewRouter mounts /api/v1 routes on r (caller should pass a chi router or mount point).
func NewRouter(d Deps) http.Handler {
	if d.GatewayURL == "" {
		d.GatewayURL = os.Getenv("GATEWAY_URL")
		if d.GatewayURL == "" {
			d.GatewayURL = "http://localhost:8080"
		}
	}
	// Zero-value Deps get free caps.
	if d.Caps.Plan == "" && !d.Caps.Licensed && d.Caps.MaxServers == 0 {
		d.Caps = license.FreeCaps()
	}
	h := &handlers{d: d, mfa: session.NewMFAGate(d.Now), now: d.Now}
	r := chi.NewRouter()

	r.Post("/register", h.register)
	r.Post("/login", h.login)
	r.Post("/login/mfa", h.loginMFA)
	r.Post("/join", h.join)
	r.Get("/invites/public/{code}", h.publicInviteLookup)
	r.Post("/password-reset/request", h.requestPasswordReset)
	r.Post("/password-reset/confirm", h.confirmPasswordReset)
	r.Get("/public-config", h.publicConfig) // registration open/closed for login UI
	if d.Billing != nil {
		r.Get("/pricing", d.Billing.Pricing) // public ladder — no auth
	}

	r.Group(func(r chi.Router) {
		r.Use(d.Sessions.Middleware)
		r.Get("/me", h.me)
		r.Put("/me/password", h.changeMyPassword)
		r.Get("/2fa", h.get2FA)
		r.Post("/2fa/setup", h.setup2FA)
		r.Post("/2fa/confirm", h.confirm2FA)
		r.Delete("/2fa", h.delete2FA)
		r.Get("/usage", h.usage)
		r.Post("/usage/tools", h.usageTools)
		r.Get("/usage/tools", h.usageTools) // frontend may GET
		r.Get("/onboarding", h.onboarding)
		r.Post("/onboarding/complete", h.completeOnboarding)
		r.Get("/activity", h.listActivity)
		r.Get("/update-check", h.updateCheck)
		r.Get("/version", h.versionInfo)
		r.Get("/servers", h.listServers)
		r.Get("/servers/{id}/status", h.serverStatus)
		r.Get("/servers/{id}/tools", h.listServerTools)
		r.Get("/servers/{id}/connect", h.getConnectStatus)
		r.Get("/llm/providers", h.listLLMProviders)
		r.Get("/llm/models", h.listLLMModels)
		r.Get("/keys", h.listKeys)
		r.Post("/keys", h.createKey)
		r.Get("/keys/{id}/tools", h.getKeyTools)
		r.Get("/keys/{id}/servers", h.getKeyServers)
		r.Get("/keys/{id}/providers", h.getKeyProviders)
		r.Get("/usage/servers", h.usageServers)
		r.Get("/usage/keys/{id}/servers", h.usageKeyServers)
		r.Get("/usage/providers", h.usageProviders)
		r.Get("/usage/keys/{id}/providers", h.usageKeyProviders)
		// Kill is the emergency fire alarm — any teammate can pull it (member or admin).
		r.Post("/keys/{id}/kill", h.killKey)

		// Key mutate: admin any; member own only — enforced in handlers.
		r.Delete("/keys/{id}", h.deleteKey)
		r.Put("/keys/{id}/tools", h.putKeyTools)
		r.Put("/keys/{id}/servers", h.putKeyServers)
		r.Put("/keys/{id}/providers", h.putKeyProviders)
		r.Post("/keys/{id}/unkill", h.unkillKey)

		r.Group(func(r chi.Router) {
			r.Use(session.RequireAdmin)
			r.Get("/users", h.listUsers)
			r.Patch("/users/{id}", h.patchUser)
			r.Delete("/users/{id}", h.deleteUser)
			r.Post("/users/{id}/reset-password", h.adminResetPassword)
			r.Get("/invites", h.listInvites)
			r.Post("/invites", h.createInvite)
			r.Delete("/invites/{id}", h.deleteInvite)
			r.Post("/invites/email", h.createEmailInvite)
			r.Get("/invites/email", h.listEmailInvites)
			r.Get("/settings/smtp", h.getSMTP)
			r.Put("/settings/smtp", h.putSMTP)
			r.Post("/settings/smtp/test", h.testSMTP)
			r.Get("/settings/templates", h.getTemplates)
			r.Put("/settings/templates", h.putTemplates)
			r.Post("/servers", h.createServer)
			r.Post("/servers/check-command", h.checkCommand)
			r.Post("/servers/parse-mcp-json", h.parseMCPJSON)
			r.Patch("/servers/{id}", h.patchServer)
			r.Delete("/servers/{id}", h.deleteServer)
			r.Post("/servers/{id}/reindex", h.reindexServer)
			r.Patch("/servers/{id}/tools", h.patchServerTool)
			r.Patch("/servers/{id}/tools/bulk", h.bulkServerTools)
			r.Post("/servers/{id}/connect", h.connectServer)
			r.Post("/servers/{id}/reconnect", h.reconnectServer)
			r.Delete("/servers/{id}/oauth", h.deleteOAuth)
			r.Post("/llm/providers", h.createLLMProvider)
			r.Patch("/llm/providers/{id}", h.patchLLMProvider)
			r.Delete("/llm/providers/{id}", h.deleteLLMProvider)
			r.Get("/llm/providers/{id}/health", h.healthLLMProvider)
			r.Post("/llm/models", h.createLLMModel)
			r.Patch("/llm/models/{id}", h.patchLLMModel)
			r.Delete("/llm/models/{id}", h.deleteLLMModel)
			r.Post("/update", h.applyUpdate)
			r.Get("/releases/{tag}", h.releaseNotes)
			if d.Billing != nil {
				r.Post("/billing/checkout", d.Billing.Checkout)
				r.Post("/billing/portal", d.Billing.Portal)
				r.Get("/licenses/me", d.Billing.MeLicense)
				r.Post("/licenses/reissue", d.Billing.ReissueLicense)
			}
		})
	})

	return r
}

type handlers struct {
	d   Deps
	mfa *session.MFAGate
	now func() time.Time // optional clock for MFA/TOTP tests
}

func (h *handlers) publicConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"registration_enabled": !h.d.DisableRegister,
	})
}

func (h *handlers) register(w http.ResponseWriter, r *http.Request) {
	if h.d.DisableRegister {
		// Demo / locked installs: keep login + invite join; block new free accounts.
		writeErr(w, http.StatusForbidden, "Registration is disabled on this gateway.")
		return
	}
	var body struct {
		Email       string `json:"email"`
		Password    string `json:"password"`
		AccountName string `json:"account_name"`
		Name        string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	body.Email = strings.TrimSpace(strings.ToLower(body.Email))
	if body.Email == "" || len(body.Password) < 8 || body.AccountName == "" {
		writeErr(w, http.StatusBadRequest, "email, password (8+), and account_name required")
		return
	}
	if body.Name == "" {
		body.Name = body.Email
	}
	hash, err := session.HashPassword(body.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "hash failed")
		return
	}
	acctID := "acct_" + uuid.NewString()
	userID := "usr_" + uuid.NewString()
	caps := h.effectiveCaps(nil)
	plan := caps.Plan
	if plan == "" {
		plan = store.PlanFree
	}
	if err := h.d.Store.CreateAccount(r.Context(), store.Account{
		ID:         acctID,
		Name:       body.AccountName,
		Plan:       plan,
		MaxSeats:   caps.MaxSeats,
		MaxServers: caps.MaxServers,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "create account failed")
		return
	}
	if err := h.d.Store.CreateUser(r.Context(), store.User{
		ID:           userID,
		AccountID:    acctID,
		Email:        body.Email,
		PasswordHash: hash,
		Name:         body.Name,
		Role:         "admin",
	}); err != nil {
		writeErr(w, http.StatusConflict, "email already registered")
		return
	}
	token, err := h.d.Sessions.Issue(userID, acctID, body.Email, "admin")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "token issue failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"token": token,
		"user":  h.userDTO(r, userID, acctID, body.Email, body.Name, "admin", plan, caps.MaxSeats, caps.MaxServers, 1),
	})
}

func (h *handlers) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	u, err := h.d.Store.GetUserByEmail(r.Context(), strings.TrimSpace(strings.ToLower(body.Email)))
	if err != nil || !session.CheckPassword(u.PasswordHash, body.Password) {
		writeErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	acct, err := h.d.Store.GetAccount(r.Context(), u.AccountID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "account missing")
		return
	}
	// Enrolled users get a short-lived mfa_token — never a session until TOTP/recovery OK.
	if enabled, _ := h.d.Store.TOTPEnabled(r.Context(), u.ID); enabled {
		mfaTok, _, err := h.d.Sessions.IssueMFA(u.ID, h.clock())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "token issue failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"mfa_required": true,
			"mfa_token":    mfaTok,
		})
		return
	}
	token, err := h.d.Sessions.Issue(u.ID, u.AccountID, u.Email, u.Role)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "token issue failed")
		return
	}
	seatsUsed, _ := h.d.Store.CountUsers(r.Context(), u.AccountID)
	writeJSON(w, http.StatusOK, map[string]any{
		"token": token,
		"user":  h.userDTO(r, u.ID, u.AccountID, u.Email, u.Name, u.Role, acct.Plan, acct.MaxSeats, acct.MaxServers, seatsUsed),
	})
}

func (h *handlers) me(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	u, err := h.d.Store.GetUser(r.Context(), c.UserID)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "user not found")
		return
	}
	acct, err := h.d.Store.GetAccount(r.Context(), c.AccountID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "account not found")
		return
	}
	seatsUsed, _ := h.d.Store.CountUsers(r.Context(), c.AccountID)
	writeJSON(w, http.StatusOK, h.userDTO(r, u.ID, u.AccountID, u.Email, u.Name, u.Role, acct.Plan, acct.MaxSeats, acct.MaxServers, seatsUsed))
}

func (h *handlers) userDTO(r *http.Request, id, acctID, email, name, role, plan string, seats, servers, seatsUsed int) map[string]any {
	toolPolicy := license.CapsForPlan(plan).ToolPolicy
	if h.d.Caps.Licensed {
		toolPolicy = h.d.Caps.ToolPolicy
	}
	totpOn := false
	if r != nil {
		totpOn, _, _ = h.totpFields(r, id)
	}
	return map[string]any{
		"id": id, "account_id": acctID, "email": email, "name": name, "role": role,
		"plan": plan, "max_seats": seats, "max_servers": servers,
		"seats_used": seatsUsed, "tool_policy": toolPolicy,
		"totp_enabled": totpOn,
	}
}

func (h *handlers) usage(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	period := store.PeriodStartUTC(time.Now().UTC())
	keys, err := h.d.Store.ListUsageSummariesByAccount(r.Context(), c.AccountID, period)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "usage failed")
		return
	}
	var tokens, budget, reqToday int64
	for _, k := range keys {
		tokens += k.TokensUsed
		budget += k.MonthlyBudget
	}
	daily, _ := h.d.Store.ListDailyUsage(r.Context(), c.AccountID, period, time.Now().UTC())
	today := store.DayStartUTC(time.Now().UTC()).Format("2006-01-02")
	dailyOut := make([]map[string]any, 0, len(daily))
	for _, d := range daily {
		ds := d.Day.Format("2006-01-02")
		dailyOut = append(dailyOut, map[string]any{"date": ds, "tokens": d.TokensUsed})
		if ds == today {
			reqToday = d.Requests
		}
	}
	srvCount, _ := h.d.Store.CountServers(r.Context(), c.AccountID)
	keyList, _ := h.d.Store.ListAPIKeysByAccount(r.Context(), c.AccountID)
	activeKeys := 0
	for _, k := range keyList {
		if k.Enabled && !k.KilledAt.Valid {
			activeKeys++
		}
	}
	llmTok, llmExact, llmEst, llmProviders, _ := h.d.Store.AggregateProviderUsage(r.Context(), c.AccountID, period)
	writeJSON(w, http.StatusOK, map[string]any{
		"period":         period.Format("2006-01"),
		"tokens_used":    tokens,
		"monthly_budget": budget,
		"requests_today": reqToday,
		"daily":          dailyOut,
		"servers_count":  srvCount,
		"active_keys":    activeKeys,
		"keys":           keys,
		"llm": map[string]any{
			"tokens":           llmTok,
			"tokens_exact":     llmExact,
			"tokens_estimated": llmEst,
			"providers":        llmProviders,
		},
	})
}

func (h *handlers) usageTools(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	serverID := r.URL.Query().Get("server_id")
	aggs, err := h.d.Store.AggregateToolCallsByServer(r.Context(), c.AccountID, serverID, 5)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "tools aggregate failed")
		return
	}
	out := make([]map[string]any, 0, len(aggs))
	for _, a := range aggs {
		out = append(out, map[string]any{"name": a.Name, "call_count": a.CallCount})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handlers) listServers(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	list, err := h.d.Store.ListServersByAccount(r.Context(), c.AccountID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list failed")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, s := range list {
		tc, _ := h.d.Store.CountTools(r.Context(), s.ID)
		oauthStatus := ""
		if s.AuthType == store.AuthTypeOAuthDevice || s.AuthType == store.AuthTypeOAuthPKCE {
			oauthStatus, _ = h.d.Store.OAuthCredentialStatus(r.Context(), c.AccountID, s.ID)
		}
		row := map[string]any{
			"id": s.ID, "name": s.Name, "base_url": s.BaseURL, "enabled": s.Enabled,
			"tool_count": tc, "auth_header": s.AuthHeader, "auth_type": s.AuthType,
			"transport": s.Transport, "command": s.Command,
			"created_at": s.CreatedAt.Format(time.RFC3339Nano),
			"last_index_error": s.LastIndexError,
			"last_latency_ms":  nil,
			"oauth_status":     oauthStatus,
		}
		if s.Transport == store.TransportStdio && h.d.Stdio != nil {
			st := h.d.Stdio.StatusSnapshot(s.ID)
			row["process"] = map[string]any{"state": st.State, "in_flight": st.InFlight, "strikes": st.Strikes}
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handlers) createServer(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	acct, err := h.d.Store.GetAccount(r.Context(), c.AccountID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "account not found")
		return
	}
	n, _ := h.d.Store.CountServers(r.Context(), c.AccountID)
	caps := h.effectiveCaps(acct)
	if n >= caps.MaxServers {
		if h.d.Caps.Licensed {
			writeErr(w, http.StatusPaymentRequired, "Plan limit reached. Upgrade to add more servers.")
		} else {
			writeErr(w, http.StatusPaymentRequired,
				"Free-tier limit reached (3 servers). Set TOKENCONTROLPLANE_LICENSE_KEY for the licensed build.")
		}
		return
	}
	var body struct {
		Name         string            `json:"name"`
		BaseURL      string            `json:"base_url"`
		AuthType     string            `json:"auth_type"`
		AuthHeader   string            `json:"auth_header"`
		AuthValue    string            `json:"auth_value"`
		Auth         *authBody         `json:"auth"`
		Transport    string            `json:"transport"`
		Command      string            `json:"command"`
		Args         []string          `json:"args"`
		Env          map[string]string `json:"env"`
		Workdir      string            `json:"workdir"`
		CwdIsolation *bool             `json:"cwd_isolation"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeErr(w, http.StatusBadRequest, "name required")
		return
	}
	transport := body.Transport
	if transport == "" {
		transport = store.TransportHTTP
	}
	if transport != store.TransportHTTP && transport != store.TransportStdio {
		writeErr(w, http.StatusBadRequest, "transport must be http or stdio")
		return
	}

	atype, aheader, avalue := resolveAuthType(body.AuthType, body.AuthHeader, body.AuthValue, body.Auth)
	argsJSON, envJSON := "", ""
	cwdIso := true
	if body.CwdIsolation != nil {
		cwdIso = *body.CwdIsolation
	}

	if transport == store.TransportStdio {
		if err := stdio.ValidateStdioServer(body.Command, body.BaseURL, atype); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		atype = store.AuthTypeNone
		aheader, avalue = "Authorization", ""
		var err error
		argsJSON, err = stdio.EncodeArgsJSON(body.Args)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid args")
			return
		}
		envJSON, err = stdio.EncodeEnvJSON(body.Env)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid env")
			return
		}
		if _, err := stdio.ParseArgsJSON(argsJSON); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if _, err := stdio.ParseEnvJSON(envJSON); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	} else if body.BaseURL == "" {
		writeErr(w, http.StatusBadRequest, "name and base_url required")
		return
	}

	id := "srv_" + uuid.NewString()
	srv := store.MCPServer{
		ID: id, AccountID: c.AccountID, Name: body.Name, BaseURL: body.BaseURL,
		AuthType: atype, AuthHeader: aheader, AuthValue: avalue, Enabled: true,
		Transport: transport, Command: body.Command, ArgsJSON: argsJSON, EnvJSON: envJSON,
		Workdir: body.Workdir, CwdIsolation: cwdIso,
	}
	if err := h.d.Store.CreateServer(r.Context(), srv); err != nil {
		writeErr(w, http.StatusInternalServerError, "create failed")
		return
	}
	_ = h.d.Store.InsertActivityEvent(r.Context(), c.AccountID, store.ActivityServerAdded, srv.Name,
		"Server '"+srv.Name+"' added", map[string]any{"server_id": id})

	var toolsOut []map[string]any
	var indexErr any
	// OAuth servers need Connect before indexing — skip tools/list until connected.
	if atype == store.AuthTypeOAuthDevice || atype == store.AuthTypeOAuthPKCE {
		toolsOut = []map[string]any{}
		indexErr = nil
	} else if h.d.Indexer != nil {
		defs, ierr := h.d.Indexer.IndexServer(id)
		if ierr != "" {
			indexErr = ierr
		} else {
			indexErr = nil
		}
		toolsOut = make([]map[string]any, 0, len(defs))
		for _, t := range defs {
			toolsOut = append(toolsOut, map[string]any{
				"name": t.Name, "description": t.Description,
			})
		}
		if h.d.Policy != nil {
			_ = h.d.Policy.InvalidateServer(r.Context(), h.d.Store, id)
		}
	}
	if toolsOut == nil {
		toolsOut = []map[string]any{}
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": id, "name": srv.Name, "base_url": srv.BaseURL, "enabled": true,
		"auth_type": atype, "transport": transport, "command": srv.Command,
		"tool_count": len(toolsOut), "tools": toolsOut, "indexing_error": indexErr,
	})
}

func (h *handlers) patchServer(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")
	srv, err := h.d.Store.GetServer(r.Context(), id)
	if err != nil || srv.AccountID != c.AccountID {
		writeErr(w, http.StatusNotFound, "server not found")
		return
	}
	var body struct {
		Name         *string           `json:"name"`
		Enabled      *bool             `json:"enabled"`
		BaseURL      *string           `json:"base_url"`
		AuthType     *string           `json:"auth_type"`
		AuthHeader   *string           `json:"auth_header"`
		AuthValue    *string           `json:"auth_value"`
		Auth         *authBody         `json:"auth"`
		Command      *string           `json:"command"`
		Args         []string          `json:"args"`
		Env          map[string]string `json:"env"`
		Workdir      *string           `json:"workdir"`
		CwdIsolation *bool             `json:"cwd_isolation"`
		ArgsSet      bool              `json:"-"`
	}
	dec := json.NewDecoder(r.Body)
	var raw map[string]json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	// Re-decode into typed fields.
	b, _ := json.Marshal(raw)
	_ = json.Unmarshal(b, &body)
	if _, ok := raw["args"]; ok {
		body.ArgsSet = true
	}
	if body.Name != nil {
		srv.Name = *body.Name
	}
	if body.Enabled != nil {
		srv.Enabled = *body.Enabled
	}
	if body.BaseURL != nil {
		srv.BaseURL = *body.BaseURL
	}
	if body.Auth != nil && body.Auth.Type != "" {
		atype, aheader, avalue := resolveAuthType("", "", "", body.Auth)
		srv.AuthType = atype
		srv.AuthHeader = aheader
		srv.AuthValue = avalue
	} else {
		if body.AuthType != nil {
			srv.AuthType = *body.AuthType
		}
		if body.AuthHeader != nil {
			srv.AuthHeader = *body.AuthHeader
		}
		if body.AuthValue != nil {
			srv.AuthValue = *body.AuthValue
		}
	}
	if body.Command != nil {
		srv.Command = *body.Command
	}
	if body.ArgsSet {
		aj, err := stdio.EncodeArgsJSON(body.Args)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid args")
			return
		}
		srv.ArgsJSON = aj
	}
	if _, ok := raw["env"]; ok {
		ej, err := stdio.EncodeEnvJSON(body.Env)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid env")
			return
		}
		srv.EnvJSON = ej
	}
	if body.Workdir != nil {
		srv.Workdir = *body.Workdir
	}
	if body.CwdIsolation != nil {
		srv.CwdIsolation = *body.CwdIsolation
	}
	if srv.Transport == store.TransportStdio {
		if err := stdio.ValidateStdioServer(srv.Command, srv.BaseURL, srv.AuthType); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		srv.AuthType = store.AuthTypeNone
	}
	if err := h.d.Store.UpdateServer(r.Context(), *srv); err != nil {
		writeErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	tc, _ := h.d.Store.CountTools(r.Context(), id)
	writeJSON(w, http.StatusOK, map[string]any{
		"id": srv.ID, "name": srv.Name, "base_url": srv.BaseURL, "enabled": srv.Enabled,
		"transport": srv.Transport, "tool_count": tc,
	})
}

func (h *handlers) deleteServer(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")
	srv, _ := h.d.Store.GetServer(r.Context(), id)
	name := id
	if srv != nil {
		name = srv.Name
		if srv.Transport == store.TransportStdio && h.d.Stdio != nil {
			_ = h.d.Stdio.Stop(id)
		}
	}
	if err := h.d.Store.DeleteServer(r.Context(), c.AccountID, id); err != nil {
		writeErr(w, http.StatusNotFound, "server not found")
		return
	}
	if h.d.Policy != nil {
		h.d.Policy.RemoveServer(id)
	}
	_ = h.d.Store.InsertActivityEvent(r.Context(), c.AccountID, store.ActivityServerRemoval, name,
		"Server '"+name+"' removed", map[string]any{"server_id": id})
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) reindexServer(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")
	srv, err := h.d.Store.GetServer(r.Context(), id)
	if err != nil || srv.AccountID != c.AccountID {
		writeErr(w, http.StatusNotFound, "server not found")
		return
	}
	if h.d.Indexer == nil {
		writeErr(w, http.StatusServiceUnavailable, "indexer unavailable")
		return
	}
	defs, ierr := h.d.Indexer.IndexServer(id)
	if h.d.Policy != nil {
		_ = h.d.Policy.InvalidateServer(r.Context(), h.d.Store, id)
	}
	toolsOut := make([]map[string]any, 0, len(defs))
	for _, t := range defs {
		toolsOut = append(toolsOut, map[string]any{"name": t.Name, "description": t.Description})
	}
	out := map[string]any{
		"id": id, "tool_count": len(toolsOut), "tools": toolsOut, "indexing_error": nil,
	}
	if ierr != "" {
		out["indexing_error"] = ierr
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handlers) serverStatus(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")
	srv, err := h.d.Store.GetServer(r.Context(), id)
	if err != nil || srv.AccountID != c.AccountID {
		writeErr(w, http.StatusNotFound, "server not found")
		return
	}
	out := map[string]any{}
	if h.d.Breaker != nil {
		for k, v := range h.d.Breaker.StatusMap(id) {
			out[k] = v
		}
	} else {
		out["state"] = "closed"
		out["consecutive_failures"] = 0
	}
	if srv.Transport == store.TransportStdio && h.d.Stdio != nil {
		st := h.d.Stdio.StatusSnapshot(id)
		out["transport"] = "stdio"
		out["process"] = st
	} else {
		out["transport"] = "http"
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handlers) checkCommand(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Command string `json:"command"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Command) == "" {
		writeErr(w, http.StatusBadRequest, "command required")
		return
	}
	resolved, err := stdio.ResolveCommand(body.Command)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"found": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"found": true, "resolved": resolved})
}

func (h *handlers) parseMCPJSON(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Raw string `json:"raw"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	entries, err := stdio.ParseMCPJSON(body.Raw)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"servers": entries})
}

func (h *handlers) listKeys(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	period := store.PeriodStartUTC(time.Now().UTC())
	keys, err := h.d.Store.ListAPIKeysByAccount(r.Context(), c.AccountID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list failed")
		return
	}
	out := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		u, _ := h.d.Store.GetUsage(r.Context(), k.ID, period)
		var lastUsed any
		if k.LastUsedAt.Valid {
			lastUsed = k.LastUsedAt.Time.UTC().Format(time.RFC3339Nano)
		}
		out = append(out, map[string]any{
			"id": k.ID, "name": k.Name, "killed": k.KilledAt.Valid,
			"last_used_at": lastUsed, "tokens_used": u.TokensUsed, "monthly_budget": k.MonthlyBudget,
			"created_at": k.CreatedAt.Format(time.RFC3339Nano),
			"owner":      ownerDTO(r.Context(), h.d.Store, k.OwnerID),
			"tool_policy": func() map[string]any {
				p, err := h.d.Store.GetKeyToolPolicy(r.Context(), k.ID)
				if err != nil {
					return map[string]any{"mode": "all", "allowed": []string{}, "count": 0}
				}
				allowed := p.Allowed
				if allowed == nil {
					allowed = []string{}
				}
				return map[string]any{"mode": p.Mode, "allowed": allowed, "count": len(allowed)}
			}(),
			"server_policy": func() map[string]any {
				p, err := h.d.Store.GetKeyServerPolicy(r.Context(), k.ID)
				if err != nil {
					return map[string]any{"mode": "all", "allowed": []string{}, "count": 0}
				}
				allowed := p.Allowed
				if allowed == nil {
					allowed = []string{}
				}
				return map[string]any{"mode": p.Mode, "allowed": allowed, "count": len(allowed)}
			}(),
			"provider_policy": func() map[string]any {
				p, err := h.d.Store.GetKeyProviderPolicy(r.Context(), k.ID)
				if err != nil {
					return map[string]any{"mode": "all", "allowed": []string{}, "count": 0}
				}
				allowed := p.Allowed
				if allowed == nil {
					allowed = []string{}
				}
				return map[string]any{"mode": p.Mode, "allowed": allowed, "count": len(allowed)}
			}(),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handlers) createKey(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	acct, err := h.d.Store.GetAccount(r.Context(), c.AccountID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "account not found")
		return
	}
	n, _ := h.d.Store.CountAPIKeys(r.Context(), c.AccountID)
	caps := h.effectiveCaps(acct)
	if n >= caps.MaxKeys {
		if h.d.Caps.Licensed {
			writeErr(w, http.StatusPaymentRequired, "Plan limit reached. Upgrade to add more API keys.")
		} else {
			writeErr(w, http.StatusPaymentRequired,
				"Free-tier limit reached. Set TOKENCONTROLPLANE_LICENSE_KEY for the licensed build.")
		}
		return
	}
	var body struct {
		Name          string `json:"name"`
		MonthlyBudget int64  `json:"monthly_budget"`
		ServerID      string `json:"server_id"` // preferred server for snippets
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Name == "" {
		body.Name = "default"
	}
	// Free tier gets a hard monthly token budget (bytes/4); 0 remains unlimited on paid plans.
	if body.MonthlyBudget == 0 && caps.Plan == license.PlanFree {
		body.MonthlyBudget = caps.MonthlyTokens
	}
	plain, err := generateGatewayKey()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "keygen failed")
		return
	}
	id := "key_" + uuid.NewString()
	key := store.APIKey{
		ID: id, AccountID: c.AccountID, KeyHash: auth.HashKey(plain), Name: body.Name,
		MonthlyBudget: body.MonthlyBudget, Enabled: true,
		OwnerID: sql.NullString{String: c.UserID, Valid: true},
	}
	if err := h.d.Store.CreateAPIKey(r.Context(), key); err != nil {
		writeErr(w, http.StatusInternalServerError, "create failed")
		return
	}
	h.d.Keys.PutKey(&auth.CachedKey{
		ID: id, AccountID: c.AccountID, KeyHash: key.KeyHash, Name: key.Name, MonthlyBudget: key.MonthlyBudget, Enabled: true,
	})
	_ = h.d.Store.InsertActivityEvent(r.Context(), c.AccountID, store.ActivityKeyCreated, body.Name,
		"API key '"+body.Name+"' created", map[string]any{"key_id": id})

	serverID := body.ServerID
	serverName := "tokencontrolplane"
	if serverID == "" {
		list, _ := h.d.Store.ListServersByAccount(r.Context(), c.AccountID)
		if len(list) > 0 {
			serverID = list[0].ID
			serverName = list[0].Name
		} else {
			serverID = "YOUR_SERVER_ID"
		}
	} else if srv, err := h.d.Store.GetServer(r.Context(), serverID); err == nil {
		serverName = srv.Name
	}
	snips, err := snippets.Build(h.d.GatewayURL, serverID, serverName, plain)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "snippets failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": id, "name": body.Name, "key": plain,
		"snippets": map[string]string{
			"claude_desktop": snips.ClaudeDesktop,
			"cursor":         snips.Cursor,
			"windsurf":       snips.Windsurf,
		},
	})
}

func (h *handlers) deleteKey(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")
	k, err := h.d.Store.GetAPIKey(r.Context(), id)
	if err != nil || k.AccountID != c.AccountID {
		writeErr(w, http.StatusNotFound, "key not found")
		return
	}
	if !h.canWriteKey(c, k) {
		writeErr(w, http.StatusForbidden, "not your key")
		return
	}
	if err := h.d.Store.DeleteAPIKey(r.Context(), c.AccountID, id); err != nil {
		writeErr(w, http.StatusNotFound, "key not found")
		return
	}
	if h.d.Policy != nil {
		h.d.Policy.RemoveKey(id)
	}
	_ = h.d.Keys.Reload(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

// killKey is the emergency fire alarm: any teammate (member or admin) can kill any
// account key. Editing/deleting keys stays owner-or-admin (see canWriteKey).
func (h *handlers) killKey(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")
	k, err := h.d.Store.GetAPIKey(r.Context(), id)
	if err != nil || k.AccountID != c.AccountID {
		writeErr(w, http.StatusNotFound, "key not found")
		return
	}
	if err := h.d.Keys.Kill(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, "kill failed")
		return
	}
	_ = h.d.Store.InsertActivityEvent(r.Context(), c.AccountID, store.ActivityKeyKilledManual, k.Name,
		"Key '"+k.Name+"' killed manually", map[string]any{"key_id": id})
	writeJSON(w, http.StatusOK, map[string]string{"status": "killed"})
}

func (h *handlers) unkillKey(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")
	k, err := h.d.Store.GetAPIKey(r.Context(), id)
	if err != nil || k.AccountID != c.AccountID {
		writeErr(w, http.StatusNotFound, "key not found")
		return
	}
	if !h.canWriteKey(c, k) {
		writeErr(w, http.StatusForbidden, "not your key")
		return
	}
	reset := r.URL.Query().Get("reset") == "true"
	if err := h.d.Keys.Unkill(r.Context(), id, reset); err != nil {
		writeErr(w, http.StatusInternalServerError, "unkill failed")
		return
	}
	_ = h.d.Store.InsertActivityEvent(r.Context(), c.AccountID, store.ActivityKeyUnkilled, k.Name,
		"Key '"+k.Name+"' unkilled", map[string]any{"key_id": id, "reset": reset})
	writeJSON(w, http.StatusOK, map[string]string{"status": "unkilled"})
}

func generateGatewayKey() (string, error) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	const n = 32
	b := make([]byte, n)
	max := big.NewInt(int64(len(alphabet)))
	for i := 0; i < n; i++ {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b[i] = alphabet[v.Int64()]
	}
	return "tcp_" + string(b), nil
}

// effectiveCaps: licensed self-hosted gateways use license.Claims for all accounts;
// dashboard-purchased plans use the account row (PlanCaps from Lemon Squeezy webhook). One code path.
func (h *handlers) effectiveCaps(acct *store.Account) license.Caps {
	if h.d.Caps.Licensed {
		return h.d.Caps
	}
	if acct != nil {
		c := license.CapsForPlan(acct.Plan)
		if acct.MaxSeats > 0 {
			c.MaxSeats = acct.MaxSeats
		}
		if acct.MaxServers > 0 {
			c.MaxServers = acct.MaxServers
		}
		c.MaxKeys = store.PlanMaxKeys(acct.Plan)
		return c
	}
	if h.d.Caps.MaxServers > 0 {
		return h.d.Caps
	}
	return license.FreeCaps()
}

func (h *handlers) onboarding(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	acct, err := h.d.Store.GetAccount(r.Context(), c.AccountID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "account not found")
		return
	}
	servers, _ := h.d.Store.CountServers(r.Context(), c.AccountID)
	keys, _ := h.d.Store.CountAPIKeys(r.Context(), c.AccountID)
	writeJSON(w, http.StatusOK, map[string]any{
		"has_servers": servers > 0,
		"has_keys":    keys > 0,
		"onboarded":   acct.OnboardedAt.Valid,
	})
}

func (h *handlers) completeOnboarding(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	if err := h.d.Store.SetAccountOnboarded(r.Context(), c.AccountID); err != nil {
		writeErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"onboarded": true})
}

func (h *handlers) listActivity(w http.ResponseWriter, r *http.Request) {
	c, _ := session.ClaimsFromContext(r.Context())
	limit := 50
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil {
			limit = n
		}
	}
	events, err := h.d.Store.ListActivityEvents(r.Context(), c.AccountID, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list failed")
		return
	}
	out := make([]map[string]any, 0, len(events))
	for _, e := range events {
		out = append(out, map[string]any{
			"id": e.ID, "kind": e.Kind, "subject": e.Subject, "summary": e.Summary,
			"detail": e.Detail, "created_at": e.CreatedAt.Format(time.RFC3339Nano),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handlers) updateCheck(w http.ResponseWriter, r *http.Request) {
	cur := h.d.Version
	if cur == "" {
		cur = "dev"
	}
	now := time.Now().UTC()
	out := map[string]any{
		"current": stripVAPI(cur), "latest": stripVAPI(cur), "available": false,
		"checked_at": now.Format(time.RFC3339),
	}
	if os.Getenv("NO_UPDATE_CHECK") == "1" {
		writeJSON(w, http.StatusOK, out)
		return
	}
	info, err := h.cachedUpdateCheck(cur)
	if err != nil {
		// Fail silently for air-gap / network errors.
		writeJSON(w, http.StatusOK, out)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"current": info.Current, "latest": info.Latest, "available": info.Available,
		"checked_at": now.Format(time.RFC3339),
	})
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
