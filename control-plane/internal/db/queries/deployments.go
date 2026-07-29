package queries

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
)

func InsertDeployment(db *sql.DB, id, serverID, appName, image string, port int, domain string) error {
	_, err := db.Exec(
		"INSERT INTO deployments (id, server_id, app_name, image, port, domain) VALUES (?, ?, ?, ?, ?, ?)",
		id, serverID, appName, image, port, domain,
	)
	return err
}

func QueryDeploymentsByServer(db *sql.DB, serverID string) (*sql.Rows, error) {
	return db.Query(
		"SELECT id, app_name, image, port, domain, status, created_at, updated_at FROM deployments WHERE server_id = ?",
		serverID,
	)
}

func UpdateDeploymentStatus(db *sql.DB, deploymentID, status string) error {
	_, err := db.Exec(
		"UPDATE deployments SET status = ?, updated_at = ? WHERE id = ?",
		status, time.Now().UTC().Format(time.RFC3339), deploymentID,
	)
	return err
}