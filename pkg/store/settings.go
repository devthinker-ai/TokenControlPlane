package store

import (
	"context"
	"database/sql"
	"time"
)

// Account setting keys (per-account KV).
const (
	SettingSMTPConfig = "smtp_config"
	SettingTplInvite  = "tpl_invite"
	SettingTplReset   = "tpl_reset"
)

// GetAccountSetting returns a KV value (ok=false if missing).
func (s *Store) GetAccountSetting(ctx context.Context, accountID, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `
SELECT value FROM account_settings WHERE account_id = ? AND key = ?`, accountID, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// SetAccountSetting upserts a KV value.
func (s *Store) SetAccountSetting(ctx context.Context, accountID, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO account_settings (account_id, key, value, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(account_id, key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		accountID, key, value, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// EmailInvite is an audit row for invites sent by email.
type EmailInvite struct {
	ID        string
	AccountID string
	Email     string
	InviteID  string
	SentAt    time.Time
	CreatedBy string
}

// InsertEmailInvite records a sent (or attempted) email invite.
func (s *Store) InsertEmailInvite(ctx context.Context, ei EmailInvite) error {
	if ei.SentAt.IsZero() {
		ei.SentAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO email_invites (id, account_id, email, invite_id, sent_at, created_by)
VALUES (?, ?, ?, ?, ?, ?)`,
		ei.ID, ei.AccountID, ei.Email, ei.InviteID,
		ei.SentAt.UTC().Format(time.RFC3339Nano), ei.CreatedBy)
	return err
}

// ListEmailInvites returns email-invite audit rows newest-first.
func (s *Store) ListEmailInvites(ctx context.Context, accountID string) ([]EmailInvite, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, account_id, email, invite_id, sent_at, created_by
FROM email_invites WHERE account_id = ?
ORDER BY sent_at DESC`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EmailInvite
	for rows.Next() {
		var ei EmailInvite
		var sent string
		if err := rows.Scan(&ei.ID, &ei.AccountID, &ei.Email, &ei.InviteID, &sent, &ei.CreatedBy); err != nil {
			return nil, err
		}
		ei.SentAt = parseTime(sent)
		out = append(out, ei)
	}
	return out, rows.Err()
}

// PasswordReset is a self-service reset token (sha256-at-rest).
type PasswordReset struct {
	ID         string
	AccountID  string
	UserID     string
	CodeHash   string
	ExpiresAt  time.Time
	ConsumedAt sql.NullTime
	CreatedAt  time.Time
}

// CreatePasswordReset inserts a hashed reset token.
func (s *Store) CreatePasswordReset(ctx context.Context, pr PasswordReset) error {
	if pr.CreatedAt.IsZero() {
		pr.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO password_resets (id, account_id, user_id, code_hash, expires_at, consumed_at, created_at)
VALUES (?, ?, ?, ?, ?, NULL, ?)`,
		pr.ID, pr.AccountID, pr.UserID, pr.CodeHash,
		pr.ExpiresAt.UTC().Format(time.RFC3339Nano),
		pr.CreatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

// GetPasswordResetByHash looks up a reset token by sha256 hex.
func (s *Store) GetPasswordResetByHash(ctx context.Context, codeHash string) (*PasswordReset, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, account_id, user_id, code_hash, expires_at, consumed_at, created_at
FROM password_resets WHERE code_hash = ?`, codeHash)
	return scanPasswordReset(row)
}

// ConsumePasswordReset stamps consumed_at if still unused and unexpired.
func (s *Store) ConsumePasswordReset(ctx context.Context, id string) error {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
UPDATE password_resets SET consumed_at = ?
WHERE id = ? AND consumed_at IS NULL AND expires_at > ?`,
		now.Format(time.RFC3339Nano), id, now.Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// CountPasswordResetsSince counts reset tokens created for a user since t (rate limit).
func (s *Store) CountPasswordResetsSince(ctx context.Context, userID string, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM password_resets
WHERE user_id = ? AND created_at > ?`, userID, since.UTC().Format(time.RFC3339Nano)).Scan(&n)
	return n, err
}

// SetUserPassword updates password_hash for a user.
func (s *Store) SetUserPassword(ctx context.Context, userID, hash string) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE users SET password_hash = ? WHERE id = ?`, hash, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func scanPasswordReset(row scannable) (*PasswordReset, error) {
	var pr PasswordReset
	var expires, created string
	var consumed sql.NullString
	if err := row.Scan(&pr.ID, &pr.AccountID, &pr.UserID, &pr.CodeHash, &expires, &consumed, &created); err != nil {
		return nil, err
	}
	pr.ExpiresAt = parseTime(expires)
	pr.CreatedAt = parseTime(created)
	if consumed.Valid && consumed.String != "" {
		pr.ConsumedAt = sql.NullTime{Time: parseTime(consumed.String), Valid: true}
	}
	return &pr, nil
}
