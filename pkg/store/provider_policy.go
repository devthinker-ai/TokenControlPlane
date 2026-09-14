package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const (
	ProviderPolicyAll    = "all"
	ProviderPolicyCustom = "custom"
)

// KeyProviderPolicy is per-key provider allowlist mode + grants.
type KeyProviderPolicy struct {
	Mode    string   // all|custom
	Allowed []string // provider IDs when custom
}

// KeyProviderBudget is a per-provider monthly token cap for a key (0 = no cap).
type KeyProviderBudget struct {
	ProviderID    string
	MonthlyBudget int64
}

// ProviderUsageRow is one provider's usage for the current period.
type ProviderUsageRow struct {
	ProviderID       string
	Name             string
	TokensUsed       int64
	TokensExact      int64
	TokensEstimated  int64
	Requests         int64
	MonthlyBudget    int64
}

// ProviderUsageByKey is a key breakdown under a provider in account usage.
type ProviderUsageByKey struct {
	KeyID            string
	Name             string
	TokensUsed       int64
	TokensExact      int64
	TokensEstimated  int64
	Requests         int64
}

// AccountProviderUsage is account-level per-provider usage.
type AccountProviderUsage struct {
	ProviderID       string
	Name             string
	TokensUsed       int64
	TokensExact      int64
	TokensEstimated  int64
	Requests         int64
	ByKey            []ProviderUsageByKey
}

