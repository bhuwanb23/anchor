package queries

import (
	"database/sql"
	"encoding/json"
	"time"
)

// PendingCommand is a command queued while the agent was offline.
type PendingCommand struct {
	ID          string
	ServerID    string
	CommandType string
	Payload     string // full command JSON envelope
	ProjectKey  string
	ExpiresAt   sql.NullString
	CreatedAt   string
}

// EnqueuePendingCommand stores a command for later delivery.
// For deploy commands with the same project_key, older deploys are removed.
func EnqueuePendingCommand(db *sql.DB, id, serverID, commandType, payload, projectKey, expiresAt string) error {
	if commandType == "deploy" && projectKey != "" {
		_, _ = db.Exec(
			`DELETE FROM pending_commands WHERE server_id = ? AND command_type = 'deploy' AND project_key = ?`,
			serverID, projectKey,
		)
	}
	var exp interface{}
	if expiresAt != "" {
		exp = expiresAt
	}
	_, err := db.Exec(
		`INSERT INTO pending_commands (id, server_id, command_type, payload, project_key, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, serverID, commandType, payload, projectKey, exp, time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

// ListPendingCommands returns non-expired pending commands oldest-first.
func ListPendingCommands(db *sql.DB, serverID string) ([]PendingCommand, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	rows, err := db.Query(
		`SELECT id, server_id, command_type, payload, project_key, expires_at, created_at
		 FROM pending_commands
		 WHERE server_id = ?
		   AND (expires_at IS NULL OR expires_at = '' OR expires_at > ?)
		 ORDER BY created_at ASC`,
		serverID, now,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PendingCommand
	for rows.Next() {
		var c PendingCommand
		if err := rows.Scan(&c.ID, &c.ServerID, &c.CommandType, &c.Payload, &c.ProjectKey, &c.ExpiresAt, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DeletePendingCommands removes delivered pending commands for a server.
func DeletePendingCommands(db *sql.DB, serverID string) error {
	_, err := db.Exec(`DELETE FROM pending_commands WHERE server_id = ?`, serverID)
	return err
}

// DeletePendingCommand removes one pending command.
func DeletePendingCommand(db *sql.DB, id string) error {
	_, err := db.Exec(`DELETE FROM pending_commands WHERE id = ?`, id)
	return err
}

// PendingCommandsAsJSON returns pending command envelopes for hello_ack.
func PendingCommandsAsJSON(db *sql.DB, serverID string) ([]json.RawMessage, error) {
	cmds, err := ListPendingCommands(db, serverID)
	if err != nil {
		return nil, err
	}
	out := make([]json.RawMessage, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, json.RawMessage(c.Payload))
	}
	return out, nil
}

// DeleteExpiredPendingCommands removes pending commands whose expiry has passed.
func DeleteExpiredPendingCommands(db *sql.DB) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.Exec(
		`DELETE FROM pending_commands WHERE expires_at IS NOT NULL AND expires_at != '' AND expires_at < ?`,
		now,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
