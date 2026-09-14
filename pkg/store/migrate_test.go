package store_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
	_ "modernc.org/sqlite"
)

func TestMigrationsFreshAndIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh.db")
	st, err := store.OpenWithOptions(path, store.OpenOptions{AppVersion: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	ver, err := st.SchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if ver != 11 {
		t.Fatalf("schema version=%d", ver)
	}
	v, ok, err := st.GetMeta("app_version")
	if err != nil || !ok || v != "1.0.0" {
		t.Fatalf("app_version=%q ok=%v err=%v", v, ok, err)
	}
	for _, table := range []string{"accounts", "users", "mcp_servers", "api_keys", "activity_events", "schema_migrations", "meta", "oauth_tokens", "oauth_sessions", "key_tool_grants", "invites", "llm_providers", "llm_models", "usage_by_provider", "key_provider_grants", "account_settings", "email_invites", "password_resets", "totp"} {
		var name string
		err := st.DB().QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
		if err != nil {
			t.Fatalf("missing table %s: %v", table, err)
		}
	}
	_ = st.Close()

	// Re-open: idempotent, nothing re-applied.
	st2, err := store.OpenWithOptions(path, store.OpenOptions{AppVersion: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	ids, err := st2.ListAppliedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 11 || ids[0] != 1 || ids[10] != 11 {
		t.Fatalf("applied=%v", ids)
	}
}

func TestMigrationTamperFailsLoudly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tamper.db")
	st, err := store.OpenWithOptions(path, store.OpenOptions{AppVersion: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.DB().Exec(`DELETE FROM schema_migrations WHERE id = 1`)
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Close()

	_, err = store.OpenWithOptions(path, store.OpenOptions{AppVersion: "1.0.0"})
	if err == nil {
		t.Fatal("expected migrate to fail after tampering schema_migrations")
	}
	if !contains(err.Error(), "0001_initial") && !contains(err.Error(), "migration") {
		t.Fatalf("error should name migration: %v", err)
	}
}

func TestLegacyBootstrapStamps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE accounts (
  id TEXT PRIMARY KEY, name TEXT NOT NULL, plan TEXT NOT NULL DEFAULT 'free',
  max_seats INTEGER NOT NULL DEFAULT 3, max_servers INTEGER NOT NULL DEFAULT 3,
  created_at TEXT NOT NULL
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
  created_at TEXT NOT NULL
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
  last_used_at TEXT
);
CREATE TABLE tools (
  id TEXT PRIMARY KEY,
  server_id TEXT NOT NULL,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  input_schema TEXT NOT NULL DEFAULT '{}'
)`)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	st, err := store.OpenWithOptions(path, store.OpenOptions{AppVersion: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ver, err := st.SchemaVersion()
	if err != nil || ver != 11 {
		t.Fatalf("ver=%d err=%v", ver, err)
	}
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
