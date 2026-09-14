package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const (
	ServerPolicyAll    = "all"
	ServerPolicyCustom = "custom"
)

// KeyServerPolicy is per-key server allowlist mode + grants.
type KeyServerPolicy struct {
	Mode    string   // all|custom
	Allowed []string // server IDs when custom
}

// KeyServerBudget is a per-server monthly token cap for a key (0 = no cap).
type KeyServerBudget struct {
	ServerID      string
	MonthlyBudget int64
}

// ServerUsageRow is one server's usage for the current period (account or key view).
type ServerUsageRow struct {
	ServerID      string
	Name          string
	TokensUsed    int64
	Requests      int64
	MonthlyBudget int64 // key-scoped views only
}

// ServerUsageByKey is a key breakdown under a server in account usage.
type ServerUsageByKey struct {
	KeyID      string
	Name       string
	TokensUsed int64
	Requests   int64
}

// AccountServerUsage is account-level per-server usage with per-key breakdown.
type AccountServerUsage struct {
	ServerID   string
	Name       string
	TokensUsed int64
	Requests   int64
	ByKey      []ServerUsageByKey
}

// GetKeyServerPolicy returns mode + allowed server IDs.
func (s *Store) GetKeyServerPolicy(ctx context.Context, keyID string) (KeyServerPolicy, error) {
	var mode string
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(server_policy, 'all') FROM api_keys WHERE id = ?`, keyID).Scan(&mode)
	if err != nil {
		return KeyServerPolicy{}, err
	}
	if mode == "" {
		mode = ServerPolicyAll
	}
	rows, err := s.db.QueryContext(ctx, `SELECT server_id FROM key_server_grants WHERE key_id = ?`, keyID)
	if err != nil {
		return KeyServerPolicy{}, err
	}
	defer rows.Close()
	var allowed []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return KeyServerPolicy{}, err
		}
		allowed = append(allowed, id)
	}
	if allowed == nil {
		allowed = []string{}
	}
	return KeyServerPolicy{Mode: mode, Allowed: allowed}, rows.Err()
}

// SetKeyServerPolicy sets mode and replaces server grants atomically.
func (s *Store) SetKeyServerPolicy(ctx context.Context, keyID, mode string, serverIDs []string) error {
	if mode != ServerPolicyAll && mode != ServerPolicyCustom {
		return fmt.Errorf("invalid server_policy %q", mode)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `UPDATE api_keys SET server_policy = ? WHERE id = ?`, mode, keyID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM key_server_grants WHERE key_id = ?`, keyID); err != nil {
		return err
	}
	if mode == ServerPolicyCustom {
		for _, sid := range serverIDs {
			if sid == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO key_server_grants (key_id, server_id) VALUES (?, ?)`, keyID, sid); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// ListKeyServerBudgets returns per-server budgets for a key.
func (s *Store) ListKeyServerBudgets(ctx context.Context, keyID string) ([]KeyServerBudget, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT server_id, monthly_budget FROM key_server_budgets WHERE key_id = ?`, keyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []KeyServerBudget
	for rows.Next() {
		var b KeyServerBudget
		if err := rows.Scan(&b.ServerID, &b.MonthlyBudget); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	if out == nil {
		out = []KeyServerBudget{}
	}
	return out, rows.Err()
}

// SetKeyServerBudgets replaces all per-server budgets for a key (transactional).
func (s *Store) SetKeyServerBudgets(ctx context.Context, keyID string, budgets []KeyServerBudget) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM key_server_budgets WHERE key_id = ?`, keyID); err != nil {
		return err
	}
	for _, b := range budgets {
		if b.ServerID == "" || b.MonthlyBudget < 0 {
			continue
		}
		if b.MonthlyBudget == 0 {
			continue // omit = no cap
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO key_server_budgets (key_id, server_id, monthly_budget) VALUES (?, ?, ?)`,
			keyID, b.ServerID, b.MonthlyBudget); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetKeyServerAccess sets server policy + grants + budgets in one transaction.
func (s *Store) SetKeyServerAccess(ctx context.Context, keyID, mode string, serverIDs []string, budgets []KeyServerBudget) error {
	if mode != ServerPolicyAll && mode != ServerPolicyCustom {
		return fmt.Errorf("invalid server_policy %q", mode)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `UPDATE api_keys SET server_policy = ? WHERE id = ?`, mode, keyID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM key_server_grants WHERE key_id = ?`, keyID); err != nil {
		return err
	}
	if mode == ServerPolicyCustom {
		for _, sid := range serverIDs {
			if sid == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO key_server_grants (key_id, server_id) VALUES (?, ?)`, keyID, sid); err != nil {
				return err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM key_server_budgets WHERE key_id = ?`, keyID); err != nil {
		return err
	}
	allowed := map[string]struct{}{}
	for _, sid := range serverIDs {
		allowed[sid] = struct{}{}
	}
	for _, b := range budgets {
		if b.ServerID == "" || b.MonthlyBudget <= 0 {
			continue
		}
		if mode == ServerPolicyCustom {
			if _, ok := allowed[b.ServerID]; !ok {
				return fmt.Errorf("budget for server %s not in allowed list", b.ServerID)
			}
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO key_server_budgets (key_id, server_id, monthly_budget) VALUES (?, ?, ?)`,
			keyID, b.ServerID, b.MonthlyBudget); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// IncrUsageWithServer increments key-level usage and per-server usage in one transaction.
func (s *Store) IncrUsageWithServer(ctx context.Context, keyID, serverID string, periodStart time.Time, bytesIn, bytesOut, tokens, requests int64) error {
	ps := periodStart.UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO usage (key_id, period_start, tokens_used, bytes_in, bytes_out, requests)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(key_id, period_start) DO UPDATE SET
  tokens_used = tokens_used + excluded.tokens_used,
  bytes_in = bytes_in + excluded.bytes_in,
  bytes_out = bytes_out + excluded.bytes_out,
  requests = requests + excluded.requests`,
		keyID, ps, tokens, bytesIn, bytesOut, requests); err != nil {
		return err
	}
	if serverID != "" {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO usage_by_server (key_id, server_id, period_start, tokens_used, bytes_in, bytes_out, requests)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(key_id, server_id, period_start) DO UPDATE SET
  tokens_used = tokens_used + excluded.tokens_used,
  bytes_in = bytes_in + excluded.bytes_in,
  bytes_out = bytes_out + excluded.bytes_out,
  requests = requests + excluded.requests`,
			keyID, serverID, ps, tokens, bytesIn, bytesOut, requests); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetUsageByServer returns tokens used for one key+server in a period.
func (s *Store) GetUsageByServer(ctx context.Context, keyID, serverID string, periodStart time.Time) (int64, error) {
	ps := periodStart.UTC().Format(time.RFC3339Nano)
	var n int64
	err := s.db.QueryRowContext(ctx, `
SELECT tokens_used FROM usage_by_server WHERE key_id = ? AND server_id = ? AND period_start = ?`,
		keyID, serverID, ps).Scan(&n)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return n, err
}

// MapUsageByServer returns serverID → tokens for a key in the period.
func (s *Store) MapUsageByServer(ctx context.Context, keyID string, periodStart time.Time) (map[string]int64, error) {
	ps := periodStart.UTC().Format(time.RFC3339Nano)
	rows, err := s.db.QueryContext(ctx, `
SELECT server_id, tokens_used FROM usage_by_server WHERE key_id = ? AND period_start = ?`, keyID, ps)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var sid string
		var n int64
		if err := rows.Scan(&sid, &n); err != nil {
			return nil, err
		}
		out[sid] = n
	}
	return out, rows.Err()
}

// ListKeyServerUsage returns per-server usage (+ budget) for one key.
func (s *Store) ListKeyServerUsage(ctx context.Context, keyID string, periodStart time.Time) ([]ServerUsageRow, error) {
	ps := periodStart.UTC().Format(time.RFC3339Nano)
	rows, err := s.db.QueryContext(ctx, `
SELECT s.id, s.name, COALESCE(u.tokens_used, 0), COALESCE(u.requests, 0), COALESCE(b.monthly_budget, 0)
FROM mcp_servers s
JOIN api_keys k ON k.account_id = s.account_id AND k.id = ?
LEFT JOIN usage_by_server u ON u.key_id = k.id AND u.server_id = s.id AND u.period_start = ?
LEFT JOIN key_server_budgets b ON b.key_id = k.id AND b.server_id = s.id
ORDER BY s.name`, keyID, ps)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ServerUsageRow
	for rows.Next() {
		var r ServerUsageRow
		if err := rows.Scan(&r.ServerID, &r.Name, &r.TokensUsed, &r.Requests, &r.MonthlyBudget); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if out == nil {
		out = []ServerUsageRow{}
	}
	return out, rows.Err()
}

// ListServerUsage returns account-wide per-server usage with per-key breakdown.
func (s *Store) ListServerUsage(ctx context.Context, accountID string, periodStart time.Time) ([]AccountServerUsage, error) {
	ps := periodStart.UTC().Format(time.RFC3339Nano)
	servers, err := s.ListServersByAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	out := make([]AccountServerUsage, 0, len(servers))
	for _, srv := range servers {
		rows, err := s.db.QueryContext(ctx, `
SELECT k.id, k.name, COALESCE(u.tokens_used, 0), COALESCE(u.requests, 0)
FROM api_keys k
LEFT JOIN usage_by_server u ON u.key_id = k.id AND u.server_id = ? AND u.period_start = ?
WHERE k.account_id = ?
ORDER BY k.name`, srv.ID, ps, accountID)
		if err != nil {
			return nil, err
		}
		var byKey []ServerUsageByKey
		var totalTok, totalReq int64
		for rows.Next() {
			var bk ServerUsageByKey
			if err := rows.Scan(&bk.KeyID, &bk.Name, &bk.TokensUsed, &bk.Requests); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if bk.TokensUsed == 0 && bk.Requests == 0 {
				continue
			}
			totalTok += bk.TokensUsed
			totalReq += bk.Requests
			byKey = append(byKey, bk)
		}
		_ = rows.Close()
		if byKey == nil {
			byKey = []ServerUsageByKey{}
		}
		out = append(out, AccountServerUsage{
			ServerID: srv.ID, Name: srv.Name,
			TokensUsed: totalTok, Requests: totalReq, ByKey: byKey,
		})
	}
	return out, nil
}

// ListAllKeyServerPolicies loads server modes + grants + budgets for policy cache warm.
func (s *Store) ListAllKeyServerPolicies(ctx context.Context) (modes map[string]string, grants map[string][]string, budgets map[string]map[string]int64, err error) {
	modes = map[string]string{}
	grants = map[string][]string{}
	budgets = map[string]map[string]int64{}

	rows, err := s.db.QueryContext(ctx, `SELECT id, COALESCE(server_policy, 'all') FROM api_keys`)
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, mode string
		if err := rows.Scan(&id, &mode); err != nil {
			return nil, nil, nil, err
		}
		modes[id] = mode
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, err
	}

	grows, err := s.db.QueryContext(ctx, `SELECT key_id, server_id FROM key_server_grants`)
	if err != nil {
		return nil, nil, nil, err
	}
	defer grows.Close()
	for grows.Next() {
		var kid, sid string
		if err := grows.Scan(&kid, &sid); err != nil {
			return nil, nil, nil, err
		}
		grants[kid] = append(grants[kid], sid)
	}
	if err := grows.Err(); err != nil {
		return nil, nil, nil, err
	}

	brows, err := s.db.QueryContext(ctx, `SELECT key_id, server_id, monthly_budget FROM key_server_budgets`)
	if err != nil {
		return nil, nil, nil, err
	}
	defer brows.Close()
	for brows.Next() {
		var kid, sid string
		var b int64
		if err := brows.Scan(&kid, &sid, &b); err != nil {
			return nil, nil, nil, err
		}
		if budgets[kid] == nil {
			budgets[kid] = map[string]int64{}
		}
		budgets[kid][sid] = b
	}
	return modes, grants, budgets, brows.Err()
}

// ServerBelongsToAccount checks ownership.
func (s *Store) ServerBelongsToAccount(ctx context.Context, accountID, serverID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mcp_servers WHERE id = ? AND account_id = ?`, serverID, accountID).Scan(&n)
	return n > 0, err
}
