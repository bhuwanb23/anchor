package db

import (
	"database/sql"
	"fmt"
	"os"

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