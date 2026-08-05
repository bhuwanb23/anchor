package db

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// lifecycleDB migrates the full real schema (all 23 migrations) into an
// in-memory database with foreign keys enabled, then seeds one of everything:
// expired and fresh rows for every retention job.
func lifecycleDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON;"); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}

	_, err = db.Exec(`
		INSERT INTO users (id, email, password_hash) VALUES ('user-1', 'owner@example.com', 'hash');
		INSERT INTO teams (id, name, owner_id) VALUES ('team-1', 'Agency', 'user-1');
		INSERT INTO servers (id, user_id, name, token) VALUES ('srv-1', 'user-1', 'prod', 'tok-1');

		-- Expired + fresh registration tokens.
		INSERT INTO registration_tokens (id, token_hash, user_id, server_name, expires_at, used_at)
			VALUES ('reg-exp', 'h-exp', 'user-1', 'srv', datetime('now', '-1 hour'), NULL);
		INSERT INTO registration_tokens (id, token_hash, user_id, server_name, expires_at, used_at)
			VALUES ('reg-fresh', 'h-fresh', 'user-1', 'srv', datetime('now', '+1 day'), NULL);

		-- Expired + fresh password resets.
		INSERT INTO password_resets (id, token_hash, user_id, created_at, expires_at)
			VALUES ('pw-exp', 'p-exp', 'user-1', datetime('now'), datetime('now', '-2 hours'));
		INSERT INTO password_resets (id, token_hash, user_id, created_at, expires_at)
			VALUES ('pw-fresh', 'p-fresh', 'user-1', datetime('now'), datetime('now', '+1 day'));

		-- Expired + fresh refresh tokens.
		INSERT INTO refresh_tokens (id, token_hash, user_id, created_at, expires_at)
			VALUES ('rt-exp', 'r-exp', 'user-1', datetime('now'), datetime('now', '-2 hours'));
		INSERT INTO refresh_tokens (id, token_hash, user_id, created_at, expires_at)
			VALUES ('rt-fresh', 'r-fresh', 'user-1', datetime('now'), datetime('now', '+1 day'));

		-- Pending commands: expired (hourly), old (daily), fresh (survives both).
		INSERT INTO pending_commands (id, server_id, command_type, payload, expires_at, created_at)
			VALUES ('pc-exp', 'srv-1', 'restart', '{}', datetime('now', '-1 hour'), datetime('now'));
		INSERT INTO pending_commands (id, server_id, command_type, payload, expires_at, created_at)
			VALUES ('pc-old', 'srv-1', 'deploy', '{}', NULL, datetime('now', '-40 days'));
		INSERT INTO pending_commands (id, server_id, command_type, payload, expires_at, created_at)
			VALUES ('pc-fresh', 'srv-1', 'restart', '{}', datetime('now', '+1 day'), datetime('now'));

		-- Invitations: expired + fresh.
		INSERT INTO invitations (id, team_id, email, role, token, invited_by, expires_at)
			VALUES ('inv-exp', 'team-1', 'a@e.com', 'member', 't-exp', 'user-1', datetime('now', '-1 hour'));
		INSERT INTO invitations (id, team_id, email, role, token, invited_by, expires_at)
			VALUES ('inv-fresh', 'team-1', 'b@e.com', 'member', 't-fresh', 'user-1', datetime('now', '+1 day'));

		-- Metrics across every retention tier.
		INSERT INTO metrics_history (id, server_id, recorded_at, cpu_percent, granularity)
			VALUES ('raw-old', 'srv-1', datetime('now', '-8 days'), 1, 'raw');
		INSERT INTO metrics_history (id, server_id, recorded_at, cpu_percent, granularity)
			VALUES ('raw-fresh', 'srv-1', datetime('now', '-1 day'), 1, 'raw');
		INSERT INTO metrics_history (id, server_id, recorded_at, cpu_percent, granularity)
			VALUES ('hourly-old', 'srv-1', datetime('now', '-31 days'), 1, 'hourly');
		INSERT INTO metrics_history (id, server_id, recorded_at, cpu_percent, granularity)
			VALUES ('hourly-fresh', 'srv-1', datetime('now', '-5 days'), 1, 'hourly');
		INSERT INTO metrics_history (id, server_id, recorded_at, cpu_percent, granularity)
			VALUES ('daily-old', 'srv-1', datetime('now', '-400 days'), 1, 'daily');
		INSERT INTO metrics_history (id, server_id, recorded_at, cpu_percent, granularity)
			VALUES ('daily-fresh', 'srv-1', datetime('now', '-60 days'), 1, 'daily');

		-- Server events: old + fresh.
		INSERT INTO server_events (id, server_id, event_type, created_at)
			VALUES ('ev-old', 'srv-1', 'warning', datetime('now', '-95 days'));
		INSERT INTO server_events (id, server_id, event_type, created_at)
			VALUES ('ev-fresh', 'srv-1', 'warning', datetime('now'));

		-- Alerts + delivery emails: old + fresh.
		INSERT INTO alerts (id, server_id, severity, type, status)
			VALUES ('a-old', 'srv-1', 'critical', 'oom', 'active');
		INSERT INTO alerts (id, server_id, severity, type, status)
			VALUES ('a-fresh', 'srv-1', 'warning', 'disk', 'active');
		INSERT INTO alert_emails (id, alert_id, server_id, severity, type, to_email, subject, body, created_at)
			VALUES ('mail-old', 'a-old', 'srv-1', 'critical', 'oom', 'o@e.com', 'S', 'B', datetime('now', '-40 days'));
		INSERT INTO alert_emails (id, alert_id, server_id, severity, type, to_email, subject, body, created_at)
			VALUES ('mail-fresh', 'a-fresh', 'srv-1', 'warning', 'disk', 'o@e.com', 'S', 'B', datetime('now'));
	`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func rowCount(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func exists(t *testing.T, db *sql.DB, query string, args ...any) bool {
	return rowCount(t, db, query, args...) > 0
}

// Step 4B — the hourly pass removes expired tokens/resets/sessions/commands/
// invitations and applies the raw + hourly metrics retention, leaving fresh
// rows and the daily tier untouched.
func TestRunHourlyMaintenance(t *testing.T) {
	db := lifecycleDB(t)
	defer db.Close()

	report := RunHourlyMaintenance(db)

	// Expired tokens gone; fresh kept.
	if exists(t, db, `SELECT COUNT(*) FROM registration_tokens WHERE id = 'reg-exp'`) {
		t.Error("expired registration token survived")
	}
	if !exists(t, db, `SELECT COUNT(*) FROM registration_tokens WHERE id = 'reg-fresh'`) {
		t.Error("fresh registration token was deleted")
	}
	if exists(t, db, `SELECT COUNT(*) FROM password_resets WHERE id = 'pw-exp'`) {
		t.Error("expired password reset survived")
	}
	if exists(t, db, `SELECT COUNT(*) FROM refresh_tokens WHERE id = 'rt-exp'`) {
		t.Error("expired refresh token survived")
	}
	if exists(t, db, `SELECT COUNT(*) FROM pending_commands WHERE id = 'pc-exp'`) {
		t.Error("expired pending command survived")
	}
	// pc-old is 40 days old — that is the daily job's job, not the hourly one.
	if !exists(t, db, `SELECT COUNT(*) FROM pending_commands WHERE id = 'pc-old'`) {
		t.Error("40-day-old command should survive until the daily pass")
	}
	if exists(t, db, `SELECT COUNT(*) FROM invitations WHERE id = 'inv-exp'`) {
		t.Error("expired invitation survived")
	}

	// Metrics: raw + hourly retention applied; daily untouched.
	if exists(t, db, `SELECT COUNT(*) FROM metrics_history WHERE id = 'raw-old'`) {
		t.Error("old raw metric survived")
	}
	if exists(t, db, `SELECT COUNT(*) FROM metrics_history WHERE id = 'hourly-old'`) {
		t.Error("old hourly metric survived")
	}
	if !exists(t, db, `SELECT COUNT(*) FROM metrics_history WHERE id = 'hourly-fresh'`) {
		t.Error("fresh hourly metric was deleted")
	}
	if !exists(t, db, `SELECT COUNT(*) FROM metrics_history WHERE id = 'daily-old'`) {
		t.Error("hourly pass must not touch daily rows")
	}

	// The rollup ran and produced rows: the seeded raw-fresh row at -1 day is
	// inside the rollup window and must have been aggregated into an hourly
	// average. A broken rollup would leave this at 0 and fail the test.
	if report.Jobs["hourly_rollup"] < 1 {
		t.Fatalf("hourly_rollup=%d want >= 1", report.Jobs["hourly_rollup"])
	}
}

// Step 4B/4C — the daily pass applies the long-retention deletes, the daily
// rollup, VACUUM, and the size log.
func TestRunDailyMaintenance(t *testing.T) {
	db := lifecycleDB(t)
	defer db.Close()

	report := RunDailyMaintenance(db, "") // empty path skips the size check

	// 30-day command retention.
	if exists(t, db, `SELECT COUNT(*) FROM pending_commands WHERE id = 'pc-old'`) {
		t.Error("40-day-old command survived the daily pass")
	}
	if !exists(t, db, `SELECT COUNT(*) FROM pending_commands WHERE id = 'pc-fresh'`) {
		t.Error("fresh command was deleted")
	}
	// 90-day event retention.
	if exists(t, db, `SELECT COUNT(*) FROM server_events WHERE id = 'ev-old'`) {
		t.Error("95-day-old event survived")
	}
	if !exists(t, db, `SELECT COUNT(*) FROM server_events WHERE id = 'ev-fresh'`) {
		t.Error("fresh event was deleted")
	}
	// 30-day alert-email retention.
	if exists(t, db, `SELECT COUNT(*) FROM alert_emails WHERE id = 'mail-old'`) {
		t.Error("40-day-old alert email survived")
	}
	if !exists(t, db, `SELECT COUNT(*) FROM alert_emails WHERE id = 'mail-fresh'`) {
		t.Error("fresh alert email was deleted")
	}
	// 12-month daily-metrics retention.
	if exists(t, db, `SELECT COUNT(*) FROM metrics_history WHERE id = 'daily-old'`) {
		t.Error("400-day-old daily metric survived")
	}
	if !exists(t, db, `SELECT COUNT(*) FROM metrics_history WHERE id = 'daily-fresh'`) {
		t.Error("fresh daily metric was deleted")
	}

	if report.Jobs["old_server_events"] != 1 {
		t.Fatalf("old_server_events=%d want 1", report.Jobs["old_server_events"])
	}
	if report.Jobs["old_alert_emails"] != 1 {
		t.Fatalf("old_alert_emails=%d want 1", report.Jobs["old_alert_emails"])
	}
}
