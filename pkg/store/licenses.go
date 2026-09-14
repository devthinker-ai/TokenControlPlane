package store

import (
	"context"
	"database/sql"
	"time"
)

// License is a purchased RS256 key row (Lemon Squeezy order).
type License struct {
	ID           string
	AccountID    string
	Plan         string
	Key          string
	LQOrderID    sql.NullString
	PurchasedAt  time.Time
	ExpiresAt    time.Time
	RevokedAt    sql.NullTime
	CreatedAt    time.Time
	WindowMonths int  // update window length (12 standard, 24 founders)
	IsFounders   bool // launch offer; refunded rows still count toward founders slots
}

// InsertLicense stores a newly minted license key.
func (s *Store) InsertLicense(ctx context.Context, lic License) error {
	if lic.CreatedAt.IsZero() {
		lic.CreatedAt = time.Now().UTC()
	}
	if lic.WindowMonths <= 0 {
		lic.WindowMonths = 12
	}
	var orderID any
	if lic.LQOrderID.Valid {
		orderID = lic.LQOrderID.String
	}
	var revoked any
	if lic.RevokedAt.Valid {
		revoked = lic.RevokedAt.Time.UTC().Format(time.RFC3339Nano)
	}
	founders := 0
	if lic.IsFounders {
		founders = 1
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO licenses (id, account_id, plan, key, lq_order_id, purchased_at, expires_at, revoked_at, created_at, window_months, is_founders)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		lic.ID, lic.AccountID, lic.Plan, lic.Key, orderID,
		lic.PurchasedAt.UTC().Format(time.RFC3339Nano),
		lic.ExpiresAt.UTC().Format(time.RFC3339Nano),
		revoked,
		lic.CreatedAt.UTC().Format(time.RFC3339Nano),
		lic.WindowMonths,
		founders)
	return err
}

// GetActiveLicense returns the latest non-revoked license for an account (may be expired).
func (s *Store) GetActiveLicense(ctx context.Context, accountID string) (*License, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, account_id, plan, key, lq_order_id, purchased_at, expires_at, revoked_at, created_at, window_months, is_founders
FROM licenses
WHERE account_id = ? AND revoked_at IS NULL
ORDER BY purchased_at DESC
LIMIT 1`, accountID)
	return scanLicense(row)
}

// GetLicenseByOrderID finds a license by Lemon Squeezy order id.
func (s *Store) GetLicenseByOrderID(ctx context.Context, orderID string) (*License, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, account_id, plan, key, lq_order_id, purchased_at, expires_at, revoked_at, created_at, window_months, is_founders
FROM licenses WHERE lq_order_id = ?`, orderID)
	return scanLicense(row)
}

// CountFounders returns how many founders licenses were ever minted (including revoked).
// Refunded founders do NOT free a slot — prevents refund-churn gaming the launch price.
func (s *Store) CountFounders(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM licenses WHERE is_founders = 1`).Scan(&n)
	return n, err
}

// RevokeLicense marks a license revoked (refund). Keeps the row for support.
func (s *Store) RevokeLicense(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE licenses SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		time.Now().UTC().Format(time.RFC3339Nano), id)
	return err
}

// UpdateLicenseKey replaces the JWT on an existing license (reissue).
func (s *Store) UpdateLicenseKey(ctx context.Context, id, key string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE licenses SET key = ? WHERE id = ?`, key, id)
	return err
}

func scanLicense(row *sql.Row) (*License, error) {
	var lic License
	var orderID, revoked, purchased, expires, created sql.NullString
	var founders int
	if err := row.Scan(&lic.ID, &lic.AccountID, &lic.Plan, &lic.Key, &orderID,
		&purchased, &expires, &revoked, &created, &lic.WindowMonths, &founders); err != nil {
		return nil, err
	}
	lic.LQOrderID = orderID
	lic.PurchasedAt = parseTime(purchased.String)
	lic.ExpiresAt = parseTime(expires.String)
	lic.CreatedAt = parseTime(created.String)
	lic.IsFounders = founders != 0
	if lic.WindowMonths <= 0 {
		lic.WindowMonths = 12
	}
	if revoked.Valid && revoked.String != "" {
		lic.RevokedAt = sql.NullTime{Time: parseTime(revoked.String), Valid: true}
	}
	return &lic, nil
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, _ = time.Parse(time.RFC3339, s)
	}
	return t
}
