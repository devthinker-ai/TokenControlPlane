-- Migration 0001: initial schema (phases 1–5).
-- Fresh installs only. Legacy DBs without schema_migrations are stamped, not re-run.

CREATE TABLE accounts (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  plan TEXT NOT NULL DEFAULT 'free',
  stripe_customer_id TEXT,
  stripe_subscription_id TEXT,
  max_seats INTEGER NOT NULL DEFAULT 3,
  max_servers INTEGER NOT NULL DEFAULT 3,
  onboarded_at TEXT,
  created_at TEXT NOT NULL
);

CREATE TABLE users (
  id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL,
  email TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  name TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT 'admin',
  created_at TEXT NOT NULL,
  FOREIGN KEY (account_id) REFERENCES accounts(id)
);

CREATE TABLE mcp_servers (
  id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL DEFAULT 'acct_default',
  name TEXT NOT NULL,
  base_url TEXT NOT NULL,
  auth_header TEXT NOT NULL DEFAULT 'Authorization',
  auth_value TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1,
  last_index_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  FOREIGN KEY (account_id) REFERENCES accounts(id)
);

CREATE TABLE api_keys (
  id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL DEFAULT 'acct_default',
  key_hash TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  token_budget INTEGER NOT NULL DEFAULT 0,
  monthly_budget INTEGER NOT NULL DEFAULT 0,
  rate_limit_rpm INTEGER NOT NULL DEFAULT 0,
  enabled INTEGER NOT NULL DEFAULT 1,
  killed_at TEXT,
  created_at TEXT NOT NULL,
  last_used_at TEXT,
  FOREIGN KEY (account_id) REFERENCES accounts(id)
);

CREATE TABLE usage (
  key_id TEXT NOT NULL,
  period_start TEXT NOT NULL,
  tokens_used INTEGER NOT NULL DEFAULT 0,
  bytes_in INTEGER NOT NULL DEFAULT 0,
  bytes_out INTEGER NOT NULL DEFAULT 0,
  requests INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (key_id, period_start),
  FOREIGN KEY (key_id) REFERENCES api_keys(id)
);

CREATE TABLE usage_daily (
  account_id TEXT NOT NULL,
  day TEXT NOT NULL,
  tokens_used INTEGER NOT NULL DEFAULT 0,
  requests INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (account_id, day)
);

CREATE TABLE tool_calls (
  id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL,
  server_id TEXT NOT NULL,
  key_id TEXT NOT NULL,
  tool_name TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE tools (
  id TEXT PRIMARY KEY,
  server_id TEXT NOT NULL,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  input_schema TEXT NOT NULL DEFAULT '{}',
  FOREIGN KEY (server_id) REFERENCES mcp_servers(id),
  UNIQUE (server_id, name)
);

CREATE TABLE stripe_events (
  id TEXT PRIMARY KEY,
  processed_at TEXT NOT NULL
);

CREATE TABLE activity_events (
  id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  subject TEXT NOT NULL DEFAULT '',
  detail TEXT NOT NULL DEFAULT '{}',
  summary TEXT NOT NULL,
  created_at TEXT NOT NULL,
  FOREIGN KEY (account_id) REFERENCES accounts(id)
);

CREATE INDEX idx_tools_server ON tools(server_id);
CREATE INDEX idx_users_account ON users(account_id);
CREATE INDEX idx_tool_calls_account ON tool_calls(account_id);
CREATE INDEX idx_activity_account ON activity_events(account_id, created_at);
CREATE INDEX idx_servers_account ON mcp_servers(account_id);
CREATE INDEX idx_keys_account ON api_keys(account_id);

INSERT OR IGNORE INTO accounts (id, name, plan, max_seats, max_servers, created_at)
VALUES ('acct_default', 'Default', 'free', 3, 3, '1970-01-01T00:00:00Z');
