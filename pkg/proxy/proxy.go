package proxy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/devthinker-ai/TokenControlPlane/pkg/auth"
	"github.com/devthinker-ai/TokenControlPlane/pkg/auth/upstream"
	"github.com/devthinker-ai/TokenControlPlane/pkg/policy"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
	"github.com/devthinker-ai/TokenControlPlane/pkg/upstream/stdio"
)

const circuitOpenBody = `{"error":"Upstream server unavailable (circuit open). Retry shortly."}`
const oauthExpiredBody = `{"error":"upstream oauth expired — reconnect"}`

// Handler proxies /mcp/{server_id}/* to the configured upstream MCP server.
type Handler struct {
	store   *store.Store
	breaker *Breaker
	guard   *upstream.TokenGuard
	policy  *policy.Cache
	stdio   *stdio.Manager
}

// NewHandler creates a proxy handler with an optional circuit breaker.
func NewHandler(s *store.Store, b *Breaker) *Handler {
	if b == nil {
		b = NewBreaker()
	}
	return &Handler{store: s, breaker: b}
}

// SetTokenGuard injects the shared OAuth token guard.
func (h *Handler) SetTokenGuard(g *upstream.TokenGuard) {
	h.guard = g
}

// SetPolicyCache injects the tool policy cache (11a).
func (h *Handler) SetPolicyCache(c *policy.Cache) {
	h.policy = c
}

// SetStdioManager injects the local stdio process manager (11b).
func (h *Handler) SetStdioManager(m *stdio.Manager) {
	h.stdio = m
}

