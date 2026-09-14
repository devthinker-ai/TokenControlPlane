-- Migration 0002: Lemon Squeezy one-time licenses (Phase 7).
-- Renames Stripe webhook idempotency table → webhook_events.
-- Keeps legacy stripe_* columns on accounts (unused; no drop without two-phase).
-- Fresh legacy DBs without stripe_events: create empty stub first so INSERT is safe.

CREATE TABLE IF NOT EXISTS stripe_events (
  id TEXT PRIMARY KEY,
  processed_at TEXT NOT NULL
);

CREATE TABLE webhook_events (
  id TEXT PRIMARY KEY,
  processed_at TEXT NOT NULL
);

INSERT INTO webhook_events (id, processed_at)
SELECT id, processed_at FROM stripe_events;

DROP TABLE stripe_events;

ALTER TABLE accounts ADD COLUMN needs_manual_key INTEGER;
ALTER TABLE accounts ADD COLUMN ls_customer_id TEXT;

CREATE TABLE licenses (
  id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL REFERENCES accounts(id),
  plan TEXT NOT NULL,
  key TEXT NOT NULL,
  lq_order_id TEXT UNIQUE,
  purchased_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  revoked_at TEXT,
  created_at TEXT NOT NULL
);

CREATE INDEX licenses_account_id ON licenses(account_id);
