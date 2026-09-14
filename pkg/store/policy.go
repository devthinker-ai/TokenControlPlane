package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const (
	ToolPolicyAll    = "all"
	ToolPolicyCustom = "custom"
)

// ToolWithStats is a catalog row plus 30-day call count for the dashboard.
type ToolWithStats struct {
	Name        string
	Description string
	InputSchema string
	Enabled     bool
	Calls30d    int64
}

// KeyToolPolicy is the per-key tool access mode + allowlist.
type KeyToolPolicy struct {
	Mode    string   // all|custom
	Allowed []string // grant tool names when custom
}

// ListToolsWithStats returns tools for a server with calls_30d from tool_calls.
func (s *Store) ListToolsWithStats(ctx context.Context, serverID string) ([]ToolWithStats, error) {
	since := time.Now().UTC().Add(-30 * 24 * time.Hour).Format(time.RFC3339Nano)
	rows, err := s.db.QueryContext(ctx, `
SELECT t.name, t.description, t.input_schema, COALESCE(t.enabled, 1),
       COALESCE((
         SELECT COUNT(*) FROM tool_calls tc
         WHERE tc.server_id = t.server_id AND tc.tool_name = t.name AND tc.created_at >= ?
       ), 0)
FROM tools t
WHERE t.server_id = ?
ORDER BY t.name`, since, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ToolWithStats
	for rows.Next() {
		var t ToolWithStats
		var en int
		if err := rows.Scan(&t.Name, &t.Description, &t.InputSchema, &en, &t.Calls30d); err != nil {
			return nil, err
		}
		t.Enabled = en == 1
		out = append(out, t)
	}
	return out, rows.Err()
}

// SetToolEnabled toggles a single tool on a server.
func (s *Store) SetToolEnabled(ctx context.Context, serverID, name string, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	res, err := s.db.ExecContext(ctx, `UPDATE tools SET enabled = ? WHERE server_id = ? AND name = ?`, v, serverID, name)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// SetToolsEnabledBulk sets enabled for all tools on a server, or a name subset.
func (s *Store) SetToolsEnabledBulk(ctx context.Context, serverID string, enabled bool, names []string) (int64, error) {
	v := 0
	if enabled {
		v = 1
	}
	if len(names) == 0 {
		res, err := s.db.ExecContext(ctx, `UPDATE tools SET enabled = ? WHERE server_id = ?`, v, serverID)
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var total int64
	for _, name := range names {
		res, err := tx.ExecContext(ctx, `UPDATE tools SET enabled = ? WHERE server_id = ? AND name = ?`, v, serverID, name)
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		total += n
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return total, nil
}

// GetKeyToolPolicy returns mode + allowed tool names for a key.
func (s *Store) GetKeyToolPolicy(ctx context.Context, keyID string) (KeyToolPolicy, error) {
	var mode string
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(tool_policy, 'all') FROM api_keys WHERE id = ?`, keyID).Scan(&mode)
	if err != nil {
		return KeyToolPolicy{}, err
	}
	if mode == "" {
		mode = ToolPolicyAll
	}
	grants, err := s.GetGrantSet(ctx, keyID)
	if err != nil {
		return KeyToolPolicy{}, err
	}
	allowed := make([]string, 0, len(grants))
	for name := range grants {
		allowed = append(allowed, name)
	}
	return KeyToolPolicy{Mode: mode, Allowed: allowed}, nil
}

// GetGrantSet returns the allowlist set for a key (empty when none).
func (s *Store) GetGrantSet(ctx context.Context, keyID string) (map[string]struct{}, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT tool_name FROM key_tool_grants WHERE key_id = ?`, keyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]struct{}{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = struct{}{}
	}
	return out, rows.Err()
}

// SetKeyToolPolicy sets mode and replaces grants atomically.
func (s *Store) SetKeyToolPolicy(ctx context.Context, keyID, mode string, toolNames []string) error {
	if mode != ToolPolicyAll && mode != ToolPolicyCustom {
		return fmt.Errorf("invalid tool_policy %q", mode)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `UPDATE api_keys SET tool_policy = ? WHERE id = ?`, mode, keyID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM key_tool_grants WHERE key_id = ?`, keyID); err != nil {
		return err
	}
	if mode == ToolPolicyCustom {
		for _, name := range toolNames {
			if name == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO key_tool_grants (key_id, tool_name) VALUES (?, ?)`, keyID, name); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// ListEnabledToolNames returns enabled tool names for a server (for cache warm).
func (s *Store) ListEnabledToolMap(ctx context.Context, serverID string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT name, COALESCE(enabled, 1) FROM tools WHERE server_id = ?`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		var en int
		if err := rows.Scan(&name, &en); err != nil {
			return nil, err
		}
		out[name] = en == 1
	}
	return out, rows.Err()
}

// ListAllServerToolMaps returns serverID → name→enabled for cache warm.
func (s *Store) ListAllServerToolMaps(ctx context.Context) (map[string]map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT server_id, name, COALESCE(enabled, 1) FROM tools`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string]bool{}
	for rows.Next() {
		var sid, name string
		var en int
		if err := rows.Scan(&sid, &name, &en); err != nil {
			return nil, err
		}
		if out[sid] == nil {
			out[sid] = map[string]bool{}
		}
		out[sid][name] = en == 1
	}
	return out, rows.Err()
}

// ListAllKeyPolicies returns keyID → (mode, grants) for cache warm.
func (s *Store) ListAllKeyPolicies(ctx context.Context) (map[string]KeyToolPolicy, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, COALESCE(tool_policy, 'all') FROM api_keys`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]KeyToolPolicy{}
	for rows.Next() {
		var id, mode string
		if err := rows.Scan(&id, &mode); err != nil {
			return nil, err
		}
		out[id] = KeyToolPolicy{Mode: mode, Allowed: nil}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	grows, err := s.db.QueryContext(ctx, `SELECT key_id, tool_name FROM key_tool_grants`)
	if err != nil {
		return nil, err
	}
	defer grows.Close()
	for grows.Next() {
		var kid, name string
		if err := grows.Scan(&kid, &name); err != nil {
			return nil, err
		}
		p := out[kid]
		p.Allowed = append(p.Allowed, name)
		out[kid] = p
	}
	return out, grows.Err()
}

// ToolExistsInAccount returns true if any server owned by account has this tool name.
func (s *Store) ToolExistsInAccount(ctx context.Context, accountID, toolName string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM tools t
JOIN mcp_servers s ON s.id = t.server_id
WHERE s.account_id = ? AND t.name = ?`, accountID, toolName).Scan(&n)
	return n > 0, err
}

// ListToolNamesInAccount returns the set of all tool names across an account's servers.
func (s *Store) ListToolNamesInAccount(ctx context.Context, accountID string) (map[string]struct{}, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT t.name FROM tools t
JOIN mcp_servers s ON s.id = t.server_id
WHERE s.account_id = ?`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]struct{}{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = struct{}{}
	}
	return out, rows.Err()
}
