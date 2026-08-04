package alerts

import (
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/yourname/yourplatform/control-plane/internal/config"
	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
)

type sentEmail struct {
	to, subject, body string
}

// recordingSender implements mailer.Sender and captures deliveries.
type recordingSender struct {
	mu    sync.Mutex
	sends []sentEmail
	fail  error
}

func (r *recordingSender) Send(to, subject, body string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail != nil {
		return r.fail
	}
	r.sends = append(r.sends, sentEmail{to, subject, body})
	return nil
}

func (r *recordingSender) all() []sentEmail {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]sentEmail(nil), r.sends...)
}

func setupDeliveryDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		PRAGMA foreign_keys=ON;
		CREATE TABLE users (id TEXT PRIMARY KEY, email TEXT UNIQUE NOT NULL);
		CREATE TABLE servers (
			id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT NOT NULL
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
	`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func cfg(whStart, whEnd int, fallback string) *config.Config {
	return &config.Config{
		AlertEmailTo: fallback,
		WorkHourStart: whStart,
		WorkHourEnd:   whEnd,
	}
}

func alert(id, severity, status, project string) queries.AlertRecord {
	return alertOn("srv-1", id, severity, status, project)
}

func alertOn(serverID, id, severity, status, project string) queries.AlertRecord {
	return queries.AlertRecord{
		ID: id, ServerID: serverID, Project: project, Severity: severity,
		Type: "container_ram", Status: status, Title: "Title", Message: "Message",
	}
}

func queueCounts(t *testing.T, db *sql.DB) (immediate, batch int) {
	t.Helper()
	for _, isBatch := range []bool{false, true} {
		jobs, err := queries.ListQueuedEmails(db, isBatch)
		if err != nil {
			t.Fatal(err)
		}
		if isBatch {
			batch = len(jobs)
		} else {
			immediate = len(jobs)
		}
	}
	return
}

// seedAndHandle mirrors the production path (UpsertAlert then HandleAlert):
// the alert row must exist before its email job can reference it via FK.
// The upsert must update in place — INSERT OR REPLACE would delete the row
// and the FK ON DELETE CASCADE would wipe the alert's queued email jobs.
func seedAndHandle(t *testing.T, d *Delivery, a queries.AlertRecord) {
	t.Helper()
	upsertAlertRow(t, d, a)
	d.HandleAlert(a.ServerID, a)
}

// seedAt inserts the alert row and delivers it at a fixed clock time.
func seedAt(t *testing.T, d *Delivery, a queries.AlertRecord, now time.Time) {
	t.Helper()
	upsertAlertRow(t, d, a)
	d.handleAlertAt(a.ServerID, a, now)
}

func upsertAlertRow(t *testing.T, d *Delivery, a queries.AlertRecord) {
	t.Helper()
	if _, err := d.db.Exec(
		`INSERT INTO alerts
			(id, server_id, severity, type, status, title, message, fired_at, resolved_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
			severity = excluded.severity, type = excluded.type, status = excluded.status,
			title = excluded.title, message = excluded.message, fired_at = excluded.fired_at,
			resolved_at = excluded.resolved_at`,
		a.ID, a.ServerID, a.Severity, a.Type, a.Status, a.Title, a.Message, a.FiredAt, nullOr(a.ResolvedAt),
	); err != nil {
		t.Fatal(err)
	}
}

func nullOr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func TestDelivery_CriticalImmediate(t *testing.T) {
	db := setupDeliveryDB(t)
	defer db.Close()
	sender := &recordingSender{}
	d := NewDelivery(db, sender, cfg(9, 18, ""))

	seedAndHandle(t, d, alert("a-1", "critical", "active", "shop"))

	imm, batch := queueCounts(t, db)
	if imm != 1 || batch != 0 {
		t.Fatalf("immediate=%d batch=%d, want 1/0 for critical", imm, batch)
	}
	// The immediate worker delivers it.
	if n := d.processImmediate(); n != 1 {
		t.Fatalf("processImmediate sent=%d want 1", n)
	}
	sent := sender.all()
	if len(sent) != 1 || !strings.HasPrefix(sent[0].subject, "[YourPlatform] CRITICAL") {
		t.Fatalf("sent=%+v", sent)
	}
	if sent[0].to != "owner@example.com" {
		t.Fatalf("recipient=%q want owner email", sent[0].to)
	}
	if imm, _ = queueCounts(t, db); imm != 0 {
		t.Fatal("immediate queue should be drained after delivery")
	}
}

func TestDelivery_WarningBatches(t *testing.T) {
	db := setupDeliveryDB(t)
	defer db.Close()
	d := NewDelivery(db, &recordingSender{}, cfg(9, 18, ""))

	seedAndHandle(t, d, alert("a-1", "warning", "active", "shop"))

	imm, batch := queueCounts(t, db)
	if imm != 0 || batch != 1 {
		t.Fatalf("immediate=%d batch=%d, want 0/1 for warning", imm, batch)
	}
}

func TestDelivery_ResolvedWorkingHoursGate(t *testing.T) {
	db := setupDeliveryDB(t)
	defer db.Close()
	d := NewDelivery(db, &recordingSender{}, cfg(9, 18, ""))

	// Outside the window (fixed clock at 03:00 UTC): no job.
	seedAt(t, d, alert("a-1", "critical", "resolved", "shop"),
		time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC))
	// Inside the window (14:00 UTC): batch job.
	seedAt(t, d, alert("a-2", "critical", "resolved", "shop"),
		time.Date(2026, 1, 1, 14, 0, 0, 0, time.UTC))

	_, batch := queueCounts(t, db)
	if batch != 1 {
		t.Fatalf("batch=%d want 1 (only the within-hours resolved alert)", batch)
	}
}

func TestDelivery_NoRecipientSkips(t *testing.T) {
	db := setupDeliveryDB(t)
	defer db.Close()
	// An orphan server whose owner has no users row → no owner email.
	if _, err := db.Exec(`INSERT INTO servers (id, user_id, name) VALUES ('srv-orphan', 'ghost', 'orphan')`); err != nil {
		t.Fatal(err)
	}

	// No fallback configured → nothing enqueued.
	d := NewDelivery(db, &recordingSender{}, cfg(9, 18, ""))
	seedAndHandle(t, d, alertOn("srv-orphan", "a-1", "critical", "active", "shop"))
	imm, _ := queueCounts(t, db)
	if imm != 0 {
		t.Fatalf("critical without recipient should not enqueue, got %d", imm)
	}

	// With a configured fallback it does.
	d2 := NewDelivery(db, &recordingSender{}, cfg(9, 18, "ops@example.com"))
	seedAndHandle(t, d2, alertOn("srv-orphan", "a-2", "critical", "active", "shop"))
	imm, _ = queueCounts(t, db)
	if imm != 1 {
		t.Fatalf("fallback recipient should enqueue, got %d", imm)
	}
}

func TestDelivery_EscalationRequeuesImmediate(t *testing.T) {
	db := setupDeliveryDB(t)
	defer db.Close()
	sender := &recordingSender{}
	d := NewDelivery(db, sender, cfg(9, 18, ""))

	seedAndHandle(t, d, alert("a-1", "warning", "active", "shop"))
	imm, batch := queueCounts(t, db)
	if imm != 0 || batch != 1 {
		t.Fatalf("after warning: immediate=%d batch=%d want 0/1", imm, batch)
	}

	seedAndHandle(t, d, alert("a-1", "critical", "active", "shop")) // escalation

	// The critical email goes out immediately and supersedes the queued
	// warning digest job for the same incident.
	imm, batch = queueCounts(t, db)
	if imm != 1 || batch != 0 {
		t.Fatalf("after escalation: immediate=%d batch=%d want 1/0", imm, batch)
	}

	// Re-fire of the identical critical condition must not duplicate.
	seedAndHandle(t, d, alert("a-1", "critical", "active", "shop"))
	imm, _ = queueCounts(t, db)
	if imm != 1 {
		t.Fatalf("duplicate critical re-fire should update, got %d jobs", imm)
	}
}

func TestDelivery_HourlyDigest(t *testing.T) {
	db := setupDeliveryDB(t)
	defer db.Close()
	sender := &recordingSender{}
	d := NewDelivery(db, sender, cfg(9, 18, ""))

	// Two warnings for the same recipient.
	seedAndHandle(t, d, alert("a-1", "warning", "active", "shop"))
	seedAndHandle(t, d, alert("a-2", "warning", "active", "blog"))

	// Age them past the one-hour digest window.
	old := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	if _, err := db.Exec(`UPDATE alert_emails SET created_at = ?`, old); err != nil {
		t.Fatal(err)
	}

	d.flushDigests()

	sent := sender.all()
	if len(sent) != 1 {
		t.Fatalf("digest sends=%d want 1 (one per recipient), got %+v", len(sent), sent)
	}
	if !strings.Contains(sent[0].subject, "2 alerts need your attention") {
		t.Fatalf("digest subject=%q", sent[0].subject)
	}
	if !strings.Contains(sent[0].body, "shop") || !strings.Contains(sent[0].body, "blog") {
		t.Fatalf("digest body missing both projects: %q", sent[0].body)
	}

	// Jobs drained.
	_, batch := queueCounts(t, db)
	if batch != 0 {
		t.Fatalf("batch queue should be drained, got %d", batch)
	}
}

func TestDelivery_DigestWaitsForAge(t *testing.T) {
	db := setupDeliveryDB(t)
	defer db.Close()
	sender := &recordingSender{}
	d := NewDelivery(db, sender, cfg(9, 18, ""))

	seedAndHandle(t, d, alert("a-1", "warning", "active", "shop"))
	d.flushDigests() // job is fresh → must NOT be sent

	if len(sender.all()) != 0 {
		t.Fatal("fresh batch job must not be digested before one hour")
	}
	_, batch := queueCounts(t, db)
	if batch != 1 {
		t.Fatalf("job should remain queued, got %d", batch)
	}
}

func TestDelivery_DigestFailureMarksFailed(t *testing.T) {
	db := setupDeliveryDB(t)
	defer db.Close()
	sender := &recordingSender{fail: &smtpErr{}}
	d := NewDelivery(db, sender, cfg(9, 18, ""))

	seedAndHandle(t, d, alert("a-1", "warning", "active", "shop"))
	old := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	_, _ = db.Exec(`UPDATE alert_emails SET created_at = ?`, old)

	d.flushDigests()
	var status string
	_ = db.QueryRow(`SELECT status FROM alert_emails`).Scan(&status)
	if status != "queued" {
		t.Fatalf("after failed digest status=%q want queued (retry)", status)
	}
}

type smtpErr struct{}

func (e *smtpErr) Error() string { return "smtp refused" }

func TestInWorkingHours(t *testing.T) {
	d := NewDelivery(nil, nil, cfg(9, 18, ""))
	cases := []struct {
		hour int
		want bool
	}{
		{8, false}, {9, true}, {12, true}, {17, true}, {18, false}, {23, false}, {3, false},
	}
	for _, c := range cases {
		got := d.inWorkingHours(time.Date(2026, 1, 1, c.hour, 0, 0, 0, time.UTC))
		if got != c.want {
			t.Errorf("hour %02d: got %v want %v", c.hour, got, c.want)
		}
	}

	// Unset window (start==end) is always within.
	d2 := NewDelivery(nil, nil, cfg(0, 0, ""))
	if !d2.inWorkingHours(time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC)) {
		t.Error("unset working window should always be within hours")
	}
}
