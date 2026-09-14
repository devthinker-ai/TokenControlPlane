// Package upstream implements OAuth providers for connecting the gateway to
// OAuth-protected MCP servers (device-code + PKCE). All token injection for
// indexer/proxy must go through TokenGuard — never ad-hoc in those packages.
package upstream

import (
	"context"
	"errors"
	"time"

	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

const DefaultClientID = "tokencontrolplane"

// ErrExpired is returned when a refresh fails and the server must reconnect.
var ErrExpired = errors.New("upstream oauth expired — reconnect")

// ErrAuthorizationPending means the user has not completed the device flow yet.
var ErrAuthorizationPending = errors.New("authorization_pending")

// ErrSlowDown means the AS asked us to increase the poll interval.
var ErrSlowDown = errors.New("slow_down")

// ErrExpiredToken means the device code expired before sign-in.
var ErrExpiredToken = errors.New("expired_token")

// TokenSet is the credential bundle returned by providers and stored in oauth_tokens.
type TokenSet struct {
	AccessToken        string
	RefreshToken       string
	TokenType          string
	ExpiresAt          time.Time
	Scope              string
	AuthServer         string
	TokenEndpoint      string
	DeviceAuthEndpoint string
	ClientID           string
}

// Session is the Begin() result shown to the dashboard (device code or authorize URL).
type Session struct {
	Flow             string // device|pkce
	DeviceCode       string
	VerificationURI  string
	VerificationCode string
	ExpiresIn        int
	Interval         int
	AuthorizeURL     string
	State            string
	CodeVerifier     string
	// Internal endpoints captured during Begin for Poll/Refresh.
	AuthServer         string
	TokenEndpoint      string
	DeviceAuthEndpoint string
	ClientID           string
	Scopes             string
	ProtectedResource  string // protected-resource metadata URL
}

// Provider is an upstream OAuth flow implementation.
type Provider interface {
	// Begin starts an out-of-band flow. Device: POST device-auth, return session.
	// PKCE: return authorize URL (+ state/verifier for the redirect handler).
	Begin(ctx context.Context, srv *store.MCPServer) (Session, error)
	// Poll short-polls until the user completes the flow (device), or is
	// called from the redirect handler after code exchange (PKCE).
	Poll(ctx context.Context, sess *store.OAuthSession) (TokenSet, error)
	// Refresh exchanges a refresh_token for a new TokenSet.
	Refresh(ctx context.Context, t *TokenSet) (TokenSet, error)
	// Header returns "Bearer <access_token>" for injection.
	Header(t *TokenSet) (string, error)
}

// NormalizeScopes joins a scope string for requests.
func NormalizeScopes(scopes string) string {
	if scopes == "" {
		return "openid email offline_access"
	}
	return scopes
}
