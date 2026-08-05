package queries

import (
	"database/sql"
)

// ServerEvent represents an audit log entry for a server.
type ServerEvent struct {
	ID          string
	ServerID    string
	ProjectName sql.NullString
	EventType   string
	Title       string
	Detail      sql.NullString
	ActorID     sql.NullString
	ActorType   sql.NullString
	RelatedID   sql.NullString
	RelatedType sql.NullString
	OccurredAt  string
	CreatedAt   string
}

// InsertServerEvent creates a new server event.
func InsertServerEvent(db *sql.DB, id, serverID, eventType, title string) error {
	_, err := db.Exec(
		`INSERT INTO server_events (id, server_id, event_type, title, created_at)
		 VALUES (?, ?, ?, ?, datetime('now'))`,
		id, serverID, eventType, title,
	)
	return err
}

// InsertServerEventDetailed creates a server event with full details.
func InsertServerEventDetailed(db *sql.DB, id, serverID, eventType, title, detail, actorID, actorType, relatedID, relatedType string) error {
	_, err := db.Exec(
		`INSERT INTO server_events (id, server_id, event_type, title, detail, actor_id, actor_type, related_id, related_type, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		id, serverID, eventType, title, detail, actorID, actorType, relatedID, relatedType,
	)
	return err
}

// ListEventsByServer returns events for a server, most recent first.
func ListEventsByServer(db *sql.DB, serverID string, limit int) ([]ServerEvent, error) {
	rows, err := db.Query(
		`SELECT id, server_id, event_type, title, detail, created_at
		 FROM server_events WHERE server_id = ?
		 ORDER BY created_at DESC LIMIT ?`,
		serverID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []ServerEvent
	for rows.Next() {
		var e ServerEvent
		if err := rows.Scan(
			&e.ID, &e.ServerID, &e.EventType, &e.Title, &e.Detail, &e.CreatedAt,
		); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	if events == nil {
		events = []ServerEvent{}
	}
	return events, nil
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
