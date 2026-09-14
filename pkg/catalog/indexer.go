// Package catalog indexes upstream MCP tool schemas out-of-band (never on the proxy hot path).
package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/devthinker-ai/TokenControlPlane/pkg/auth/upstream"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

// Indexer connects to upstream MCP servers via mcp-go's streamable HTTP client
// and caches tools/list results in SQLite for the dashboard.
type Indexer struct {
	store  *store.Store
	logger *slog.Logger
	guard  *upstream.TokenGuard
	stdio  StdioIndexer
}

// StdioIndexer lists tools from a local stdio process (optional).
type StdioIndexer interface {
	ListTools(ctx context.Context, serverID string) ([]mcp.Tool, error)
	Restart(ctx context.Context, serverID string) error
}

// NewIndexer creates an indexer.
func NewIndexer(s *store.Store, logger *slog.Logger) *Indexer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Indexer{store: s, logger: logger}
}

// SetTokenGuard injects the shared OAuth token guard (called from main after construction).
func (idx *Indexer) SetTokenGuard(g *upstream.TokenGuard) {
	idx.guard = g
}

// SetStdioIndexer injects the stdio process manager for local servers.
func (idx *Indexer) SetStdioIndexer(s StdioIndexer) {
	idx.stdio = s
}

// IndexServerAsync kicks off indexing in a background goroutine.
// Failures are logged and never surface to the proxy.
func (idx *Indexer) IndexServerAsync(serverID string) {
	go func() { _, _ = idx.IndexServer(serverID) }()
}

// IndexServer runs tools/list against the upstream and replaces stored tool defs.
// Returns indexed tools and a user-facing error string (empty on success).
func (idx *Indexer) IndexServer(serverID string) (tools []store.ToolDef, indexErr string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	srv, err := idx.store.GetServer(ctx, serverID)
	if err != nil {
		msg := "server not found"
		idx.logger.Error("catalog: get server", "server_id", serverID, "err", err)
		_ = idx.store.SetServerIndexError(ctx, serverID, msg)
		return nil, msg
	}

	if srv.Transport == store.TransportStdio {
		return idx.indexStdio(ctx, srv)
	}

	opts, err := idx.transportOpts(ctx, srv)
	if err != nil {
		msg := sanitizeIndexError(err)
		if errors.Is(err, upstream.ErrExpired) {
			msg = upstream.ErrExpired.Error()
		}
		_ = idx.store.SetServerIndexError(ctx, serverID, msg)
		return nil, msg
	}

	defs, msg := idx.indexOnce(ctx, srv, opts)
	if msg != "" && isUnauthorized(msg) && isOAuth(srv) && idx.guard != nil {
		// One forced refresh + retry on 401.
		if _, rerr := idx.guard.ForceRefresh(ctx, srv.AccountID, serverID); rerr == nil {
			opts, _ = idx.transportOpts(ctx, srv)
			defs, msg = idx.indexOnce(ctx, srv, opts)
		}
		if msg != "" && isUnauthorized(msg) {
			_ = idx.store.MarkOAuthTokenExpired(ctx, srv.AccountID, serverID)
			msg = upstream.ErrExpired.Error()
			_ = idx.store.SetServerIndexError(ctx, serverID, msg)
			return nil, msg
		}
	}
	if msg != "" {
		_ = idx.store.SetServerIndexError(ctx, serverID, msg)
		return nil, msg
	}
	_ = idx.store.SetServerIndexError(ctx, serverID, "")
	idx.logger.Info("catalog: indexed", "server_id", serverID, "tools", len(defs))
	return defs, ""
}

func (idx *Indexer) indexStdio(ctx context.Context, srv *store.MCPServer) ([]store.ToolDef, string) {
	if idx.stdio == nil {
		msg := "stdio manager not configured"
		_ = idx.store.SetServerIndexError(ctx, srv.ID, msg)
		return nil, msg
	}
	// Reindex = restart + ListTools so the restart is explicit.
	if err := idx.stdio.Restart(ctx, srv.ID); err != nil {
		msg := sanitizeIndexError(err)
		idx.logger.Error("catalog: stdio restart", "server_id", srv.ID, "err", err)
		_ = idx.store.SetServerIndexError(ctx, srv.ID, msg)
		return nil, msg
	}
	tools, err := idx.stdio.ListTools(ctx, srv.ID)
	if err != nil {
		msg := sanitizeIndexError(err)
		idx.logger.Error("catalog: stdio list tools", "server_id", srv.ID, "err", err)
		_ = idx.store.SetServerIndexError(ctx, srv.ID, msg)
		return nil, msg
	}
	defs := make([]store.ToolDef, 0, len(tools))
	for _, t := range tools {
		schemaJSON, err := marshalToolSchema(t)
		if err != nil {
			schemaJSON = []byte("{}")
		}
		defs = append(defs, store.ToolDef{
			ID:          uuid.NewString(),
			ServerID:    srv.ID,
			Name:        t.Name,
			Description: t.Description,
			InputSchema: string(schemaJSON),
		})
	}
	if err := idx.store.ReplaceTools(ctx, srv.ID, defs); err != nil {
		idx.logger.Error("catalog: persist tools", "server_id", srv.ID, "err", err)
		return nil, "failed to persist tools"
	}
	_ = idx.store.SetServerIndexError(ctx, srv.ID, "")
	idx.logger.Info("catalog: indexed stdio", "server_id", srv.ID, "tools", len(defs))
	return defs, ""
}

