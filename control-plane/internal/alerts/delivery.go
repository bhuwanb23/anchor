// Package alerts implements Layer 4C Step 6 — alert delivery from the
// control plane.
//
// The agent only pushes alerts over WebSocket. The control plane decides how
// they are delivered:
//
//	CRITICAL  → email immediately (worker runs every few seconds)
//	WARNING   → email batched into a single hourly digest
//	RESOLVED  → email in the next hourly digest, only within working hours
//
// All alert handling is asynchronous: enqueueing is a fast DB write and the
// actual SMTP delivery runs in background workers, so nothing here can block
// the agent's metrics collection loop.
//
// WhatsApp (post-MVP) plugs in as an additional notifier; the architecture is
// designed in docs/layers/layer4c-step6.md.
package alerts

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yourname/yourplatform/control-plane/internal/config"
	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
	"github.com/yourname/yourplatform/control-plane/internal/mailer"
)

const (
	immediateTick = 20 * time.Second // how often critical emails are retried
	digestTick    = time.Hour        // how often the warning/resolved digest is flushed
	digestAge     = time.Hour        // a batch job must be this old before it is emailed
	maxBatchAge   = 7 * 24 * time.Hour
)

// Delivery routes persisted alerts to email (and, post-MVP, other channels).
type Delivery struct {
	db         *sql.DB
	sender     mailer.Sender
	toFallback string
	whStart    int
	whEnd      int
}

// NewDelivery builds the delivery manager. When the sender is nil a
// LogSender is used so delivery is observable without SMTP configuration.
func NewDelivery(db *sql.DB, sender mailer.Sender, cfg *config.Config) *Delivery {
	if sender == nil {
		sender = &mailer.LogSender{}
	}
	d := &Delivery{db: db, sender: sender}
	if cfg != nil {
		d.toFallback = cfg.AlertEmailTo
		d.whStart = cfg.WorkHourStart
		d.whEnd = cfg.WorkHourEnd
	}
	return d
}

// Run starts the background delivery workers. It returns when ctx is done.
func (d *Delivery) Run(ctx context.Context) {
	go d.immediateLoop(ctx)
	go d.digestLoop(ctx)
	slog.Info("alert delivery started",
		"smtp", fmt.Sprintf("%T", d.sender), "working_hours", fmt.Sprintf("%02d-%02d", d.whStart, d.whEnd))
	<-ctx.Done()
}

func (d *Delivery) immediateLoop(ctx context.Context) {
	t := time.NewTicker(immediateTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n := d.processImmediate(); n > 0 {
				slog.Info("delivered immediate alert emails", "count", n)
			}
		}
	}
}

func (d *Delivery) digestLoop(ctx context.Context) {
	// Flush anything already past the digest age on startup, then hourly.
	d.flushDigests()
	t := time.NewTicker(digestTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			d.flushDigests()
		}
	}
}

// HandleAlert applies the Step 6A delivery rules for one incoming alert. It
// is a fast, non-blocking DB write; actual sending happens in the workers.
func (d *Delivery) HandleAlert(serverID string, a queries.AlertRecord) {
	d.handleAlertAt(serverID, a, time.Now())
}

// handleAlertAt is HandleAlert with an injectable clock for tests.
func (d *Delivery) handleAlertAt(serverID string, a queries.AlertRecord, now time.Time) {
	if a.Status == "acknowledged" || (a.Message == "" && a.Title == "") {
		return
	}

	to := d.recipientFor(serverID)
	if to == "" {
		slog.Debug("alert email skipped: no recipient",
			"server_id", serverID, "type", a.Type, "severity", a.Severity)
		return
	}

	subject, body := buildEmail(a)

	// Step 6A delivery rules.
	immediate := false
	switch {
	case a.Status == "resolved":
		if !d.inWorkingHours(now) {
			slog.Debug("resolved alert email deferred: outside working hours",
				"server_id", serverID, "type", a.Type)
			return
		}
	case a.Severity == "critical":
		immediate = true
	default: // warning and everything else → hourly batch
	}

	job := queries.EmailJob{
		ID:       uuid.New().String(),
		AlertID:  a.ID,
		ServerID: serverID,
		Severity: a.Severity,
		Type:     a.Type,
		Project:  a.Project,
		ToEmail:  to,
		Subject:  subject,
		Body:     body,
		IsBatch:  !immediate,
	}
	if err := queries.InsertAlertEmail(d.db, job); err != nil {
		slog.Warn("failed to enqueue alert email", "server_id", serverID, "error", err)
		return
	}
	// Escalation: the new critical email supersedes any queued warning digest
	// job for the same alert, so the user is not notified twice.
	if immediate {
		_ = queries.SupersedeAlertBatchEmails(d.db, a.ID)
	}
	slog.Debug("alert email enqueued", "server_id", serverID, "type", a.Type,
		"severity", a.Severity, "batch", !immediate)
}

