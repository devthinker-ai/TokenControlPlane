package store

import (
	"context"
	"database/sql"
	"time"
)

// OAuth token / session status values.
const (
	OAuthStatusConnected    = "connected"
	OAuthStatusExpired      = "expired"
	OAuthStatusPending      = "pending"
	OAuthStatusDisconnected = "disconnected"

	OAuthFlowDevice = "device"
	OAuthFlowPKCE   = "pkce"
)

// OAuthToken is a persisted upstream OAuth credential set for one (account, server).
// AccessToken and RefreshToken are plaintext in v1 (same documented status as AuthValue).
type OAuthToken struct {
	ID                 string
	AccountID          string
	ServerID           string
	AuthServer         string
	TokenEndpoint      string
	DeviceAuthEndpoint string // nullable
	ClientID           string
	Scopes             string
	AccessToken        string
	RefreshToken       string
	TokenType          string
	ExpiresAt          time.Time // zero = unknown
	Status             string    // connected|expired
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// OAuthSession is an in-flight device or PKCE flow.
type OAuthSession struct {
	ID                            string
	AccountID                     string
	ServerID                      string
	Flow                          string // device|pkce
	DeviceCode                    string
	VerificationURI               string
	VerificationCode              string
	IntervalS                     int
	ExpiresAt                     time.Time
	AuthorizeURL                  string
	State                         string
	CodeVerifier                  string
	AuthServer                    string
	TokenEndpoint                 string
	DeviceAuthEndpoint            string
	ClientID                      string
	Scopes                        string
	ProtectedResourceMetadataURL  string
	CreatedAt                     time.Time
}

// UpsertOAuthToken inserts or replaces the token row for (account, server).
func (s *Store) UpsertOAuthToken(ctx context.Context, t OAuthToken) error {
	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	if t.TokenType == "" {
		t.TokenType = "Bearer"
	}
	if t.Status == "" {
		t.Status = OAuthStatusConnected
	}
	expires := ""
	if !t.ExpiresAt.IsZero() {
		expires = t.ExpiresAt.UTC().Format(time.RFC3339)
	}
	var deviceAuth any
	if t.DeviceAuthEndpoint != "" {
		deviceAuth = t.DeviceAuthEndpoint
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO oauth_tokens (
  id, account_id, server_id, auth_server, token_endpoint, device_auth_endpoint,
  client_id, scopes, access_token, refresh_token, token_type, expires_at, status, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(account_id, server_id) DO UPDATE SET
  auth_server = excluded.auth_server,
  token_endpoint = excluded.token_endpoint,
  device_auth_endpoint = excluded.device_auth_endpoint,
  client_id = excluded.client_id,
  scopes = excluded.scopes,
  access_token = excluded.access_token,
  refresh_token = excluded.refresh_token,
  token_type = excluded.token_type,
  expires_at = excluded.expires_at,
  status = excluded.status,
  updated_at = excluded.updated_at`,
		t.ID, t.AccountID, t.ServerID, t.AuthServer, t.TokenEndpoint, deviceAuth,
		t.ClientID, t.Scopes, t.AccessToken, t.RefreshToken, t.TokenType, expires, t.Status,
		t.CreatedAt.UTC().Format(time.RFC3339Nano), t.UpdatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

// GetOAuthToken returns the token for (account, server), or sql.ErrNoRows.
func (s *Store) GetOAuthToken(ctx context.Context, accountID, serverID string) (*OAuthToken, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, account_id, server_id, auth_server, token_endpoint, COALESCE(device_auth_endpoint, ''),
       client_id, scopes, access_token, refresh_token, token_type, expires_at, status, created_at, updated_at
FROM oauth_tokens WHERE account_id = ? AND server_id = ?`, accountID, serverID)
	return scanOAuthToken(row)
}

// MarkOAuthTokenExpired sets status=expired (refresh failed — reconnect needed).
func (s *Store) MarkOAuthTokenExpired(ctx context.Context, accountID, serverID string) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE oauth_tokens SET status = ?, updated_at = ? WHERE account_id = ? AND server_id = ?`,
		OAuthStatusExpired, time.Now().UTC().Format(time.RFC3339Nano), accountID, serverID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteOAuthToken removes the stored token for (account, server).
func (s *Store) DeleteOAuthToken(ctx context.Context, accountID, serverID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM oauth_tokens WHERE account_id = ? AND server_id = ?`, accountID, serverID)
	return err
}

