package upstream

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

// PKCEProvider uses mcp-go's OAuthHandler for authorization-code + PKCE.
// Best-effort in v1: if AS metadata can't be resolved, returns a clear error.
type PKCEProvider struct {
	HTTPClient  *http.Client
	RedirectURI string // e.g. http://localhost:8080/oauth/redirect
	DiscoverFn  func(ctx context.Context, client *http.Client, mcpURL string) (*Discovered, error)
	// handlers keyed by session state for ProcessAuthorizationResponse (tests + redirect).
	handlers map[string]*transport.OAuthHandler
}

func (p *PKCEProvider) client() *http.Client {
	if p.HTTPClient != nil {
		return p.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (p *PKCEProvider) discover(ctx context.Context, mcpURL string) (*Discovered, error) {
	fn := p.DiscoverFn
	if fn == nil {
		fn = Discover
	}
	return fn(ctx, p.client(), mcpURL)
}

// Begin discovers metadata, optionally registers the client, and returns an authorize URL.
func (p *PKCEProvider) Begin(ctx context.Context, srv *store.MCPServer) (Session, error) {
	d, err := p.discover(ctx, srv.BaseURL)
	if err != nil {
		return Session{}, err
	}
	if d.AuthorizationEndpoint == "" {
		return Session{}, fmt.Errorf("this server advertises authorization-code only; device flow not available — and authorization_endpoint could not be resolved from AS metadata")
	}
	if p.RedirectURI == "" {
		return Session{}, fmt.Errorf("PKCE redirect URI not configured (GATEWAY_URL)")
	}

	storeMem := transport.NewMemoryTokenStore()
	cfg := transport.OAuthConfig{
		ClientID:                     d.ClientID,
		RedirectURI:                  p.RedirectURI,
		Scopes:                       strings.Fields(NormalizeScopes(d.Scopes)),
		TokenStore:                   storeMem,
		AuthServerMetadataURL:        strings.TrimRight(d.AuthServer, "/") + "/.well-known/oauth-authorization-server",
		ProtectedResourceMetadataURL: d.ProtectedResourceMetadataURL,
		PKCEEnabled:                  true,
		HTTPClient:                   p.client(),
	}
	handler := transport.NewOAuthHandler(cfg)
	handler.SetBaseURL(srv.BaseURL)

	if handler.GetClientID() == "" || d.RegistrationEndpoint != "" {
		if err := handler.RegisterClient(ctx, "tokencontrolplane"); err != nil {
			// Registration is optional when client_id is already known.
			if handler.GetClientID() == "" {
				return Session{}, fmt.Errorf("dynamic client registration failed: %w", err)
			}
		}
	}

	state, err := randomURLString(32)
	if err != nil {
		return Session{}, err
	}
	verifier, err := randomURLString(64)
	if err != nil {
		return Session{}, err
	}
	challenge := pkceChallenge(verifier)

	authURL, err := handler.GetAuthorizationURL(ctx, state, challenge)
	if err != nil {
		return Session{}, fmt.Errorf("authorize URL: %w", err)
	}

	if p.handlers == nil {
		p.handlers = map[string]*transport.OAuthHandler{}
	}
	p.handlers[state] = handler

	return Session{
		Flow:              store.OAuthFlowPKCE,
		AuthorizeURL:      authURL,
		State:             state,
		CodeVerifier:      verifier,
		ExpiresIn:         600,
		Interval:          3,
		AuthServer:        d.AuthServer,
		TokenEndpoint:     d.TokenEndpoint,
		ClientID:          handler.GetClientID(),
		Scopes:            NormalizeScopes(d.Scopes),
		ProtectedResource: d.ProtectedResourceMetadataURL,
	}, nil
}

// Poll for PKCE is unused on the poll loop — CompletePKCEWithStore handles the redirect.
func (p *PKCEProvider) Poll(ctx context.Context, sess *store.OAuthSession) (TokenSet, error) {
	return TokenSet{}, fmt.Errorf("PKCE uses redirect callback, not poll")
}

// CompletePKCEWithStore exchanges code using a caller-owned TokenStore so refresh_token is retained.
func (p *PKCEProvider) CompletePKCEWithStore(ctx context.Context, sess *store.OAuthSession, code string, tokenStore transport.TokenStore) (TokenSet, error) {
	cfg := transport.OAuthConfig{
		ClientID:                     sess.ClientID,
		RedirectURI:                  p.RedirectURI,
		Scopes:                       strings.Fields(sess.Scopes),
		TokenStore:                   tokenStore,
		AuthServerMetadataURL:        strings.TrimRight(sess.AuthServer, "/") + "/.well-known/oauth-authorization-server",
		ProtectedResourceMetadataURL: sess.ProtectedResourceMetadataURL,
		PKCEEnabled:                  true,
		HTTPClient:                   p.client(),
	}
	handler := transport.NewOAuthHandler(cfg)
	// Establish expected state.
	_, _ = handler.GetAuthorizationURL(ctx, sess.State, pkceChallenge(sess.CodeVerifier))
	if err := handler.ProcessAuthorizationResponse(ctx, code, sess.State, sess.CodeVerifier); err != nil {
		return TokenSet{}, err
	}
	tok, err := tokenStore.GetToken(ctx)
	if err != nil {
		return TokenSet{}, err
	}
	return TokenSet{
		AccessToken:   tok.AccessToken,
		RefreshToken:  tok.RefreshToken,
		TokenType:     tok.TokenType,
		ExpiresAt:     tok.ExpiresAt,
		Scope:         tok.Scope,
		AuthServer:    sess.AuthServer,
		TokenEndpoint: sess.TokenEndpoint,
		ClientID:      sess.ClientID,
	}, nil
}

func (p *PKCEProvider) Refresh(ctx context.Context, t *TokenSet) (TokenSet, error) {
	// Reuse device provider's form/JSON refresh — same token endpoint semantics.
	d := &DeviceProvider{HTTPClient: p.client()}
	return d.Refresh(ctx, t)
}

func (p *PKCEProvider) Header(t *TokenSet) (string, error) {
	d := &DeviceProvider{}
	return d.Header(t)
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func randomURLString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
