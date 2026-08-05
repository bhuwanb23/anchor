package db

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
	_ "modernc.org/sqlite"
)

func Open(path string) (*sql.DB, error) {
	if err := os.MkdirAll("./data", 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA foreign_keys=ON;",
		"PRAGMA busy_timeout=5000;",
	}

	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			return nil, fmt.Errorf("pragma %q: %w", pragma, err)
		}
	}

	return db, nil
}

// Migrate applies every migration in order, exactly once. A schema_migrations
// table records what has already been applied, so restarting the control plane
// is safe even for migrations built on ALTER TABLE (SQLite does not support
// ADD COLUMN IF NOT EXISTS).
func Migrate(database *sql.DB) error {
	if _, err := database.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	migrations := []string{
		"001_users.sql",
		"002_servers.sql",
		"003_deployments.sql",
		"004_tokens.sql",
		"005_servers_agent.sql",
		"006_server_health.sql",
		"007_custom_domains.sql",
		"008_backups.sql",
		"009_backup_metadata.sql",
		"010_restore_jobs.sql",
		"011_backup_verification.sql",
		"012_backup_storage.sql",
		"013_pending_commands.sql",
		"014_metrics.sql",
		"015_alerts.sql",
		"016_alert_delivery.sql",
		"017_users_auth.sql",
		"018_refresh_tokens.sql",
		"019_teams.sql",
		"020_password_resets.sql",
		"021_commands.sql",
	}

	for _, migration := range migrations {
		var applied int
		if err := database.QueryRow(
			"SELECT COUNT(*) FROM schema_migrations WHERE name = ?", migration,
		).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", migration, err)
		}
		if applied > 0 {
			continue
		}

		path := "./internal/db/migrations/" + migration
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", migration, err)
		}

		if _, err := database.Exec(string(data)); err != nil {
			// Backwards compatibility: databases created by the pre-tracking
			// runner may already have columns that a later ALTER migration
			// adds. A duplicate-column error means the change is already
			// present, so record it as applied and move on.
			if isDuplicateColumn(err) {
				slog.Warn("migration column already present, skipping", "migration", migration, "error", err)
			} else {
				return fmt.Errorf("run migration %s: %w", migration, err)
			}
		}

		if _, err := database.Exec(
			"INSERT INTO schema_migrations (name) VALUES (?)", migration,
		); err != nil {
			return fmt.Errorf("record migration %s: %w", migration, err)
		}
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
	return queries.DeleteExpiredRegistrationTokens(db)
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
