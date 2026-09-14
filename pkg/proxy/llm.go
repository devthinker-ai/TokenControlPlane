package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/devthinker-ai/TokenControlPlane/pkg/auth"
	"github.com/devthinker-ai/TokenControlPlane/pkg/policy"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

const (
	llmIdleTimeout = 120 * time.Second // between SSE frames after first byte
	// Depth max 2: primary + one fallback hop (no infinite mutual loops).
	llmMaxFallbackDepth = 2
)

// LLMHandler proxies OpenAI-compatible POST /v1/chat/completions.
type LLMHandler struct {
	store  *store.Store
	meter  *Meter
	policy *policy.Cache

	denialMu    sync.Mutex
	denialLast  map[string]time.Time
	idleTimeout time.Duration // overridable in tests
}

// NewLLMHandler creates the LLM chat-completions proxy.
func NewLLMHandler(st *store.Store, m *Meter, pol *policy.Cache) *LLMHandler {
	return &LLMHandler{
		store:       st,
		meter:       m,
		policy:      pol,
		denialLast:  map[string]time.Time{},
		idleTimeout: llmIdleTimeout,
	}
}

type chatRequestMeta struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
}

type usageBlob struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

func (u usageBlob) total() int64 {
	if u.TotalTokens > 0 {
		return u.TotalTokens
	}
	return u.PromptTokens + u.CompletionTokens
}

type llmRoute struct {
	Model    *store.LLMModel
	Provider *store.LLMProvider
	Alias    string
}

type resolveError struct {
	status int
	msg    string
}

func (e *resolveError) Error() string { return e.msg }

type forwardResult struct {
	bytesIn      int64
	bytesOut     int64
	exactTokens  int64
	bytesStarted bool
	status       int
}

// ServeHTTP handles POST /v1/chat/completions.
func (h *LLMHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	key, ok := auth.KeyFromContext(r.Context())
	if !ok {
		auth.WriteErrorJSON(w, http.StatusUnauthorized, "missing key")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		auth.WriteErrorJSON(w, http.StatusBadRequest, "read body failed")
		return
	}
	_ = r.Body.Close()

	var meta chatRequestMeta
	if err := json.Unmarshal(body, &meta); err != nil || meta.Model == "" {
		auth.WriteErrorJSON(w, http.StatusBadRequest, "model required")
		return
	}

	route, err := h.resolveRoute(r.Context(), key, meta.Model)
	if err != nil {
		h.writeResolveError(w, err)
		return
	}

	h.proxyWithFallback(w, r, key, body, meta.Stream, route, 0)
}

func (h *LLMHandler) writeResolveError(w http.ResponseWriter, err error) {
	if re, ok := err.(*resolveError); ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(re.status)
		msg, _ := json.Marshal(re.msg)
		_, _ = w.Write([]byte(`{"error":` + string(msg) + `}`))
		return
	}
	auth.WriteErrorJSON(w, http.StatusInternalServerError, "route resolve failed")
}

func (h *LLMHandler) resolveRoute(ctx context.Context, key *auth.CachedKey, modelName string) (*llmRoute, error) {
	m, err := h.store.GetLLMModelByName(ctx, key.AccountID, modelName)
	if err == nil {
		return h.routeFromModel(ctx, key, m, modelName)
	}

	// Pass-through only when the key has exactly one granted enabled provider.
	pp, _ := h.store.GetKeyProviderPolicy(ctx, key.ID)
	providers, err := h.store.ListGrantedProvidersForKey(ctx, key.ID, key.AccountID, pp.Mode)
	if err != nil {
		return nil, err
	}
	enabled := make([]store.LLMProvider, 0, len(providers))
	for _, p := range providers {
		if p.Enabled {
			enabled = append(enabled, p)
		}
	}
	if len(enabled) == 1 {
		p := enabled[0]
		if found, ferr := h.store.FindLLMModelByUpstreamID(ctx, key.AccountID, modelName); ferr == nil && found.ProviderID == p.ID {
			return h.routeFromModel(ctx, key, found, modelName)
		}
		synth := &store.LLMModel{
			AccountID:  key.AccountID,
			Name:       modelName,
			Model:      modelName,
			ProviderID: p.ID,
		}
		return h.routeFromModel(ctx, key, synth, modelName)
	}

	return nil, &resolveError{
		status: http.StatusNotFound,
		msg:    fmt.Sprintf("model '%s' not found — configure a route in /v1/llm/models", modelName),
	}
}

