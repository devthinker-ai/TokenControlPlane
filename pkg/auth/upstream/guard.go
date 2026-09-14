package upstream

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

// TokenGuard retrieves (and auto-refreshes) OAuth tokens for indexer + proxy.
// One instance is shared; never duplicate refresh logic in those packages.
type TokenGuard struct {
	Store    *store.Store
	Provider Provider // used for Refresh (DeviceProvider is fine for both)
	Logger   *slog.Logger

	mu      sync.Mutex
	refresh map[string]*sync.Mutex // per-server refresh lock
}

// NewTokenGuard creates a guard. provider may be a DeviceProvider (JSON+form refresh).
func NewTokenGuard(st *store.Store, provider Provider, logger *slog.Logger) *TokenGuard {
	if logger == nil {
		logger = slog.Default()
	}
	return &TokenGuard{
		Store:    st,
		Provider: provider,
		Logger:   logger,
		refresh:  map[string]*sync.Mutex{},
	}
}

func (g *TokenGuard) lockFor(serverID string) *sync.Mutex {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.refresh[serverID] == nil {
		g.refresh[serverID] = &sync.Mutex{}
	}
	return g.refresh[serverID]
}

// TokenFor returns a valid access token for the server, refreshing if within 60s of expiry.
func (g *TokenGuard) TokenFor(ctx context.Context, accountID, serverID string) (*TokenSet, error) {
	return g.tokenFor(ctx, accountID, serverID, false)
}

// ForceRefresh refreshes regardless of expiry (used after upstream 401).
func (g *TokenGuard) ForceRefresh(ctx context.Context, accountID, serverID string) (*TokenSet, error) {
	return g.tokenFor(ctx, accountID, serverID, true)
}

func (g *TokenGuard) tokenFor(ctx context.Context, accountID, serverID string, force bool) (*TokenSet, error) {
	tok, err := g.Store.GetOAuthToken(ctx, accountID, serverID)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("%w: no token", ErrExpired)
	}
	if err != nil {
		return nil, err
	}
	if tok.Status == store.OAuthStatusExpired {
		return nil, ErrExpired
	}

	ts := tokenFromStore(tok)
	needRefresh := force || shouldRefresh(tok.ExpiresAt)
	if !needRefresh {
		return &ts, nil
	}
	if tok.RefreshToken == "" {
		_ = g.Store.MarkOAuthTokenExpired(ctx, accountID, serverID)
		return nil, ErrExpired
	}

	lk := g.lockFor(serverID)
	lk.Lock()
	defer lk.Unlock()

	// Re-read after lock — another goroutine may have refreshed.
	tok, err = g.Store.GetOAuthToken(ctx, accountID, serverID)
	if err != nil {
		return nil, err
	}
	if tok.Status == store.OAuthStatusExpired {
		return nil, ErrExpired
	}
	if !force && !shouldRefresh(tok.ExpiresAt) {
		ts = tokenFromStore(tok)
		return &ts, nil
	}

	refreshed, err := g.Provider.Refresh(ctx, &TokenSet{
		RefreshToken:       tok.RefreshToken,
		TokenEndpoint:      tok.TokenEndpoint,
		AuthServer:         tok.AuthServer,
		DeviceAuthEndpoint: tok.DeviceAuthEndpoint,
		ClientID:           tok.ClientID,
	})
	if err != nil {
		g.Logger.Warn("upstream oauth refresh failed", "server_id", serverID, "err", err)
		_ = g.Store.MarkOAuthTokenExpired(ctx, accountID, serverID)
		srv, _ := g.Store.GetServer(ctx, serverID)
		name := serverID
		if srv != nil {
			name = srv.Name
		}
		_ = g.Store.InsertActivityEvent(ctx, accountID, store.ActivityOAuthExpired, name,
			"OAuth expired for '"+name+"' — reconnect", map[string]any{"server_id": serverID})
		_ = g.Store.SetServerIndexError(ctx, serverID, ErrExpired.Error())
		return nil, ErrExpired
	}

	persist := store.OAuthToken{
		ID:                 tok.ID,
		AccountID:          accountID,
		ServerID:           serverID,
		AuthServer:         firstNonEmpty(refreshed.AuthServer, tok.AuthServer),
		TokenEndpoint:      firstNonEmpty(refreshed.TokenEndpoint, tok.TokenEndpoint),
		DeviceAuthEndpoint: firstNonEmpty(refreshed.DeviceAuthEndpoint, tok.DeviceAuthEndpoint),
		ClientID:           firstNonEmpty(refreshed.ClientID, tok.ClientID),
		Scopes:             firstNonEmpty(refreshed.Scope, tok.Scopes),
		AccessToken:        refreshed.AccessToken,
		RefreshToken:       firstNonEmpty(refreshed.RefreshToken, tok.RefreshToken),
		TokenType:          firstNonEmpty(refreshed.TokenType, tok.TokenType),
		ExpiresAt:          refreshed.ExpiresAt,
		Status:             store.OAuthStatusConnected,
		CreatedAt:          tok.CreatedAt,
	}
	if persist.ID == "" {
		persist.ID = "oat_" + uuid.NewString()
	}
	if err := g.Store.UpsertOAuthToken(ctx, persist); err != nil {
		return nil, err
	}
	out := tokenFromStore(&persist)
	return &out, nil
}

// Header returns the Authorization header value for a TokenSet.
func (g *TokenGuard) Header(t *TokenSet) (string, error) {
	if g.Provider != nil {
		return g.Provider.Header(t)
	}
	d := &DeviceProvider{}
	return d.Header(t)
}

// PersistToken writes a successful TokenSet after Poll / PKCE complete.
func PersistToken(ctx context.Context, st *store.Store, accountID, serverID string, ts TokenSet) error {
	existing, err := st.GetOAuthToken(ctx, accountID, serverID)
	id := "oat_" + uuid.NewString()
	created := time.Now().UTC()
	if err == nil && existing != nil {
		id = existing.ID
		created = existing.CreatedAt
	}
	return st.UpsertOAuthToken(ctx, store.OAuthToken{
		ID:                 id,
		AccountID:          accountID,
		ServerID:           serverID,
		AuthServer:         ts.AuthServer,
		TokenEndpoint:      ts.TokenEndpoint,
		DeviceAuthEndpoint: ts.DeviceAuthEndpoint,
		ClientID:           firstNonEmpty(ts.ClientID, DefaultClientID),
		Scopes:             ts.Scope,
		AccessToken:        ts.AccessToken,
		RefreshToken:       ts.RefreshToken,
		TokenType:          firstNonEmpty(ts.TokenType, "Bearer"),
		ExpiresAt:          ts.ExpiresAt,
		Status:             store.OAuthStatusConnected,
		CreatedAt:          created,
	})
}

func shouldRefresh(expiresAt time.Time) bool {
	if expiresAt.IsZero() {
		return false
	}
	return time.Until(expiresAt) < 60*time.Second
}

func tokenFromStore(tok *store.OAuthToken) TokenSet {
	return TokenSet{
		AccessToken:        tok.AccessToken,
		RefreshToken:       tok.RefreshToken,
		TokenType:          tok.TokenType,
		ExpiresAt:          tok.ExpiresAt,
		Scope:              tok.Scopes,
		AuthServer:         tok.AuthServer,
		TokenEndpoint:      tok.TokenEndpoint,
		DeviceAuthEndpoint: tok.DeviceAuthEndpoint,
		ClientID:           tok.ClientID,
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
