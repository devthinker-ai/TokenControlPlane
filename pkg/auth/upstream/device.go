package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

// DeviceProvider implements RFC 8628 device flow + Higgsfield JSON shape.
type DeviceProvider struct {
	HTTPClient *http.Client
	// DiscoverFn allows tests to inject discovery; nil uses Discover.
	DiscoverFn func(ctx context.Context, client *http.Client, mcpURL string) (*Discovered, error)
}

func (p *DeviceProvider) client() *http.Client {
	if p.HTTPClient != nil {
		return p.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (p *DeviceProvider) discover(ctx context.Context, mcpURL string) (*Discovered, error) {
	fn := p.DiscoverFn
	if fn == nil {
		fn = Discover
	}
	return fn(ctx, p.client(), mcpURL)
}

// Begin POSTs to the device-auth endpoint and returns the user-facing session.
func (p *DeviceProvider) Begin(ctx context.Context, srv *store.MCPServer) (Session, error) {
	d, err := p.discover(ctx, srv.BaseURL)
	if err != nil {
		return Session{}, err
	}
	if !d.SupportsDevice || d.DeviceAuthEndpoint == "" {
		return Session{}, fmt.Errorf("this server advertises authorization-code only; device flow not available")
	}

	scopes := NormalizeScopes(d.Scopes)
	body, _ := json.Marshal(map[string]string{
		"client_id": d.ClientID,
		"scope":     scopes,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.DeviceAuthEndpoint, bytes.NewReader(body))
	if err != nil {
		return Session{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// Also try form-encoded if JSON fails (RFC 8628 default).
	resp, err := p.client().Do(req)
	if err != nil {
		return Session{}, err
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Retry as application/x-www-form-urlencoded (strict RFC 8628).
		form := url.Values{}
		form.Set("client_id", d.ClientID)
		form.Set("scope", scopes)
		req2, err := http.NewRequestWithContext(ctx, http.MethodPost, d.DeviceAuthEndpoint, strings.NewReader(form.Encode()))
		if err != nil {
			return Session{}, err
		}
		req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req2.Header.Set("Accept", "application/json")
		resp2, err := p.client().Do(req2)
		if err != nil {
			return Session{}, err
		}
		raw, _ = io.ReadAll(io.LimitReader(resp2.Body, 1<<20))
		_ = resp2.Body.Close()
		if resp2.StatusCode < 200 || resp2.StatusCode >= 300 {
			return Session{}, fmt.Errorf("device authorization failed: HTTP %d: %s", resp2.StatusCode, truncate(string(raw), 200))
		}
	}

	var parsed struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int    `json:"expires_in"`
		Interval                int    `json:"interval"`
		Error                   string `json:"error"`
		Detail                  string `json:"detail"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Session{}, fmt.Errorf("device authorization response: %w", err)
	}
	if parsed.Error != "" || parsed.Detail != "" {
		msg := parsed.Error
		if msg == "" {
			msg = parsed.Detail
		}
		return Session{}, fmt.Errorf("device authorization: %s", msg)
	}
	if parsed.DeviceCode == "" {
		return Session{}, fmt.Errorf("device authorization: missing device_code")
	}
	verify := parsed.VerificationURI
	if parsed.VerificationURIComplete != "" {
		verify = parsed.VerificationURIComplete
	}
	if parsed.ExpiresIn <= 0 {
		parsed.ExpiresIn = 900
	}
	if parsed.Interval <= 0 {
		parsed.Interval = 5
	}

	return Session{
		Flow:               store.OAuthFlowDevice,
		DeviceCode:         parsed.DeviceCode,
		VerificationURI:    verify,
		VerificationCode:   parsed.UserCode,
		ExpiresIn:          parsed.ExpiresIn,
		Interval:           parsed.Interval,
		AuthServer:         d.AuthServer,
		TokenEndpoint:      d.TokenEndpoint,
		DeviceAuthEndpoint: d.DeviceAuthEndpoint,
		ClientID:           d.ClientID,
		Scopes:             scopes,
		ProtectedResource:  d.ProtectedResourceMetadataURL,
	}, nil
}

// Poll once against the token endpoint. Returns ErrAuthorizationPending /
// ErrSlowDown / ErrExpiredToken, or a TokenSet on success.
func (p *DeviceProvider) Poll(ctx context.Context, sess *store.OAuthSession) (TokenSet, error) {
	if sess.ExpiresAt.Before(time.Now().UTC()) {
		return TokenSet{}, ErrExpiredToken
	}
	body, _ := json.Marshal(map[string]string{
		"grant_type":  "urn:ietf:params:oauth:grant-type:device_code",
		"device_code": sess.DeviceCode,
		"client_id":   sess.ClientID,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sess.TokenEndpoint, bytes.NewReader(body))
	if err != nil {
		return TokenSet{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := p.client().Do(req)
	if err != nil {
		return TokenSet{}, err
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()

	// Higgsfield returns 200 with {"detail":"authorization_pending"}; RFC uses error field.
	if err := mapPollError(raw, resp.StatusCode); err != nil {
		if err == ErrAuthorizationPending || err == ErrSlowDown || err == ErrExpiredToken {
			return TokenSet{}, err
		}
		// Retry form-encoded once on hard failure (strict RFC servers).
		if resp.StatusCode >= 400 {
			form := url.Values{}
			form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
			form.Set("device_code", sess.DeviceCode)
			form.Set("client_id", sess.ClientID)
			req2, e2 := http.NewRequestWithContext(ctx, http.MethodPost, sess.TokenEndpoint, strings.NewReader(form.Encode()))
			if e2 != nil {
				return TokenSet{}, err
			}
			req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req2.Header.Set("Accept", "application/json")
			resp2, e2 := p.client().Do(req2)
			if e2 != nil {
				return TokenSet{}, err
			}
			raw, _ = io.ReadAll(io.LimitReader(resp2.Body, 1<<20))
			_ = resp2.Body.Close()
			if e3 := mapPollError(raw, resp2.StatusCode); e3 != nil {
				return TokenSet{}, e3
			}
		} else {
			return TokenSet{}, err
		}
	}

	return parseTokenResponse(raw, sess.AuthServer, sess.TokenEndpoint, sess.DeviceAuthEndpoint, sess.ClientID)
}

// Refresh exchanges refresh_token at the token endpoint (JSON then form).
func (p *DeviceProvider) Refresh(ctx context.Context, t *TokenSet) (TokenSet, error) {
	if t.RefreshToken == "" {
		return TokenSet{}, fmt.Errorf("no refresh_token")
	}
	body, _ := json.Marshal(map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": t.RefreshToken,
		"client_id":     t.ClientID,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.TokenEndpoint, bytes.NewReader(body))
	if err != nil {
		return TokenSet{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := p.client().Do(req)
	if err != nil {
		return TokenSet{}, err
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		form := url.Values{}
		form.Set("grant_type", "refresh_token")
		form.Set("refresh_token", t.RefreshToken)
		form.Set("client_id", t.ClientID)
		req2, e2 := http.NewRequestWithContext(ctx, http.MethodPost, t.TokenEndpoint, strings.NewReader(form.Encode()))
		if e2 != nil {
			return TokenSet{}, fmt.Errorf("refresh failed: HTTP %d: %s", resp.StatusCode, truncate(string(raw), 200))
		}
		req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req2.Header.Set("Accept", "application/json")
		resp2, e2 := p.client().Do(req2)
		if e2 != nil {
			return TokenSet{}, e2
		}
		raw, _ = io.ReadAll(io.LimitReader(resp2.Body, 1<<20))
		_ = resp2.Body.Close()
		if resp2.StatusCode < 200 || resp2.StatusCode >= 300 {
			return TokenSet{}, fmt.Errorf("refresh failed: HTTP %d: %s", resp2.StatusCode, truncate(string(raw), 200))
		}
	}

	ts, err := parseTokenResponse(raw, t.AuthServer, t.TokenEndpoint, t.DeviceAuthEndpoint, t.ClientID)
	if err != nil {
		return TokenSet{}, err
	}
	if ts.RefreshToken == "" {
		ts.RefreshToken = t.RefreshToken
	}
	return ts, nil
}

func (p *DeviceProvider) Header(t *TokenSet) (string, error) {
	if t == nil || t.AccessToken == "" {
		return "", fmt.Errorf("no access token")
	}
	tt := t.TokenType
	if tt == "" || strings.EqualFold(tt, "bearer") {
		tt = "Bearer"
	}
	return tt + " " + t.AccessToken, nil
}

func mapPollError(raw []byte, status int) error {
	var parsed struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
		Detail           string `json:"detail"`
		AccessToken      string `json:"access_token"`
	}
	_ = json.Unmarshal(raw, &parsed)
	if parsed.AccessToken != "" {
		return nil
	}
	code := parsed.Error
	if code == "" {
		code = parsed.Detail
	}
	code = strings.TrimSpace(strings.ToLower(code))
	switch code {
	case "authorization_pending":
		return ErrAuthorizationPending
	case "slow_down":
		return ErrSlowDown
	case "expired_token", "access_denied":
		return ErrExpiredToken
	}
	if status >= 400 {
		if code != "" {
			return fmt.Errorf("token poll: %s", code)
		}
		return fmt.Errorf("token poll: HTTP %d: %s", status, truncate(string(raw), 200))
	}
	// 200 with neither token nor known pending shape.
	if code != "" {
		return fmt.Errorf("token poll: %s", code)
	}
	return fmt.Errorf("token poll: unexpected response: %s", truncate(string(raw), 200))
}

func parseTokenResponse(raw []byte, authServer, tokenEP, deviceEP, clientID string) (TokenSet, error) {
	var parsed struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
		Scope        string `json:"scope"`
		Error        string `json:"error"`
		Detail       string `json:"detail"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return TokenSet{}, err
	}
	if parsed.AccessToken == "" {
		msg := parsed.Error
		if msg == "" {
			msg = parsed.Detail
		}
		if msg == "" {
			msg = "missing access_token"
		}
		return TokenSet{}, fmt.Errorf("token response: %s", msg)
	}
	tt := parsed.TokenType
	if tt == "" {
		tt = "Bearer"
	}
	var exp time.Time
	if parsed.ExpiresIn > 0 {
		exp = time.Now().UTC().Add(time.Duration(parsed.ExpiresIn) * time.Second)
	}
	return TokenSet{
		AccessToken:        parsed.AccessToken,
		RefreshToken:       parsed.RefreshToken,
		TokenType:          tt,
		ExpiresAt:          exp,
		Scope:              parsed.Scope,
		AuthServer:         authServer,
		TokenEndpoint:      tokenEP,
		DeviceAuthEndpoint: deviceEP,
		ClientID:           clientID,
	}, nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