// GetKeyProviderPolicy returns mode + allowed provider IDs.
func (s *Store) GetKeyProviderPolicy(ctx context.Context, keyID string) (KeyProviderPolicy, error) {
	var mode string
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(provider_policy, 'all') FROM api_keys WHERE id = ?`, keyID).Scan(&mode)
	if err != nil {
		return KeyProviderPolicy{}, err
	}
	if mode == "" {
		mode = ProviderPolicyAll
	}
	rows, err := s.db.QueryContext(ctx, `SELECT provider_id FROM key_provider_grants WHERE key_id = ?`, keyID)
	if err != nil {
		return KeyProviderPolicy{}, err
	}
	defer rows.Close()
	var allowed []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return KeyProviderPolicy{}, err
		}
		allowed = append(allowed, id)
	}
	if allowed == nil {
		allowed = []string{}
	}
	return KeyProviderPolicy{Mode: mode, Allowed: allowed}, rows.Err()
}

// SetKeyProviderAccess sets provider policy + grants + budgets in one transaction.
func (s *Store) SetKeyProviderAccess(ctx context.Context, keyID, mode string, providerIDs []string, budgets []KeyProviderBudget) error {
	if mode != ProviderPolicyAll && mode != ProviderPolicyCustom {
		return fmt.Errorf("invalid provider_policy %q", mode)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `UPDATE api_keys SET provider_policy = ? WHERE id = ?`, mode, keyID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM key_provider_grants WHERE key_id = ?`, keyID); err != nil {
		return err
	}
	if mode == ProviderPolicyCustom {
		for _, pid := range providerIDs {
			if pid == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO key_provider_grants (key_id, provider_id) VALUES (?, ?)`, keyID, pid); err != nil {
				return err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM key_provider_budgets WHERE key_id = ?`, keyID); err != nil {
		return err
	}
	allowed := map[string]struct{}{}
	for _, pid := range providerIDs {
		allowed[pid] = struct{}{}
	}
	for _, b := range budgets {
		if b.ProviderID == "" || b.MonthlyBudget <= 0 {
			continue
		}
		if mode == ProviderPolicyCustom {
			if _, ok := allowed[b.ProviderID]; !ok {
				return fmt.Errorf("budget for provider %s not in allowed list", b.ProviderID)
			}
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO key_provider_budgets (key_id, provider_id, monthly_budget) VALUES (?, ?, ?)`,
			keyID, b.ProviderID, b.MonthlyBudget); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListKeyProviderBudgets returns per-provider budgets for a key.
func (s *Store) ListKeyProviderBudgets(ctx context.Context, keyID string) ([]KeyProviderBudget, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT provider_id, monthly_budget FROM key_provider_budgets WHERE key_id = ?`, keyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []KeyProviderBudget
	for rows.Next() {
		var b KeyProviderBudget
		if err := rows.Scan(&b.ProviderID, &b.MonthlyBudget); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	if out == nil {
		out = []KeyProviderBudget{}
	}
	return out, rows.Err()
}

// IncrUsageWithProvider increments key-level usage and per-provider usage in one transaction.
// exactTokens and estimatedTokens should sum to tokens (caller decides which bucket).
func (s *Store) IncrUsageWithProvider(ctx context.Context, keyID, providerID string, periodStart time.Time, bytesIn, bytesOut, tokens, exactTokens, estimatedTokens, requests int64) error {
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
	if providerID != "" {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO usage_by_provider (key_id, provider_id, period_start, tokens_used, tokens_exact, tokens_estimated, bytes_in, bytes_out, requests)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(key_id, provider_id, period_start) DO UPDATE SET
  tokens_used = tokens_used + excluded.tokens_used,
  tokens_exact = tokens_exact + excluded.tokens_exact,
  tokens_estimated = tokens_estimated + excluded.tokens_estimated,
  bytes_in = bytes_in + excluded.bytes_in,
  bytes_out = bytes_out + excluded.bytes_out,
  requests = requests + excluded.requests`,
			keyID, providerID, ps, tokens, exactTokens, estimatedTokens, bytesIn, bytesOut, requests); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// MapUsageByProvider returns providerID → tokens for a key in the period.
func (s *Store) MapUsageByProvider(ctx context.Context, keyID string, periodStart time.Time) (map[string]int64, error) {
	ps := periodStart.UTC().Format(time.RFC3339Nano)
	rows, err := s.db.QueryContext(ctx, `
SELECT provider_id, tokens_used FROM usage_by_provider WHERE key_id = ? AND period_start = ?`, keyID, ps)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var pid string
		var n int64
		if err := rows.Scan(&pid, &n); err != nil {
			return nil, err
		}
		out[pid] = n
	}
	return out, rows.Err()
}

// ListKeyProviderUsage returns per-provider usage (+ budget) for one key.
func (s *Store) ListKeyProviderUsage(ctx context.Context, keyID string, periodStart time.Time) ([]ProviderUsageRow, error) {
	ps := periodStart.UTC().Format(time.RFC3339Nano)
	rows, err := s.db.QueryContext(ctx, `
SELECT p.id, p.name, COALESCE(u.tokens_used, 0), COALESCE(u.tokens_exact, 0), COALESCE(u.tokens_estimated, 0),
       COALESCE(u.requests, 0), COALESCE(b.monthly_budget, 0)
FROM llm_providers p
JOIN api_keys k ON k.account_id = p.account_id AND k.id = ?
LEFT JOIN usage_by_provider u ON u.key_id = k.id AND u.provider_id = p.id AND u.period_start = ?
LEFT JOIN key_provider_budgets b ON b.key_id = k.id AND b.provider_id = p.id
ORDER BY p.name`, keyID, ps)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProviderUsageRow
	for rows.Next() {
		var r ProviderUsageRow
		if err := rows.Scan(&r.ProviderID, &r.Name, &r.TokensUsed, &r.TokensExact, &r.TokensEstimated, &r.Requests, &r.MonthlyBudget); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if out == nil {
		out = []ProviderUsageRow{}
	}
	return out, rows.Err()
}

// ListProviderUsage returns account-wide per-provider usage with per-key breakdown.
func (s *Store) ListProviderUsage(ctx context.Context, accountID string, periodStart time.Time) ([]AccountProviderUsage, error) {
	ps := periodStart.UTC().Format(time.RFC3339Nano)
	providers, err := s.ListLLMProvidersByAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	out := make([]AccountProviderUsage, 0, len(providers))
	for _, p := range providers {
		rows, err := s.db.QueryContext(ctx, `
SELECT k.id, k.name, COALESCE(u.tokens_used, 0), COALESCE(u.tokens_exact, 0), COALESCE(u.tokens_estimated, 0), COALESCE(u.requests, 0)
FROM api_keys k
LEFT JOIN usage_by_provider u ON u.key_id = k.id AND u.provider_id = ? AND u.period_start = ?
WHERE k.account_id = ?
ORDER BY k.name`, p.ID, ps, accountID)
		if err != nil {
			return nil, err
		}
		var byKey []ProviderUsageByKey
		var totalTok, totalExact, totalEst, totalReq int64
		for rows.Next() {
			var bk ProviderUsageByKey
			if err := rows.Scan(&bk.KeyID, &bk.Name, &bk.TokensUsed, &bk.TokensExact, &bk.TokensEstimated, &bk.Requests); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if bk.TokensUsed == 0 && bk.Requests == 0 {
				continue
			}
			totalTok += bk.TokensUsed
			totalExact += bk.TokensExact
			totalEst += bk.TokensEstimated
			totalReq += bk.Requests
			byKey = append(byKey, bk)
		}
		_ = rows.Close()
		if byKey == nil {
			byKey = []ProviderUsageByKey{}
		}
		out = append(out, AccountProviderUsage{
			ProviderID: p.ID, Name: p.Name,
			TokensUsed: totalTok, TokensExact: totalExact, TokensEstimated: totalEst,
			Requests: totalReq, ByKey: byKey,
		})
	}
	return out, nil
}

// AggregateProviderUsage returns account totals across all providers for Overview.
func (s *Store) AggregateProviderUsage(ctx context.Context, accountID string, periodStart time.Time) (tokens, exact, estimated int64, providers int, err error) {
	list, err := s.ListProviderUsage(ctx, accountID, periodStart)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	active := 0
	for _, p := range list {
		tokens += p.TokensUsed
		exact += p.TokensExact
		estimated += p.TokensEstimated
		if p.TokensUsed > 0 || p.Requests > 0 {
			active++
		}
	}
	return tokens, exact, estimated, active, nil
}

// ListAllKeyProviderPolicies loads provider modes + grants + budgets for policy cache warm.
func (s *Store) ListAllKeyProviderPolicies(ctx context.Context) (modes map[string]string, grants map[string][]string, budgets map[string]map[string]int64, err error) {
	modes = map[string]string{}
	grants = map[string][]string{}
	budgets = map[string]map[string]int64{}

	rows, err := s.db.QueryContext(ctx, `SELECT id, COALESCE(provider_policy, 'all') FROM api_keys`)
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

	grows, err := s.db.QueryContext(ctx, `SELECT key_id, provider_id FROM key_provider_grants`)
	if err != nil {
		return nil, nil, nil, err
	}
	defer grows.Close()
	for grows.Next() {
		var kid, pid string
		if err := grows.Scan(&kid, &pid); err != nil {
			return nil, nil, nil, err
		}
		grants[kid] = append(grants[kid], pid)
	}
	if err := grows.Err(); err != nil {
		return nil, nil, nil, err
	}

	brows, err := s.db.QueryContext(ctx, `SELECT key_id, provider_id, monthly_budget FROM key_provider_budgets`)
	if err != nil {
		return nil, nil, nil, err
	}
	defer brows.Close()
	for brows.Next() {
		var kid, pid string
		var b int64
		if err := brows.Scan(&kid, &pid, &b); err != nil {
			return nil, nil, nil, err
		}
		if budgets[kid] == nil {
			budgets[kid] = map[string]int64{}
		}
		budgets[kid][pid] = b
	}
	return modes, grants, budgets, brows.Err()
}
