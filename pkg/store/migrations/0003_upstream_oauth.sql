-- Migration 0003: upstream OAuth for MCP servers (Phase 10).
-- auth_type: none | static | oauth_device | oauth_pkce
-- Tokens/sessions are plaintext in v1 (same as auth_value); encryption-at-rest is v1.1.

ALTER TABLE mcp_servers ADD COLUMN auth_type TEXT NOT NULL DEFAULT 'static';

CREATE TABLE oauth_tokens (
  id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL,
  server_id TEXT NOT NULL,
  auth_server TEXT NOT NULL,
  token_endpoint TEXT NOT NULL,
  device_auth_endpoint TEXT,
  client_id TEXT NOT NULL,
  scopes TEXT NOT NULL DEFAULT '',
  access_token TEXT NOT NULL,
  refresh_token TEXT NOT NULL,
  token_type TEXT NOT NULL DEFAULT 'Bearer',
  expires_at TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'connected',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (account_id, server_id),
  FOREIGN KEY (account_id) REFERENCES accounts(id),
  FOREIGN KEY (server_id) REFERENCES mcp_servers(id)
);

-- In-flight device / PKCE flows. Extra PKCE columns (state, code_verifier, …)
-- beyond the minimal device-flow shape so one table covers both providers.
CREATE TABLE oauth_sessions (
  id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL,
  server_id TEXT NOT NULL,
  flow TEXT NOT NULL DEFAULT 'device',
  device_code TEXT NOT NULL DEFAULT '',
  verification_uri TEXT NOT NULL DEFAULT '',
  verification_code TEXT NOT NULL DEFAULT '',
  interval_s INTEGER NOT NULL DEFAULT 5,
  expires_at TEXT NOT NULL,
  authorize_url TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL DEFAULT '',
  code_verifier TEXT NOT NULL DEFAULT '',
  auth_server TEXT NOT NULL DEFAULT '',
  token_endpoint TEXT NOT NULL DEFAULT '',
  device_auth_endpoint TEXT NOT NULL DEFAULT '',
  client_id TEXT NOT NULL DEFAULT 'tokencontrolplane',
  scopes TEXT NOT NULL DEFAULT '',
  protected_resource_metadata_url TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  UNIQUE (account_id, server_id),
  FOREIGN KEY (account_id) REFERENCES accounts(id),
  FOREIGN KEY (server_id) REFERENCES mcp_servers(id)
);

CREATE INDEX idx_oauth_tokens_server ON oauth_tokens(server_id);
CREATE INDEX idx_oauth_sessions_state ON oauth_sessions(state);
