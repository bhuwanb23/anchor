package queries

import (
	"database/sql"
	"time"
)

// UpsertAlert inserts a new alert or updates an existing one by id (for
// escalations and resolutions, which reuse the alert id from the agent).
func UpsertAlert(db *sql.DB, a AlertRecord) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`INSERT INTO alerts (id, server_id, project, container, severity, type, status,
			title, message, detail, action, metrics, fired_at, resolved_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			project = excluded.project,
			container = excluded.container,
			severity = excluded.severity,
			type = excluded.type,
			status = excluded.status,
			title = excluded.title,
			message = excluded.message,
			detail = excluded.detail,
			action = excluded.action,
			metrics = excluded.metrics,
			resolved_at = excluded.resolved_at,
			updated_at = excluded.updated_at`,
		a.ID, a.ServerID, nullString(a.Project), nullString(a.Container),
		a.Severity, a.Type, a.Status,
		nullString(a.Title), nullString(a.Message), nullString(a.Detail), nullString(a.Action),
		nullString(a.MetricsJSON), nullString(a.FiredAt), nullString(a.ResolvedAt),
		now,
	)
	return err
}

// ListAlertsByServer returns the most recent alerts for a server, newest
// first, with active alerts floated to the top of the list.
func ListAlertsByServer(db *sql.DB, serverID string, limit int) ([]AlertRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.Query(
		`SELECT id, server_id, project, container, severity, type, status,
			title, message, detail, action, metrics, fired_at, resolved_at, created_at
		FROM alerts WHERE server_id = ?
		ORDER BY CASE WHEN status = 'active' THEN 0 ELSE 1 END, fired_at DESC
		LIMIT ?`,
		serverID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AlertRecord
	for rows.Next() {
		var a AlertRecord
		var project, container, title, message, detail, action, metrics, firedAt, resolvedAt, createdAt sql.NullString
		if err := rows.Scan(&a.ID, &a.ServerID, &project, &container, &a.Severity, &a.Type, &a.Status,
			&title, &message, &detail, &action, &metrics, &firedAt, &resolvedAt, &createdAt); err != nil {
			return nil, err
		}
		a.Project = project.String
		a.Container = container.String
		a.Title = title.String
		a.Message = message.String
		a.Detail = detail.String
		a.Action = action.String
		a.MetricsJSON = metrics.String
		a.FiredAt = firedAt.String
		a.ResolvedAt = resolvedAt.String
		a.CreatedAt = createdAt.String
		out = append(out, a)
	}
	return out, rows.Err()
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// AlertRecord mirrors the agent's Alert JSON for persistence.
type AlertRecord struct {
	ID          string
	ServerID    string
	Project     string
	Container   string
	Severity    string
	Type        string
	Status      string
	Title       string
	Message     string
	Detail      string
	Action      string
	MetricsJSON string
	FiredAt     string
	ResolvedAt  string
	CreatedAt   string
}
