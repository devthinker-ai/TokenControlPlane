package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

func (s *Store) CreateAccount(ctx context.Context, a Account) error {
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	if a.Plan == "" {
		a.Plan = PlanFree
	}
	seats, servers := PlanCaps(a.Plan)
	if a.MaxSeats == 0 {
		a.MaxSeats = seats
	}
	if a.MaxServers == 0 {
		a.MaxServers = servers
	}
	var lsCust any
	if a.LSCustomerID.Valid {
		lsCust = a.LSCustomerID.String
	}
	var needs any
	if a.NeedsManualKey.Valid {
		if a.NeedsManualKey.Bool {
			needs = 1
		} else {
			needs = 0
		}
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO accounts (id, name, plan, max_seats, max_servers, created_at, ls_customer_id, needs_manual_key)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.Name, a.Plan,
		a.MaxSeats, a.MaxServers, a.CreatedAt.UTC().Format(time.RFC3339Nano), lsCust, needs)
	return err
}

func (s *Store) GetAccount(ctx context.Context, id string) (*Account, error) {
	row := s.db.QueryRowContext(ctx, accountSelect+` WHERE id = ?`, id)
	return scanAccount(row)
}

const accountSelect = `
SELECT id, name, plan, max_seats, max_servers,
       onboarded_at, created_at, ls_customer_id, needs_manual_key
FROM accounts`

func scanAccount(row *sql.Row) (*Account, error) {
	var a Account
	var created string
	var onboarded, lsCust sql.NullString
	var needs sql.NullInt64
	if err := row.Scan(&a.ID, &a.Name, &a.Plan, &a.MaxSeats, &a.MaxServers,
		&onboarded, &created, &lsCust, &needs); err != nil {
		return nil, err
	}
	a.LSCustomerID = lsCust
	if needs.Valid {
		a.NeedsManualKey = sql.NullBool{Bool: needs.Int64 != 0, Valid: true}
	}
	if onboarded.Valid && onboarded.String != "" {
		t, err := time.Parse(time.RFC3339Nano, onboarded.String)
		if err != nil {
			t, _ = time.Parse(time.RFC3339, onboarded.String)
		}
		a.OnboardedAt = sql.NullTime{Time: t, Valid: true}
	}
	t, err := time.Parse(time.RFC3339Nano, created)
	if err != nil {
		t, _ = time.Parse(time.RFC3339, created)
	}
	a.CreatedAt = t
	return &a, nil
}

// UpdateAccountPlan sets plan + caps. Optional lsCustomerID updates ls_customer_id when non-nil.
func (s *Store) UpdateAccountPlan(ctx context.Context, id, plan string, lsCustomerID *string) error {
	seats, servers := PlanCaps(plan)
	var lsCust any
	if lsCustomerID != nil {
		lsCust = *lsCustomerID
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE accounts SET plan = ?, max_seats = ?, max_servers = ?,
  ls_customer_id = COALESCE(?, ls_customer_id)
WHERE id = ?`, plan, seats, servers, lsCust, id)
	return err
}

// SetAccountLSCustomerID stores the Lemon Squeezy customer id for portal links.
func (s *Store) SetAccountLSCustomerID(ctx context.Context, id, customerID string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE accounts SET ls_customer_id = ? WHERE id = ?`, customerID, id)
	return err
}

// SetAccountNeedsManualKey flags accounts that need a key re-minted (private key was unset).
func (s *Store) SetAccountNeedsManualKey(ctx context.Context, id string, needs bool) error {
	v := 0
	if needs {
		v = 1
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE accounts SET needs_manual_key = ? WHERE id = ?`, v, id)
	return err
}

// GetAccountByEmail finds the account via users.email (unique).
func (s *Store) GetAccountByEmail(ctx context.Context, email string) (*Account, error) {
	row := s.db.QueryRowContext(ctx, accountSelect+`
 WHERE id = (SELECT account_id FROM users WHERE email = ? LIMIT 1)`, email)
	return scanAccount(row)
}

// GetAccountByLSCustomer finds an account by Lemon Squeezy customer id.
func (s *Store) GetAccountByLSCustomer(ctx context.Context, customerID string) (*Account, error) {
	row := s.db.QueryRowContext(ctx, accountSelect+` WHERE ls_customer_id = ?`, customerID)
	return scanAccount(row)
}

// CreateAccountIfMissing creates an account + placeholder user for a buyer email, or returns the existing one.
func (s *Store) CreateAccountIfMissing(ctx context.Context, email, plan string) (*Account, error) {
	if acct, err := s.GetAccountByEmail(ctx, email); err == nil {
		return acct, nil
	}
	if plan == "" {
		plan = PlanFree
	}
	acctID := "acct_" + uuid.NewString()
	seats, servers := PlanCaps(plan)
	acct := Account{
		ID:         acctID,
		Name:       email,
		Plan:       plan,
		MaxSeats:   seats,
		MaxServers: servers,
		CreatedAt:  time.Now().UTC(),
	}
	if err := s.CreateAccount(ctx, acct); err != nil {
		return nil, err
	}
	// Placeholder user — buyer registers/logs in later with this email (password reset / support).
	// Random hash so the row is valid; they cannot know the password.
	hash := fmt.Sprintf("argon2id$v=19$m=65536,t=1,p=4$pending$%s", uuid.NewString())
	if err := s.CreateUser(ctx, User{
		ID:           "usr_" + uuid.NewString(),
		AccountID:    acctID,
		Email:        email,
		PasswordHash: hash,
		Name:         email,
		Role:         "admin",
	}); err != nil {
		return nil, err
	}
	return s.GetAccount(ctx, acctID)
}

func (s *Store) CreateUser(ctx context.Context, u User) error {
	if u.CreatedAt.IsZero() {
		u.CreatedAt = time.Now().UTC()
	}
	if u.Role == "" {
		u.Role = "admin"
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO users (id, account_id, email, password_hash, name, role, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.AccountID, u.Email, u.PasswordHash, u.Name, u.Role, u.CreatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, account_id, email, password_hash, name, role, created_at FROM users WHERE email = ?`, email)
	return scanUser(row)
}

