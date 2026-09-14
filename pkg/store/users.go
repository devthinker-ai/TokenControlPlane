package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"time"
)

// Invite is a one-time join code (plaintext never stored — only sha256).
type Invite struct {
	ID            string
	AccountID     string
	CodeHash      string
	Role          string // v1: always "member"
	CreatedBy     string
	CreatedByName string // joined for list UI; empty on insert path
	ExpiresAt     time.Time
	UsedAt        sql.NullTime
	UsedBy        sql.NullString
	CreatedAt     time.Time
}

// HashInviteCode returns the sha256 hex digest used as invites.code_hash.
func HashInviteCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

// CountUsers returns humans in the account (seat usage).
func (s *Store) CountUsers(ctx context.Context, accountID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE account_id = ?`, accountID).Scan(&n)
	return n, err
}

// CountAdmins returns admin-role users in the account.
func (s *Store) CountAdmins(ctx context.Context, accountID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM users WHERE account_id = ? AND role = 'admin'`, accountID).Scan(&n)
	return n, err
}

// ListUsersByAccount returns dashboard users (no password hashes in DTO — caller strips).
func (s *Store) ListUsersByAccount(ctx context.Context, accountID string) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, account_id, email, password_hash, name, role, created_at
FROM users WHERE account_id = ? ORDER BY created_at ASC`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		var created string
		if err := rows.Scan(&u.ID, &u.AccountID, &u.Email, &u.PasswordHash, &u.Name, &u.Role, &created); err != nil {
			return nil, err
		}
		u.CreatedAt = parseTime(created)
		out = append(out, u)
	}
	return out, rows.Err()
}

// UpdateUserRole sets role (admin|member). Caller enforces last-admin invariant.
func (s *Store) UpdateUserRole(ctx context.Context, accountID, userID, role string) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE users SET role = ? WHERE id = ? AND account_id = ?`, role, userID, accountID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteUser removes a dashboard user. Keys keep working with owner_id cleared
// (explicit UPDATE — SQLite FK ON DELETE may be off depending on connection).
func (s *Store) DeleteUser(ctx context.Context, accountID, userID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE api_keys SET owner_id = NULL WHERE owner_id = ?`, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM invites WHERE created_by = ? OR used_by = ?`, userID, userID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = ? AND account_id = ?`, userID, accountID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

// CountPendingInvites counts unused, unexpired invites for an account.
func (s *Store) CountPendingInvites(ctx context.Context, accountID string) (int, error) {
	var n int
	now := time.Now().UTC().Format(time.RFC3339Nano)
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM invites
WHERE account_id = ? AND used_at IS NULL AND expires_at > ?`, accountID, now).Scan(&n)
	return n, err
}

// InsertInvite stores a hashed invite code.
func (s *Store) InsertInvite(ctx context.Context, inv Invite) error {
	if inv.CreatedAt.IsZero() {
		inv.CreatedAt = time.Now().UTC()
	}
	if inv.Role == "" {
		inv.Role = "member"
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO invites (id, account_id, code_hash, role, created_by, expires_at, used_at, used_by, created_at)
VALUES (?, ?, ?, ?, ?, ?, NULL, NULL, ?)`,
		inv.ID, inv.AccountID, inv.CodeHash, inv.Role, inv.CreatedBy,
		inv.ExpiresAt.UTC().Format(time.RFC3339Nano),
		inv.CreatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

// ListPendingInvites returns unused invites (may include expired for admin visibility).
func (s *Store) ListPendingInvites(ctx context.Context, accountID string) ([]Invite, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT i.id, i.account_id, i.code_hash, i.role, i.created_by, COALESCE(u.name, ''),
       i.expires_at, i.used_at, i.used_by, i.created_at
FROM invites i
LEFT JOIN users u ON u.id = i.created_by
WHERE i.account_id = ? AND i.used_at IS NULL
ORDER BY i.created_at DESC`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Invite
	for rows.Next() {
		inv, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *inv)
	}
	return out, rows.Err()
}

// GetInviteByCodeHash looks up any invite by hash (caller checks used/expired).
func (s *Store) GetInviteByCodeHash(ctx context.Context, codeHash string) (*Invite, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT i.id, i.account_id, i.code_hash, i.role, i.created_by, COALESCE(u.name, ''),
       i.expires_at, i.used_at, i.used_by, i.created_at
FROM invites i
LEFT JOIN users u ON u.id = i.created_by
WHERE i.code_hash = ?`, codeHash)
	return scanInvite(row)
}

// MarkInviteUsed stamps used_at/used_by. Idempotent guard: only if still unused.
func (s *Store) MarkInviteUsed(ctx context.Context, inviteID, userID string) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE invites SET used_at = ?, used_by = ? WHERE id = ? AND used_at IS NULL`,
		time.Now().UTC().Format(time.RFC3339Nano), userID, inviteID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteInvite revokes a pending invite (admin).
func (s *Store) DeleteInvite(ctx context.Context, accountID, inviteID string) error {
	res, err := s.db.ExecContext(ctx, `
DELETE FROM invites WHERE id = ? AND account_id = ? AND used_at IS NULL`, inviteID, accountID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func scanInvite(row scannable) (*Invite, error) {
	var inv Invite
	var expires, created string
	var usedAt, usedBy sql.NullString
	if err := row.Scan(&inv.ID, &inv.AccountID, &inv.CodeHash, &inv.Role, &inv.CreatedBy,
		&inv.CreatedByName, &expires, &usedAt, &usedBy, &created); err != nil {
		return nil, err
	}
	inv.ExpiresAt = parseTime(expires)
	inv.CreatedAt = parseTime(created)
	if usedAt.Valid && usedAt.String != "" {
		inv.UsedAt = sql.NullTime{Time: parseTime(usedAt.String), Valid: true}
	}
	inv.UsedBy = usedBy
	return &inv, nil
}
