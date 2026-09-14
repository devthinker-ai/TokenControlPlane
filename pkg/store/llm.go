package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// LLMProvider is an OpenAI-compatible upstream (base_url ends at /v1).
type LLMProvider struct {
	ID             string
	AccountID      string
	Name           string
	BaseURL        string
	AuthHeader     string
	AuthValue      string
	DefaultModel   string
	TimeoutSeconds int
	Enabled        bool
	LastHealthError string
	CreatedAt      time.Time
}

// LLMModel is a route alias: clients send name, gateway rewrites to Model @ Provider.
type LLMModel struct {
	ID              string
	AccountID       string
	Name            string // route alias
	Model           string // upstream model id
	ProviderID      string
	FallbackModelID sql.NullString
	CreatedAt       time.Time
}

func (s *Store) CreateLLMProvider(ctx context.Context, p LLMProvider) error {
	if p.AuthHeader == "" {
		p.AuthHeader = "Authorization"
	}
	if p.TimeoutSeconds <= 0 {
		p.TimeoutSeconds = 300
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now().UTC()
	}
	enabled := 0
	if p.Enabled {
		enabled = 1
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO llm_providers (
  id, account_id, name, base_url, auth_header, auth_value, default_model,
  timeout_seconds, enabled, last_health_error, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.AccountID, p.Name, p.BaseURL, p.AuthHeader, p.AuthValue, p.DefaultModel,
		p.TimeoutSeconds, enabled, p.LastHealthError, p.CreatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) GetLLMProvider(ctx context.Context, id string) (*LLMProvider, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, account_id, name, base_url, auth_header, auth_value, default_model,
       timeout_seconds, enabled, COALESCE(last_health_error, ''), created_at
FROM llm_providers WHERE id = ?`, id)
	return scanLLMProvider(row)
}

func scanLLMProvider(row scannable) (*LLMProvider, error) {
	var p LLMProvider
	var enabled int
	var created string
	err := row.Scan(&p.ID, &p.AccountID, &p.Name, &p.BaseURL, &p.AuthHeader, &p.AuthValue,
		&p.DefaultModel, &p.TimeoutSeconds, &enabled, &p.LastHealthError, &created)
	if err != nil {
		return nil, err
	}
	p.Enabled = enabled != 0
	p.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return &p, nil
}

func (s *Store) ListLLMProvidersByAccount(ctx context.Context, accountID string) ([]LLMProvider, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, account_id, name, base_url, auth_header, auth_value, default_model,
       timeout_seconds, enabled, COALESCE(last_health_error, ''), created_at
FROM llm_providers WHERE account_id = ? ORDER BY name`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LLMProvider
	for rows.Next() {
		p, err := scanLLMProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	if out == nil {
		out = []LLMProvider{}
	}
	return out, rows.Err()
}

func (s *Store) UpdateLLMProvider(ctx context.Context, p LLMProvider) error {
	if p.AuthHeader == "" {
		p.AuthHeader = "Authorization"
	}
	if p.TimeoutSeconds <= 0 {
		p.TimeoutSeconds = 300
	}
	enabled := 0
	if p.Enabled {
		enabled = 1
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE llm_providers SET name=?, base_url=?, auth_header=?, auth_value=?, default_model=?,
  timeout_seconds=?, enabled=?, last_health_error=? WHERE id=? AND account_id=?`,
		p.Name, p.BaseURL, p.AuthHeader, p.AuthValue, p.DefaultModel,
		p.TimeoutSeconds, enabled, p.LastHealthError, p.ID, p.AccountID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) SetLLMProviderEnabled(ctx context.Context, id string, enabled bool) error {
	en := 0
	if enabled {
		en = 1
	}
	_, err := s.db.ExecContext(ctx, `UPDATE llm_providers SET enabled=? WHERE id=?`, en, id)
	return err
}

func (s *Store) SetLLMProviderHealthError(ctx context.Context, id, healthErr string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE llm_providers SET last_health_error=? WHERE id=?`, healthErr, id)
	return err
}

func (s *Store) DeleteLLMProvider(ctx context.Context, accountID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM llm_providers WHERE id=? AND account_id=?`, id, accountID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) CountLLMProviders(ctx context.Context, accountID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM llm_providers WHERE account_id=?`, accountID).Scan(&n)
	return n, err
}

func (s *Store) CountLLMModelsByProvider(ctx context.Context, providerID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM llm_models WHERE provider_id=?`, providerID).Scan(&n)
	return n, err
}

func (s *Store) ProviderBelongsToAccount(ctx context.Context, accountID, providerID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM llm_providers WHERE id=? AND account_id=?`, providerID, accountID).Scan(&n)
	return n > 0, err
}

func (s *Store) CreateLLMModel(ctx context.Context, m LLMModel) error {
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	var fb any
	if m.FallbackModelID.Valid {
		fb = m.FallbackModelID.String
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO llm_models (id, account_id, name, model, provider_id, fallback_model_id, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.AccountID, m.Name, m.Model, m.ProviderID, fb, m.CreatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) GetLLMModel(ctx context.Context, id string) (*LLMModel, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, account_id, name, model, provider_id, fallback_model_id, created_at
FROM llm_models WHERE id = ?`, id)
	return scanLLMModel(row)
}

func (s *Store) GetLLMModelByName(ctx context.Context, accountID, name string) (*LLMModel, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, account_id, name, model, provider_id, fallback_model_id, created_at
FROM llm_models WHERE account_id = ? AND name = ?`, accountID, name)
	return scanLLMModel(row)
}

func scanLLMModel(row scannable) (*LLMModel, error) {
	var m LLMModel
	var created string
	err := row.Scan(&m.ID, &m.AccountID, &m.Name, &m.Model, &m.ProviderID, &m.FallbackModelID, &created)
	if err != nil {
		return nil, err
	}
	m.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return &m, nil
}

func (s *Store) ListLLMModelsByAccount(ctx context.Context, accountID string) ([]LLMModel, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, account_id, name, model, provider_id, fallback_model_id, created_at
FROM llm_models WHERE account_id = ? ORDER BY name`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LLMModel
	for rows.Next() {
		m, err := scanLLMModel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	if out == nil {
		out = []LLMModel{}
	}
	return out, rows.Err()
}

func (s *Store) UpdateLLMModel(ctx context.Context, m LLMModel) error {
	var fb any
	if m.FallbackModelID.Valid {
		fb = m.FallbackModelID.String
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE llm_models SET name=?, model=?, provider_id=?, fallback_model_id=?
WHERE id=? AND account_id=?`,
		m.Name, m.Model, m.ProviderID, fb, m.ID, m.AccountID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) DeleteLLMModel(ctx context.Context, accountID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM llm_models WHERE id=? AND account_id=?`, id, accountID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ListGrantedProvidersForKey returns providers the key may use (all account providers when mode=all).
func (s *Store) ListGrantedProvidersForKey(ctx context.Context, keyID, accountID, mode string) ([]LLMProvider, error) {
	if mode != ProviderPolicyCustom {
		return s.ListLLMProvidersByAccount(ctx, accountID)
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT p.id, p.account_id, p.name, p.base_url, p.auth_header, p.auth_value, p.default_model,
       p.timeout_seconds, p.enabled, COALESCE(p.last_health_error, ''), p.created_at
FROM llm_providers p
JOIN key_provider_grants g ON g.provider_id = p.id AND g.key_id = ?
WHERE p.account_id = ?
ORDER BY p.name`, keyID, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LLMProvider
	for rows.Next() {
		p, err := scanLLMProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	if out == nil {
		out = []LLMProvider{}
	}
	return out, rows.Err()
}

// FindLLMModelByUpstreamID finds a model whose upstream model id matches (for pass-through).
func (s *Store) FindLLMModelByUpstreamID(ctx context.Context, accountID, upstreamModel string) (*LLMModel, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, account_id, name, model, provider_id, fallback_model_id, created_at
FROM llm_models WHERE account_id = ? AND model = ? LIMIT 1`, accountID, upstreamModel)
	m, err := scanLLMModel(row)
	if err == sql.ErrNoRows {
		return nil, sql.ErrNoRows
	}
	return m, err
}

// ValidateFallbackChain ensures fallback points at another model in the same account and depth ≤ 2.
func (s *Store) ValidateFallbackChain(ctx context.Context, accountID, modelID, fallbackID string) error {
	if fallbackID == "" {
		return nil
	}
	if fallbackID == modelID {
		return fmt.Errorf("fallback cannot point to self")
	}
	fb, err := s.GetLLMModel(ctx, fallbackID)
	if err != nil || fb.AccountID != accountID {
		return fmt.Errorf("fallback model not found")
	}
	// Depth max 2: primary → fallback → (optional second hop). If fallback already
	// has a fallback that itself has a fallback, reject.
	if fb.FallbackModelID.Valid && fb.FallbackModelID.String != "" {
		fb2, err := s.GetLLMModel(ctx, fb.FallbackModelID.String)
		if err == nil && fb2.FallbackModelID.Valid && fb2.FallbackModelID.String != "" {
			return fmt.Errorf("fallback chain depth exceeds 2")
		}
	}
	return nil
}
