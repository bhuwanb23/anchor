package queries

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func setupSnapshotDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE servers (
			id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'disconnected',
			ip_address TEXT NOT NULL DEFAULT '', token TEXT
		);
		CREATE TABLE metrics_history (
			id TEXT PRIMARY KEY, server_id TEXT NOT NULL, recorded_at TEXT NOT NULL, collected_in_ms INTEGER,
			cpu_percent REAL, ram_used_mb INTEGER, ram_total_mb INTEGER, ram_percent REAL,
			disk_used_gb REAL, disk_total_gb REAL, disk_percent REAL, load_1min REAL, load_per_core REAL,
			caddy_running INTEGER NOT NULL DEFAULT 0, caddy_routes_count INTEGER NOT NULL DEFAULT 0,
			last_backup_age_sec INTEGER, container_count INTEGER NOT NULL DEFAULT 0,
			granularity TEXT NOT NULL DEFAULT 'raw'
		);
		INSERT INTO servers (id, user_id, name, status) VALUES ('srv-1', 'user-1', 'prod', 'connected');
		INSERT INTO metrics_history (id, server_id, recorded_at, cpu_percent, ram_percent, disk_percent, ram_used_mb, ram_total_mb, caddy_running, caddy_routes_count, container_count)
			VALUES ('m-1', 'srv-1', '2026-08-05T00:00:00Z', 12.5, 33.0, 40.0, 2048, 8192, 1, 3, 1);
		INSERT INTO metrics_history (id, server_id, recorded_at, cpu_percent)
			VALUES ('m-2', 'srv-1', '2026-08-05T00:01:00Z', 9.0);
		-- A rolled-up hourly row with a NEWER-looking recorded_at must never
		-- win: GetLatestMetric only reads raw samples (Layer 5C Step 4A).
		INSERT INTO metrics_history (id, server_id, recorded_at, cpu_percent, granularity)
			VALUES ('m-hourly', 'srv-1', '2026-08-05T01:00:00Z', 99.0, 'hourly');
	`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestGetServerStatus(t *testing.T) {
	db := setupSnapshotDB(t)

	status, err := GetServerStatus(db, "srv-1")
	if err != nil {
		t.Fatalf("GetServerStatus: %v", err)
	}
	if status != "connected" {
		t.Fatalf("status = %q, want connected", status)
	}

	if _, err := GetServerStatus(db, "srv-unknown"); err == nil {
		t.Fatal("expected error for unknown server")
	}
}

func TestGetLatestMetric(t *testing.T) {
	db := setupSnapshotDB(t)

	m, err := GetLatestMetric(db, "srv-1")
	if err != nil {
		t.Fatalf("GetLatestMetric: %v", err)
	}
	if m == nil {
		t.Fatal("expected a metric row")
	}
	// Newest RAW sample wins — the hourly rollup row with a later recorded_at
	// must be ignored (it is an average, not a live sample).
	if m.RecordedAt != "2026-08-05T00:01:00Z" {
		t.Fatalf("recorded_at = %q, want the newest raw sample", m.RecordedAt)
	}
	if m.CPUPercent == nil || *m.CPUPercent != 9.0 {
		t.Fatalf("cpu_percent = %v, want 9.0", m.CPUPercent)
	}
	// Fields absent from the newest (partial) sample stay nil.
	if m.RAMUsedMB != nil {
		t.Fatalf("ram_used_mb should be nil for partial row, got %v", *m.RAMUsedMB)
	}
	// NOT NULL DEFAULT 0 columns resolve to 0/false for a partial row.
	if m.CaddyRunning == nil || *m.CaddyRunning {
		t.Fatalf("caddy_running should be false for the partial row, got %v", m.CaddyRunning)
	}
	if m.CaddyRoutesCount == nil || *m.CaddyRoutesCount != 0 {
		t.Fatalf("caddy_routes_count = %v, want 0 (default)", m.CaddyRoutesCount)
	}

	missing, err := GetLatestMetric(db, "srv-nope")
	if err == nil {
		t.Fatal("expected error for server without metrics")
	}
	_ = missing
}