func (idx *Indexer) transportOpts(ctx context.Context, srv *store.MCPServer) ([]transport.StreamableHTTPCOption, error) {
	opts := []transport.StreamableHTTPCOption{}
	switch srv.AuthType {
	case store.AuthTypeOAuthDevice, store.AuthTypeOAuthPKCE:
		if idx.guard == nil {
			return nil, fmt.Errorf("oauth token guard not configured")
		}
		accountID := srv.AccountID
		serverID := srv.ID
		guard := idx.guard
		opts = append(opts, transport.WithHTTPHeaderFunc(func(_ context.Context) map[string]string {
			t, err := guard.TokenFor(ctx, accountID, serverID)
			if err != nil {
				return nil
			}
			val, err := guard.Header(t)
			if err != nil {
				return nil
			}
			return map[string]string{"Authorization": val}
		}))
	default:
		if srv.AuthValue != "" {
			header := srv.AuthHeader
			if header == "" {
				header = "Authorization"
			}
			val := srv.AuthValue
			opts = append(opts, transport.WithHTTPHeaders(map[string]string{header: val}))
		}
	}
	return opts, nil
}

func (idx *Indexer) indexOnce(ctx context.Context, srv *store.MCPServer, opts []transport.StreamableHTTPCOption) ([]store.ToolDef, string) {
	c, err := mcpclient.NewStreamableHttpClient(srv.BaseURL, opts...)
	if err != nil {
		msg := sanitizeIndexError(fmt.Errorf("could not create client: %w", err))
		idx.logger.Error("catalog: create client", "server_id", srv.ID, "err", err)
		return nil, msg
	}
	defer func() { _ = c.Close() }()

	if err := c.Start(ctx); err != nil {
		msg := friendlyIndexErr(err)
		idx.logger.Error("catalog: start client", "server_id", srv.ID, "err", err)
		return nil, msg
	}

	_, err = c.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "tokencontrolplane-indexer",
				Version: "0.1.0",
			},
		},
	})
	if err != nil {
		msg := friendlyIndexErr(err)
		idx.logger.Error("catalog: initialize", "server_id", srv.ID, "err", err)
		return nil, msg
	}

	result, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		msg := friendlyIndexErr(err)
		idx.logger.Error("catalog: tools/list", "server_id", srv.ID, "err", err)
		return nil, msg
	}

	defs := make([]store.ToolDef, 0, len(result.Tools))
	for _, t := range result.Tools {
		schemaJSON, err := marshalToolSchema(t)
		if err != nil {
			idx.logger.Warn("catalog: marshal schema", "tool", t.Name, "err", err)
			schemaJSON = []byte("{}")
		}
		defs = append(defs, store.ToolDef{
			ID:          uuid.NewString(),
			ServerID:    srv.ID,
			Name:        t.Name,
			Description: t.Description,
			InputSchema: string(schemaJSON),
		})
	}

	if err := idx.store.ReplaceTools(ctx, srv.ID, defs); err != nil {
		idx.logger.Error("catalog: persist tools", "server_id", srv.ID, "err", err)
		return nil, "failed to persist tools"
	}
	return defs, ""
}

func isOAuth(srv *store.MCPServer) bool {
	return srv.AuthType == store.AuthTypeOAuthDevice || srv.AuthType == store.AuthTypeOAuthPKCE
}

func isUnauthorized(msg string) bool {
	ls := strings.ToLower(msg)
	return strings.Contains(ls, "401") || strings.Contains(ls, "unauthorized")
}

func friendlyIndexErr(err error) string {
	if err == nil {
		return "indexing failed"
	}
	s := err.Error()
	ls := strings.ToLower(s)
	switch {
	case strings.Contains(ls, "401") || strings.Contains(ls, "unauthorized"):
		return "401 unauthorized — check the upstream API key"
	case strings.Contains(ls, "403") || strings.Contains(ls, "forbidden"):
		return "403 forbidden — upstream rejected credentials"
	case strings.Contains(ls, "connection refused") || strings.Contains(ls, "refused"):
		return "connection refused — is the server URL reachable?"
	case strings.Contains(ls, "no such host") || strings.Contains(ls, "lookup "):
		return "DNS lookup failed — check the server hostname"
	case strings.Contains(ls, "timeout") || strings.Contains(ls, "deadline exceeded"):
		return "timeout contacting upstream MCP server"
	default:
		return sanitizeIndexError(err)
	}
}

// sanitizeIndexError keeps the error readable but bounds it — never echo multi-KB
// upstream HTML/bodies into API responses or SQLite.
func sanitizeIndexError(err error) string {
	if err == nil {
		return "indexing failed"
	}
	s := err.Error()
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > 300 {
		s = string(r[:300]) + "…"
	}
	return s
}

func marshalToolSchema(t mcp.Tool) ([]byte, error) {
	if len(t.RawInputSchema) > 0 {
		return t.RawInputSchema, nil
	}
	return json.Marshal(t.InputSchema)
}
