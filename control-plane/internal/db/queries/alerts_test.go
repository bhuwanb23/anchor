package queries

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupAlertsDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		PRAGMA foreign_keys=ON;
		CREATE TABLE users (id TEXT PRIMARY KEY, email TEXT UNIQUE NOT NULL);
		CREATE TABLE servers (
			id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT NOT NULL,
			FOREIGN KEY (user_id) REFERENCES users(id)
		);
		CREATE TABLE alerts (
			id TEXT PRIMARY KEY, server_id TEXT NOT NULL, project TEXT, container TEXT,
			severity TEXT NOT NULL, type TEXT NOT NULL, status TEXT NOT NULL,
			title TEXT, message TEXT, detail TEXT, action TEXT, metrics TEXT,
			fired_at TEXT, resolved_at TEXT,
			read_at TEXT, acknowledged_at TEXT, acknowledged_by TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now')),
			FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE CASCADE
		);
		CREATE TABLE alert_emails (
			id TEXT PRIMARY KEY, alert_id TEXT NOT NULL, server_id TEXT NOT NULL,
			severity TEXT NOT NULL, type TEXT NOT NULL, project TEXT, to_email TEXT NOT NULL,
			subject TEXT NOT NULL, body TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'queued',
			is_batch INTEGER NOT NULL DEFAULT 0, attempts INTEGER NOT NULL DEFAULT 0,
			error TEXT, created_at TEXT, sent_at TEXT,
			FOREIGN KEY (alert_id) REFERENCES alerts(id) ON DELETE CASCADE,
			FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE CASCADE
		);
		CREATE UNIQUE INDEX idx_alert_emails_dedup ON alert_emails(alert_id, severity, status);
		INSERT INTO users (id, email) VALUES ('user-1', 'owner@example.com');
		INSERT INTO servers (id, user_id, name) VALUES ('srv-1', 'user-1', 'prod');
		INSERT INTO servers (id, user_id, name) VALUES ('srv-2', 'user-1', 'staging');
		INSERT INTO alerts (id, server_id, severity, type, status, title, message, fired_at)
			VALUES ('a-1', 'srv-1', 'critical', 'container_oom', 'active', 'OOM', 'ran out', '2026-01-01T10:00:00Z');
		INSERT INTO alerts (id, server_id, severity, type, status, title, message, fired_at)
			VALUES ('a-2', 'srv-1', 'warning', 'disk', 'active', 'Disk', 'full', '2026-01-01T09:00:00Z');
		INSERT INTO alerts (id, server_id, severity, type, status, title, message, fired_at, resolved_at)
			VALUES ('a-3', 'srv-2', 'critical', 'caddy_down', 'resolved', 'Caddy', 'down', '2026-01-01T08:00:00Z', '2026-01-01T09:00:00Z');
	`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func seedAlert(t *testing.T, db *sql.DB, id, serverID, severity, typ, status string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO alerts (id, server_id, severity, type, status, title, message, fired_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, serverID, severity, typ, status, "Title", "Message", "2026-01-02T00:00:00Z",
	); err != nil {
		t.Fatal(err)
	}
}

func TestAlertAckReadLifecycle(t *testing.T) {
	db := setupAlertsDB(t)
	defer db.Close()

	// Unread count across both servers: a-1, a-2 active & unread; a-3 resolved.
	n, err := UnreadAlertCountForUser(db, "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("unread=%d want 2", n)
	}

	// Acknowledge a-1.
	if err := AcknowledgeAlert(db, "a-1", "srv-1", "user-1"); err != nil {
		t.Fatal(err)
	}
	alerts, err := ListAlertsByServer(db, "srv-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 2 {
		t.Fatalf("len=%d want 2", len(alerts))
	}
	for _, a := range alerts {
		if a.ID == "a-1" {
			if a.Status != "acknowledged" || a.AcknowledgedBy != "user-1" || a.AcknowledgedAt == "" {
				t.Fatalf("a-1 not acknowledged: %+v", a)
			}
		}
	}

	// Acknowledged alerts are no longer unread.
	n, _ = UnreadAlertCountForUser(db, "user-1")
	if n != 1 {
		t.Fatalf("unread after ack=%d want 1", n)
	}

	// Mark all read.
	if err := MarkAlertsReadForUser(db, "user-1"); err != nil {
		t.Fatal(err)
	}
	n, _ = UnreadAlertCountForUser(db, "user-1")
	if n != 0 {
		t.Fatalf("unread after read=%d want 0", n)
	}

	// Ack of a resolved alert is a no-op (still resolved).
	if err := AcknowledgeAlert(db, "a-3", "srv-2", "user-1"); err != nil {
		t.Fatal(err)
	}
	all, err := ListRecentAlertsForUser(db, "user-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("recent=%d want 3", len(all))
	}
	if all[0].ServerName == "" {
		t.Fatal("expected server_name attached for global list")
	}
	for _, a := range all {
		if a.ID == "a-3" && a.Status != "resolved" {
			t.Fatalf("a-3 status changed to %q, want resolved", a.Status)
		}
	}
}

// Step 3C #3: active-only alerts for a server, newest first.
func TestListActiveAlertsByServer(t *testing.T) {
	db := setupAlertsDB(t)
	defer db.Close()

	alerts, err := ListActiveAlertsByServer(db, "srv-1")
	if err != nil {
		t.Fatal(err)
	}
	// a-1 and a-2 are active; a-3 (resolved) is on a different server anyway.
	if len(alerts) != 2 {
		t.Fatalf("len=%d want 2", len(alerts))
	}
	// Newest first by fired_at.
	if alerts[0].ID != "a-1" || alerts[1].ID != "a-2" {
		t.Fatalf("order wrong: %s, %s", alerts[0].ID, alerts[1].ID)
	}
	for _, a := range alerts {
		if a.Status != "active" {
			t.Fatalf("got status %q, want active only", a.Status)
		}
	}

	// Server with no active alerts: empty slice, not nil.
	if _, err := db.Exec(`UPDATE alerts SET status = 'resolved', resolved_at = '2026-01-01T11:00:00Z' WHERE server_id = 'srv-1'`); err != nil {
		t.Fatal(err)
	}
	gone, err := ListActiveAlertsByServer(db, "srv-1")
	if err != nil {
		t.Fatal(err)
	}
	if gone == nil || len(gone) != 0 {
		t.Fatalf("expected empty slice, got %v", gone)
	}
}

func TestGetServerOwnerEmail(t *testing.T) {
	db := setupAlertsDB(t)
	defer db.Close()
	email, err := GetServerOwnerEmail(db, "srv-1")
	if err != nil || email != "owner@example.com" {
		t.Fatalf("email=%q err=%v", email, err)
	}
}

func TestEmailQueue_DedupAndEscalation(t *testing.T) {
	db := setupAlertsDB(t)
	defer db.Close()
	seedAlert(t, db, "a-x", "srv-1", "warning", "container_ram", "active")

	job := func(id, severity, status string, isBatch bool) EmailJob {
		return EmailJob{
			ID: id, AlertID: "a-x", ServerID: "srv-1", Severity: severity,
			Type: "container_ram", Project: "shop", ToEmail: "owner@example.com",
			Subject: "S", Body: "B", Status: "queued", IsBatch: isBatch,
		}
	}

	// First warning enqueues as a batch job.
	if err := InsertAlertEmail(db, job("e-1", "warning", "active", true)); err != nil {
		t.Fatal(err)
	}
	// Same condition again — should update e-1, not insert.
	if err := InsertAlertEmail(db, job("e-2", "warning", "active", true)); err != nil {
		t.Fatal(err)
	}
	q, err := ListQueuedEmails(db, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(q) != 1 || q[0].ID != "e-1" {
		t.Fatalf("batch jobs=%+v, want single updated e-1", q)
	}

	// Escalation to critical → severity changes → fresh immediate job.
	if err := InsertAlertEmail(db, job("e-3", "critical", "active", false)); err != nil {
		t.Fatal(err)
	}
	imm, err := ListQueuedEmails(db, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(imm) != 1 || imm[0].Severity != "critical" {
		t.Fatalf("immediate jobs=%+v", imm)
	}

	// Resolution → status changes → new batch job.
	if err := InsertAlertEmail(db, job("e-4", "critical", "resolved", true)); err != nil {
		t.Fatal(err)
	}
	all, _ := ListQueuedEmails(db, true)
	if len(all) != 2 {
		t.Fatalf("batch jobs after resolve=%d want 2", len(all))
	}

	// Sent + failed transitions.
	if err := MarkEmailSent(db, "e-1"); err != nil {
		t.Fatal(err)
	}
	if err := MarkEmailFailed(db, "e-3", "boom"); err != nil {
		t.Fatal(err)
	}
	var status, errMsg string
	if err := db.QueryRow(`SELECT status, error FROM alert_emails WHERE id='e-3'`).Scan(&status, &errMsg); err != nil {
		t.Fatal(err)
	}
	if status != "queued" || errMsg != "boom" {
		t.Fatalf("after 1 failure status=%q err=%q, want queued", status, errMsg)
	}
	// Two more failures → failed.
	_ = MarkEmailFailed(db, "e-3", "boom")
	_ = MarkEmailFailed(db, "e-3", "boom")
	if err := db.QueryRow(`SELECT status FROM alert_emails WHERE id='e-3'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "failed" {
		t.Fatalf("after 3 failures status=%q want failed", status)
	}

	// Sanity: created_at stored as RFC 3339 for the digest worker.
	var createdAt string
	_ = db.QueryRow(`SELECT created_at FROM alert_emails WHERE id='e-1'`).Scan(&createdAt)
	if _, err := time.Parse(time.RFC3339, createdAt); err != nil {
		t.Fatalf("created_at %q not RFC3339: %v", createdAt, err)
	}
}

