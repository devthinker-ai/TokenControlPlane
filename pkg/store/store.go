// Package store provides SQLite persistence for the TokenControlPlane (no ORM).
package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Account is a billing/tenant container (free|pro|team).
type Account struct {
	ID             string
	Name           string
	Plan           string // free|pro|team
	LSCustomerID   sql.NullString
	NeedsManualKey sql.NullBool
	MaxSeats       int
	MaxServers     int
	OnboardedAt    sql.NullTime
	CreatedAt      time.Time
}

// Auth type values for MCPServer.AuthType.
const (
	AuthTypeNone        = "none"
	AuthTypeStatic      = "static"
	AuthTypeOAuthDevice = "oauth_device"
	AuthTypeOAuthPKCE   = "oauth_pkce"
)

// Transport values for MCPServer.Transport.
const (
	TransportHTTP  = "http"
	TransportStdio = "stdio"
)

// MCPServer is an upstream MCP endpoint (remote HTTP or local stdio).
// AuthValue and OAuth tokens are plaintext in v1; encryption-at-rest is deferred to v1.1.
// ArgsJSON / EnvJSON are validated JSON (array / object) at write time.
type MCPServer struct {
	ID             string
	AccountID      string
	Name           string
	BaseURL        string
	AuthType       string // none|static|oauth_device|oauth_pkce
	AuthHeader     string // default "Authorization" (static only)
	AuthValue      string // full header value, e.g. "Bearer sk-upstream" (static only)
	Transport      string // http|stdio
	Command        string // stdio: executable (e.g. npx)
	ArgsJSON       string // stdio: JSON array of strings
	EnvJSON        string // stdio: JSON object merged over process env
	Workdir        string // stdio: optional cwd
	CwdIsolation   bool   // stdio: default sandbox under ~/.tokencontrolplane/sandbox/<id>
	Enabled        bool
	LastIndexError string // empty when last index succeeded
	CreatedAt      time.Time
}

// APIKey is a machine gateway key (tcp_*). Only the SHA-256 hash is stored.
type APIKey struct {
	ID            string
	AccountID     string
	KeyHash       string
	Name          string
	MonthlyBudget int64 // estimated tokens (bytes/4) per calendar-month; 0 = unlimited
	RateLimitRPM  int   // requests per minute; 0 = unlimited
	Enabled       bool
	KilledAt      sql.NullTime
	CreatedAt     time.Time
	LastUsedAt    sql.NullTime
	OwnerID       sql.NullString // dashboard user who created it; NULL = legacy/shared
}

// User is a dashboard login (JWT sessions — not machine keys).
type User struct {
	ID           string
	AccountID    string
	Email        string
	PasswordHash string
	Name         string
	Role         string // admin|member
	CreatedAt    time.Time
}

// DailyUsage is per-account (or per-key) token totals for one UTC day.
type DailyUsage struct {
	AccountID  string
	Day        time.Time // date at 00:00 UTC
	TokensUsed int64
	Requests   int64
}

// ToolCall is a best-effort tools/call log entry for the dashboard.
type ToolCall struct {
	ID        string
	AccountID string
	ServerID  string
	KeyID     string
	ToolName  string
	CreatedAt time.Time
}

// DefaultAccountID owns legacy phase-1/2 rows after migration.
const DefaultAccountID = "acct_default"

const (
	PlanFree = "free"
	PlanPro  = "pro"
	PlanTeam = "team"
)

// PlanCaps returns seat/server limits for a plan name (kept in sync with license.CapsForPlan).
func PlanCaps(plan string) (maxSeats, maxServers int) {
	switch plan {
	case PlanPro:
		return 10, 10
	case PlanTeam:
		return 25, 25
	default:
		return 3, 3
	}
}

// PlanMaxKeys returns API key limits for a plan (kept in sync with license.CapsForPlan).
func PlanMaxKeys(plan string) int {
	switch plan {
	case PlanPro:
		return 20
	case PlanTeam:
		return 50
	default:
		return 5
	}
}

// Usage is incremental metering for one key in one calendar-month UTC period.
type Usage struct {
	KeyID       string
	PeriodStart time.Time
	TokensUsed  int64
	BytesIn     int64
	BytesOut    int64
	Requests    int64
}

// UsageSummary is the admin /admin/usage row for the current period.
type UsageSummary struct {
	KeyID         string
	Name          string
	TokensUsed    int64
	MonthlyBudget int64
	Requests      int64
	KilledAt      sql.NullTime
	LastUsedAt    sql.NullTime
}

// ToolDef is a cached tools/list entry for the dashboard.
type ToolDef struct {
	ID          string
	ServerID    string
	Name        string
	Description string
	InputSchema string // JSON
	Enabled     bool
}

// Store wraps a SQLite database.
type Store struct {
	db     *sql.DB
	dbPath string
}

