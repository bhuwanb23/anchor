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
	HourUTC           sql.NullInt64
	LastBackupAt      sql.NullString
	NextBackupAt      sql.NullString
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
	ID                string
	ServerID          string
	Status            string
	StartedAt         sql.NullString
	CompletedAt       sql.NullString
	ErrorMessage      sql.NullString
	SnapshotID        sql.NullString
	DurationSeconds   sql.NullInt64
	SizeNewBytes      sql.NullInt64
	SizeTotalBytes    sql.NullInt64
	ProjectResults    sql.NullString
	RetentionApplied  sql.NullInt64
	SnapshotsPruned   sql.NullInt64
	CreatedAt         string
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
		"SELECT id, server_id, status, started_at, completed_at, error_message, snapshot_id, duration_seconds, size_new_bytes, size_total_bytes, project_results, retention_applied, snapshots_pruned, created_at FROM backup_jobs WHERE server_id = ? ORDER BY created_at DESC LIMIT ?",
		serverID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []BackupJob
	for rows.Next() {
		var j BackupJob
		if err := rows.Scan(&j.ID, &j.ServerID, &j.Status, &j.StartedAt, &j.CompletedAt, &j.ErrorMessage, &j.SnapshotID, &j.DurationSeconds, &j.SizeNewBytes, &j.SizeTotalBytes, &j.ProjectResults, &j.RetentionApplied, &j.SnapshotsPruned, &j.CreatedAt); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

func GetBackupJobByID(db *sql.DB, jobID string) (*BackupJob, error) {
	var j BackupJob
	err := db.QueryRow(
		"SELECT id, server_id, status, started_at, completed_at, error_message, snapshot_id, duration_seconds, size_new_bytes, size_total_bytes, project_results, retention_applied, snapshots_pruned, created_at FROM backup_jobs WHERE id = ?",
		jobID,
	).Scan(&j.ID, &j.ServerID, &j.Status, &j.StartedAt, &j.CompletedAt, &j.ErrorMessage, &j.SnapshotID, &j.DurationSeconds, &j.SizeNewBytes, &j.SizeTotalBytes, &j.ProjectResults, &j.RetentionApplied, &j.SnapshotsPruned, &j.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &j, nil
}

func UpdateBackupJobFull(db *sql.DB, jobID, status string, durationSeconds *int64, sizeNewBytes, sizeTotalBytes *int64, projectResults *string, retentionApplied *bool, snapshotsPruned *int, errorMessage *string, snapshotID *string) error {
	var startedAt, completedAt sql.NullString
	if status == "running" {
		startedAt = sql.NullString{String: time.Now().UTC().Format(time.RFC3339), Valid: true}
	}
	if status == "success" || status == "partial" || status == "failed" {
		completedAt = sql.NullString{String: time.Now().UTC().Format(time.RFC3339), Valid: true}
	}

	var durSec, sizeNew, sizeTotal sql.NullInt64
	if durationSeconds != nil {
		durSec = sql.NullInt64{Int64: *durationSeconds, Valid: true}
	}
	if sizeNewBytes != nil {
		sizeNew = sql.NullInt64{Int64: *sizeNewBytes, Valid: true}
	}
	if sizeTotalBytes != nil {
		sizeTotal = sql.NullInt64{Int64: *sizeTotalBytes, Valid: true}
	}

	var retApplied, pruned sql.NullInt64
	if retentionApplied != nil && *retentionApplied {
		retApplied = sql.NullInt64{Int64: 1, Valid: true}
	}
	if snapshotsPruned != nil {
		pruned = sql.NullInt64{Int64: int64(*snapshotsPruned), Valid: true}
	}

	_, err := db.Exec(
		`UPDATE backup_jobs SET 
			status = ?, 
			started_at = COALESCE(?, started_at), 
			completed_at = ?, 
			error_message = ?, 
			snapshot_id = ?,
			duration_seconds = ?,
			size_new_bytes = ?,
			size_total_bytes = ?,
			project_results = ?,
			retention_applied = ?,
			snapshots_pruned = ?
		WHERE id = ?`,
		status, startedAt, completedAt, toNullString(errorMessage), toNullString(snapshotID),
		durSec, sizeNew, sizeTotal, toNullString(projectResults), retApplied, pruned,
		jobID,
	)
	return err
}

func UpdateBackupSchedule(db *sql.DB, serverID string, hourUTC int) error {
	_, err := db.Exec(
		"UPDATE backup_configs SET hour_utc = ?, updated_at = ? WHERE server_id = ?",
		hourUTC, time.Now().UTC().Format(time.RFC3339), serverID,
	)
	return err
}

func UpdateLastBackupTime(db *sql.DB, serverID string) error {
	_, err := db.Exec(
		"UPDATE backup_configs SET last_backup_at = ?, updated_at = ? WHERE server_id = ?",
		time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339), serverID,
	)
	return err
}

func GetBackupUsage(db *sql.DB, serverID string) (totalSizeBytes int64, snapshotCount int, err error) {
	err = db.QueryRow(
		"SELECT COALESCE(SUM(size_bytes), 0), COUNT(*) FROM backup_snapshots WHERE server_id = ?",
		serverID,
	).Scan(&totalSizeBytes, &snapshotCount)
	return
}

func GetBackupConfigWithSchedule(db *sql.DB, serverID string) (*BackupConfig, error) {
	var c BackupConfig
	err := db.QueryRow(
		"SELECT id, server_id, enabled, schedule, retention_daily, retention_weekly, retention_monthly, s3_endpoint, s3_access_key, s3_secret_key, s3_bucket, s3_region, hour_utc, last_backup_at, next_backup_at, created_at, updated_at FROM backup_configs WHERE server_id = ?",
		serverID,
	).Scan(&c.ID, &c.ServerID, &c.Enabled, &c.Schedule, &c.RetentionDaily, &c.RetentionWeekly, &c.RetentionMonthly, &c.S3Endpoint, &c.S3AccessKey, &c.S3SecretKey, &c.S3Bucket, &c.S3Region, &c.HourUTC, &c.LastBackupAt, &c.NextBackupAt, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ---------------------------------------------------------------------------
// Restore job queries
// ---------------------------------------------------------------------------

// InsertRestoreJob inserts a restore job record with job_type='restore'.
func InsertRestoreJob(db *sql.DB, id, serverID, snapshotID, projectName string) error {
	_, err := db.Exec(
		"INSERT INTO backup_jobs (id, server_id, job_type, snapshot_id, project_name, status) VALUES (?, ?, 'restore', ?, ?, 'running')",
		id, serverID, snapshotID, projectName,
	)
	return err
}

// GetRestoreJobsByServer returns restore jobs for a server.
func GetRestoreJobsByServer(db *sql.DB, serverID string, limit int) ([]BackupJob, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := db.Query(
		`SELECT id, server_id, status, started_at, completed_at, error_message, snapshot_id, 
		        duration_seconds, size_new_bytes, size_total_bytes, project_results, 
		        retention_applied, snapshots_pruned, created_at 
		 FROM backup_jobs WHERE server_id = ? AND job_type = 'restore' ORDER BY created_at DESC LIMIT ?`,
		serverID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []BackupJob
	for rows.Next() {
		var j BackupJob
		if err := rows.Scan(&j.ID, &j.ServerID, &j.Status, &j.StartedAt, &j.CompletedAt, &j.ErrorMessage, &j.SnapshotID, &j.DurationSeconds, &j.SizeNewBytes, &j.SizeTotalBytes, &j.ProjectResults, &j.RetentionApplied, &j.SnapshotsPruned, &j.CreatedAt); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// UpdateRestoreJobStatus updates a restore job's status.
func UpdateRestoreJobStatus(db *sql.DB, jobID, status string, errorMessage *string) error {
	var completedAt sql.NullString
	if status == "success" || status == "partial" || status == "failed" {
		completedAt = sql.NullString{String: time.Now().UTC().Format(time.RFC3339), Valid: true}
	}
	_, err := db.Exec(
		"UPDATE backup_jobs SET status = ?, completed_at = ?, error_message = ? WHERE id = ?",
		status, completedAt, toNullString(errorMessage), jobID,
	)
	return err
}

// GetRestoreJobByID returns a single restore job.
func GetRestoreJobByID(db *sql.DB, jobID string) (*BackupJob, error) {
	var j BackupJob
	err := db.QueryRow(
		`SELECT id, server_id, status, started_at, completed_at, error_message, snapshot_id, 
		        duration_seconds, size_new_bytes, size_total_bytes, project_results, 
		        retention_applied, snapshots_pruned, created_at 
		 FROM backup_jobs WHERE id = ? AND job_type = 'restore'`,
		jobID,
	).Scan(&j.ID, &j.ServerID, &j.Status, &j.StartedAt, &j.CompletedAt, &j.ErrorMessage, &j.SnapshotID, &j.DurationSeconds, &j.SizeNewBytes, &j.SizeTotalBytes, &j.ProjectResults, &j.RetentionApplied, &j.SnapshotsPruned, &j.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &j, nil
}

func toNullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}
