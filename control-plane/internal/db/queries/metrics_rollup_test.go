package queries

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// setupMetricsRollupDB mirrors the metrics_history schema from migrations
// 014 + 023, including the partial unique index that makes rollups
// idempotent. recorded_at values are seeded with SQLite datetime('now', ...)
// so string comparisons against datetime('now', ...) in the queries are
// consistent.
func setupMetricsRollupDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE servers (id TEXT PRIMARY KEY, name TEXT NOT NULL);
		CREATE TABLE metrics_history (
			id TEXT PRIMARY KEY,
			server_id TEXT NOT NULL,
			recorded_at TEXT NOT NULL,
			cpu_percent REAL,
			ram_used_mb INTEGER,
			ram_total_mb INTEGER,
			disk_used_gb REAL,
			disk_total_gb REAL,
			load_1min REAL,
			granularity TEXT NOT NULL DEFAULT 'raw',
			FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE CASCADE
		);
		CREATE UNIQUE INDEX idx_metrics_history_rollup
			ON metrics_history(server_id, recorded_at) WHERE granularity != 'raw';
		INSERT INTO servers (id, name) VALUES ('srv-1', 'prod'), ('srv-2', 'staging');
	`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func seedRawMetric(t *testing.T, db *sql.DB, id, serverID string, cpu float64) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO metrics_history (id, server_id, recorded_at, cpu_percent, granularity)
		 VALUES (?, ?, datetime('now', '-2 hours'), ?, 'raw')`,
		id, serverID, cpu,
	); err != nil {
		t.Fatal(err)
	}
}

// Step 4A — raw metrics aggregate into hourly averages, and re-running the
// rollup for the same bucket inserts nothing (idempotent via the partial
// unique index).
func TestRollupHourlyMetrics_AveragesAndIdempotent(t *testing.T) {
	db := setupMetricsRollupDB(t)
	defer db.Close()

	seedRawMetric(t, db, "m1", "srv-1", 10)
	seedRawMetric(t, db, "m2", "srv-1", 30) // avg 20
	seedRawMetric(t, db, "m3", "srv-2", 100)

	n, err := RollupHourlyMetrics(db)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("inserted=%d want 2 hourly rows", n)
	}

	// srv-1's hourly average is the mean of 10 and 30.
	var avg float64
	err = db.QueryRow(
		`SELECT cpu_percent FROM metrics_history
		 WHERE granularity = 'hourly' AND server_id = 'srv-1'
		   AND recorded_at = strftime('%Y-%m-%dT%H:00:00Z', datetime('now', '-2 hours'))`,
	).Scan(&avg)
	if err != nil {
		t.Fatal(err)
	}
	if avg != 20 {
		t.Fatalf("hourly avg=%v want 20", avg)
	}

	// Second run must not duplicate rows.
	if _, err := RollupHourlyMetrics(db); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM metrics_history WHERE granularity = 'hourly'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("hourly rows after rerun=%d want 2", count)
	}
}

// Step 4A — hourly averages roll up into daily averages (date-only bucket).
func TestRollupDailyMetrics_FromHourly(t *testing.T) {
	db := setupMetricsRollupDB(t)
	defer db.Close()

	// Two hourly buckets on the same day, one on another day.
	for i, cpu := range []float64{10, 30} {
		if _, err := db.Exec(
			`INSERT INTO metrics_history (id, server_id, recorded_at, cpu_percent, granularity)
			 VALUES (?, 'srv-1', strftime('%Y-%m-%dT%H:00:00Z', datetime('now', '-2 days', '-2 hours')), ?, 'hourly')`,
			"h1-"+string(rune('a'+i)), cpu,
		); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(
		`INSERT INTO metrics_history (id, server_id, recorded_at, cpu_percent, granularity)
		 VALUES ('h3', 'srv-1', strftime('%Y-%m-%dT%H:00:00Z', datetime('now', '-2 days', '-5 hours')), 50, 'hourly')`,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO metrics_history (id, server_id, recorded_at, cpu_percent, granularity)
		 VALUES ('h4', 'srv-2', strftime('%Y-%m-%dT%H:00:00Z', datetime('now', '-3 days', '-1 hours')), 80, 'hourly')`,
	); err != nil {
		t.Fatal(err)
	}

	n, err := RollupDailyMetrics(db)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("inserted=%d want 2 daily rows", n)
	}

	// srv-1's day bucket averages 10, 30 and 50 → 30.
	var avg float64
	err = db.QueryRow(
		`SELECT cpu_percent FROM metrics_history
		 WHERE granularity = 'daily' AND server_id = 'srv-1'
		   AND recorded_at = strftime('%Y-%m-%d', datetime('now', '-2 days'))`,
	).Scan(&avg)
	if err != nil {
		t.Fatal(err)
	}
	if avg != 30 {
		t.Fatalf("daily avg=%v want 30", avg)
	}

	// Idempotent on rerun.
	if _, err := RollupDailyMetrics(db); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM metrics_history WHERE granularity = 'daily'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("daily rows after rerun=%d want 2", count)
	}
}

// Step 4A — retention tiers: raw 7 days, hourly 30 days, daily 12 months.
// Each delete only touches its own granularity.
func TestMetricsRetentionTiers(t *testing.T) {
	db := setupMetricsRollupDB(t)
	defer db.Close()

	seed := func(id string, days int, gran string) {
		t.Helper()
		if _, err := db.Exec(
			`INSERT INTO metrics_history (id, server_id, recorded_at, cpu_percent, granularity)
			 VALUES (?, 'srv-1', datetime('now', ?), 1, ?)`,
			id, sqlIntDays(days), gran,
		); err != nil {
			t.Fatal(err)
		}
	}
	seed("raw-old", -8, "raw")
	seed("raw-fresh", -1, "raw")
	seed("hourly-old", -31, "hourly")
	seed("hourly-fresh", -5, "hourly")
	seed("daily-old", -400, "daily")
	seed("daily-fresh", -60, "daily")

	if n, err := DeleteOldRawMetrics(db); err != nil {
		t.Fatal(err)
	} else if n != 1 {
		t.Fatalf("raw deleted=%d want 1", n)
	}
	if n, err := DeleteOldHourlyMetrics(db); err != nil {
		t.Fatal(err)
	} else if n != 1 {
		t.Fatalf("hourly deleted=%d want 1", n)
	}
	if n, err := DeleteOldDailyMetrics(db); err != nil {
		t.Fatal(err)
	} else if n != 1 {
		t.Fatalf("daily deleted=%d want 1", n)
	}

	var remaining int
	if err := db.QueryRow(`SELECT COUNT(*) FROM metrics_history`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 3 {
		t.Fatalf("remaining=%d want 3 (one per tier)", remaining)
	}
}

// sqlIntDays converts a negative day count to a SQLite modifier string
// ("-8 days") for datetime('now', ?).
func sqlIntDays(days int) string {
	if days < 0 {
		return "-" + string(rune('0'+(-days))) + " days"
	}
	return "+" + string(rune('0'+days)) + " days"
}