// OpenOptions configures store open / migrate.
type OpenOptions struct {
	AppVersion string
	Logger     *slog.Logger
}

// Open opens (or creates) a SQLite database at path with mode 0600.
func Open(path string) (*Store, error) {
	return OpenWithOptions(path, OpenOptions{})
}

// OpenWithOptions opens the DB and runs numbered migrations.
func OpenWithOptions(path string, opts OpenOptions) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil && filepath.Dir(path) != "." && filepath.Dir(path) != "" {
		return nil, fmt.Errorf("mkdir db dir: %w", err)
	}
	// busy_timeout: concurrent TouchAPIKeyUsed + metering UPDATEs must wait, not fail.
	dsn := path
	barePath := path
	if !strings.Contains(path, "?") {
		dsn = path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	} else {
		if i := strings.Index(path, "?"); i >= 0 {
			barePath = path[:i]
		}
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite + incremental UPDATEs; keep writes serialized
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if err := os.Chmod(barePath, 0o600); err != nil && !os.IsNotExist(err) {
		// File may not exist yet on some drivers until first write; ignore.
	}
	s := &Store{db: db, dbPath: barePath}
	if err := s.migrate(barePath, opts.AppVersion, opts.Logger); err != nil {
		_ = db.Close()
		return nil, err
	}
	// Ensure 0600 after schema create (chmod the bare path, not the DSN).
	_ = os.Chmod(barePath, 0o600)
	return s, nil
}

// Close closes the underlying database.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB exposes the raw *sql.DB for packages that need transactions (tests).
func (s *Store) DB() *sql.DB { return s.db }

// Ping checks SQLite connectivity (readyz).
func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// PeriodStartUTC returns the first instant of the calendar month containing t (UTC).
func PeriodStartUTC(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func (s *Store) CreateServer(ctx context.Context, srv MCPServer) error {
	if srv.AuthHeader == "" {
		srv.AuthHeader = "Authorization"
	}
	if srv.AuthType == "" {
		if srv.AuthValue == "" {
			srv.AuthType = AuthTypeNone
		} else {
			srv.AuthType = AuthTypeStatic
		}
	}
	if srv.Transport == "" {
		srv.Transport = TransportHTTP
	}
	if srv.AccountID == "" {
		srv.AccountID = DefaultAccountID
	}
	if srv.CreatedAt.IsZero() {
		srv.CreatedAt = time.Now().UTC()
	}
	enabled := 0
	if srv.Enabled {
		enabled = 1
	}
	cwdIso := 0
	if srv.CwdIsolation {
		cwdIso = 1
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO mcp_servers (
  id, account_id, name, base_url, auth_type, auth_header, auth_value, enabled, created_at,
  transport, command, args, env, workdir, cwd_isolation
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		srv.ID, srv.AccountID, srv.Name, srv.BaseURL, srv.AuthType, srv.AuthHeader, srv.AuthValue, enabled,
		srv.CreatedAt.UTC().Format(time.RFC3339Nano),
		srv.Transport, srv.Command, srv.ArgsJSON, srv.EnvJSON, srv.Workdir, cwdIso)
	return err
}

func (s *Store) GetServer(ctx context.Context, id string) (*MCPServer, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, account_id, name, base_url, COALESCE(auth_type, 'static'), auth_header, auth_value, enabled,
       COALESCE(last_index_error, ''), created_at,
       COALESCE(transport, 'http'), COALESCE(command, ''), COALESCE(args, ''), COALESCE(env, ''),
       COALESCE(workdir, ''), COALESCE(cwd_isolation, 1)
FROM mcp_servers WHERE id = ?`, id)
	return scanMCPServer(row)
}

func scanMCPServer(row scannable) (*MCPServer, error) {
	var srv MCPServer
	var enabled, cwdIso int
	var created string
	if err := row.Scan(
		&srv.ID, &srv.AccountID, &srv.Name, &srv.BaseURL, &srv.AuthType, &srv.AuthHeader, &srv.AuthValue, &enabled,
		&srv.LastIndexError, &created,
		&srv.Transport, &srv.Command, &srv.ArgsJSON, &srv.EnvJSON, &srv.Workdir, &cwdIso,
	); err != nil {
		return nil, err
	}
	srv.Enabled = enabled == 1
	srv.CwdIsolation = cwdIso == 1
	t, err := time.Parse(time.RFC3339Nano, created)
	if err != nil {
		t, _ = time.Parse(time.RFC3339, created)
	}
	srv.CreatedAt = t
	return &srv, nil
}

func (s *Store) SetServerEnabled(ctx context.Context, id string, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	res, err := s.db.ExecContext(ctx, `UPDATE mcp_servers SET enabled = ? WHERE id = ?`, v, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) CreateAPIKey(ctx context.Context, key APIKey) error {
	if key.CreatedAt.IsZero() {
		key.CreatedAt = time.Now().UTC()
	}
	if key.AccountID == "" {
		key.AccountID = DefaultAccountID
	}
	enabled := 0
	if key.Enabled {
		enabled = 1
	}
	var killed any
	if key.KilledAt.Valid {
		killed = key.KilledAt.Time.UTC().Format(time.RFC3339Nano)
	}
	var ownerID any
	if key.OwnerID.Valid && key.OwnerID.String != "" {
		ownerID = key.OwnerID.String
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO api_keys (id, account_id, key_hash, name, token_budget, monthly_budget, rate_limit_rpm, enabled, killed_at, created_at, last_used_at, owner_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?)`,
		key.ID, key.AccountID, key.KeyHash, key.Name, key.MonthlyBudget, key.MonthlyBudget, key.RateLimitRPM, enabled, killed, key.CreatedAt.UTC().Format(time.RFC3339Nano), ownerID)
	return err
}

func (s *Store) GetAPIKeyByHash(ctx context.Context, hash string) (*APIKey, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, account_id, key_hash, name, monthly_budget, rate_limit_rpm, enabled, killed_at, created_at, last_used_at, owner_id
FROM api_keys WHERE key_hash = ?`, hash)
	return scanAPIKey(row)
}

func (s *Store) GetAPIKey(ctx context.Context, id string) (*APIKey, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, account_id, key_hash, name, monthly_budget, rate_limit_rpm, enabled, killed_at, created_at, last_used_at, owner_id
FROM api_keys WHERE id = ?`, id)
	return scanAPIKey(row)
}

