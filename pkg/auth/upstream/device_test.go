package upstream

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

func TestDeviceFlow_HiggsfieldShape(t *testing.T) {
	var polls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "dc-hf",
			"verification_uri": "https://example.com/device?code=ABC",
			"expires_in":       900,
			"interval":         1,
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		n := polls.Add(1)
		if n < 2 {
			_ = json.NewEncoder(w).Encode(map[string]string{"detail": "authorization_pending"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "at-hf",
			"refresh_token": "rt-hf",
			"token_type":    "Bearer",
			"expires_in":    3600,
			"scope":         "openid email offline_access",
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	p := &DeviceProvider{
		HTTPClient: srv.Client(),
		DiscoverFn: func(ctx context.Context, client *http.Client, mcpURL string) (*Discovered, error) {
			return &Discovered{
				AuthServer:         srv.URL,
				TokenEndpoint:      srv.URL + "/token",
				DeviceAuthEndpoint: srv.URL + "/authorize",
				ClientID:           DefaultClientID,
				Scopes:             "openid email offline_access",
				SupportsDevice:     true,
			}, nil
		},
	}

	sess, err := p.Begin(context.Background(), &store.MCPServer{BaseURL: "https://mcp.example/mcp"})
	if err != nil {
		t.Fatal(err)
	}
	if sess.DeviceCode != "dc-hf" || !strings.Contains(sess.VerificationURI, "device") {
		t.Fatalf("session=%+v", sess)
	}

	storeSess := &store.OAuthSession{
		DeviceCode:    sess.DeviceCode,
		TokenEndpoint: sess.TokenEndpoint,
		ClientID:      sess.ClientID,
		AuthServer:    sess.AuthServer,
		ExpiresAt:     time.Now().Add(time.Minute),
	}
	var ts TokenSet
	for i := 0; i < 5; i++ {
		ts, err = p.Poll(context.Background(), storeSess)
		if err == ErrAuthorizationPending {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		break
	}
	if ts.AccessToken != "at-hf" || ts.RefreshToken != "rt-hf" {
		t.Fatalf("token=%+v", ts)
	}
}

func TestDeviceFlow_RFC8628Shape(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/device_authorization", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code":               "dc-rfc",
			"user_code":                 "WDJB-MJHT",
			"verification_uri":          "https://example.com/device",
			"verification_uri_complete": "https://example.com/device?user_code=WDJB-MJHT",
			"expires_in":                600,
			"interval":                  1,
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "authorization_pending",
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	p := &DeviceProvider{
		HTTPClient: srv.Client(),
		DiscoverFn: func(ctx context.Context, client *http.Client, mcpURL string) (*Discovered, error) {
			return &Discovered{
				AuthServer:         srv.URL,
				TokenEndpoint:      srv.URL + "/token",
				DeviceAuthEndpoint: srv.URL + "/device_authorization",
				ClientID:           DefaultClientID,
				SupportsDevice:     true,
			}, nil
		},
	}
	sess, err := p.Begin(context.Background(), &store.MCPServer{BaseURL: "https://x/mcp"})
	if err != nil {
		t.Fatal(err)
	}
	if sess.VerificationCode != "WDJB-MJHT" {
		t.Fatalf("user_code=%q", sess.VerificationCode)
	}
	_, err = p.Poll(context.Background(), &store.OAuthSession{
		DeviceCode:    sess.DeviceCode,
		TokenEndpoint: sess.TokenEndpoint,
		ClientID:      sess.ClientID,
		ExpiresAt:     time.Now().Add(time.Minute),
	})
	if err != ErrAuthorizationPending {
		t.Fatalf("want pending, got %v", err)
	}
}

func TestDeviceFlow_SlowDownThenGrant(t *testing.T) {
	var polls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code": "dc-sd", "verification_uri": "https://x", "expires_in": 900, "interval": 1,
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		n := polls.Add(1)
		switch n {
		case 1, 2:
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "slow_down"})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "at-sd", "refresh_token": "rt-sd", "expires_in": 60,
			})
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	p := &DeviceProvider{
		HTTPClient: srv.Client(),
		DiscoverFn: func(ctx context.Context, client *http.Client, mcpURL string) (*Discovered, error) {
			return &Discovered{
				AuthServer: srv.URL, TokenEndpoint: srv.URL + "/token",
				DeviceAuthEndpoint: srv.URL + "/authorize", ClientID: DefaultClientID, SupportsDevice: true,
			}, nil
		},
	}
	sess, _ := p.Begin(context.Background(), &store.MCPServer{BaseURL: "https://x"})
	storeSess := &store.OAuthSession{
		DeviceCode: sess.DeviceCode, TokenEndpoint: sess.TokenEndpoint,
		ClientID: sess.ClientID, ExpiresAt: time.Now().Add(time.Minute),
	}
	slow := 0
	var ts TokenSet
	var err error
	for i := 0; i < 6; i++ {
		ts, err = p.Poll(context.Background(), storeSess)
		if err == ErrSlowDown {
			slow++
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		break
	}
	if slow != 2 {
		t.Fatalf("slow_down count=%d", slow)
	}
	if ts.AccessToken != "at-sd" {
		t.Fatalf("token=%+v", ts)
	}
}

func TestDeviceRefresh_SuccessAndFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["refresh_token"] == "good" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "at-new", "refresh_token": "rt-new", "expires_in": 120,
			})
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	p := &DeviceProvider{HTTPClient: srv.Client()}
	ts, err := p.Refresh(context.Background(), &TokenSet{
		RefreshToken: "good", TokenEndpoint: srv.URL + "/token", ClientID: DefaultClientID,
	})
	if err != nil || ts.AccessToken != "at-new" {
		t.Fatalf("refresh success: %+v err=%v", ts, err)
	}
	_, err = p.Refresh(context.Background(), &TokenSet{
		RefreshToken: "bad", TokenEndpoint: srv.URL + "/token", ClientID: DefaultClientID,
	})
	if err == nil {
		t.Fatal("expected refresh failure")
	}
}