func TestSupersedeAlertBatchEmails(t *testing.T) {
	db := setupAlertsDB(t)
	defer db.Close()
	seedAlert(t, db, "a-s", "srv-1", "critical", "container_oom", "active")

	if err := InsertAlertEmail(db, EmailJob{
		ID: "s-1", AlertID: "a-s", ServerID: "srv-1", Severity: "warning",
		Type: "container_oom", ToEmail: "o@e.com", Subject: "S", Body: "B", IsBatch: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := InsertAlertEmail(db, EmailJob{
		ID: "s-2", AlertID: "a-s", ServerID: "srv-1", Severity: "critical",
		Type: "container_oom", ToEmail: "o@e.com", Subject: "S", Body: "B",
	}); err != nil {
		t.Fatal(err)
	}

	if err := SupersedeAlertBatchEmails(db, "a-s"); err != nil {
		t.Fatal(err)
	}
	// Batch (warning) job dropped; immediate (critical) job remains.
	imm, _ := ListQueuedEmails(db, false)
	batch, _ := ListQueuedEmails(db, true)
	if len(imm) != 1 || len(batch) != 0 {
		t.Fatalf("after supersede: immediate=%d batch=%d want 1/0", len(imm), len(batch))
	}
}

// The API serializes AlertRecord directly — its JSON must be snake_case to
// match the dashboard's Alert type (regression guard for the Step 6 wire).
func TestAlertRecord_JSONWireShape(t *testing.T) {
	r := AlertRecord{
		ID: "a", ServerID: "s", ServerName: "prod", Severity: "critical",
		Level: "critical", Type: "oom", Status: "active", Title: "T",
		Message: "M", FiredAt: "2026-01-01T00:00:00Z",
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"id", "server_id", "server_name", "severity", "level", "type", "status", "title", "message", "fired_at"} {
		if _, ok := m[k]; !ok {
			t.Errorf("AlertRecord JSON missing snake_case key %q: %s", k, b)
		}
	}
	if _, ok := m["ID"]; ok {
		t.Errorf("AlertRecord JSON leaked PascalCase key: %s", b)
	}
}
