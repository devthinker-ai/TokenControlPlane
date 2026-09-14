-- Migration 0007: perpetual + paid-updates commercial model (Phase 12).
-- Adds update-window length and founders flag on licenses.
-- Both columns have defaults so a future downgrade can DROP them safely.
-- Refunded founders rows keep is_founders=1 — slots are not recycled (anti-churn).

ALTER TABLE licenses ADD COLUMN window_months INTEGER NOT NULL DEFAULT 12;
ALTER TABLE licenses ADD COLUMN is_founders   INTEGER NOT NULL DEFAULT 0;
