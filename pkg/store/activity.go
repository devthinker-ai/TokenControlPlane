package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Activity event kinds (dashboard feed).
const (
	ActivityKeyCreated      = "key_created"
	ActivityKeyKilledManual = "key_killed_manual"
	ActivityKeyKilledAuto   = "key_killed_auto"
	ActivityKeyUnkilled     = "key_un_killed"
	ActivityBudgetExceeded  = "budget_exceeded"
	ActivityRateLimited     = "rate_limited"
	ActivityCircuitOpen     = "circuit_open"
	ActivityCircuitClosed   = "circuit_closed"
	ActivityPlanChanged     = "plan_changed"
	ActivityServerAdded     = "server_added"
	ActivityServerRemoval   = "server_removal"
	ActivityOAuthConnected  = "oauth_connected"
	ActivityOAuthExpired    = "oauth_expired"
	ActivityToolCallDenied  = "tool_call_denied"
	ActivityServerRestart   = "server_restart"
	ActivityServerScopeDenied = "server_scope_denied"
	ActivityServerBudgetExceeded = "server_budget_exceeded"
	ActivityLLMFallback            = "llm_fallback"
	ActivityProviderScopeDenied    = "provider_scope_denied"
	ActivityProviderBudgetExceeded = "provider_budget_exceeded"
	ActivityInviteEmailSent        = "invite_email_sent"
	ActivityPasswordResetAdmin     = "password_reset_admin"
	ActivityPasswordResetSelf      = "password_reset_self"
	ActivitySMTPTestSent           = "smtp_test_sent"
	Activity2FAEnabled             = "2fa_enabled"
	Activity2FARemoved             = "2fa_removed"
)

// ActivityEvent is a short human-readable audit row for the dashboard feed.
type ActivityEvent struct {
	ID        string
	AccountID string
	Kind      string
	Subject   string
	Detail    string // JSON object
	Summary   string
	CreatedAt time.Time
}

// InsertActivityEvent persists a feed row. Detail may be nil (stored as {}).
func (s *Store) InsertActivityEvent(ctx context.Context, accountID, kind, subject, summary string, detail any) error {
	if accountID == "" {
		return nil
	}
	detailJSON := "{}"
	if detail != nil {
		b, err := json.Marshal(detail)
		if err != nil {
			return err
		}
		detailJSON = string(b)
	}
	id := "act_" + uuid.NewString()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO activity_events (id, account_id, kind, subject, detail, summary, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, accountID, kind, subject, detailJSON, summary, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// ListActivityEvents returns newest-first events for an account.
func (s *Store) ListActivityEvents(ctx context.Context, accountID string, limit int) ([]ActivityEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, account_id, kind, subject, detail, summary, created_at
FROM activity_events WHERE account_id = ?
ORDER BY created_at DESC LIMIT ?`, accountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ActivityEvent
	for rows.Next() {
		var e ActivityEvent
		var created string
		if err := rows.Scan(&e.ID, &e.AccountID, &e.Kind, &e.Subject, &e.Detail, &e.Summary, &created); err != nil {
			return nil, err
		}
		t, err := time.Parse(time.RFC3339Nano, created)
		if err != nil {
			t, _ = time.Parse(time.RFC3339, created)
		}
		e.CreatedAt = t
		out = append(out, e)
	}
	return out, rows.Err()
}

// SetAccountOnboarded marks the wizard complete (idempotent).
func (s *Store) SetAccountOnboarded(ctx context.Context, accountID string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE accounts SET onboarded_at = COALESCE(onboarded_at, ?) WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339Nano), accountID)
	return err
}

// SetServerIndexError records the last tools/list outcome (empty err = success).
func (s *Store) SetServerIndexError(ctx context.Context, serverID, indexErr string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE mcp_servers SET last_index_error = ? WHERE id = ?`, indexErr, serverID)
	return err
}
