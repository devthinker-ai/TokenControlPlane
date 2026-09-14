package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// TOTP is a dashboard user's authenticator enrollment (secret never logged).
type TOTP struct {
	UserID        string
	Secret        string
	Enabled       bool
	RecoveryCodes []string // sha256 hex digests
	CreatedAt     time.Time
	ConfirmedAt   sql.NullTime
}

// UpsertTOTP inserts or replaces the TOTP row for a user.
func (s *Store) UpsertTOTP(ctx context.Context, userID, secret string, enabled bool, recoveryCodes []string) error {
	if recoveryCodes == nil {
		recoveryCodes = []string{}
	}
	b, err := json.Marshal(recoveryCodes)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx, `
INSERT INTO totp (user_id, secret, enabled, recovery_codes, created_at, confirmed_at)
VALUES (?, ?, ?, ?, ?, NULL)
ON CONFLICT(user_id) DO UPDATE SET
  secret = excluded.secret,
  enabled = excluded.enabled,
  recovery_codes = excluded.recovery_codes,
  confirmed_at = CASE WHEN excluded.enabled = 1 THEN COALESCE(totp.confirmed_at, ?) ELSE NULL END`,
		userID, secret, boolToInt(enabled), string(b), now, now)
	return err
}

// GetTOTP returns the enrollment for a user (sql.ErrNoRows if none).
func (s *Store) GetTOTP(ctx context.Context, userID string) (*TOTP, error) {
	var t TOTP
	var enabled int
	var codesJSON string
	var created string
	var confirmed sql.NullString
	err := s.db.QueryRowContext(ctx, `
SELECT user_id, secret, enabled, recovery_codes, created_at, confirmed_at
FROM totp WHERE user_id = ?`, userID).Scan(
		&t.UserID, &t.Secret, &enabled, &codesJSON, &created, &confirmed)
	if err != nil {
		return nil, err
	}
	t.Enabled = enabled != 0
	t.CreatedAt = parseTime(created)
	if confirmed.Valid && confirmed.String != "" {
		t.ConfirmedAt = sql.NullTime{Time: parseTime(confirmed.String), Valid: true}
	}
	if err := json.Unmarshal([]byte(codesJSON), &t.RecoveryCodes); err != nil {
		t.RecoveryCodes = []string{}
	}
	return &t, nil
}

// SetTOTPEnabled flips the enabled flag; sets confirmed_at on first enable.
func (s *Store) SetTOTPEnabled(ctx context.Context, userID string, enabled bool) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if enabled {
		_, err := s.db.ExecContext(ctx, `
UPDATE totp SET enabled = 1, confirmed_at = COALESCE(confirmed_at, ?) WHERE user_id = ?`, now, userID)
		return err
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE totp SET enabled = 0 WHERE user_id = ?`, userID)
	return err
}

// SetTOTPConfirmed enables TOTP, stores hashed recovery codes, and sets confirmed_at.
func (s *Store) SetTOTPConfirmed(ctx context.Context, userID string, recoveryHashes []string) error {
	if recoveryHashes == nil {
		recoveryHashes = []string{}
	}
	b, err := json.Marshal(recoveryHashes)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `
UPDATE totp SET enabled = 1, recovery_codes = ?, confirmed_at = ? WHERE user_id = ?`,
		string(b), now, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ConsumeRecoveryCode removes a matching hash from the list. Returns true if consumed.
func (s *Store) ConsumeRecoveryCode(ctx context.Context, userID, codeHash string) (bool, error) {
	t, err := s.GetTOTP(ctx, userID)
	if err != nil {
		return false, err
	}
	idx := -1
	for i, h := range t.RecoveryCodes {
		if h == codeHash {
			idx = i
			break
		}
	}
	if idx < 0 {
		return false, nil
	}
	next := append(t.RecoveryCodes[:idx:idx], t.RecoveryCodes[idx+1:]...)
	b, err := json.Marshal(next)
	if err != nil {
		return false, err
	}
	_, err = s.db.ExecContext(ctx, `
UPDATE totp SET recovery_codes = ? WHERE user_id = ?`, string(b), userID)
	if err != nil {
		return false, err
	}
	return true, nil
}

// DeleteTOTP removes enrollment entirely (idempotent — no error if missing).
func (s *Store) DeleteTOTP(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM totp WHERE user_id = ?`, userID)
	return err
}

// TOTPEnabled reports whether the user has confirmed 2FA (false if no row).
func (s *Store) TOTPEnabled(ctx context.Context, userID string) (bool, error) {
	var enabled int
	err := s.db.QueryRowContext(ctx, `SELECT enabled FROM totp WHERE user_id = ?`, userID).Scan(&enabled)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return enabled != 0, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
