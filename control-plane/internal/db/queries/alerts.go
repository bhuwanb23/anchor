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

// Qualified with `alerts.` so the same constant works in the joined
// ListRecentAlertsForUser query without ambiguous-column errors.
const alertColumns = `alerts.id, alerts.server_id, alerts.project, alerts.container,
	alerts.severity, alerts.type, alerts.status, alerts.title, alerts.message,
	alerts.detail, alerts.action, alerts.metrics, alerts.fired_at, alerts.resolved_at,
	alerts.read_at, alerts.acknowledged_at, alerts.acknowledged_by, alerts.created_at`

func scanAlert(scanner interface{ Scan(...any) error }) (AlertRecord, error) {
	var a AlertRecord
	var project, container, title, message, detail, action, metrics, firedAt, resolvedAt, createdAt sql.NullString
	var readAt, ackAt, ackBy sql.NullString
	if err := scanner.Scan(&a.ID, &a.ServerID, &project, &container, &a.Severity, &a.Type, &a.Status,
		&title, &message, &detail, &action, &metrics, &firedAt, &resolvedAt,
		&readAt, &ackAt, &ackBy, &createdAt); err != nil {
		return a, err
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
	a.ReadAt = readAt.String
	a.AcknowledgedAt = ackAt.String
	a.AcknowledgedBy = ackBy.String
	a.CreatedAt = createdAt.String
	// Level is the legacy severity label the dashboard still reads.
	if a.Status == "resolved" {
		a.Level = "resolved"
	} else {
		a.Level = a.Severity
	}
	return a, nil
}

// ListAlertsByServer returns the most recent alerts for a server, newest
// first, with active alerts floated to the top of the list.
func ListAlertsByServer(db *sql.DB, serverID string, limit int) ([]AlertRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.Query(
		`SELECT `+alertColumns+` FROM alerts WHERE alerts.server_id = ?
		ORDER BY CASE WHEN alerts.status = 'active' THEN 0 ELSE 1 END, alerts.fired_at DESC
		LIMIT ?`,
		serverID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AlertRecord
	for rows.Next() {
		a, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if out == nil {
		out = []AlertRecord{}
	}
	return out, rows.Err()
}

// ListActiveAlertsByServer returns only the currently active (unresolved,
// unacknowledged) alerts for a server, newest first (Layer 5C Step 3C #3).
// Used for the server_state snapshot and alert-history views that show the
// live incident list.
func ListActiveAlertsByServer(db *sql.DB, serverID string) ([]AlertRecord, error) {
	rows, err := db.Query(
		`SELECT `+alertColumns+` FROM alerts
		 WHERE alerts.server_id = ? AND alerts.status = 'active'
		 ORDER BY alerts.fired_at DESC`,
		serverID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AlertRecord
	for rows.Next() {
		a, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if out == nil {
		out = []AlertRecord{}
	}
	return out, rows.Err()
}

// ListRecentAlertsForUser returns recent alerts across every server owned by
// the user (newest first), with the owning server's name attached so a global
// notification center can render them without an extra lookup.
func ListRecentAlertsForUser(db *sql.DB, userID string, limit int) ([]AlertRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.Query(
		`SELECT `+alertColumns+` FROM alerts
		JOIN servers ON servers.id = alerts.server_id
		WHERE servers.user_id = ?
		ORDER BY CASE WHEN alerts.status = 'active' THEN 0 ELSE 1 END, alerts.fired_at DESC
		LIMIT ?`,
		userID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AlertRecord
	for rows.Next() {
		a, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if name, err := ServerName(db, out[i].ServerID); err == nil {
			out[i].ServerName = name
		}
	}
	if out == nil {
		out = []AlertRecord{}
	}
	return out, nil
}

// UnreadAlertCountForUser counts active, never-read alerts across the user's
// servers — the bell badge in the dashboard.
func UnreadAlertCountForUser(db *sql.DB, userID string) (int, error) {
	var count int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM alerts
		JOIN servers ON servers.id = alerts.server_id
		WHERE servers.user_id = ? AND alerts.status = 'active' AND alerts.read_at IS NULL`,
		userID,
	).Scan(&count)
	return count, err
}

// MarkAlertsReadForUser stamps read_at on every unread active alert across the
// user's servers (called when the notification center is opened).
func MarkAlertsReadForUser(db *sql.DB, userID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`UPDATE alerts SET read_at = ?
		WHERE read_at IS NULL AND status = 'active'
		AND server_id IN (SELECT id FROM servers WHERE user_id = ?)`,
		now, userID,
	)
	return err
}

// AcknowledgeAlert marks an active alert as acknowledged by a user. Resolved
// alerts are left untouched (they are already closed).
func AcknowledgeAlert(db *sql.DB, alertID, serverID, userID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`UPDATE alerts SET status = 'acknowledged', acknowledged_at = ?, acknowledged_by = ?
		WHERE id = ? AND server_id = ? AND status = 'active'`,
		now, userID, alertID, serverID,
	)
	return err
}

// ServerName returns a server's display name.
func ServerName(db *sql.DB, serverID string) (string, error) {
	var name string
	err := db.QueryRow(`SELECT name FROM servers WHERE id = ?`, serverID).Scan(&name)
	return name, err
}

// GetServerOwnerEmail returns the email of the user who owns a server. This is
// the primary alert delivery address.
func GetServerOwnerEmail(db *sql.DB, serverID string) (string, error) {
	var email string
	err := db.QueryRow(
		`SELECT users.email FROM servers
		JOIN users ON users.id = servers.user_id
		WHERE servers.id = ?`,
		serverID,
	).Scan(&email)
	return email, err
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// AlertRecord mirrors the agent's Alert JSON for persistence. JSON tags are
// snake_case to match the wire format the dashboard's Alert type expects.
type AlertRecord struct {
	ID              string `json:"id"`
	ServerID        string `json:"server_id"`
	ServerName      string `json:"server_name,omitempty"`
	Project         string `json:"project,omitempty"`
	Container       string `json:"container,omitempty"`
	Severity        string `json:"severity"`
	Level           string `json:"level,omitempty"`
	Type            string `json:"type"`
	Status          string `json:"status"`
	Title           string `json:"title,omitempty"`
	Message         string `json:"message,omitempty"`
	Detail          string `json:"detail,omitempty"`
	Action          string `json:"action,omitempty"`
	MetricsJSON     string `json:"metrics,omitempty"`
	FiredAt         string `json:"fired_at"`
	ResolvedAt      string `json:"resolved_at,omitempty"`
	ReadAt          string `json:"read_at,omitempty"`
	AcknowledgedAt  string `json:"acknowledged_at,omitempty"`
	AcknowledgedBy  string `json:"acknowledged_by,omitempty"`
	CreatedAt       string `json:"created_at"`
}

// --- Email delivery queue ---

// EmailJob is one row of the alert_emails delivery queue.
type EmailJob struct {
	ID        string
	AlertID   string
	ServerID  string
	Severity  string
	Type      string
	Project   string
	ToEmail   string
	Subject   string
	Body      string
	Status    string
	IsBatch   bool
	Attempts  int
	Error     string
	CreatedAt string
	SentAt    string
}

// InsertAlertEmail enqueues (or re-enqueues) an email for an alert condition.
// The unique (alert_id, severity, status) index means identical conditions
// update the pending job instead of creating duplicates. created_at is stored
// in RFC 3339 so the digest worker can reason about job age.
func InsertAlertEmail(db *sql.DB, j EmailJob) error {
	batch := 0
	if j.IsBatch {
		batch = 1
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`INSERT INTO alert_emails (id, alert_id, server_id, severity, type, project,
			to_email, subject, body, status, is_batch, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'queued', ?, ?)
		ON CONFLICT(alert_id, severity, status) DO UPDATE SET
			subject = excluded.subject,
			body = excluded.body,
			to_email = excluded.to_email,
			is_batch = excluded.is_batch,
			status = 'queued',
			error = NULL`,
		j.ID, j.AlertID, j.ServerID, j.Severity, j.Type, nullString(j.Project),
		j.ToEmail, j.Subject, j.Body, batch, now,
	)
	return err
}

// ListQueuedEmails returns pending email jobs. Immediate jobs (critical) are
// delivered by a fast worker; batch jobs (warning/resolved) wait for the
// hourly digest.
func ListQueuedEmails(db *sql.DB, isBatch bool) ([]EmailJob, error) {
	batch := 0
	if isBatch {
		batch = 1
	}
	rows, err := db.Query(
		`SELECT id, alert_id, server_id, severity, type, project, to_email,
			subject, body, status, is_batch, attempts, error, created_at, sent_at
		FROM alert_emails WHERE status = 'queued' AND is_batch = ?
		ORDER BY created_at ASC`,
		batch,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []EmailJob
	for rows.Next() {
		var j EmailJob
		var project, errMsg, sentAt sql.NullString
		var isBatch int
		if err := rows.Scan(&j.ID, &j.AlertID, &j.ServerID, &j.Severity, &j.Type, &project,
			&j.ToEmail, &j.Subject, &j.Body, &j.Status, &isBatch, &j.Attempts, &errMsg, &j.CreatedAt, &sentAt); err != nil {
			return nil, err
		}
		j.Project = project.String
		j.Error = errMsg.String
		j.SentAt = sentAt.String
		j.IsBatch = isBatch == 1
		out = append(out, j)
	}
	return out, rows.Err()
}

// MarkEmailSent records successful delivery of an email job.
func MarkEmailSent(db *sql.DB, id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`UPDATE alert_emails SET status = 'sent', sent_at = ?, error = NULL WHERE id = ?`,
		now, id,
	)
	return err
}

// MarkEmailFailed records a failed delivery attempt. After 3 attempts the job
// is marked failed and dropped.
func MarkEmailFailed(db *sql.DB, id, errMsg string) error {
	_, err := db.Exec(
		`UPDATE alert_emails SET attempts = attempts + 1,
			status = CASE WHEN attempts + 1 >= 3 THEN 'failed' ELSE 'queued' END,
			error = ? WHERE id = ?`,
		errMsg, id,
	)
	return err
}

// SupersedeAlertBatchEmails drops queued batch (warning/resolved) jobs for an
// alert when it escalates to critical — the immediate critical email replaces
// them, so the user is not notified twice about the same incident.
func SupersedeAlertBatchEmails(db *sql.DB, alertID string) error {
	_, err := db.Exec(
		`DELETE FROM alert_emails WHERE alert_id = ? AND status = 'queued' AND is_batch = 1`,
		alertID,
	)
	return err
}
