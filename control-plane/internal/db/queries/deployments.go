package queries

import (
	"database/sql"
	"time"
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

// ---------------------------------------------------------------------------
// Typed deployment queries (Layer 5C Step 3A/3C)
// ---------------------------------------------------------------------------

// Deployment is a full row of the deployments table.
type Deployment struct {
	ID        string
	ServerID  string
	AppName   string
	Image     string
	Port      int
	Domain    sql.NullString
	Status    string
	CreatedAt string
	UpdatedAt string
}

const deploymentColumns = `id, server_id, app_name, image, port, domain, status, created_at, updated_at`

func scanDeployment(scanner interface{ Scan(...any) error }) (Deployment, error) {
	var d Deployment
	if err := scanner.Scan(&d.ID, &d.ServerID, &d.AppName, &d.Image, &d.Port, &d.Domain, &d.Status, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return d, err
	}
	return d, nil
}

// GetDeploymentByID returns a single deployment (Pattern 1).
// Returns nil, nil when the deployment does not exist.
func GetDeploymentByID(db *sql.DB, deploymentID string) (*Deployment, error) {
	d, err := scanDeployment(db.QueryRow(
		"SELECT "+deploymentColumns+" FROM deployments WHERE id = ?",
		deploymentID,
	))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// ListDeploymentsByApp returns the most recent deployments for an app on a
// server, newest first (Layer 5C Step 3C #2). The schema keys deployments by
// server_id + app_name; limit defaults to 20.
func ListDeploymentsByApp(db *sql.DB, serverID, appName string, limit int) ([]Deployment, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := db.Query(
		"SELECT "+deploymentColumns+" FROM deployments "+
			"WHERE server_id = ? AND app_name = ? "+
			"ORDER BY created_at DESC LIMIT ?",
		serverID, appName, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Deployment
	for rows.Next() {
		d, err := scanDeployment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if out == nil {
		out = []Deployment{}
	}
	return out, rows.Err()
}

// ListDeploymentsByServer returns the most recent deployments for a server,
// newest first (Pattern 3). limit defaults to 50.
func ListDeploymentsByServer(db *sql.DB, serverID string, limit int) ([]Deployment, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.Query(
		"SELECT "+deploymentColumns+" FROM deployments "+
			"WHERE server_id = ? ORDER BY created_at DESC LIMIT ?",
		serverID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Deployment
	for rows.Next() {
		d, err := scanDeployment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if out == nil {
		out = []Deployment{}
	}
	return out, rows.Err()
}