func (s *Store) GetUser(ctx context.Context, id string) (*User, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, account_id, email, password_hash, name, role, created_at FROM users WHERE id = ?`, id)
	return scanUser(row)
}

func scanUser(row *sql.Row) (*User, error) {
	var u User
	var created string
	if err := row.Scan(&u.ID, &u.AccountID, &u.Email, &u.PasswordHash, &u.Name, &u.Role, &created); err != nil {
		return nil, err
	}
	t, err := time.Parse(time.RFC3339Nano, created)
	if err != nil {
		t, _ = time.Parse(time.RFC3339, created)
	}
	u.CreatedAt = t
	return &u, nil
}

func (s *Store) CountServers(ctx context.Context, accountID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mcp_servers WHERE account_id = ?`, accountID).Scan(&n)
	return n, err
}

func (s *Store) CountAPIKeys(ctx context.Context, accountID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_keys WHERE account_id = ?`, accountID).Scan(&n)
	return n, err
}

func (s *Store) ListServersByAccount(ctx context.Context, accountID string) ([]MCPServer, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, account_id, name, base_url, COALESCE(auth_type, 'static'), auth_header, auth_value, enabled,
       COALESCE(last_index_error, ''), created_at,
       COALESCE(transport, 'http'), COALESCE(command, ''), COALESCE(args, ''), COALESCE(env, ''),
       COALESCE(workdir, ''), COALESCE(cwd_isolation, 1)
FROM mcp_servers WHERE account_id = ? ORDER BY name`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MCPServer
	for rows.Next() {
		srv, err := scanMCPServer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *srv)
	}
	return out, rows.Err()
}

func (s *Store) UpdateServer(ctx context.Context, srv MCPServer) error {
	if srv.AuthType == "" {
		srv.AuthType = AuthTypeStatic
	}
	if srv.Transport == "" {
		srv.Transport = TransportHTTP
	}
	enabled := 0
	if srv.Enabled {
		enabled = 1
	}
	cwdIso := 0
	if srv.CwdIsolation {
		cwdIso = 1
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE mcp_servers SET name = ?, base_url = ?, auth_type = ?, auth_header = ?, auth_value = ?, enabled = ?,
  transport = ?, command = ?, args = ?, env = ?, workdir = ?, cwd_isolation = ?
WHERE id = ? AND account_id = ?`,
		srv.Name, srv.BaseURL, srv.AuthType, srv.AuthHeader, srv.AuthValue, enabled,
		srv.Transport, srv.Command, srv.ArgsJSON, srv.EnvJSON, srv.Workdir, cwdIso,
		srv.ID, srv.AccountID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) DeleteServer(ctx context.Context, accountID, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM tools WHERE server_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM oauth_tokens WHERE server_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM oauth_sessions WHERE server_id = ?`, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM mcp_servers WHERE id = ? AND account_id = ?`, id, accountID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

func (s *Store) CountTools(ctx context.Context, serverID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tools WHERE server_id = ?`, serverID).Scan(&n)
	return n, err
}