// ServeHTTP looks up the upstream and reverse-proxies the request.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "server_id")
	if serverID == "" {
		http.Error(w, `{"error":"missing server_id"}`, http.StatusBadRequest)
		return
	}

	srv, err := h.store.GetServer(r.Context(), serverID)
	if err != nil {
		http.Error(w, `{"error":"server not found"}`, http.StatusNotFound)
		return
	}
	if !srv.Enabled {
		// WHY 403 before 503: disabled server beats circuit-open (precedence layer 2).
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"server disabled"}`))
		return
	}

	// Server-scope gate (11a): before circuit / upstream.
	if key, ok := auth.KeyFromContext(r.Context()); ok && h.policy != nil {
		if !h.policy.AllowedServer(key.ID, serverID) {
			h.emitServerScopeDenied(srv, key)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"API key is not authorized for server '` + srv.Name + `'"}`))
			return
		}
		// Per-server budget pre-flight (402; does not kill the key).
		if b := h.policy.ServerBudget(key.ID, serverID); b > 0 {
			used := int64(0)
			if key.ServerTokensUsed != nil {
				used = key.ServerTokensUsed[serverID]
			}
			if used >= b {
				h.emitServerBudgetExceeded(srv, key, used, b)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusPaymentRequired)
				_, _ = w.Write([]byte(`{"error":"Per-server budget limit exceeded for '` + srv.Name + `'. Access suspended by TokenControlPlane."}`))
				return
			}
		}
	}

	// WHY 503 after auth/budget: circuit is an upstream-health signal, not a client fault.
	if !h.breaker.Allow(serverID) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(circuitOpenBody))
		return
	}

	// Stdio bridge — not ReverseProxy (local subprocess).
	if srv.Transport == store.TransportStdio {
		var reqBody []byte
		if r.Body != nil {
			reqBody, _ = io.ReadAll(io.LimitReader(r.Body, 1<<20+1))
			_ = r.Body.Close()
		}
		// Policy is enforced inside the bridge (incl. batch) — do not use
		// checkToolCallPolicy's HTTP forwardRaw path here.
		if h.stdio == nil {
			http.Error(w, `{"error":"stdio manager unavailable"}`, http.StatusServiceUnavailable)
			return
		}
		stdio.BridgeDeps{Manager: h.stdio, Policy: h.policy, Store: h.store}.ServeHTTP(w, r, srv, reqBody)
		return
	}

	// Pre-flight OAuth: fail with 401 (not 500) when token is expired.
	if isOAuthServer(srv) {
		if h.guard == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(oauthExpiredBody))
			return
		}
		if _, err := h.guard.TokenFor(r.Context(), srv.AccountID, srv.ID); errors.Is(err, upstream.ErrExpired) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(oauthExpiredBody))
			return
		} else if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(oauthExpiredBody))
			return
		}
	}

	// Buffer body for tool-policy gate (≤1MB); restore for ReverseProxy.
	var reqBody []byte
	if r.Body != nil {
		reqBody, _ = io.ReadAll(io.LimitReader(r.Body, 1<<20+1))
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(reqBody))
	}
	if h.checkToolCallPolicy(w, r, srv, reqBody) {
		return // short-circuit — no upstream
	}
	// Restore body after check consumed nothing (check only reads the slice).
	r.Body = io.NopCloser(bytes.NewReader(reqBody))

	target, err := url.Parse(srv.BaseURL)
	if err != nil || target.Scheme == "" || target.Host == "" {
		http.Error(w, `{"error":"invalid upstream URL"}`, http.StatusInternalServerError)
		return
	}

	rpcMethod := peekMethod(reqBody)
	keyID := ""
	if key, ok := auth.KeyFromContext(r.Context()); ok {
		keyID = key.ID
	}

	proxy := &httputil.ReverseProxy{
		Director:      h.director(srv, target),
		FlushInterval: -1, // WHY -1: SSE frames must reach the client per-write (no 4KB buffering).
		ModifyResponse: func(resp *http.Response) error {
			// tools/list filter (11a) — map lookups only on hot path.
			if rpcMethod == "tools/list" && h.policy != nil && keyID != "" && resp.Body != nil {
				raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20+1))
				_ = resp.Body.Close()
				if err == nil && len(raw) <= 4<<20 {
					if filtered, ok := filterToolsListBody(h.policy, srv.ID, keyID, raw, resp.Header.Get("Content-Type")); ok {
						resp.Body = io.NopCloser(bytes.NewReader(filtered))
						resp.ContentLength = int64(len(filtered))
						resp.Header.Set("Content-Length", strconv.Itoa(len(filtered)))
						raw = filtered
					} else {
						resp.Body = io.NopCloser(bytes.NewReader(raw))
						resp.ContentLength = int64(len(raw))
					}
				} else {
					resp.Body = io.NopCloser(bytes.NewReader(raw))
				}
			}
			// OAuth: on upstream 401, force-refresh once.
			if resp.StatusCode == http.StatusUnauthorized && isOAuthServer(srv) && h.guard != nil {
				if _, err := h.guard.ForceRefresh(context.Background(), srv.AccountID, srv.ID); err != nil {
					resp.Header.Set("Content-Type", "application/json")
					resp.Body = io.NopCloser(strings.NewReader(oauthExpiredBody))
					resp.ContentLength = int64(len(oauthExpiredBody))
				}
			}
			// 5xx = upstream failure; 4xx does NOT count (client/protocol errors).
			if resp.StatusCode >= 500 {
				if h.breaker.RecordFailure(serverID) {
					h.emitCircuitOpen(srv, resp.StatusCode)
				}
			} else {
				if h.breaker.RecordSuccess(serverID) {
					h.emitCircuitClosed(srv)
				}
			}
			return nil
		},
		ErrorHandler: func(rw http.ResponseWriter, req *http.Request, e error) {
			if isClientDisconnect(req.Context(), e) {
				return
			}
			if h.breaker.RecordFailure(serverID) {
				h.emitCircuitOpen(srv, 0)
			}
			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(http.StatusBadGateway)
			_, _ = rw.Write([]byte(`{"error":"upstream unavailable"}`))
		},
	}
	proxy.ServeHTTP(w, r)
}

