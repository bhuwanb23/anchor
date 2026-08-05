package queries

import (
	"database/sql"
	"time"
)

// CommandRecord is one row of the commands audit/status table (Layer 5B Step 4).
type CommandRecord struct {
	ID          string
	ServerID    string
	CommandType string
	ProjectKey  string
	Payload     string
	Status      string
	IssuedBy    string
	CreatedAt   string
	StartedAt   string
	CompletedAt string
	Result      string
}

// InsertCommand records a browser-issued command in its initial queued state.
func InsertCommand(db *sql.DB, id, serverID, commandType, payload, projectKey, status, issuedBy, createdAt string) error {
	_, err := db.Exec(
		`INSERT INTO commands (id, server_id, command_type, project_key, payload, status, issued_by, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, serverID, commandType, projectKey, payload, status, issuedBy, createdAt,
	)
	return err
}

// GetCommandByID returns a command row, or nil when it does not exist.
func GetCommandByID(db *sql.DB, id string) (*CommandRecord, error) {
	row := db.QueryRow(
		`SELECT id, server_id, command_type, project_key, payload, status, issued_by,
		        created_at, COALESCE(started_at,''), COALESCE(completed_at,''), COALESCE(result,'')
		 FROM commands WHERE id = ?`, id)
	var r CommandRecord
	if err := row.Scan(&r.ID, &r.ServerID, &r.CommandType, &r.ProjectKey, &r.Payload,
		&r.Status, &r.IssuedBy, &r.CreatedAt, &r.StartedAt, &r.CompletedAt, &r.Result); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

// HasInProgressCommand reports whether a queued or in-progress command of the
// same type for the same project already exists on the server (Layer 5B
// Step 4C deduplication). Commands without a project key are never deduped.
func HasInProgressCommand(db *sql.DB, serverID, commandType, projectKey string) (bool, error) {
	if projectKey == "" {
		return false, nil
	}
	var one int
	err := db.QueryRow(
		`SELECT 1 FROM commands
		 WHERE server_id = ? AND command_type = ? AND project_key = ?
		   AND status IN ('queued', 'in_progress')
		 LIMIT 1`,
		serverID, commandType, projectKey,
	).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// UpdateCommandStatus advances a command's status, recording started_at when it
// becomes in_progress and completed_at when it reaches a terminal state.
func UpdateCommandStatus(db *sql.DB, id, status, result string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`UPDATE commands SET
		   status = ?,
		   result = ?,
		   started_at = COALESCE(started_at, CASE WHEN ? = 'in_progress' THEN ? END),
		   completed_at = CASE WHEN ? IN ('success', 'failed', 'timeout') THEN ? ELSE completed_at END
		 WHERE id = ?`,
		status, result, status, now, status, now, id,
	)
	return err
}

// UpdateCommandStatusIfActive advances a command's status only while it is
// still queued or in_progress. The atomic guard makes the timeout timer safe:
// a command that completed right before the deadline is never overwritten.
func UpdateCommandStatusIfActive(db *sql.DB, id, status, result string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`UPDATE commands SET
		   status = ?,
		   result = ?,
		   started_at = COALESCE(started_at, CASE WHEN ? = 'in_progress' THEN ? END),
		   completed_at = CASE WHEN ? IN ('success', 'failed', 'timeout') THEN ? ELSE completed_at END
		 WHERE id = ? AND status IN ('queued', 'in_progress')`,
		status, result, status, now, status, now, id,
	)
	return err
}

// UpdateCommandResult records a late result (e.g. arriving after a timeout)
// without changing the terminal status, for audit purposes (Layer 5B Step 4B).
func UpdateCommandResult(db *sql.DB, id, result string) error {
	_, err := db.Exec(
		`UPDATE commands SET result = ?, completed_at = COALESCE(completed_at, ?) WHERE id = ?`,
		result, time.Now().UTC().Format(time.RFC3339), id,
	)
	return err
}
