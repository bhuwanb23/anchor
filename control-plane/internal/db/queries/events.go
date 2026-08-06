package queries

import (
	"database/sql"
)

// ServerEvent represents an audit log entry for a server. The columns mirror
// the server_events schema from migration 006 (event_type, check_name,
// message, details) — not the plan's title/detail variant, which was never
// migrated.
type ServerEvent struct {
	ID        string
	ServerID  string
	EventType string
	CheckName sql.NullString
	Message   sql.NullString
	Details   sql.NullString
	CreatedAt string
}

const eventColumns = `id, server_id, event_type, check_name, message, details, created_at`

func scanEvent(scanner interface{ Scan(...any) error }) (ServerEvent, error) {
	var e ServerEvent
	if err := scanner.Scan(&e.ID, &e.ServerID, &e.EventType, &e.CheckName, &e.Message, &e.Details, &e.CreatedAt); err != nil {
		return e, err
	}
	return e, nil
}

// InsertServerEvent creates a new server event. check_name names the check or
// subsystem that produced it (e.g. "auto_fixed", "warning", "auto_remediation",
// "backup_storage_warning"); message and details carry the human-readable
// content. Used by the WS handlers and alert delivery (Layer 6 / 4C).
func InsertServerEvent(db *sql.DB, id, serverID, eventType, checkName, message, details string) error {
	_, err := db.Exec(
		`INSERT INTO server_events (id, server_id, event_type, check_name, message, details)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, serverID, eventType, nullString(checkName), nullString(message), nullString(details),
	)
	return err
}

// ListEventsByServer returns events for a server, most recent first
// (Pattern 3). limit defaults to 50; the empty result is an empty slice,
// never nil.
func ListEventsByServer(db *sql.DB, serverID string, limit int) ([]ServerEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.Query(
		"SELECT "+eventColumns+" FROM server_events WHERE server_id = ? ORDER BY created_at DESC LIMIT ?",
		serverID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []ServerEvent
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	if events == nil {
		events = []ServerEvent{}
	}
	return events, rows.Err()
}

// DeleteOldEvents removes events older than the given number of days.
func DeleteOldEvents(db *sql.DB, days int) (int64, error) {
	result, err := db.Exec(
		`DELETE FROM server_events WHERE created_at < datetime('now', '-' || ? || ' days')`,
		days,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