// recipientFor resolves the delivery address: the server owner's account
// email, falling back to the configured ALERT_EMAIL_TO override.
func (d *Delivery) recipientFor(serverID string) string {
	if email, err := queries.GetServerOwnerEmail(d.db, serverID); err == nil && email != "" {
		return email
	}
	return d.toFallback
}

// processImmediate sends every queued critical (non-batch) email. Returns the
// number of successful deliveries.
func (d *Delivery) processImmediate() int {
	jobs, err := queries.ListQueuedEmails(d.db, false)
	if err != nil {
		slog.Warn("list immediate alert emails", "error", err)
		return 0
	}
	sent := 0
	for _, j := range jobs {
		if d.deliver(j) {
			sent++
		}
	}
	return sent
}

// flushDigests sends one digest email per recipient covering queued
// warning/resolved jobs that have aged past digestAge, then marks them sent.
func (d *Delivery) flushDigests() {
	jobs, err := queries.ListQueuedEmails(d.db, true)
	if err != nil {
		slog.Warn("list batch alert emails", "error", err)
		return
	}
	now := time.Now()
	cutoff := now.Add(-digestAge)
	oldest := now.Add(-maxBatchAge)

	byRecipient := map[string][]queries.EmailJob{}
	for _, j := range jobs {
		created := parseJobTime(j.CreatedAt, now)
		// Drop anything stuck in the queue for over a week.
		if created.Before(oldest) {
			_ = queries.MarkEmailSent(d.db, j.ID)
			continue
		}
		if created.After(cutoff) {
			continue // not old enough for this digest yet
		}
		byRecipient[j.ToEmail] = append(byRecipient[j.ToEmail], j)
	}

	for to, group := range byRecipient {
		subject := fmt.Sprintf("[YourPlatform] %d alert%s need your attention",
			len(group), plural(len(group)))
		body := buildDigest(group)
		if err := d.sender.Send(to, subject, body); err != nil {
			slog.Warn("failed to send alert digest", "to", to, "count", len(group), "error", err)
			for _, j := range group {
				_ = queries.MarkEmailFailed(d.db, j.ID, err.Error())
			}
			continue
		}
		slog.Info("sent alert digest", "to", to, "count", len(group))
		for _, j := range group {
			_ = queries.MarkEmailSent(d.db, j.ID)
		}
	}
}

// deliver sends one email job and records the outcome.
func (d *Delivery) deliver(j queries.EmailJob) bool {
	if err := d.sender.Send(j.ToEmail, j.Subject, j.Body); err != nil {
		slog.Warn("failed to send alert email", "to", j.ToEmail, "type", j.Type, "error", err)
		_ = queries.MarkEmailFailed(d.db, j.ID, err.Error())
		return false
	}
	_ = queries.MarkEmailSent(d.db, j.ID)
	return true
}

// inWorkingHours reports whether the local time is within the configured
// working window (default 09:00–18:00). Resolved-alert emails only go out
// inside this window.
func (d *Delivery) inWorkingHours(t time.Time) bool {
	h := t.Hour()
	start, end := d.whStart, d.whEnd
	if start == end { // unset window → always within hours
		return true
	}
	if start > end {
		return h >= start || h < end
	}
	return h >= start && h < end
}

// parseJobTime parses a queued job's created_at, tolerating both RFC 3339
// (written explicitly) and SQLite's datetime('now') format (legacy rows).
func parseJobTime(s string, fallback time.Time) time.Time {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	if t, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
		return t
	}
	return fallback
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// buildEmail renders the plain-text subject/body for a single alert.
func buildEmail(a queries.AlertRecord) (subject, body string) {
	severity := strings.ToUpper(a.Severity)
	subject = fmt.Sprintf("[YourPlatform] %s: %s", severity, a.Title)
	var b strings.Builder
	b.WriteString(a.Title + "\n\n")
	if a.Message != "" && a.Message != a.Title {
		b.WriteString(a.Message + "\n\n")
	}
	if a.Detail != "" {
		b.WriteString("Details: " + a.Detail + "\n\n")
	}
	if a.Action != "" {
		b.WriteString("What you can do:\n" + a.Action + "\n\n")
	}
	if a.Status == "resolved" {
		b.WriteString("Status: resolved — no action needed.\n\n")
	}
	if a.Project != "" {
		b.WriteString("Project: " + a.Project + "\n")
	}
	if a.FiredAt != "" {
		b.WriteString("Time: " + a.FiredAt + "\n")
	}
	b.WriteString("Server: " + a.ServerID + "\n")
	return subject, b.String()
}

// buildDigest renders one hourly summary email covering multiple warnings.
func buildDigest(group []queries.EmailJob) string {
	var b strings.Builder
	b.WriteString("YourPlatform detected the following issues in the last hour:\n\n")
	for _, j := range group {
		prefix := "•"
		if j.Severity == "resolved" {
			prefix = "✓"
		}
		b.WriteString(fmt.Sprintf("%s [%s] %s\n", prefix, j.Severity, j.Subject))
		if j.Project != "" {
			b.WriteString("    Project: " + j.Project + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("Open the dashboard to review and acknowledge these alerts.\n")
	return b.String()
}