func isOAuthServer(srv *store.MCPServer) bool {
	return srv.AuthType == store.AuthTypeOAuthDevice || srv.AuthType == store.AuthTypeOAuthPKCE
}

func (h *Handler) emitCircuitOpen(srv *store.MCPServer, status int) {
	detail := map[string]any{"server_id": srv.ID}
	summary := "Circuit opened for '" + srv.Name + "'"
	if status > 0 {
		detail["upstream_status"] = status
		summary += " (upstream " + strconv.Itoa(status) + ")"
	}
	_ = h.store.InsertActivityEvent(context.Background(), srv.AccountID, store.ActivityCircuitOpen, srv.Name, summary, detail)
}

func (h *Handler) emitCircuitClosed(srv *store.MCPServer) {
	_ = h.store.InsertActivityEvent(context.Background(), srv.AccountID, store.ActivityCircuitClosed, srv.Name,
		"Circuit closed for '"+srv.Name+"' — upstream recovered",
		map[string]any{"server_id": srv.ID})
}

func (h *Handler) emitServerScopeDenied(srv *store.MCPServer, key *auth.CachedKey) {
	throttleKey := key.ID + "\x00scope\x00" + srv.ID
	if v, ok := denialThrottle.Load(throttleKey); ok {
		if time.Since(v.(time.Time)) < 10*time.Minute {
			return
		}
	}
	denialThrottle.Store(throttleKey, time.Now())
	_ = h.store.InsertActivityEvent(context.Background(), srv.AccountID, store.ActivityServerScopeDenied, srv.Name,
		"Key '"+key.Name+"' denied for server '"+srv.Name+"'",
		map[string]any{"server_id": srv.ID, "key_id": key.ID})
}

func (h *Handler) emitServerBudgetExceeded(srv *store.MCPServer, key *auth.CachedKey, used, budget int64) {
	throttleKey := key.ID + "\x00sbudget\x00" + srv.ID
	if v, ok := denialThrottle.Load(throttleKey); ok {
		if time.Since(v.(time.Time)) < 10*time.Minute {
			return
		}
	}
	denialThrottle.Store(throttleKey, time.Now())
	_ = h.store.InsertActivityEvent(context.Background(), srv.AccountID, store.ActivityServerBudgetExceeded, srv.Name,
		"Per-server budget exceeded for '"+srv.Name+"' on key '"+key.Name+"'",
		map[string]any{"server_id": srv.ID, "key_id": key.ID, "tokens_used": used, "budget": budget})
}

func isClientDisconnect(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "client disconnected") ||
		strings.Contains(msg, "connection reset by peer") && strings.Contains(msg, "read")
}

// director rewrites the outbound request toward the upstream MCP server.
func (h *Handler) director(srv *store.MCPServer, target *url.URL) func(*http.Request) {
	return func(req *http.Request) {
		serverID := chi.URLParam(req, "server_id")
		prefix := "/mcp/" + serverID
		remainder := strings.TrimPrefix(req.URL.Path, prefix)
		if remainder == "" {
			remainder = "/"
		}

		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.URL.Path = singleJoiningSlash(target.Path, remainder)
		req.URL.RawPath = ""
		req.Host = target.Host

		req.Header.Del("Authorization")

		if isOAuthServer(srv) && h.guard != nil {
			t, err := h.guard.TokenFor(req.Context(), srv.AccountID, srv.ID)
			if err == nil {
				if val, herr := h.guard.Header(t); herr == nil {
					req.Header.Set("Authorization", val)
				}
			}
			return
		}

		authHeader := srv.AuthHeader
		if authHeader == "" {
			authHeader = "Authorization"
		}
		if srv.AuthValue != "" {
			val := srv.AuthValue
			if strings.EqualFold(authHeader, "Authorization") && !strings.Contains(val, " ") {
				val = "Bearer " + val
			}
			req.Header.Set(authHeader, val)
		}
	}
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		if a == "" {
			return b
		}
		return a + "/" + b
	}
	return a + b
}
