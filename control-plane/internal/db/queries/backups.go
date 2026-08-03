package queries

import (
	"database/sql"
	"time"
)

type BackupConfig struct {
	ID                string
	ServerID          string
	Enabled           bool
	Schedule          string
	RetentionDaily    int
	RetentionWeekly   int
	RetentionMonthly  int
	S3Endpoint        sql.NullString
	S3AccessKey       sql.NullString
	S3SecretKey       sql.NullString
	S3Bucket          sql.NullString
	S3Region          sql.NullString
	CreatedAt         string
	UpdatedAt         string
}

type BackupSnapshot struct {
	ID        string
	ServerID  string
	SnapshotID string
	Paths     string
	SizeBytes int64
	CreatedAt string
}

type BackupJob struct {
	ID           string
	ServerID     string
	Status       string
	StartedAt    sql.NullString
	CompletedAt  sql.NullString
	ErrorMessage sql.NullString
	SnapshotID   sql.NullString
	CreatedAt    string
}

func InsertBackupConfig(db *sql.DB, id, serverID string) error {
	_, err := db.Exec(
		"INSERT INTO backup_configs (id, server_id) VALUES (?, ?)",
		id, serverID,
	)
	return err
}

func GetBackupConfigByServer(db *sql.DB, serverID string) (*BackupConfig, error) {
	var c BackupConfig
	err := db.QueryRow(
		"SELECT id, server_id, enabled, schedule, retention_daily, retention_weekly, retention_monthly, s3_endpoint, s3_access_key, s3_secret_key, s3_bucket, s3_region, created_at, updated_at FROM backup_configs WHERE server_id = ?",
		serverID,
	).Scan(&c.ID, &c.ServerID, &c.Enabled, &c.Schedule, &c.RetentionDaily, &c.RetentionWeekly, &c.RetentionMonthly, &c.S3Endpoint, &c.S3AccessKey, &c.S3SecretKey, &c.S3Bucket, &c.S3Region, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func UpdateBackupConfig(db *sql.DB, serverID string, enabled bool, schedule string, retentionDaily, retentionWeekly, retentionMonthly int, s3Endpoint, s3AccessKey, s3SecretKey, s3Bucket, s3Region *string) error {
	_, err := db.Exec(
		`UPDATE backup_configs SET enabled = ?, schedule = ?, retention_daily = ?, retention_weekly = ?, retention_monthly = ?, s3_endpoint = ?, s3_access_key = ?, s3_secret_key = ?, s3_bucket = ?, s3_region = ?, updated_at = ? WHERE server_id = ?`,
		enabled, schedule, retentionDaily, retentionWeekly, retentionMonthly,
		toNullString(s3Endpoint), toNullString(s3AccessKey), toNullString(s3SecretKey),
		toNullString(s3Bucket), toNullString(s3Region),
		time.Now().UTC().Format(time.RFC3339), serverID,
	)
	return err
}

func InsertBackupSnapshot(db *sql.DB, id, serverID, snapshotID, paths string, sizeBytes int64) error {
	_, err := db.Exec(
		"INSERT INTO backup_snapshots (id, server_id, snapshot_id, paths, size_bytes) VALUES (?, ?, ?, ?, ?)",
		id, serverID, snapshotID, paths, sizeBytes,
	)
	return err
}

func GetBackupSnapshotsByServer(db *sql.DB, serverID string, limit int) ([]BackupSnapshot, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.Query(
		"SELECT id, server_id, snapshot_id, paths, size_bytes, created_at FROM backup_snapshots WHERE server_id = ? ORDER BY created_at DESC LIMIT ?",
		serverID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snapshots []BackupSnapshot
	for rows.Next() {
		var s BackupSnapshot
		if err := rows.Scan(&s.ID, &s.ServerID, &s.SnapshotID, &s.Paths, &s.SizeBytes, &s.CreatedAt); err != nil {
			return nil, err
		}
		snapshots = append(snapshots, s)
	}
	return snapshots, rows.Err()
}

func DeleteBackupSnapshot(db *sql.DB, snapshotID string) error {
	_, err := db.Exec("DELETE FROM backup_snapshots WHERE id = ?", snapshotID)
	return err
}

func InsertBackupJob(db *sql.DB, id, serverID string) error {
	_, err := db.Exec(
		"INSERT INTO backup_jobs (id, server_id) VALUES (?, ?)",
		id, serverID,
	)
	return err
}

func UpdateBackupJobStatus(db *sql.DB, jobID, status string, errorMessage *string, snapshotID *string) error {
	var startedAt, completedAt sql.NullString
	if status == "running" {
		startedAt = sql.NullString{String: time.Now().UTC().Format(time.RFC3339), Valid: true}
	}
	if status == "completed" || status == "failed" {
		completedAt = sql.NullString{String: time.Now().UTC().Format(time.RFC3339), Valid: true}
	}
	_, err := db.Exec(
		"UPDATE backup_jobs SET status = ?, started_at = COALESCE(?, started_at), completed_at = ?, error_message = ?, snapshot_id = ? WHERE id = ?",
		status, startedAt, completedAt, toNullString(errorMessage), toNullString(snapshotID), jobID,
	)
	return err
}

func GetBackupJobsByServer(db *sql.DB, serverID string, limit int) ([]BackupJob, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := db.Query(
		"SELECT id, server_id, status, started_at, completed_at, error_message, snapshot_id, created_at FROM backup_jobs WHERE server_id = ? ORDER BY created_at DESC LIMIT ?",
		serverID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []BackupJob
	for rows.Next() {
		var j BackupJob
		if err := rows.Scan(&j.ID, &j.ServerID, &j.Status, &j.StartedAt, &j.CompletedAt, &j.ErrorMessage, &j.SnapshotID, &j.CreatedAt); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

func toNullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}
