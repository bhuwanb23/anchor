package db

import (
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Open opens a SQLite database at the given path, creates the parent directory
// with restricted permissions, and applies production PRAGMAs.
func Open(path string) (*sql.DB, error) {
	// Ensure the parent directory exists with restricted permissions.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	// Production PRAGMAs are passed as DSN query parameters (not db.Exec) so
	// the driver applies them to EVERY pooled connection: foreign_keys,
	// busy_timeout and synchronous are per-connection settings, and a second
	// pooled connection that missed them would silently accept orphaned rows
	// and fail with SQLITE_BUSY under contention. journal_mode=WAL is
	// persistent at the database level but is set per-connection here too,
	// which matches the driver's documented DSN behavior.
	dsn := path + "?" + strings.Join([]string{
		"_busy_timeout=5000",
		"_foreign_keys=1",
		"_journal_mode=WAL",
		"_synchronous=NORMAL",
		"_pragma=cache_size(-64000)",
		"_pragma=temp_store(MEMORY)",
	}, "&")

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Force the first connection open so the DSN PRAGMAs run (which creates
	// the database file on a fresh start) and a bad DSN fails at Open, not
	// at the first query.
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("open sqlite connection: %w", err)
	}

	// Connection pool: limit to 2 connections (1 write + 1 read).
	// SQLite WAL handles concurrent reads with one writer.
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(2)

	// Set file permissions on Linux/macOS (no-op on Windows).
	if runtime.GOOS != "windows" {
		_ = os.Chmod(path, 0o600)
	}

	return db, nil
}

// Migrate applies every migration in order, exactly once. A schema_migrations
// table records what has already been applied, so restarting the control plane
// is safe even for migrations built on ALTER TABLE (SQLite does not support
// ADD COLUMN IF NOT EXISTS). Each migration is wrapped in a transaction so
// partial failures are rolled back cleanly.
func Migrate(database *sql.DB) error {
	if _, err := database.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	// Read all embedded migration files and sort by version number.
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	type migrationEntry struct {
		name    string
		version int
	}
	var migrations []migrationEntry
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		// Extract version number from filename (e.g., "001_users.sql" → 1).
		parts := strings.SplitN(e.Name(), "_", 2)
		if len(parts) < 2 {
			continue
		}
		v, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		migrations = append(migrations, migrationEntry{name: e.Name(), version: v})
	}

	// Sort by version number.
	for i := 0; i < len(migrations); i++ {
		for j := i + 1; j < len(migrations); j++ {
			if migrations[j].version < migrations[i].version {
				migrations[i], migrations[j] = migrations[j], migrations[i]
			}
		}
	}

	applied := 0
	for _, m := range migrations {
		var count int
		if err := database.QueryRow(
			"SELECT COUNT(*) FROM schema_migrations WHERE name = ?", m.name,
		).Scan(&count); err != nil {
			return fmt.Errorf("check migration %s: %w", m.name, err)
		}
		if count > 0 {
			continue
		}

		data, err := migrationsFS.ReadFile("migrations/" + m.name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", m.name, err)
		}

		// Wrap each migration in a transaction.
		tx, err := database.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", m.name, err)
		}

		if _, err := tx.Exec(string(data)); err != nil {
			_ = tx.Rollback()
			// Backwards compatibility: databases created by the pre-tracking
			// runner may already have columns that a later ALTER migration
			// adds. A duplicate-column error means the change is already
			// present — but the transaction is dead after Rollback, so record
			// the migration on the outer connection and continue.
			if isDuplicateColumn(err) {
				slog.Warn("migration column already present, skipping", "migration", m.name, "error", err)
				if _, err := database.Exec(
					"INSERT INTO schema_migrations (name) VALUES (?)", m.name,
				); err != nil {
					return fmt.Errorf("record migration %s: %w", m.name, err)
				}
				applied++
				continue
			}
			return fmt.Errorf("run migration %s: %w", m.name, err)
		}

		if _, err := tx.Exec(
			"INSERT INTO schema_migrations (name) VALUES (?)", m.name,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", m.name, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", m.name, err)
		}
		applied++
	}

	if applied > 0 {
		slog.Info("database migrations applied", "count", applied)
	}
	return nil
}

// isDuplicateColumn reports whether a SQLite error is a duplicate-column
// error, which happens when an ALTER TABLE ADD COLUMN is re-applied to a
// table that already has the column.
func isDuplicateColumn(err error) bool {
	return strings.Contains(err.Error(), "duplicate column name")
}

func InsertServer(db *sql.DB, id, userID, name, token string) error {
	return queries.InsertServer(db, id, userID, name, token)
}

func QueryServersByUser(db *sql.DB, userID string) (*sql.Rows, error) {
	return queries.QueryServersByUser(db, userID)
}

func GetServerByToken(db *sql.DB, token string) (id, userID, name string, err error) {
	return queries.GetServerByToken(db, token)
}

func InsertDeployment(db *sql.DB, id, serverID, appName, image string, port int, domain string) error {
	return queries.InsertDeployment(db, id, serverID, appName, image, port, domain)
}

func QueryDeploymentsByServer(db *sql.DB, serverID string) (*sql.Rows, error) {
	return queries.QueryDeploymentsByServer(db, serverID)
}

func CreateRegistrationToken(db *sql.DB, id, tokenHash, userID, serverName, expiresAt string) error {
	return queries.CreateRegistrationToken(db, id, tokenHash, userID, serverName, expiresAt)
}

func FindRegistrationTokenByHash(db *sql.DB, tokenHash string) (id, userID, serverName, expiresAt string, usedAt sql.NullString, err error) {
	return queries.FindRegistrationTokenByHash(db, tokenHash)
}

func MarkRegistrationTokenUsed(db *sql.DB, tokenID, ip string) error {
	return queries.MarkRegistrationTokenUsed(db, tokenID, ip)
}

func DeleteExpiredRegistrationTokens(db *sql.DB) error {
	_, err := queries.DeleteExpiredRegistrationTokens(db)
	return err
}

func InsertServerWithAgent(db *sql.DB, id, userID, name, agentID, agentSecretHash, osInfo, arch string, ramMB, diskGB int, ipAddress string) error {
	return queries.InsertServerWithAgent(db, id, userID, name, agentID, agentSecretHash, osInfo, arch, ramMB, diskGB, ipAddress)
}

func GetServerByAgentID(db *sql.DB, agentID string) (id, userID, name, agentSecretHash, status string, err error) {
	return queries.GetServerByAgentID(db, agentID)
}

func UpdateServerConnection(db *sql.DB, serverID, status string) error {
	return queries.UpdateServerConnection(db, serverID, status)
}
