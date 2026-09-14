package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/devthinker-ai/TokenControlPlane/pkg/auth"
	"github.com/devthinker-ai/TokenControlPlane/pkg/policy"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

// denialThrottle limits tool_call_denied activity events (1 per key+tool / 10min).
var denialThrottle sync.Map // key: "keyID\0tool" → time.Time

type rpcMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

type callParams struct {
	Name string `json:"name"`
}

// parseRPCBody returns either a single message or a batch.
func parseRPCBody(body []byte) (single *rpcMsg, batch []rpcMsg, ok bool) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil, nil, false
	}
	if body[0] == '[' {
		var b []rpcMsg
		if err := json.Unmarshal(body, &b); err != nil {
			return nil, nil, false
		}
		return nil, b, true
	}
	var m rpcMsg
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, nil, false
	}
	return &m, nil, true
}

func toolNameFromParams(params json.RawMessage) string {
	var p callParams
	_ = json.Unmarshal(params, &p)
	return p.Name
}

func denyRPCError(id json.RawMessage, toolName string) []byte {
	if len(id) == 0 {
		id = []byte("null")
	}
	var buf bytes.Buffer
	buf.WriteString(`{"jsonrpc":"2.0","id":`)
	buf.Write(id)
	buf.WriteString(`,"error":{"code":-32602,"message":`)
	msg, _ := json.Marshal("tool '" + toolName + "' is not available for this API key")
	buf.Write(msg)
	buf.WriteString(`}}`)
	return buf.Bytes()
}

func writeJSONRPC(w http.ResponseWriter, r *http.Request, body []byte) {
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "text/event-stream") && !strings.Contains(accept, "application/json") {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: " + string(body) + "\n\n"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (h *Handler) checkToolCallPolicy(w http.ResponseWriter, r *http.Request, srv *store.MCPServer, body []byte) (denied bool) {
	if h.policy == nil || len(body) == 0 || len(body) > 1<<20 {
		return false
	}
	key, ok := auth.KeyFromContext(r.Context())
	if !ok {
		return false
	}
	single, batch, parsed := parseRPCBody(body)
	if !parsed {
		return false
	}

	if single != nil {
		if single.Method != "tools/call" {
			return false
		}
		name := toolNameFromParams(single.Params)
		if name == "" || h.policy.Allowed(srv.ID, key.ID, name) {
			return false
		}
		h.emitToolDenied(srv, key, name)
		writeJSONRPC(w, r, denyRPCError(single.ID, name))
		return true
	}

	// Batch: short-circuit entire request if any denied? Spec says: one result/error
	// per item, order preserved, allowed items still proxied.
	// That requires splitting the batch — complex for ReverseProxy.
	// Approach: if ANY denied, handle the whole batch ourselves for denied items
	// and only proxy if ALL allowed. For mixed batches, respond locally by
	// synthesizing errors for denied and leaving allowed to… still need upstream.
	//
	// Practical approach matching the prompt: if the batch has any denied tools,
	// we cannot use ReverseProxy for partial. For v1: if all allowed → proxy;
	// if any denied and all are tools/call → respond with mixed array without
	// upstream for denied, and for allowed we'd need upstream fan-out.
	//
	// Simpler correct approach for mixed: deny the whole batch item-by-item by
	// not calling upstream for denied ones. For allowed ones, we still need upstream.
	// The prompt says "allowed items still proxied" — so we must fan out.
	// Implementation: if batch has denials, handle entirely here by calling upstream
	// only for allowed items (or if no HTTP client, synthesize).
	hasCall := false
	anyDenied := false
	for _, m := range batch {
		if m.Method == "tools/call" {
			hasCall = true
			name := toolNameFromParams(m.Params)
			if name != "" && !h.policy.Allowed(srv.ID, key.ID, name) {
				anyDenied = true
			}
		}
	}
	if !hasCall || !anyDenied {
		return false
	}

	// Mixed/denied batch: respond item-by-item without ReverseProxy for denied;
	// for allowed tools/call we still need upstream — use a one-shot proxy round-trip
	// per allowed item. Keep it simple: build response array.
	results := make([]json.RawMessage, len(batch))
	var allowedBatch []rpcMsg
	var allowedIdx []int
	for i, m := range batch {
		if m.Method == "tools/call" {
			name := toolNameFromParams(m.Params)
			if name != "" && !h.policy.Allowed(srv.ID, key.ID, name) {
				h.emitToolDenied(srv, key, name)
				results[i] = denyRPCError(m.ID, name)
				continue
			}
		}
		allowedBatch = append(allowedBatch, m)
		allowedIdx = append(allowedIdx, i)
	}
	if len(allowedBatch) > 0 {
		upBody, _ := json.Marshal(allowedBatch)
		upResp, err := h.forwardRaw(r, srv, upBody)
		if err != nil {
			for _, i := range allowedIdx {
				results[i] = json.RawMessage(`{"jsonrpc":"2.0","id":null,"error":{"code":-32000,"message":"upstream unavailable"}}`)
			}
		} else {
			var upBatch []json.RawMessage
			if err := json.Unmarshal(upResp, &upBatch); err != nil {
				// single object response for single-item batch
				if len(allowedIdx) == 1 {
					results[allowedIdx[0]] = upResp
				}
			} else {
				for j, i := range allowedIdx {
					if j < len(upBatch) {
						results[i] = upBatch[j]
					}
				}
			}
		}
	}
	out, _ := json.Marshal(results)
	writeJSONRPC(w, r, out)
	return true
}

