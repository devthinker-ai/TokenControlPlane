-- Migration 0006: per-key server scope, per-server usage + budgets (Phase 11a expansion).
-- Extends 0004 tool policy without reshaping the legacy usage table.

ALTER TABLE api_keys ADD COLUMN server_policy TEXT NOT NULL DEFAULT 'all';

CREATE TABLE key_server_grants (
  key_id TEXT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
  server_id TEXT NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
  PRIMARY KEY (key_id, server_id)
);

CREATE INDEX idx_key_server_grants_key ON key_server_grants(key_id);

CREATE TABLE usage_by_server (
  key_id TEXT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
  server_id TEXT NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
  period_start TEXT NOT NULL,
  tokens_used INTEGER NOT NULL DEFAULT 0,
  bytes_in INTEGER NOT NULL DEFAULT 0,
  bytes_out INTEGER NOT NULL DEFAULT 0,
  requests INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (key_id, server_id, period_start)
);

CREATE INDEX idx_usage_by_server_period ON usage_by_server(period_start);

CREATE TABLE key_server_budgets (
  key_id TEXT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
  server_id TEXT NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
  monthly_budget INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (key_id, server_id)
);