// SaveOAuthSession upserts the in-flight session (one per account+server).
func (s *Store) SaveOAuthSession(ctx context.Context, sess OAuthSession) error {
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = time.Now().UTC()
	}
	if sess.Flow == "" {
		sess.Flow = OAuthFlowDevice
	}
	if sess.IntervalS <= 0 {
		sess.IntervalS = 5
	}
	if sess.ClientID == "" {
		sess.ClientID = "tokencontrolplane"
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO oauth_sessions (
  id, account_id, server_id, flow, device_code, verification_uri, verification_code,
  interval_s, expires_at, authorize_url, state, code_verifier, auth_server, token_endpoint,
  device_auth_endpoint, client_id, scopes, protected_resource_metadata_url, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(account_id, server_id) DO UPDATE SET
  id = excluded.id,
  flow = excluded.flow,
  device_code = excluded.device_code,
  verification_uri = excluded.verification_uri,
  verification_code = excluded.verification_code,
  interval_s = excluded.interval_s,
  expires_at = excluded.expires_at,
  authorize_url = excluded.authorize_url,
  state = excluded.state,
  code_verifier = excluded.code_verifier,
  auth_server = excluded.auth_server,
  token_endpoint = excluded.token_endpoint,
  device_auth_endpoint = excluded.device_auth_endpoint,
  client_id = excluded.client_id,
  scopes = excluded.scopes,
  protected_resource_metadata_url = excluded.protected_resource_metadata_url,
  created_at = excluded.created_at`,
		sess.ID, sess.AccountID, sess.ServerID, sess.Flow, sess.DeviceCode, sess.VerificationURI, sess.VerificationCode,
		sess.IntervalS, sess.ExpiresAt.UTC().Format(time.RFC3339Nano), sess.AuthorizeURL, sess.State, sess.CodeVerifier,
		sess.AuthServer, sess.TokenEndpoint, sess.DeviceAuthEndpoint, sess.ClientID, sess.Scopes,
		sess.ProtectedResourceMetadataURL, sess.CreatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

// GetPendingOAuthSession returns the in-flight session for (account, server).
func (s *Store) GetPendingOAuthSession(ctx context.Context, accountID, serverID string) (*OAuthSession, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, account_id, server_id, flow, device_code, verification_uri, verification_code,
       interval_s, expires_at, authorize_url, state, code_verifier, auth_server, token_endpoint,
       device_auth_endpoint, client_id, scopes, protected_resource_metadata_url, created_at
FROM oauth_sessions WHERE account_id = ? AND server_id = ?`, accountID, serverID)
	return scanOAuthSession(row)
}

// GetOAuthSessionByState looks up a PKCE session by CSRF state (public redirect).
func (s *Store) GetOAuthSessionByState(ctx context.Context, state string) (*OAuthSession, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, account_id, server_id, flow, device_code, verification_uri, verification_code,
       interval_s, expires_at, authorize_url, state, code_verifier, auth_server, token_endpoint,
       device_auth_endpoint, client_id, scopes, protected_resource_metadata_url, created_at
FROM oauth_sessions WHERE state = ?`, state)
	return scanOAuthSession(row)
}

// DeleteOAuthSession removes the in-flight session.
func (s *Store) DeleteOAuthSession(ctx context.Context, accountID, serverID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM oauth_sessions WHERE account_id = ? AND server_id = ?`, accountID, serverID)
	return err
}

// OAuthCredentialStatus returns disconnected|pending|connected|expired for the dashboard.
func (s *Store) OAuthCredentialStatus(ctx context.Context, accountID, serverID string) (string, error) {
	tok, err := s.GetOAuthToken(ctx, accountID, serverID)
	if err == nil {
		if tok.Status == OAuthStatusExpired {
			return OAuthStatusExpired, nil
		}
		return OAuthStatusConnected, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}
	_, err = s.GetPendingOAuthSession(ctx, accountID, serverID)
	if err == nil {
		return OAuthStatusPending, nil
	}
	if err == sql.ErrNoRows {
		return OAuthStatusDisconnected, nil
	}
	return "", err
}

func scanOAuthToken(row scannable) (*OAuthToken, error) {
	var t OAuthToken
	var expires, created, updated string
	if err := row.Scan(
		&t.ID, &t.AccountID, &t.ServerID, &t.AuthServer, &t.TokenEndpoint, &t.DeviceAuthEndpoint,
		&t.ClientID, &t.Scopes, &t.AccessToken, &t.RefreshToken, &t.TokenType, &expires, &t.Status,
		&created, &updated,
	); err != nil {
		return nil, err
	}
	if expires != "" {
		if et, err := time.Parse(time.RFC3339, expires); err == nil {
			t.ExpiresAt = et
		} else if et, err := time.Parse(time.RFC3339Nano, expires); err == nil {
			t.ExpiresAt = et
		}
	}
	t.CreatedAt = parseTime(created)
	t.UpdatedAt = parseTime(updated)
	return &t, nil
}

func scanOAuthSession(row scannable) (*OAuthSession, error) {
	var sess OAuthSession
	var expires, created string
	if err := row.Scan(
		&sess.ID, &sess.AccountID, &sess.ServerID, &sess.Flow, &sess.DeviceCode, &sess.VerificationURI,
		&sess.VerificationCode, &sess.IntervalS, &expires, &sess.AuthorizeURL, &sess.State, &sess.CodeVerifier,
		&sess.AuthServer, &sess.TokenEndpoint, &sess.DeviceAuthEndpoint, &sess.ClientID, &sess.Scopes,
		&sess.ProtectedResourceMetadataURL, &created,
	); err != nil {
		return nil, err
	}
	sess.ExpiresAt = parseTime(expires)
	sess.CreatedAt = parseTime(created)
	return &sess, nil
}