// forwardRaw POSTs a body to the upstream server URL (used for batch partial proxy).
func (h *Handler) forwardRaw(r *http.Request, srv *store.MCPServer, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, srv.BaseURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	// Inject auth like director
	if isOAuthServer(srv) && h.guard != nil {
		if t, err := h.guard.TokenFor(r.Context(), srv.AccountID, srv.ID); err == nil {
			if val, herr := h.guard.Header(t); herr == nil {
				req.Header.Set("Authorization", val)
			}
		}
	} else if srv.AuthValue != "" {
		header := srv.AuthHeader
		if header == "" {
			header = "Authorization"
		}
		val := srv.AuthValue
		if strings.EqualFold(header, "Authorization") && !strings.Contains(val, " ") {
			val = "Bearer " + val
		}
		req.Header.Set(header, val)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

func (h *Handler) emitToolDenied(srv *store.MCPServer, key *auth.CachedKey, toolName string) {
	throttleKey := key.ID + "\x00" + toolName
	if v, ok := denialThrottle.Load(throttleKey); ok {
		if time.Since(v.(time.Time)) < 10*time.Minute {
			return
		}
	}
	denialThrottle.Store(throttleKey, time.Now())
	_ = h.store.InsertActivityEvent(context.Background(), srv.AccountID, store.ActivityToolCallDenied, toolName,
		"Tool '"+toolName+"' denied for key '"+key.Name+"'",
		map[string]any{"server_id": srv.ID, "key_id": key.ID, "tool": toolName})
}

// filterToolsListResponse filters result.tools[] in a JSON or SSE response body.
func filterToolsListBody(cache *policy.Cache, serverID, keyID string, body []byte, contentType string) ([]byte, bool) {
	if cache == nil || len(body) == 0 {
		return body, false
	}
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "text/event-stream") {
		return filterToolsListSSE(cache, serverID, keyID, body)
	}
	return filterToolsListJSON(cache, serverID, keyID, body)
}

func filterToolsListJSON(cache *policy.Cache, serverID, keyID string, body []byte) ([]byte, bool) {
	var msg map[string]any
	if err := json.Unmarshal(body, &msg); err != nil {
		return body, false
	}
	result, _ := msg["result"].(map[string]any)
	if result == nil {
		return body, false
	}
	tools, ok := result["tools"].([]any)
	if !ok {
		return body, false
	}
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		if m, ok := t.(map[string]any); ok {
			if n, _ := m["name"].(string); n != "" {
				names = append(names, n)
			}
		}
	}
	allowed := cache.FilterToolsList(serverID, keyID, names)
	filtered := make([]any, 0, len(tools))
	for _, t := range tools {
		m, ok := t.(map[string]any)
		if !ok {
			continue
		}
		n, _ := m["name"].(string)
		if _, ok := allowed[n]; ok {
			filtered = append(filtered, t)
		}
	}
	result["tools"] = filtered
	msg["result"] = result
	out, err := json.Marshal(msg)
	if err != nil {
		return body, false
	}
	return out, true
}

func filterToolsListSSE(cache *policy.Cache, serverID, keyID string, body []byte) ([]byte, bool) {
	// Walk SSE frames; filter only the data frame that carries tools/list result.
	const maxBuf = 4 << 20
	if len(body) > maxBuf {
		return body, false
	}
	parts := bytes.Split(body, []byte("\n\n"))
	changed := false
	var out bytes.Buffer
	for i, part := range parts {
		if i > 0 {
			out.WriteString("\n\n")
		}
		if !bytes.Contains(part, []byte("\"tools\"")) {
			out.Write(part)
			continue
		}
		// Extract data: lines
		lines := bytes.Split(part, []byte("\n"))
		var dataBuf bytes.Buffer
		var other [][]byte
		for _, line := range lines {
			if bytes.HasPrefix(line, []byte("data:")) {
				payload := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
				if dataBuf.Len() > 0 {
					dataBuf.WriteByte('\n')
				}
				dataBuf.Write(payload)
			} else {
				other = append(other, line)
			}
		}
		if dataBuf.Len() == 0 {
			out.Write(part)
			continue
		}
		filtered, ok := filterToolsListJSON(cache, serverID, keyID, dataBuf.Bytes())
		if !ok {
			out.Write(part)
			continue
		}
		changed = true
		for _, line := range other {
			out.Write(line)
			out.WriteByte('\n')
		}
		out.WriteString("data: ")
		out.Write(filtered)
	}
	if !changed {
		return body, false
	}
	return out.Bytes(), true
}

// peekMethod returns the JSON-RPC method from a buffered body (single message only).
func peekMethod(body []byte) string {
	single, _, ok := parseRPCBody(body)
	if !ok || single == nil {
		return ""
	}
	return single.Method
}
