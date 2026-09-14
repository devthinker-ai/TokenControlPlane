package upstream

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

func TestTokenGuard_RefreshOnExpiry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	st, err := store.OpenWithOptions(path, store.OpenOptions{AppVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	acct := "acct_" + uuid.NewString()
	_ = st.CreateAccount(context.Background(), store.Account{
		ID: acct, Name: "t", Plan: "free", MaxSeats: 3, MaxServers: 3,
	})
	sid := "srv_" + uuid.NewString()
	_ = st.CreateServer(context.Background(), store.MCPServer{
		ID: sid, AccountID: acct, Name: "s", BaseURL: "https://x",
		AuthType: store.AuthTypeOAuthDevice, Enabled: true,
	})

	// Wire a fake refresh via DeviceProvider against httptest — reuse Refresh tests style.
	// Here we only assert shouldRefresh + expired mark without network by injecting Provider.
	p := &fakeProvider{
		refresh: func(ctx context.Context, t *TokenSet) (TokenSet, error) {
			return TokenSet{
				AccessToken: "new", RefreshToken: "rt2", TokenType: "Bearer",
				ExpiresAt: time.Now().Add(time.Hour), TokenEndpoint: t.TokenEndpoint, ClientID: t.ClientID,
			}, nil
		},
	}
	g := NewTokenGuard(st, p, nil)
	_ = st.UpsertOAuthToken(context.Background(), store.OAuthToken{
		ID: "oat_1", AccountID: acct, ServerID: sid,
		AuthServer: "https://as", TokenEndpoint: "https://as/token",
		ClientID: DefaultClientID, AccessToken: "old", RefreshToken: "rt",
		TokenType: "Bearer", ExpiresAt: time.Now().Add(30 * time.Second), // within 60s
		Status: store.OAuthStatusConnected,
	})
	ts, err := g.TokenFor(context.Background(), acct, sid)
	if err != nil || ts.AccessToken != "new" {
		t.Fatalf("got %+v err=%v", ts, err)
	}
}

func TestTokenGuard_RefreshFailureMarksExpired(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g2.db")
	st, err := store.OpenWithOptions(path, store.OpenOptions{AppVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	acct := "acct_" + uuid.NewString()
	_ = st.CreateAccount(context.Background(), store.Account{
		ID: acct, Name: "t", Plan: "free", MaxSeats: 3, MaxServers: 3,
	})
	sid := "srv_" + uuid.NewString()
	_ = st.CreateServer(context.Background(), store.MCPServer{
		ID: sid, AccountID: acct, Name: "s", BaseURL: "https://x",
		AuthType: store.AuthTypeOAuthDevice, Enabled: true,
	})
	p := &fakeProvider{
		refresh: func(ctx context.Context, t *TokenSet) (TokenSet, error) {
			return TokenSet{}, ErrExpired
		},
	}
	g := NewTokenGuard(st, p, nil)
	_ = st.UpsertOAuthToken(context.Background(), store.OAuthToken{
		ID: "oat_2", AccountID: acct, ServerID: sid,
		AuthServer: "https://as", TokenEndpoint: "https://as/token",
		ClientID: DefaultClientID, AccessToken: "old", RefreshToken: "bad",
		ExpiresAt: time.Now().Add(-time.Minute), Status: store.OAuthStatusConnected,
	})
	_, err = g.TokenFor(context.Background(), acct, sid)
	if err != ErrExpired {
		t.Fatalf("want ErrExpired, got %v", err)
	}
	tok, _ := st.GetOAuthToken(context.Background(), acct, sid)
	if tok.Status != store.OAuthStatusExpired {
		t.Fatalf("status=%s", tok.Status)
	}
}

type fakeProvider struct {
	refresh func(ctx context.Context, t *TokenSet) (TokenSet, error)
}

func (f *fakeProvider) Begin(ctx context.Context, srv *store.MCPServer) (Session, error) {
	return Session{}, nil
}
func (f *fakeProvider) Poll(ctx context.Context, sess *store.OAuthSession) (TokenSet, error) {
	return TokenSet{}, nil
}
func (f *fakeProvider) Refresh(ctx context.Context, t *TokenSet) (TokenSet, error) {
	return f.refresh(ctx, t)
}
func (f *fakeProvider) Header(t *TokenSet) (string, error) {
	return "Bearer " + t.AccessToken, nil
}