func (s *Store) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, account_id, key_hash, name, monthly_budget, rate_limit_rpm, enabled, killed_at, created_at, last_used_at, owner_id
FROM api_keys`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIKey
	for rows.Next() {
		k, err := scanAPIKeyRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *k)
	}
	return out, rows.Err()
}

func scanAPIKey(row *sql.Row) (*APIKey, error) {
	return scanAPIKeyRows(row)
}

type scannable interface {
	Scan(dest ...any) error
}

func scanAPIKeyRows(row scannable) (*APIKey, error) {
	var k APIKey
	var enabled int
	var created string
	var killed, lastUsed, ownerID sql.NullString
	if err := row.Scan(&k.ID, &k.AccountID, &k.KeyHash, &k.Name, &k.MonthlyBudget, &k.RateLimitRPM, &enabled, &killed, &created, &lastUsed, &ownerID); err != nil {
		return nil, err
	}
	k.Enabled = enabled == 1
	k.OwnerID = ownerID
	t, err := time.Parse(time.RFC3339Nano, created)
	if err != nil {
		t, _ = time.Parse(time.RFC3339, created)
	}
	k.CreatedAt = t
	if killed.Valid && killed.String != "" {
		kt, err := time.Parse(time.RFC3339Nano, killed.String)
		if err != nil {
			kt, _ = time.Parse(time.RFC3339, killed.String)
		}
		k.KilledAt = sql.NullTime{Time: kt, Valid: true}
	}
	if lastUsed.Valid {
		lu, err := time.Parse(time.RFC3339Nano, lastUsed.String)
		if err != nil {
			lu, _ = time.Parse(time.RFC3339, lastUsed.String)
		}
		k.LastUsedAt = sql.NullTime{Time: lu, Valid: true}
	}
	return &k, nil
}

func (s *Store) TouchAPIKeyUsed(ctx context.Context, id string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE api_keys SET last_used_at = ? WHERE id = ?`,
		at.UTC().Format(time.RFC3339Nano), id)
	return err
}

// SetKeyKilled sets or clears killed_at. Pass nil to clear (unkill).
func (s *Store) SetKeyKilled(ctx context.Context, id string, at *time.Time) error {
	var v any
	if at != nil {
		v = at.UTC().Format(time.RFC3339Nano)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE api_keys SET killed_at = ? WHERE id = ?`, v, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ResetUsagePeriod zeros the usage row for keyID/period (unkill?reset=true).
func (s *Store) ResetUsagePeriod(ctx context.Context, keyID string, periodStart time.Time) error {
	ps := periodStart.UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO usage (key_id, period_start, tokens_used, bytes_in, bytes_out, requests)
VALUES (?, ?, 0, 0, 0, 0)
ON CONFLICT(key_id, period_start) DO UPDATE SET
  tokens_used = 0, bytes_in = 0, bytes_out = 0, requests = 0`, keyID, ps)
	return err
}