func (h *LLMHandler) routeFromModel(ctx context.Context, key *auth.CachedKey, m *store.LLMModel, alias string) (*llmRoute, error) {
	p, err := h.store.GetLLMProvider(ctx, m.ProviderID)
	if err != nil {
		return nil, &resolveError{status: http.StatusNotFound, msg: "provider not found"}
	}
	if p.AccountID != key.AccountID {
		return nil, &resolveError{status: http.StatusNotFound, msg: "provider not found"}
	}
	if !p.Enabled {
		return nil, &resolveError{status: http.StatusForbidden, msg: "provider disabled"}
	}
	if h.policy != nil && !h.policy.AllowedProvider(key.ID, p.ID) {
		h.emitProviderDenied(p, key)
		return nil, &resolveError{status: http.StatusForbidden, msg: "provider not allowed for this key"}
	}
	return &llmRoute{Model: m, Provider: p, Alias: alias}, nil
}

func (h *LLMHandler) proxyWithFallback(w http.ResponseWriter, r *http.Request, key *auth.CachedKey, body []byte, stream bool, route *llmRoute, depth int) {
	if err := h.checkProviderBudget(key, route.Provider); err != nil {
		h.writeResolveError(w, err)
		return
	}

	rewritten, err := rewriteModelField(body, route.Model.Model)
	if err != nil {
		auth.WriteErrorJSON(w, http.StatusBadRequest, "invalid request body")
		return
	}

	result, ferr := h.doProxy(w, r, rewritten, stream, route)
	if ferr != nil && (result == nil || !result.bytesStarted) {
		if depth+1 < llmMaxFallbackDepth && route.Model.FallbackModelID.Valid && route.Model.FallbackModelID.String != "" {
			fb, err := h.store.GetLLMModel(r.Context(), route.Model.FallbackModelID.String)
			if err == nil && fb.AccountID == key.AccountID {
				next, rerr := h.routeFromModel(r.Context(), key, fb, route.Alias)
				if rerr == nil {
					h.emitFallback(key, route, next, ferr.Error())
					h.proxyWithFallback(w, r, key, body, stream, next, depth+1)
					return
				}
			}
		}
		status := http.StatusBadGateway
		if isTimeout(ferr) {
			status = http.StatusGatewayTimeout
		}
		auth.WriteErrorJSON(w, status, ferr.Error())
		return
	}

	// Mid-stream failure: client already has partial bytes; meter what we got.
	if h.meter != nil && result != nil {
		useExact := result.exactTokens > 0
		h.meter.RecordForProvider(key.ID, route.Provider.ID, result.bytesIn, result.bytesOut, 1, result.exactTokens, useExact)
	}
}

func (h *LLMHandler) checkProviderBudget(key *auth.CachedKey, p *store.LLMProvider) error {
	if h.policy == nil {
		return nil
	}
	b := h.policy.ProviderBudget(key.ID, p.ID)
	if b <= 0 {
		return nil
	}
	used := int64(0)
	if key.ProviderTokensUsed != nil {
		used = key.ProviderTokensUsed[p.ID]
	}
	if used >= b {
		h.emitProviderBudgetExceeded(p, key, used, b)
		return &resolveError{
			status: http.StatusPaymentRequired,
			msg:    fmt.Sprintf("Per-provider budget limit exceeded for '%s'. Access suspended by TokenControlPlane.", p.Name),
		}
	}
	return nil
}

