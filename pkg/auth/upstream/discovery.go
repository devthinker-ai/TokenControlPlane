package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var resourceMetadataRE = regexp.MustCompile(`(?i)resource_metadata="([^"]+)"`)

// AuthServerMeta is a subset of RFC 8414 metadata we care about.
type AuthServerMeta struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	DeviceAuthorizationEndpoint       string   `json:"device_authorization_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint"`
	ScopesSupported                   []string `json:"scopes_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
}

// ProtectedResourceMeta is RFC 9728 + optional Higgsfield hints.
type ProtectedResourceMeta struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
	ScopesSupported      []string `json:"scopes_supported"`
	// Higgsfield ships flow-specific AS hints under this key.
	HiggsfieldAuthHints *higgsfieldHints `json:"higgsfield_auth_hints"`
}

type higgsfieldHints struct {
	Options []higgsfieldOption `json:"options"`
}

type higgsfieldOption struct {
	Flow                string `json:"flow"`
	AuthorizationServer string `json:"authorization_server"`
}

// Discovered holds resolved endpoints for a server's OAuth flow.
type Discovered struct {
	ProtectedResourceMetadataURL string
	AuthServer                   string
	TokenEndpoint                string
	DeviceAuthEndpoint           string
	AuthorizationEndpoint        string
	RegistrationEndpoint         string
	Scopes                       string
	ClientID                     string
	SupportsDevice               bool
	SupportsAuthCode             bool
}

// Discover resolves OAuth endpoints for an MCP server URL.
// Order: (1) AS metadata if AuthServerMetadataURL known, (2) protected-resource
// metadata (incl. higgsfield_auth_hints), (3) fallback table in hints.go.
func Discover(ctx context.Context, client *http.Client, mcpURL string) (*Discovered, error) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	prmURL, scopes, err := probeResourceMetadata(ctx, client, mcpURL)
	if err != nil {
		// Fall through — some servers don't 401 on a bare GET; try well-known.
		prmURL = guessProtectedResourceURL(mcpURL)
	}
	if prmURL == "" {
		prmURL = guessProtectedResourceURL(mcpURL)
	}

	prm, err := fetchProtectedResource(ctx, client, prmURL)
	if err != nil {
		return nil, fmt.Errorf("protected resource metadata: %w", err)
	}

	d := &Discovered{
		ProtectedResourceMetadataURL: prmURL,
		ClientID:                     DefaultClientID,
	}
	if scopes != "" {
		d.Scopes = scopes
	} else if len(prm.ScopesSupported) > 0 {
		d.Scopes = strings.Join(prm.ScopesSupported, " ")
	}

	// Prefer device-flow AS from higgsfield_auth_hints when present.
	authServer := ""
	if prm.HiggsfieldAuthHints != nil {
		for _, opt := range prm.HiggsfieldAuthHints.Options {
			if strings.EqualFold(opt.Flow, "device_code") && opt.AuthorizationServer != "" {
				authServer = strings.TrimRight(opt.AuthorizationServer, "/")
				d.SupportsDevice = true
				break
			}
		}
		if authServer == "" {
			for _, opt := range prm.HiggsfieldAuthHints.Options {
				if opt.AuthorizationServer != "" {
					authServer = strings.TrimRight(opt.AuthorizationServer, "/")
					break
				}
			}
		}
	}
	if authServer == "" && len(prm.AuthorizationServers) > 0 {
		authServer = strings.TrimRight(prm.AuthorizationServers[0], "/")
	}
	if authServer == "" {
		return nil, fmt.Errorf("no authorization_servers in protected resource metadata at %s", prmURL)
	}
	d.AuthServer = authServer

	// Apply host hints before/alongside metadata.
	if u, err := url.Parse(authServer); err == nil {
		if hint, ok := LookupDeviceHints(u.Host); ok {
			d.DeviceAuthEndpoint = authServer + hint.DeviceAuth
			d.TokenEndpoint = authServer + hint.Token
			d.SupportsDevice = true
			if hint.ClientID != "" {
				d.ClientID = hint.ClientID
			}
			if hint.Scopes != "" && d.Scopes == "" {
				d.Scopes = hint.Scopes
			}
		}
	}

	meta, metaErr := fetchAuthServerMeta(ctx, client, authServer)
	if metaErr == nil && meta != nil {
		if meta.TokenEndpoint != "" {
			d.TokenEndpoint = meta.TokenEndpoint
		}
		if meta.DeviceAuthorizationEndpoint != "" {
			d.DeviceAuthEndpoint = meta.DeviceAuthorizationEndpoint
			d.SupportsDevice = true
		}
		if meta.AuthorizationEndpoint != "" {
			d.AuthorizationEndpoint = meta.AuthorizationEndpoint
			d.SupportsAuthCode = true
		}
		if meta.RegistrationEndpoint != "" {
			d.RegistrationEndpoint = meta.RegistrationEndpoint
		}
		if d.Scopes == "" && len(meta.ScopesSupported) > 0 {
			d.Scopes = strings.Join(meta.ScopesSupported, " ")
		}
		for _, g := range meta.GrantTypesSupported {
			if g == "urn:ietf:params:oauth:grant-type:device_code" {
				d.SupportsDevice = true
			}
			if g == "authorization_code" {
				d.SupportsAuthCode = true
			}
		}
	}

	// RFC 8628 defaults when metadata/hints left gaps but we know it's a device AS.
	if d.SupportsDevice {
		if d.DeviceAuthEndpoint == "" {
			d.DeviceAuthEndpoint = authServer + "/device_authorization"
		}
		if d.TokenEndpoint == "" {
			d.TokenEndpoint = authServer + "/token"
		}
	}
	if d.TokenEndpoint == "" {
		d.TokenEndpoint = authServer + "/token"
	}
	if d.Scopes == "" {
		d.Scopes = NormalizeScopes("")
	}

	return d, nil
}

func probeResourceMetadata(ctx context.Context, client *http.Client, mcpURL string) (prmURL, scopes string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mcpURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	wa := resp.Header.Get("WWW-Authenticate")
	if wa == "" {
		return "", "", fmt.Errorf("no WWW-Authenticate on %d", resp.StatusCode)
	}
	m := resourceMetadataRE.FindStringSubmatch(wa)
	if len(m) < 2 {
		return "", "", fmt.Errorf("no resource_metadata in WWW-Authenticate")
	}
	prmURL = m[1]
	if i := strings.Index(strings.ToLower(wa), "scope="); i >= 0 {
		rest := wa[i+6:]
		rest = strings.Trim(rest, `"`)
		if j := strings.IndexAny(rest, `", `); j >= 0 {
			// keep quoted scope value
		}
		scopeRE := regexp.MustCompile(`(?i)scope="([^"]+)"`)
		if sm := scopeRE.FindStringSubmatch(wa); len(sm) == 2 {
			scopes = sm[1]
		}
	}
	return prmURL, scopes, nil
}

