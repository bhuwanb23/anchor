package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	_ "modernc.org/sqlite"
)

// migrateDir resolves the migrations folder relative to this test file, so
// tests do not depend on the working directory.
func migrateDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "migrations")
}

func openMigrateTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// runMigrations is the same loop as Migrate but with a configurable dir so
// the test can exercise migrations without depending on CWD.
func runMigrations(db *sql.DB, dir string) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		return err
	}
	migs := []string{
		"001_users.sql", "002_servers.sql", "003_deployments.sql", "004_tokens.sql",
		"005_servers_agent.sql", "006_server_health.sql", "007_custom_domains.sql",
		"008_backups.sql", "009_backup_metadata.sql", "010_restore_jobs.sql",
		"011_backup_verification.sql", "012_backup_storage.sql", "013_pending_commands.sql",
		"014_metrics.sql", "015_alerts.sql", "016_alert_delivery.sql", "017_users_auth.sql",
		"018_refresh_tokens.sql",
	}
	for _, m := range migs {
		var applied int
		if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE name = ?", m).Scan(&applied); err != nil {
			return err
		}
		if applied > 0 {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, m))
		if err != nil {
			return err
		}
		if _, err := db.Exec(string(data)); err != nil {
			if isDuplicateColumn(err) {
				continue
			}
			return err
		}
		if _, err := db.Exec("INSERT INTO schema_migrations (name) VALUES (?)", m); err != nil {
			return err
		}
	}
	return nil
}

func TestMigrations_FreshDatabase(t *testing.T) {
	db := openMigrateTestDB(t)
	if err := runMigrations(db, migrateDir()); err != nil {
		t.Fatalf("fresh migration: %v", err)
	}

	// The 017 migration must have added name and updated_at.
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('users') WHERE name IN ('name','updated_at')").Scan(&count); err != nil {
		t.Fatalf("pragma users: %v", err)
	}
	if count != 2 {
		t.Errorf("expected name and updated_at columns, found %d", count)
	}
}

func TestMigrations_IdempotentRerun(t *testing.T) {
	db := openMigrateTestDB(t)
	if err := runMigrations(db, migrateDir()); err != nil {
		t.Fatalf("first migration: %v", err)
	}
	if err := runMigrations(db, migrateDir()); err != nil {
		t.Fatalf("second migration: %v", err)
	}
}

func TestMigrations_InsertUserWithName(t *testing.T) {
	db := openMigrateTestDB(t)
	if err := runMigrations(db, migrateDir()); err != nil {
		t.Fatalf("migration: %v", err)
	}
	if _, err := db.Exec("INSERT INTO users (id, email, name, password_hash) VALUES (?, ?, ?, ?)", "u1", "a@b.com", "Alice", "hash"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
}
