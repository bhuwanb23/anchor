package db

import (
	"database/sql"
	"fmt"
	"os"

	_ "modernc.org/sqlite"
	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
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

func Migrate(database *sql.DB) error {
	migrations := []string{
		"001_users.sql",
		"002_servers.sql",
		"003_deployments.sql",
	}

	for _, migration := range migrations {
		path := "./internal/db/migrations/" + migration
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", migration, err)
		}

		if _, err := database.Exec(string(data)); err != nil {
			return fmt.Errorf("run migration %s: %w", migration, err)
		}
	}

	return nil
}

func InsertUser(db *sql.DB, id, email, passwordHash string) error {
	return queries.InsertUser(db, id, email, passwordHash)
}

func GetUserByEmail(db *sql.DB, email string) (id string, passwordHash string, err error) {
	return queries.GetUserByEmail(db, email)
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