package stdio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/devthinker-ai/TokenControlPlane/pkg/auth"
	"github.com/devthinker-ai/TokenControlPlane/pkg/policy"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

// BridgeDeps are injected by the proxy handler for stdio-backed /mcp/{id}.
type BridgeDeps struct {
	Manager *Manager
	Policy  *policy.Cache
	Store   *store.Store
}

// ServeHTTP bridges streamable-HTTP JSON-RPC to a local stdio MCP process.
// Prefer application/json; wrap in one SSE data frame only when Accept is
// event-stream without json (documented simplification).
func (d BridgeDeps) ServeHTTP(w http.ResponseWriter, r *http.Request, srv *store.MCPServer, body []byte) {
	if d.Manager == nil {
		http.Error(w, `{"error":"stdio manager unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	keyID := ""
	if key, ok := auth.KeyFromContext(r.Context()); ok {
		keyID = key.ID
	}

	single, batch, ok := parseRPCBody(body)
	if !ok {
		writeRPC(w, r, rpcError(nil, -32700, "parse error"))
		return
	}
	if single != nil {
		writeRPC(w, r, d.dispatch(r.Context(), srv, keyID, *single))
		return
	}
	out := make([]json.RawMessage, 0, len(batch))
	for _, m := range batch {
		out = append(out, d.dispatch(r.Context(), srv, keyID, m))
	}
	b, _ := json.Marshal(out)
	writeRPC(w, r, b)
}

func (d BridgeDeps) dispatch(ctx context.Context, srv *store.MCPServer, keyID string, msg rpcMsg) json.RawMessage {
	id := msg.ID
	method := msg.Method

	switch {
	case method == "initialize":
		res, err := d.Manager.InitResult(ctx, srv.ID)
		if err != nil {
			return rpcError(id, -32000, err.Error())
		}
		return rpcResult(id, res)

	case method == "notifications/initialized" || strings.HasPrefix(method, "notifications/"):
		// Stdio child already initialized; ack notifications with empty body (no response for notifs).
		if len(id) == 0 || string(id) == "null" {
			return nil
		}
		return rpcResult(id, map[string]any{})

	case method == "ping":
		if err := d.Manager.Ping(ctx, srv.ID); err != nil {
			return rpcError(id, -32000, err.Error())
		}
		return rpcResult(id, map[string]any{})

	case method == "tools/list":
		tools, err := d.Manager.ListTools(ctx, srv.ID)
		if err != nil {
			return rpcError(id, -32000, err.Error())
		}
		names := make([]string, 0, len(tools))
		for _, t := range tools {
			names = append(names, t.Name)
		}
		allowed := map[string]struct{}{}
		if d.Policy != nil && keyID != "" {
			allowed = d.Policy.FilterToolsList(srv.ID, keyID, names)
		} else {
			for _, n := range names {
				allowed[n] = struct{}{}
			}
		}
		filtered := make([]mcp.Tool, 0, len(tools))
		for _, t := range tools {
			if _, ok := allowed[t.Name]; ok {
				filtered = append(filtered, t)
			}
		}
		return rpcResult(id, mcp.ListToolsResult{Tools: filtered})

	case method == "tools/call":
		var params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		_ = json.Unmarshal(msg.Params, &params)
		if params.Name == "" {
			return rpcError(id, -32602, "missing tool name")
		}
		if d.Policy != nil && keyID != "" && !d.Policy.Allowed(srv.ID, keyID, params.Name) {
			return denyTool(id, params.Name)
		}
		req := mcp.CallToolRequest{}
		req.Params.Name = params.Name
		req.Params.Arguments = params.Arguments
		res, err := d.Manager.CallTool(ctx, srv.ID, req)
		if err != nil {
			if IsBusy(err) {
				return rpcError(id, -32000, "server busy")
			}
			return rpcError(id, -32000, err.Error())
		}
		return rpcResult(id, res)

	default:
		if method == "" {
			return rpcError(id, -32600, "invalid request")
		}
		return rpcError(id, -32601, "method not found")
	}
}

type rpcMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

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

func rpcResult(id json.RawMessage, result any) json.RawMessage {
	if len(id) == 0 {
		id = []byte("null")
	}
	payload := map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "result": result}
	b, err := json.Marshal(payload)
	if err != nil {
		return rpcError(id, -32603, "marshal error")
	}
	return b
}

func rpcError(id json.RawMessage, code int, message string) json.RawMessage {
	if len(id) == 0 {
		id = []byte("null")
	}
	var buf bytes.Buffer
	buf.WriteString(`{"jsonrpc":"2.0","id":`)
	buf.Write(id)
	buf.WriteString(`,"error":{"code":`)
	buf.WriteString(fmt.Sprintf("%d", code))
	buf.WriteString(`,"message":`)
	msg, _ := json.Marshal(message)
	buf.Write(msg)
	buf.WriteString(`}}`)
	return buf.Bytes()
}

func denyTool(id json.RawMessage, name string) json.RawMessage {
	return rpcError(id, -32602, "tool '"+name+"' is not available for this API key")
}

func writeRPC(w http.ResponseWriter, r *http.Request, body []byte) {
	if len(body) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "text/event-stream") && !strings.Contains(accept, "application/json") {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: " + string(body) + "\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// ReadBodyLimited reads ≤1MB for the bridge (mirrors proxy buffering).
func ReadBodyLimited(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	defer r.Body.Close()
	return io.ReadAll(io.LimitReader(r.Body, 1<<20+1))
}

// ValidateStdioServer checks create/patch invariants for transport=stdio.
func ValidateStdioServer(command, baseURL, authType string) error {
	if strings.TrimSpace(command) == "" {
		return fmt.Errorf("stdio requires command")
	}
	if strings.TrimSpace(baseURL) != "" {
		return fmt.Errorf("stdio servers must have empty base_url")
	}
	if authType != "" && authType != store.AuthTypeNone {
		return fmt.Errorf("stdio auth_type must be none (use env for secrets)")
	}
	return nil
}

// BusyWaitHint exposes the configured wait for tests.
func (m *Manager) BusyWait() time.Duration {
	return m.cfg.InFlightWait
}
