package upstream

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

func TestPKCE_HappyPath(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                base,
			"authorization_endpoint":                base + "/authorize",
			"token_endpoint":                        base + "/token",
			"registration_endpoint":                 base + "/register",
			"response_types_supported":              []string{"code"},
			"code_challenge_methods_supported":      []string{"S256"},
			"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		})
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"client_id": "dyn-client",
		})
	})
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "authorization_code" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "pkce-at",
			"refresh_token": "pkce-rt",
			"token_type":    "Bearer",
			"expires_in":    3600,
		})
	})
	as := httptest.NewServer(mux)
	t.Cleanup(as.Close)

	p := &PKCEProvider{
		HTTPClient:  as.Client(),
		RedirectURI: "http://localhost:8080/oauth/redirect",
		DiscoverFn: func(ctx context.Context, client *http.Client, mcpURL string) (*Discovered, error) {
			return &Discovered{
				AuthServer:            as.URL,
				TokenEndpoint:         as.URL + "/token",
				AuthorizationEndpoint: as.URL + "/authorize",
				RegistrationEndpoint:  as.URL + "/register",
				ClientID:              "", // force registration
				Scopes:                "openid",
				SupportsAuthCode:      true,
				ProtectedResourceMetadataURL: as.URL + "/.well-known/oauth-protected-resource",
			}, nil
		},
	}

	sess, err := p.Begin(context.Background(), &store.MCPServer{BaseURL: "https://mcp.example/mcp"})
	if err != nil {
		t.Fatal(err)
	}
	if sess.AuthorizeURL == "" || sess.State == "" || sess.CodeVerifier == "" {
		t.Fatalf("session incomplete: %+v", sess)
	}
	u, err := url.Parse(sess.AuthorizeURL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(u.Path, "authorize") {
		t.Fatalf("authorize url=%s", sess.AuthorizeURL)
	}

	storeSess := &store.OAuthSession{
		State:        sess.State,
		CodeVerifier: sess.CodeVerifier,
		ClientID:     sess.ClientID,
		AuthServer:   sess.AuthServer,
		TokenEndpoint: sess.TokenEndpoint,
		Scopes:       sess.Scopes,
		ProtectedResourceMetadataURL: sess.ProtectedResource,
		ExpiresAt:    time.Now().Add(10 * time.Minute),
	}
	ts, err := p.CompletePKCEWithStore(context.Background(), storeSess, "test-code", transport.NewMemoryTokenStore())
	if err != nil {
		t.Fatal(err)
	}
	if ts.AccessToken != "pkce-at" || ts.RefreshToken != "pkce-rt" {
		t.Fatalf("token=%+v", ts)
	}
}

func TestLookupDeviceHints(t *testing.T) {
	h, ok := LookupDeviceHints("fnf-device-auth.higgsfield.ai")
	if !ok || h.DeviceAuth != "/authorize" {
		t.Fatalf("hints=%+v ok=%v", h, ok)
	}
}