func guessProtectedResourceURL(mcpURL string) string {
	u, err := url.Parse(mcpURL)
	if err != nil {
		return ""
	}
	path := strings.Trim(u.Path, "/")
	if path == "" {
		return u.Scheme + "://" + u.Host + "/.well-known/oauth-protected-resource"
	}
	return u.Scheme + "://" + u.Host + "/.well-known/oauth-protected-resource/" + path
}

func fetchProtectedResource(ctx context.Context, client *http.Client, prmURL string) (*ProtectedResourceMeta, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, prmURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, prmURL)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var prm ProtectedResourceMeta
	if err := json.Unmarshal(body, &prm); err != nil {
		return nil, err
	}
	return &prm, nil
}

func fetchAuthServerMeta(ctx context.Context, client *http.Client, authServer string) (*AuthServerMeta, error) {
	candidates := []string{
		strings.TrimRight(authServer, "/") + "/.well-known/oauth-authorization-server",
		strings.TrimRight(authServer, "/") + "/.well-known/openid-configuration",
	}
	var lastErr error
	for _, u := range candidates {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			lastErr = err
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %d from %s", resp.StatusCode, u)
			continue
		}
		var meta AuthServerMeta
		if err := json.Unmarshal(body, &meta); err != nil {
			lastErr = err
			continue
		}
		return &meta, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no auth server metadata")
	}
	return nil, lastErr
}
