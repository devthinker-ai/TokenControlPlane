-- Migration 0008: team seats — invites + key ownership (Phase 13).
-- Invites are one-time codes (no SMTP in v1). api_keys.owner_id is nullable
-- so legacy/shared keys stay account-level; ON DELETE SET NULL keeps keys
-- working after a member is removed.

CREATE TABLE invites (
  id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  code_hash TEXT NOT NULL UNIQUE,
  role TEXT NOT NULL DEFAULT 'member',
  created_by TEXT NOT NULL REFERENCES users(id),
  expires_at TEXT NOT NULL,
  used_at TEXT,
  used_by TEXT REFERENCES users(id),
  created_at TEXT NOT NULL
);
CREATE INDEX idx_invites_account ON invites(account_id);

ALTER TABLE api_keys ADD COLUMN owner_id TEXT REFERENCES users(id) ON DELETE SET NULL;
CREATE INDEX idx_api_keys_owner ON api_keys(owner_id);
