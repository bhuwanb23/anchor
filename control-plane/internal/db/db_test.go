package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// openTestDB opens an in-memory SQLite database for testing.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestMigrations_FreshDatabase(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
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
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("first migration: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second migration: %v", err)
	}
}

func TestMigrations_InsertUserWithName(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migration: %v", err)
	}
	if _, err := db.Exec("INSERT INTO users (id, email, name, password_hash) VALUES (?, ?, ?, ?)", "u1", "a@b.com", "Alice", "hash"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
}

func TestMigrations_PasswordResetsTable(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migration: %v", err)
	}
	// The 020 migration must create password_resets with the reset columns.
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('password_resets') WHERE name IN ('token_hash','expires_at','used_at')").Scan(&count); err != nil {
		t.Fatalf("pragma password_resets: %v", err)
	}
	if count != 3 {
		t.Errorf("expected token_hash, expires_at, used_at columns, found %d", count)
	}
}

func TestMigrations_TeamsTables(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migration: %v", err)
	}
	// The 019 migration must create the team tables used by Layer 5A Step 5.
	var teams, members, serverTeam, invitations int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='teams'").Scan(&teams); err != nil {
		t.Fatalf("pragma teams: %v", err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='team_members'").Scan(&members); err != nil {
		t.Fatalf("pragma team_members: %v", err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='server_team'").Scan(&serverTeam); err != nil {
		t.Fatalf("pragma server_team: %v", err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='invitations'").Scan(&invitations); err != nil {
		t.Fatalf("pragma invitations: %v", err)
	}
	if teams != 1 || members != 1 || serverTeam != 1 || invitations != 1 {
		t.Errorf("team tables missing: teams=%d members=%d server_team=%d invitations=%d", teams, members, serverTeam, invitations)
	}
}

func TestOpen_PragmasApplied(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Verify WAL mode.
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}

	// Verify foreign keys.
	var fk int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Errorf("foreign_keys = %d, want 1", fk)
	}

	// Verify synchronous.
	var sync int
	if err := db.QueryRow("PRAGMA synchronous").Scan(&sync); err != nil {
		t.Fatalf("synchronous: %v", err)
	}
	if sync != 1 { // NORMAL = 1
		t.Errorf("synchronous = %d, want 1 (NORMAL)", sync)
	}

	// Verify temp_store.
	var temp int
	if err := db.QueryRow("PRAGMA temp_store").Scan(&temp); err != nil {
		t.Fatalf("temp_store: %v", err)
	}
	if temp != 2 { // MEMORY = 2
		t.Errorf("temp_store = %d, want 2 (MEMORY)", temp)
	}
}

func TestOpen_ConnectionPoolLimits(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Verify MaxOpenConns is set to 2.
	if got := db.Stats().MaxOpenConnections; got != 2 {
		t.Errorf("MaxOpenConnections = %d, want 2", got)
	}
}

func TestOpen_CreatesParentDirectory(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "subdir", "test.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Verify the parent directory was created.
	if _, err := os.Stat(filepath.Join(dir, "subdir")); os.IsNotExist(err) {
		t.Error("parent directory was not created")
	}
}

func TestMigrations_RecordsAppliedCount(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migration: %v", err)
	}

	// Count applied migrations.
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if count != 26 {
		t.Errorf("expected 26 applied migrations, got %d", count)
	}
}

func TestMigrateDir(t *testing.T) {
	// Verify the embedded migrations can be read.
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 26 {
		t.Errorf("expected 26 embedded migrations, got %d", len(entries))
	}
}
