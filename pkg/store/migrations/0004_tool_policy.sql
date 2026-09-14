-- Migration 0004: tool catalog policy (Phase 11a).
-- Server-level per-tool enable + per-key allowlists (Pro/Team).

ALTER TABLE tools ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1;

ALTER TABLE api_keys ADD COLUMN tool_policy TEXT NOT NULL DEFAULT 'all';

CREATE TABLE key_tool_grants (
  key_id TEXT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
  tool_name TEXT NOT NULL,
  PRIMARY KEY (key_id, tool_name)
);

CREATE INDEX idx_key_tool_grants_key ON key_tool_grants(key_id);
