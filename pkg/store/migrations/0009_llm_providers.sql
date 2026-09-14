-- Migration 0009: LLM providers, model routes, per-provider usage + budgets (Phase 14).
-- Mirrors 0006 server-scope pattern; does NOT reshape usage_by_server.
-- tokens_exact / tokens_estimated support Overview "Y% exact, Z% estimated".

ALTER TABLE api_keys ADD COLUMN provider_policy TEXT NOT NULL DEFAULT 'all';

CREATE TABLE llm_providers (
  id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  base_url TEXT NOT NULL,
  auth_header TEXT NOT NULL DEFAULT 'Authorization',
  auth_value TEXT NOT NULL DEFAULT '',
  default_model TEXT NOT NULL DEFAULT '',
  timeout_seconds INTEGER NOT NULL DEFAULT 300,
  enabled INTEGER NOT NULL DEFAULT 1,
  last_health_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);

CREATE INDEX idx_llm_providers_account ON llm_providers(account_id);

CREATE TABLE llm_models (
  id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  model TEXT NOT NULL,
  provider_id TEXT NOT NULL REFERENCES llm_providers(id) ON DELETE CASCADE,
  fallback_model_id TEXT,
  created_at TEXT NOT NULL,
  UNIQUE (account_id, name)
);

CREATE INDEX idx_llm_models_account ON llm_models(account_id);

CREATE TABLE usage_by_provider (
  key_id TEXT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
  provider_id TEXT NOT NULL REFERENCES llm_providers(id) ON DELETE CASCADE,
  period_start TEXT NOT NULL,
  tokens_used INTEGER NOT NULL DEFAULT 0,
  tokens_exact INTEGER NOT NULL DEFAULT 0,
  tokens_estimated INTEGER NOT NULL DEFAULT 0,
  bytes_in INTEGER NOT NULL DEFAULT 0,
  bytes_out INTEGER NOT NULL DEFAULT 0,
  requests INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (key_id, provider_id, period_start)
);

CREATE INDEX idx_usage_by_provider_period ON usage_by_provider(period_start);

CREATE TABLE key_provider_budgets (
  key_id TEXT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
  provider_id TEXT NOT NULL REFERENCES llm_providers(id) ON DELETE CASCADE,
  monthly_budget INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (key_id, provider_id)
);

CREATE TABLE key_provider_grants (
  key_id TEXT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
  provider_id TEXT NOT NULL REFERENCES llm_providers(id) ON DELETE CASCADE,
  PRIMARY KEY (key_id, provider_id)
);

CREATE INDEX idx_key_provider_grants_key ON key_provider_grants(key_id);
