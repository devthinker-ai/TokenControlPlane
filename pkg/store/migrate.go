package store

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type migrationFile struct {
	ID   int
	Name string
	SQL  string
}

// SchemaVersion returns the highest applied migration ID (0 if none).
func (s *Store) SchemaVersion() (int, error) {
	var n sql.NullInt64
	err := s.db.QueryRow(`SELECT MAX(id) FROM schema_migrations`).Scan(&n)
	if err != nil {
		return 0, err
	}
	if !n.Valid {
		return 0, nil
	}
	return int(n.Int64), nil
}

// SetMeta writes a key/value into the meta table.
func (s *Store) SetMeta(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO meta (key, value) VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// GetMeta reads a meta value (ok=false if missing).
func (s *Store) GetMeta(key string) (string, bool, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// ListAppliedMigrations returns applied migration IDs ascending.
func (s *Store) ListAppliedMigrations() ([]int, error) {
	rows, err := s.db.Query(`SELECT id FROM schema_migrations ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Store) migrate(dbPath, appVersion string, log *slog.Logger) error {
	if log == nil {
		log = slog.Default()
	}
	if err := s.ensureMigrationBookkeeping(); err != nil {
		return err
	}
	if err := s.bootstrapLegacyIfNeeded(log); err != nil {
		return err
	}

	files, err := loadMigrationFiles()
	if err != nil {
		return err
	}
	applied, err := s.appliedSet()
	if err != nil {
		return err
	}

	pending := make([]migrationFile, 0)
	for _, f := range files {
		if !applied[f.ID] {
			pending = append(pending, f)
		}
	}
	if len(pending) > 0 {
		if err := s.backupBeforeMigrate(dbPath, log); err != nil {
			return fmt.Errorf("pre-migration backup: %w", err)
		}
	}
	for _, f := range pending {
		if err := s.applyMigration(f); err != nil {
			return fmt.Errorf("migration %s failed: %w", f.Name, err)
		}
		log.Info("applied migration", "name", f.Name, "id", f.ID)
	}

	if appVersion == "" {
		appVersion = "dev"
	}
	return s.SetMeta("app_version", appVersion)
}

func (s *Store) ensureMigrationBookkeeping() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS schema_migrations (
  id INTEGER PRIMARY KEY,
  name TEXT UNIQUE NOT NULL,
  applied_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);`)
	return err
}

// bootstrapLegacyIfNeeded stamps 0001 when a pre-migration DB already has accounts.
// If accounts exist but meta.app_version is set, this is a tampered schema_migrations
// (not a true legacy upgrade) — leave empty so applyMigration fails loudly on CREATE.
func (s *Store) bootstrapLegacyIfNeeded(log *slog.Logger) error {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	var name string
	err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='accounts'`).Scan(&name)
	if err == sql.ErrNoRows {
		return nil // fresh DB — run 0001 normally
	}
	if err != nil {
		return err
	}
	if _, ok, err := s.GetMeta("app_version"); err != nil {
		return err
	} else if ok {
		// Tables exist and we previously recorded a binary version → operator deleted
		// schema_migrations rows. Do not stamp; re-apply will fail with a clear error.
		return nil
	}
	// Legacy install: stamp 0001 without re-running CREATE TABLE.
	_, err = s.db.Exec(`INSERT INTO schema_migrations (id, name, applied_at) VALUES (1, '0001_initial.sql', ?)`,
		time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	log.Info("stamped legacy schema as migration 0001_initial.sql")
	return nil
}

func (s *Store) appliedSet() (map[int]bool, error) {
	ids, err := s.ListAppliedMigrations()
	if err != nil {
		return nil, err
	}
	m := make(map[int]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m, nil
}

func (s *Store) applyMigration(f migrationFile) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(f.SQL); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations (id, name, applied_at) VALUES (?, ?, ?)`,
		f.ID, f.Name, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		// Fail loudly on tamper / duplicate: clear message for operators.
		return fmt.Errorf("record migration %s (id=%d): %w — "+
			"schema_migrations may be out of sync with the database; restore from backup or investigate manually",
			f.Name, f.ID, err)
	}
	return tx.Commit()
}

func (s *Store) backupBeforeMigrate(dbPath string, log *slog.Logger) error {
	if dbPath == "" || strings.Contains(dbPath, ":memory:") {
		return nil
	}
	// Strip DSN query params for filesystem path.
	path := dbPath
	if i := strings.Index(path, "?"); i >= 0 {
		path = path[:i]
	}
	backup := path + ".backup"
	// VACUUM INTO requires a non-open exclusive path; use absolute.
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	backupAbs := abs + ".backup"
	_ = os.Remove(backupAbs) // one generation
	if _, err := s.db.Exec(`VACUUM INTO ?`, backupAbs); err != nil {
		return err
	}
	log.Info("pre-migration backup written", "path", backup)
	return nil
}

func loadMigrationFiles() ([]migrationFile, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, err
	}
	var files []migrationFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		id, err := parseMigrationID(e.Name())
		if err != nil {
			return nil, err
		}
		b, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return nil, err
		}
		files = append(files, migrationFile{ID: id, Name: e.Name(), SQL: string(b)})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].ID < files[j].ID })
	return files, nil
}

// MaxEmbeddedMigrationID is the highest migration ID shipped in this binary.
func MaxEmbeddedMigrationID() int {
	files, err := loadMigrationFiles()
	if err != nil || len(files) == 0 {
		return 0
	}
	return files[len(files)-1].ID
}

func parseMigrationID(name string) (int, error) {
	parts := strings.SplitN(name, "_", 2)
	if len(parts) < 1 {
		return 0, fmt.Errorf("bad migration name %q", name)
	}
	id, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, fmt.Errorf("migration %q: expected NNNN_name.sql", name)
	}
	return id, nil
}
