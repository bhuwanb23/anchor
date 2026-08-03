package ws

import (
	"database/sql"
	"testing"

	"github.com/yourname/yourplatform/control-plane/internal/db/queries"

	_ "modernc.org/sqlite"
)

func setupStorageTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE servers (
			id TEXT PRIMARY KEY,
			name TEXT
		);
		INSERT INTO servers (id, name) VALUES ('srv-1', 'test');
		CREATE TABLE backup_configs (
			id TEXT PRIMARY KEY,
			server_id TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			schedule TEXT NOT NULL DEFAULT '0 2 * * *',
			retention_daily INTEGER NOT NULL DEFAULT 7,
			retention_weekly INTEGER NOT NULL DEFAULT 4,
			retention_monthly INTEGER NOT NULL DEFAULT 3,
			storage_limit_bytes INTEGER NOT NULL DEFAULT 1073741824,
			repository_size_bytes INTEGER,
			storage_alert_level TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		INSERT INTO backup_configs (id, server_id, storage_limit_bytes) VALUES ('cfg-1', 'srv-1', 1000);
		CREATE TABLE backup_snapshots (
			id TEXT PRIMARY KEY,
			server_id TEXT NOT NULL,
			snapshot_id TEXT NOT NULL,
			paths TEXT NOT NULL,
			size_bytes INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE TABLE backup_storage_history (
			id TEXT PRIMARY KEY,
			server_id TEXT NOT NULL,
			size_bytes INTEGER NOT NULL,
			recorded_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE TABLE server_events (
			id TEXT PRIMARY KEY,
			server_id TEXT NOT NULL,
			event_type TEXT NOT NULL,
			check_name TEXT,
			message TEXT,
			details TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
	`)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}

func countAlerts(t *testing.T, db *sql.DB, checkName string) int {
	t.Helper()
	var n int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM server_events WHERE check_name = ?`,
		checkName,
	).Scan(&n)
	if err != nil {
		t.Fatalf("count alerts: %v", err)
	}
	return n
}

func TestEvaluateAndAlertStorageQuota_Warning80(t *testing.T) {
	db := setupStorageTestDB(t)
	defer db.Close()

	_ = queries.InsertBackupStorageHistory(db, "h1", "srv-1", 700)
	_ = queries.InsertBackupStorageHistory(db, "h2", "srv-1", 820)

	EvaluateAndAlertStorageQuota(db, "srv-1", 820)

	if countAlerts(t, db, "backup_storage_warning") != 1 {
		t.Fatal("expected one 80% warning alert")
	}

	// Second call should not re-alert
	EvaluateAndAlertStorageQuota(db, "srv-1", 850)
	if countAlerts(t, db, "backup_storage_warning") != 1 {
		t.Fatal("should not re-alert at same band")
	}
}

func TestEvaluateAndAlertStorageQuota_Urgent95(t *testing.T) {
	db := setupStorageTestDB(t)
	defer db.Close()

	EvaluateAndAlertStorageQuota(db, "srv-1", 960)
	if countAlerts(t, db, "backup_storage_urgent") != 1 {
		t.Fatal("expected one 95% urgent alert")
	}

	level, err := queries.GetStorageAlertLevel(db, "srv-1")
	if err != nil {
		t.Fatal(err)
	}
	if level != "95" {
		t.Errorf("level = %q, want 95", level)
	}
}

func TestEvaluateAndAlertStorageQuota_ClearsBelow80(t *testing.T) {
	db := setupStorageTestDB(t)
	defer db.Close()

	EvaluateAndAlertStorageQuota(db, "srv-1", 820)
	EvaluateAndAlertStorageQuota(db, "srv-1", 500)

	level, err := queries.GetStorageAlertLevel(db, "srv-1")
	if err != nil {
		t.Fatal(err)
	}
	if level != "" {
		t.Errorf("level = %q, want empty after drop below 80%%", level)
	}
}

func TestGetBackupUsageInfo(t *testing.T) {
	db := setupStorageTestDB(t)
	defer db.Close()

	_ = queries.UpdateRepositorySize(db, "srv-1", 420)
	_ = queries.InsertBackupStorageHistory(db, "h1", "srv-1", 400)
	_ = queries.InsertBackupStorageHistory(db, "h2", "srv-1", 420)
	_, _ = db.Exec(
		`INSERT INTO backup_snapshots (id, server_id, snapshot_id, paths, size_bytes)
		 VALUES ('s1', 'srv-1', 'snap1', '/', 100)`,
	)

	info, err := queries.GetBackupUsageInfo(db, "srv-1")
	if err != nil {
		t.Fatalf("GetBackupUsageInfo: %v", err)
	}
	if info.TotalBytes != 420 {
		t.Errorf("TotalBytes = %d, want 420", info.TotalBytes)
	}
	if info.LimitBytes != 1000 {
		t.Errorf("LimitBytes = %d, want 1000", info.LimitBytes)
	}
	if info.SnapshotCount != 1 {
		t.Errorf("SnapshotCount = %d, want 1", info.SnapshotCount)
	}
	if len(info.History) != 2 {
		t.Errorf("History len = %d, want 2", len(info.History))
	}
	if info.PercentUsed < 41 || info.PercentUsed > 43 {
		t.Errorf("PercentUsed = %f, want ~42", info.PercentUsed)
	}
}