// IncrUsage atomically increments usage for key_id in the given period.
// Never read-modify-write: SQL does the increment.
// Period rollover: callers pass PeriodStartUTC(now); a new month creates a fresh row.
func (s *Store) IncrUsage(ctx context.Context, keyID string, periodStart time.Time, bytesIn, bytesOut, tokens, requests int64) error {
	ps := periodStart.UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO usage (key_id, period_start, tokens_used, bytes_in, bytes_out, requests)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(key_id, period_start) DO UPDATE SET
  tokens_used = tokens_used + excluded.tokens_used,
  bytes_in = bytes_in + excluded.bytes_in,
  bytes_out = bytes_out + excluded.bytes_out,
  requests = requests + excluded.requests`,
		keyID, ps, tokens, bytesIn, bytesOut, requests)
	return err
}

func (s *Store) GetUsage(ctx context.Context, keyID string, periodStart time.Time) (*Usage, error) {
	ps := periodStart.UTC().Format(time.RFC3339Nano)
	row := s.db.QueryRowContext(ctx, `
SELECT key_id, period_start, tokens_used, bytes_in, bytes_out, requests
FROM usage WHERE key_id = ? AND period_start = ?`, keyID, ps)
	var u Usage
	var psStr string
	if err := row.Scan(&u.KeyID, &psStr, &u.TokensUsed, &u.BytesIn, &u.BytesOut, &u.Requests); err != nil {
		if err == sql.ErrNoRows {
			return &Usage{KeyID: keyID, PeriodStart: periodStart}, nil
		}
		return nil, err
	}
	t, err := time.Parse(time.RFC3339Nano, psStr)
	if err != nil {
		t, _ = time.Parse(time.RFC3339, psStr)
	}
	u.PeriodStart = t
	return &u, nil
}

// ListUsageSummaries returns per-key current-period usage for /admin/usage.
func (s *Store) ListUsageSummaries(ctx context.Context, periodStart time.Time) ([]UsageSummary, error) {
	ps := periodStart.UTC().Format(time.RFC3339Nano)
	rows, err := s.db.QueryContext(ctx, `
SELECT k.id, k.name, COALESCE(u.tokens_used, 0), k.monthly_budget, COALESCE(u.requests, 0),
       k.killed_at, k.last_used_at
FROM api_keys k
LEFT JOIN usage u ON u.key_id = k.id AND u.period_start = ?
ORDER BY k.name`, ps)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UsageSummary
	for rows.Next() {
		var s UsageSummary
		var killed, lastUsed sql.NullString
		if err := rows.Scan(&s.KeyID, &s.Name, &s.TokensUsed, &s.MonthlyBudget, &s.Requests, &killed, &lastUsed); err != nil {
			return nil, err
		}
		if killed.Valid && killed.String != "" {
			kt, err := time.Parse(time.RFC3339Nano, killed.String)
			if err != nil {
				kt, _ = time.Parse(time.RFC3339, killed.String)
			}
			s.KilledAt = sql.NullTime{Time: kt, Valid: true}
		}
		if lastUsed.Valid {
			lu, err := time.Parse(time.RFC3339Nano, lastUsed.String)
			if err != nil {
				lu, _ = time.Parse(time.RFC3339, lastUsed.String)
			}
			s.LastUsedAt = sql.NullTime{Time: lu, Valid: true}
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (s *Store) ReplaceTools(ctx context.Context, serverID string, tools []ToolDef) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Preserve per-tool enabled flags across reindex.
	prev := map[string]bool{}
	rows, err := tx.QueryContext(ctx, `SELECT name, COALESCE(enabled, 1) FROM tools WHERE server_id = ?`, serverID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var name string
		var en int
		if err := rows.Scan(&name, &en); err != nil {
			_ = rows.Close()
			return err
		}
		prev[name] = en == 1
	}
	_ = rows.Close()

	if _, err := tx.ExecContext(ctx, `DELETE FROM tools WHERE server_id = ?`, serverID); err != nil {
		return err
	}
	for _, t := range tools {
		en := 1
		if was, ok := prev[t.Name]; ok && !was {
			en = 0 // keep previously disabled tools disabled
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO tools (id, server_id, name, description, input_schema, enabled)
VALUES (?, ?, ?, ?, ?, ?)`, t.ID, serverID, t.Name, t.Description, t.InputSchema, en); err != nil {
			return err
		}
	}
	// Prune grants for tool names that no longer exist in the catalog.
	if _, err := tx.ExecContext(ctx, `
DELETE FROM key_tool_grants
WHERE tool_name NOT IN (SELECT DISTINCT name FROM tools)`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ListTools(ctx context.Context, serverID string) ([]ToolDef, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, server_id, name, description, input_schema, COALESCE(enabled, 1)
FROM tools WHERE server_id = ? ORDER BY name`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ToolDef
	for rows.Next() {
		var t ToolDef
		var en int
		if err := rows.Scan(&t.ID, &t.ServerID, &t.Name, &t.Description, &t.InputSchema, &en); err != nil {
			return nil, err
		}
		t.Enabled = en == 1
		out = append(out, t)
	}
	return out, rows.Err()
}