func (h *LLMHandler) doProxy(w http.ResponseWriter, r *http.Request, body []byte, stream bool, route *llmRoute) (*forwardResult, error) {
	p := route.Provider
	timeout := time.Duration(p.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	idle := h.idleTimeout
	if idle <= 0 {
		idle = llmIdleTimeout
	}

	upURL := strings.TrimRight(p.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for k, vals := range r.Header {
		lk := strings.ToLower(k)
		if lk == "authorization" || lk == "host" || lk == "content-length" || lk == "connection" || lk == "transfer-encoding" {
			continue
		}
		for _, v := range vals {
			req.Header.Add(k, v)
		}
	}
	req.Header.Set("Content-Type", "application/json")
	if p.AuthValue != "" {
		req.Header.Set(p.AuthHeader, p.AuthValue)
	}
	req.ContentLength = int64(len(body))

	client := &http.Client{
		Timeout: 0,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   timeout,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ResponseHeaderTimeout: timeout, // connect + TTFB
			IdleConnTimeout:       90 * time.Second,
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	result := &forwardResult{bytesIn: int64(len(body)), status: resp.StatusCode}

	// Pre-stream 5xx → fallback eligible (no client bytes yet).
	if resp.StatusCode >= 500 {
		return result, fmt.Errorf("upstream status %d", resp.StatusCode)
	}

	for k, vals := range resp.Header {
		lk := strings.ToLower(k)
		if lk == "connection" || lk == "transfer-encoding" || lk == "content-length" {
			continue
		}
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	result.bytesStarted = true

	ct := resp.Header.Get("Content-Type")
	if stream || strings.Contains(ct, "text/event-stream") {
		return h.copyStream(w, resp.Body, result, idle)
	}
	return h.copyBuffered(w, resp.Body, result)
}

func (h *LLMHandler) copyBuffered(w http.ResponseWriter, body io.Reader, result *forwardResult) (*forwardResult, error) {
	buf, err := io.ReadAll(body)
	if err != nil {
		return result, err
	}
	if t, ok := parseUsageFromJSON(buf); ok {
		result.exactTokens = t
	}
	n, werr := w.Write(buf)
	result.bytesOut = int64(n)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	return result, werr
}

func (h *LLMHandler) copyStream(w http.ResponseWriter, body io.Reader, result *forwardResult, idle time.Duration) (*forwardResult, error) {
	flusher, _ := w.(http.Flusher)
	var tee bytes.Buffer
	reader := io.TeeReader(body, &tee)
	buf := make([]byte, 32*1024)

	for {
		type readN struct {
			n   int
			err error
		}
		ch := make(chan readN, 1)
		go func() {
			n, err := reader.Read(buf)
			ch <- readN{n, err}
		}()

		var rn readN
		select {
		case rn = <-ch:
		case <-time.After(idle):
			// Mid-stream idle — no fallback; client has partial bytes.
			if t, ok := parseUsageFromSSE(tee.Bytes()); ok {
				result.exactTokens = t
			}
			return result, fmt.Errorf("idle timeout")
		}

		if rn.n > 0 {
			wn, werr := w.Write(buf[:rn.n])
			result.bytesOut += int64(wn)
			if flusher != nil {
				flusher.Flush()
			}
			if werr != nil {
				if t, ok := parseUsageFromSSE(tee.Bytes()); ok {
					result.exactTokens = t
				}
				return result, werr
			}
		}
		if rn.err != nil {
			if t, ok := parseUsageFromSSE(tee.Bytes()); ok {
				result.exactTokens = t
			}
			if rn.err == io.EOF {
				return result, nil
			}
			return result, rn.err
		}
	}
}

func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return true
	}
	s := err.Error()
	return strings.Contains(s, "timeout") || strings.Contains(s, "DeadlineExceeded") || strings.Contains(s, "Client.Timeout")
}

func rewriteModelField(body []byte, upstreamModel string) ([]byte, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	b, err := json.Marshal(upstreamModel)
	if err != nil {
		return nil, err
	}
	raw["model"] = b
	return json.Marshal(raw)
}

func parseUsageFromJSON(body []byte) (int64, bool) {
	var wrap struct {
		Usage *usageBlob `json:"usage"`
	}
	if err := json.Unmarshal(body, &wrap); err != nil || wrap.Usage == nil {
		return 0, false
	}
	t := wrap.Usage.total()
	if t <= 0 {
		return 0, false
	}
	return t, true
}

func parseUsageFromSSE(data []byte) (int64, bool) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var lastUsage int64
	found := false
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var wrap struct {
			Usage *usageBlob `json:"usage"`
		}
		if err := json.Unmarshal([]byte(payload), &wrap); err != nil {
			continue
		}
		if wrap.Usage != nil {
			if t := wrap.Usage.total(); t > 0 {
				lastUsage = t
				found = true
			}
		}
	}
	return lastUsage, found
}

func (h *LLMHandler) emitFallback(key *auth.CachedKey, from, to *llmRoute, reason string) {
	_ = h.store.InsertActivityEvent(context.Background(), key.AccountID, store.ActivityLLMFallback, from.Provider.Name,
		fmt.Sprintf("%s down → fell back to %s", from.Provider.Name, to.Provider.Name),
		map[string]any{
			"from": from.Provider.ID, "to": to.Provider.ID,
			"from_name": from.Provider.Name, "to_name": to.Provider.Name,
			"reason": reason,
		})
}

func (h *LLMHandler) emitProviderDenied(p *store.LLMProvider, key *auth.CachedKey) {
	if !h.throttle("provider_deny:"+key.ID+":"+p.ID) {
		return
	}
	_ = h.store.InsertActivityEvent(context.Background(), key.AccountID, store.ActivityProviderScopeDenied, p.Name,
		"Key '"+key.Name+"' denied access to provider '"+p.Name+"'",
		map[string]any{"key_id": key.ID, "provider_id": p.ID})
}

func (h *LLMHandler) emitProviderBudgetExceeded(p *store.LLMProvider, key *auth.CachedKey, used, budget int64) {
	if !h.throttle("provider_budget:"+key.ID+":"+p.ID) {
		return
	}
	_ = h.store.InsertActivityEvent(context.Background(), key.AccountID, store.ActivityProviderBudgetExceeded, p.Name,
		fmt.Sprintf("Key '%s' hit per-provider budget on '%s'", key.Name, p.Name),
		map[string]any{"key_id": key.ID, "provider_id": p.ID, "tokens_used": used, "budget": budget})
}

func (h *LLMHandler) throttle(k string) bool {
	h.denialMu.Lock()
	defer h.denialMu.Unlock()
	now := time.Now()
	if t, ok := h.denialLast[k]; ok && now.Sub(t) < 10*time.Minute {
		return false
	}
	h.denialLast[k] = now
	return true
}
