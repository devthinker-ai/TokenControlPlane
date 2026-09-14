-- Migration 0011: dashboard TOTP 2FA (Phase 19).
-- Per-user authenticator secret + hashed recovery codes. Machine keys untouched.
-- recovery_codes is a JSON array of sha256 hex digests; consumed codes are removed.

CREATE TABLE totp (
  user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  secret TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 0,
  recovery_codes TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL,
  confirmed_at TEXT
);
