-- Migration 0010: email invites, SMTP settings, password resets (Phase 18).
-- Per-account KV for SMTP + templates; email_invites audit; password_resets tokens
-- (sha256-at-rest, same style as invites).

CREATE TABLE account_settings (
  account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  key TEXT NOT NULL,
  value TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL,
  PRIMARY KEY (account_id, key)
);

CREATE TABLE email_invites (
  id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  email TEXT NOT NULL,
  invite_id TEXT NOT NULL REFERENCES invites(id) ON DELETE CASCADE,
  sent_at TEXT NOT NULL,
  created_by TEXT NOT NULL REFERENCES users(id)
);
CREATE INDEX idx_email_invites_account ON email_invites(account_id);

CREATE TABLE password_resets (
  id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  code_hash TEXT NOT NULL UNIQUE,
  expires_at TEXT NOT NULL,
  consumed_at TEXT,
  created_at TEXT NOT NULL
);
CREATE INDEX idx_password_resets_user ON password_resets(user_id);