func (s *Store) ListAPIKeysByAccount(ctx context.Context, accountID string) ([]APIKey, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, account_id, key_hash, name, monthly_budget, rate_limit_rpm, enabled, killed_at, created_at, last_used_at, owner_id
FROM api_keys WHERE account_id = ? ORDER BY created_at DESC`, accountID)
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

func (s *Store) DeleteAPIKey(ctx context.Context, accountID, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM key_tool_grants WHERE key_id = ?`, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM api_keys WHERE id = ? AND account_id = ?`, id, accountID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

func (s *Store) ListUsageSummariesByAccount(ctx context.Context, accountID string, periodStart time.Time) ([]UsageSummary, error) {
	ps := periodStart.UTC().Format(time.RFC3339Nano)
	rows, err := s.db.QueryContext(ctx, `
SELECT k.id, k.name, COALESCE(u.tokens_used, 0), k.monthly_budget, COALESCE(u.requests, 0),
       k.killed_at, k.last_used_at
FROM api_keys k
LEFT JOIN usage u ON u.key_id = k.id AND u.period_start = ?
WHERE k.account_id = ?
ORDER BY k.name`, ps, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UsageSummary
	for rows.Next() {
		var sum UsageSummary
		var killed, lastUsed sql.NullString
		if err := rows.Scan(&sum.KeyID, &sum.Name, &sum.TokensUsed, &sum.MonthlyBudget, &sum.Requests, &killed, &lastUsed); err != nil {
			return nil, err
		}
		if killed.Valid && killed.String != "" {
			kt, err := time.Parse(time.RFC3339Nano, killed.String)
			if err != nil {
				kt, _ = time.Parse(time.RFC3339, killed.String)
			}
			sum.KilledAt = sql.NullTime{Time: kt, Valid: true}
		}
		if lastUsed.Valid {
			lu, err := time.Parse(time.RFC3339Nano, lastUsed.String)
			if err != nil {
				lu, _ = time.Parse(time.RFC3339, lastUsed.String)
			}
			sum.LastUsedAt = sql.NullTime{Time: lu, Valid: true}
		}
		out = append(out, sum)
	}
	return out, rows.Err()
}

// DayStartUTC returns midnight UTC for the calendar day of t.
func DayStartUTC(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

func (s *Store) IncrDailyUsage(ctx context.Context, accountID string, day time.Time, tokens, requests int64) error {
	ds := DayStartUTC(day).Format("2006-01-02")
	_, err := s.db.ExecContext(ctx, `
INSERT INTO usage_daily (account_id, day, tokens_used, requests)
VALUES (?, ?, ?, ?)
ON CONFLICT(account_id, day) DO UPDATE SET
  tokens_used = tokens_used + excluded.tokens_used,
  requests = requests + excluded.requests`, accountID, ds, tokens, requests)
	return err
}

func (s *Store) ListDailyUsage(ctx context.Context, accountID string, from, to time.Time) ([]DailyUsage, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT account_id, day, tokens_used, requests FROM usage_daily
WHERE account_id = ? AND day >= ? AND day <= ?
ORDER BY day`, accountID, DayStartUTC(from).Format("2006-01-02"), DayStartUTC(to).Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DailyUsage
	for rows.Next() {
		var d DailyUsage
		var dayStr string
		if err := rows.Scan(&d.AccountID, &dayStr, &d.TokensUsed, &d.Requests); err != nil {
			return nil, err
		}
		t, _ := time.Parse("2006-01-02", dayStr)
		d.Day = t
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) InsertToolCall(ctx context.Context, tc ToolCall) error {
	if tc.CreatedAt.IsZero() {
		tc.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO tool_calls (id, account_id, server_id, key_id, tool_name, created_at)
VALUES (?, ?, ?, ?, ?, ?)`,
		tc.ID, tc.AccountID, tc.ServerID, tc.KeyID, tc.ToolName, tc.CreatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

// ToolCallAgg is a top-tools row.
type ToolCallAgg struct {
	Name      string
	CallCount int64
}

func (s *Store) AggregateToolCalls(ctx context.Context, accountID string, limit int) ([]ToolCallAgg, error) {
	return s.AggregateToolCallsByServer(ctx, accountID, "", limit)
}

// AggregateToolCallsByServer is AggregateToolCalls with optional server_id filter.
func (s *Store) AggregateToolCallsByServer(ctx context.Context, accountID, serverID string, limit int) ([]ToolCallAgg, error) {
	if limit <= 0 {
		limit = 5
	}
	var rows *sql.Rows
	var err error
	if serverID != "" {
		rows, err = s.db.QueryContext(ctx, `
SELECT tool_name, COUNT(*) AS c FROM tool_calls
WHERE account_id = ? AND server_id = ?
GROUP BY tool_name ORDER BY c DESC LIMIT ?`, accountID, serverID, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, `
SELECT tool_name, COUNT(*) AS c FROM tool_calls
WHERE account_id = ?
GROUP BY tool_name ORDER BY c DESC LIMIT ?`, accountID, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ToolCallAgg
	for rows.Next() {
		var a ToolCallAgg
		if err := rows.Scan(&a.Name, &a.CallCount); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// MarkWebhookEventProcessed returns true if this is the first time seeing eventID (idempotency).
func (s *Store) MarkWebhookEventProcessed(ctx context.Context, eventID string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
INSERT OR IGNORE INTO webhook_events (id, processed_at) VALUES (?, ?)`,
		eventID, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) AccountIDForKey(ctx context.Context, keyID string) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT account_id FROM api_keys WHERE id = ?`, keyID).Scan(&id)
	return id, err
}
