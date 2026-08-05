package db

import (
	"database/sql"
	"log/slog"
	"os"
	"time"

	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
)

// MaintenanceReport summarizes one cleanup run so callers (and tests) can
// assert exactly what was deleted. Keys are job names; only jobs that
// deleted rows appear (a job that found nothing is a successful no-op).
type MaintenanceReport struct {
	Jobs map[string]int64
}

// databaseSizeWarningBytes is the soft limit above which the daily size
// check logs a warning instead of an info line (Layer 5C Step 4C).
const databaseSizeWarningBytes int64 = 500 * 1024 * 1024 // 500 MB

// runJob executes one cleanup job, records its deleted-row count, and logs.
// A failing job is logged as a warning and never aborts the run — the
// control plane must keep going even if one cleanup query breaks.
func runJob(report *MaintenanceReport, scope, name string, fn func() (int64, error)) {
	n, err := fn()
	if err != nil {
		slog.Warn("cleanup job failed", "scope", scope, "job", name, "error", err)
		return
	}
	report.Jobs[name] = n
	if n > 0 {
		slog.Info("cleanup", "scope", scope, "job", name, "deleted", n)
	}
}

// RunHourlyMaintenance executes every hourly cleanup job (Layer 5C Step 4B):
//
//  1. expired registration tokens,
//  2. expired password resets,
//  3. expired refresh tokens,
//  4. expired pending commands,
//  5. expired team invitations,
//  6. metrics rollup: raw → hourly averages, then raw >7d and hourly >30d
//     retention deletes.
//
// Each job's failure is logged and swallowed; the run always completes.
func RunHourlyMaintenance(db *sql.DB) MaintenanceReport {
	report := MaintenanceReport{Jobs: map[string]int64{}}
	cutoff := time.Now().UTC().Format(time.RFC3339)

	runJob(&report, "hourly", "expired_registration_tokens", func() (int64, error) {
		return queries.DeleteExpiredRegistrationTokens(db)
	})
	runJob(&report, "hourly", "expired_password_resets", func() (int64, error) {
		return queries.DeleteExpiredPasswordResets(db, cutoff)
	})
	runJob(&report, "hourly", "expired_refresh_tokens", func() (int64, error) {
		return queries.DeleteExpiredRefreshTokens(db, cutoff)
	})
	runJob(&report, "hourly", "expired_pending_commands", func() (int64, error) {
		return queries.DeleteExpiredPendingCommands(db)
	})
	runJob(&report, "hourly", "expired_invitations", func() (int64, error) {
		return queries.DeleteExpiredInvitations(db)
	})
	runJob(&report, "hourly", "hourly_rollup", queries.RollupHourlyMetrics)
	runJob(&report, "hourly", "old_raw_metrics", func() (int64, error) {
		return queries.DeleteOldRawMetrics(db)
	})
	runJob(&report, "hourly", "old_hourly_metrics", func() (int64, error) {
		return queries.DeleteOldHourlyMetrics(db)
	})

	return report
}

// RunDailyMaintenance executes every daily cleanup job (Layer 5C Step 4B):
//
//  1. daily rollup: hourly → daily averages, then daily >12 months retention,
//  2. pending commands older than 30 days,
//  3. server events older than 90 days,
//  4. alert delivery rows older than 30 days,
//  5. VACUUM (reclaims space after the deletions),
//  6. database file-size monitoring (Step 4C).
//
// dbPath is the SQLite file path used for the size check; it may be empty
// (in-memory / test databases skip the check).
func RunDailyMaintenance(db *sql.DB, dbPath string) MaintenanceReport {
	report := MaintenanceReport{Jobs: map[string]int64{}}

	runJob(&report, "daily", "daily_rollup", queries.RollupDailyMetrics)
	runJob(&report, "daily", "old_daily_metrics", func() (int64, error) {
		return queries.DeleteOldDailyMetrics(db)
	})
	runJob(&report, "daily", "old_pending_commands", func() (int64, error) {
		return queries.DeleteOldPendingCommands(db, 30)
	})
	runJob(&report, "daily", "old_server_events", func() (int64, error) {
		return queries.DeleteOldEvents(db, 90)
	})
	runJob(&report, "daily", "old_alert_emails", func() (int64, error) {
		return queries.DeleteOldAlertEmails(db, 30)
	})

	if err := vacuumReclaimsSpace(db); err != nil {
		slog.Warn("vacuum failed", "error", err)
	}
	logDatabaseSize(dbPath)

	return report
}

// StartCleanup launches the background data-lifecycle goroutine (Layer 5C
// Step 4). It wakes every hour to run the hourly jobs and, when the tick
// lands in the UTC midnight hour, runs the daily jobs. A maintenance pass
// also runs ~30s after startup so jobs missed while the process was down are
// caught up. Job failures never stop the loop.
func StartCleanup(db *sql.DB, dbPath string) {
	go func() {
		// Catch-up pass shortly after boot.
		time.Sleep(30 * time.Second)
		RunHourlyMaintenance(db)
		if time.Now().UTC().Hour() == 0 {
			RunDailyMaintenance(db, dbPath)
		}

		hourly := time.NewTicker(time.Hour)
		defer hourly.Stop()

		// lastDaily tracks the last UTC midnight a daily run was performed
		// for, so each midnight triggers exactly one daily run even if the
		// ticker drifts.
		lastDaily := time.Now().UTC().Truncate(24 * time.Hour)
		for range hourly.C {
			RunHourlyMaintenance(db)
			now := time.Now().UTC()
			midnight := now.Truncate(24 * time.Hour)
			if now.Hour() == 0 && midnight.After(lastDaily) {
				RunDailyMaintenance(db, dbPath)
				lastDaily = midnight
			}
		}
	}()
}

// vacuumReclaimsSpace runs an incremental VACUUM when the database was opened
// with auto_vacuum=INCREMENTAL (reclaims 1000 pages without a long lock);
// otherwise it falls back to a full VACUUM, because incremental_vacuum is a
// silent no-op on databases created without auto_vacuum enabled.
func vacuumReclaimsSpace(db *sql.DB) error {
	var mode int
	if err := db.QueryRow("PRAGMA auto_vacuum").Scan(&mode); err != nil {
		return err
	}
	if mode == 2 { // INCREMENTAL
		_, err := db.Exec("PRAGMA incremental_vacuum(1000)")
		return err
	}
	_, err := db.Exec("VACUUM")
	return err
}

// logDatabaseSize stats the SQLite file and logs its size, warning when it
// exceeds the soft limit (Layer 5C Step 4C). An empty path (in-memory DB) is
// skipped quietly.
func logDatabaseSize(path string) {
	if path == "" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		slog.Warn("database size check failed", "error", err)
		return
	}
	sizeMB := info.Size() / (1024 * 1024)
	if info.Size() > databaseSizeWarningBytes {
		slog.Warn("database size above soft limit",
			"size_mb", sizeMB,
			"limit_mb", databaseSizeWarningBytes/(1024*1024))
		return
	}
	slog.Info("database size", "size_mb", sizeMB)
}
