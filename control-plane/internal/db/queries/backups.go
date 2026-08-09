package queries

import (
	"database/sql"
	"time"
)

type BackupConfig struct {
	ID                   string           `json:"id"`
	ServerID             string           `json:"server_id"`
	Enabled              bool             `json:"enabled"`
	Schedule             string           `json:"schedule"`
	RetentionDaily       int              `json:"retention_daily"`
	RetentionWeekly      int              `json:"retention_weekly"`
	RetentionMonthly     int              `json:"retention_monthly"`
	S3Endpoint           sql.NullString   `json:"s3_endpoint"`
	S3AccessKey          sql.NullString   `json:"s3_access_key"`
	S3SecretKey          sql.NullString   `json:"s3_secret_key"`
	S3Bucket             sql.NullString   `json:"s3_bucket"`
	S3Region             sql.NullString   `json:"s3_region"`
	HourUTC              sql.NullInt64    `json:"hour_utc"`
	LastBackupAt         sql.NullString   `json:"last_backup_at"`
	NextBackupAt         sql.NullString   `json:"next_backup_at"`
	StorageLimitBytes    int64            `json:"storage_limit_bytes"`
	RepositorySizeBytes  sql.NullInt64    `json:"repository_size_bytes"`
	StorageAlertLevel    sql.NullString   `json:"storage_alert_level"`
	CreatedAt            string           `json:"created_at"`
	UpdatedAt            string           `json:"updated_at"`
}

type BackupSnapshot struct {
	ID         string `json:"id"`
	ServerID   string `json:"server_id"`
	SnapshotID string `json:"snapshot_id"`
	Paths      string `json:"paths"`
	SizeBytes  int64  `json:"size_bytes"`
	CreatedAt  string `json:"created_at"`
}

type BackupJob struct {
	ID                 string         `json:"id"`
	ServerID           string         `json:"server_id"`
	Status             string         `json:"status"`
	StartedAt          sql.NullString `json:"started_at"`
	CompletedAt        sql.NullString `json:"completed_at"`
	ErrorMessage       sql.NullString `json:"error_message"`
	SnapshotID         sql.NullString `json:"snapshot_id"`
	DurationSeconds    sql.NullInt64  `json:"duration_seconds"`
	SizeNewBytes       sql.NullInt64  `json:"size_new_bytes"`
	SizeTotalBytes     sql.NullInt64  `json:"size_total_bytes"`
	ProjectResults     sql.NullString `json:"project_results"`
	RetentionApplied   sql.NullInt64  `json:"retention_applied"`
	SnapshotsPruned    sql.NullInt64  `json:"snapshots_pruned"`
	CreatedAt          string         `json:"created_at"`
	VerificationStatus sql.NullString `json:"verification_status"`
	VerificationError  sql.NullString `json:"verification_error"`
}

// BackupVerificationConfig holds per-server verification scheduling configuration.
type BackupVerificationConfig struct {
	ID                      string         `json:"id"`
	ServerID                string         `json:"server_id"`
	LastVerificationAt      sql.NullString `json:"last_verification_at"`
	NextVerificationAt      sql.NullString `json:"next_verification_at"`
	LastFullVerificationAt  sql.NullString `json:"last_full_verification_at"`
	NextFullVerificationAt  sql.NullString `json:"next_full_verification_at"`
	VerifyIntervalHours     int            `json:"verify_interval_hours"`
	FullVerifyIntervalHours int            `json:"full_verify_interval_hours"`
	CreatedAt               string         `json:"created_at"`
	UpdatedAt               string         `json:"updated_at"`
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
	if snapshots == nil {
		snapshots = []BackupSnapshot{}
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
	if jobs == nil {
		jobs = []BackupJob{}
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

// BackupStorageHistoryEntry is one point of repository size over time.
type BackupStorageHistoryEntry struct {
	SizeBytes  int64  `json:"size_bytes"`
	RecordedAt string `json:"recorded_at"`
}

// BackupUsageInfo is the full storage usage payload for the API.
type BackupUsageInfo struct {
	TotalBytes    int64                      `json:"total_bytes"`
	SnapshotCount int                        `json:"snapshot_count"`
	LimitBytes    int64                      `json:"limit_bytes"`
	PercentUsed   float64                    `json:"percent_used"`
	History       []BackupStorageHistoryEntry `json:"history"`
}

const DefaultStorageLimitBytes int64 = 1073741824 // 1 GiB

// GetBackupUsageInfo returns repository size, plan limit, and history.
func GetBackupUsageInfo(db *sql.DB, serverID string) (*BackupUsageInfo, error) {
	info := &BackupUsageInfo{
		LimitBytes: DefaultStorageLimitBytes,
		History:    []BackupStorageHistoryEntry{},
	}

	var repoSize sql.NullInt64
	var limitBytes sql.NullInt64
	err := db.QueryRow(
		`SELECT repository_size_bytes, storage_limit_bytes FROM backup_configs WHERE server_id = ?`,
		serverID,
	).Scan(&repoSize, &limitBytes)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if limitBytes.Valid && limitBytes.Int64 > 0 {
		info.LimitBytes = limitBytes.Int64
	}
	if repoSize.Valid {
		info.TotalBytes = repoSize.Int64
	}

	err = db.QueryRow(
		`SELECT COUNT(*) FROM backup_snapshots WHERE server_id = ?`,
		serverID,
	).Scan(&info.SnapshotCount)
	if err != nil {
		return nil, err
	}

	if info.LimitBytes > 0 {
		info.PercentUsed = float64(info.TotalBytes) / float64(info.LimitBytes) * 100
	}

	rows, err := db.Query(
		`SELECT size_bytes, recorded_at FROM backup_storage_history
		 WHERE server_id = ? ORDER BY recorded_at ASC LIMIT 90`,
		serverID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var e BackupStorageHistoryEntry
		if err := rows.Scan(&e.SizeBytes, &e.RecordedAt); err != nil {
			return nil, err
		}
		info.History = append(info.History, e)
	}
	return info, rows.Err()
}

// UpdateRepositorySize stores the current repository size on backup_configs.
func UpdateRepositorySize(db *sql.DB, serverID string, sizeBytes int64) error {
	_, err := db.Exec(
		`UPDATE backup_configs SET repository_size_bytes = ?, updated_at = ? WHERE server_id = ?`,
		sizeBytes, time.Now().UTC().Format(time.RFC3339), serverID,
	)
	return err
}

// InsertBackupStorageHistory records a repository size sample.
func InsertBackupStorageHistory(db *sql.DB, id, serverID string, sizeBytes int64) error {
	_, err := db.Exec(
		`INSERT INTO backup_storage_history (id, server_id, size_bytes, recorded_at) VALUES (?, ?, ?, ?)`,
		id, serverID, sizeBytes, time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

// GetStorageAlertLevel returns the last fired storage alert band ("80", "95", or empty).
func GetStorageAlertLevel(db *sql.DB, serverID string) (string, error) {
	var level sql.NullString
	err := db.QueryRow(
		`SELECT storage_alert_level FROM backup_configs WHERE server_id = ?`,
		serverID,
	).Scan(&level)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if level.Valid {
		return level.String, nil
	}
	return "", nil
}

// SetStorageAlertLevel updates the last fired storage alert band.
func SetStorageAlertLevel(db *sql.DB, serverID, level string) error {
	_, err := db.Exec(
		`UPDATE backup_configs SET storage_alert_level = ?, updated_at = ? WHERE server_id = ?`,
		level, time.Now().UTC().Format(time.RFC3339), serverID,
	)
	return err
}

// GetBackupStorageLimits returns limit and retention for alert messaging.
func GetBackupStorageLimits(db *sql.DB, serverID string) (limitBytes int64, daily, weekly, monthly int, err error) {
	limitBytes = DefaultStorageLimitBytes
	daily, weekly, monthly = 7, 4, 12
	var lim sql.NullInt64
	err = db.QueryRow(
		`SELECT storage_limit_bytes, retention_daily, retention_weekly, retention_monthly
		 FROM backup_configs WHERE server_id = ?`,
		serverID,
	).Scan(&lim, &daily, &weekly, &monthly)
	if err == sql.ErrNoRows {
		return limitBytes, daily, weekly, monthly, nil
	}
	if err != nil {
		return 0, 0, 0, 0, err
	}
	if lim.Valid && lim.Int64 > 0 {
		limitBytes = lim.Int64
	}
	return limitBytes, daily, weekly, monthly, nil
}

// EstimateDaysUntilFull estimates days until the plan limit based on recent history growth.
func EstimateDaysUntilFull(history []BackupStorageHistoryEntry, currentBytes, limitBytes int64) int {
	if limitBytes <= currentBytes || len(history) < 2 {
		if limitBytes <= currentBytes {
			return 0
		}
		return -1
	}

	first := history[0]
	last := history[len(history)-1]
	growth := last.SizeBytes - first.SizeBytes
	if growth <= 0 {
		return -1
	}

	// Assume roughly one sample per day; fall back to sample count as days.
	days := float64(len(history) - 1)
	if days < 1 {
		days = 1
	}
	bytesPerDay := float64(growth) / days
	remaining := float64(limitBytes - currentBytes)
	return int(remaining / bytesPerDay)
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
	if jobs == nil {
		jobs = []BackupJob{}
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

// UpdateBackupJobVerification updates a backup job's verification status.
func UpdateBackupJobVerification(db *sql.DB, jobID, verificationStatus, verificationError string) error {
	_, err := db.Exec(
		"UPDATE backup_jobs SET verification_status = ?, verification_error = ? WHERE id = ?",
		verificationStatus, toNullString(&verificationError), jobID,
	)
	return err
}

// InsertVerificationConfig creates a default verification config for a server.
func InsertVerificationConfig(db *sql.DB, id, serverID string) error {
	_, err := db.Exec(
		`INSERT INTO backup_verification_configs (id, server_id, verify_interval_hours, full_verify_interval_hours)
		 VALUES (?, ?, 168, 720)`,
		id, serverID,
	)
	return err
}

// GetVerificationConfigByServer returns the verification config for a server.
func GetVerificationConfigByServer(db *sql.DB, serverID string) (*BackupVerificationConfig, error) {
	var c BackupVerificationConfig
	err := db.QueryRow(
		`SELECT id, server_id, last_verification_at, next_verification_at,
		        last_full_verification_at, next_full_verification_at,
		        verify_interval_hours, full_verify_interval_hours, created_at, updated_at
		 FROM backup_verification_configs WHERE server_id = ?`,
		serverID,
	).Scan(&c.ID, &c.ServerID, &c.LastVerificationAt, &c.NextVerificationAt,
		&c.LastFullVerificationAt, &c.NextFullVerificationAt,
		&c.VerifyIntervalHours, &c.FullVerifyIntervalHours,
		&c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// UpdateVerificationConfig updates verification scheduling timestamps.
func UpdateVerificationConfig(db *sql.DB, serverID string, lastAt, nextAt, lastFullAt, nextFullAt *string) error {
	_, err := db.Exec(
		`UPDATE backup_verification_configs
		 SET last_verification_at = ?, next_verification_at = ?,
		     last_full_verification_at = ?, next_full_verification_at = ?,
		     updated_at = datetime('now')
		 WHERE server_id = ?`,
		toNullString(lastAt), toNullString(nextAt),
		toNullString(lastFullAt), toNullString(nextFullAt),
		serverID,
	)
	return err
}

// UpdateVerificationConfigIntervals updates verification interval settings.
func UpdateVerificationConfigIntervals(db *sql.DB, serverID string, verifyIntervalHours, fullVerifyIntervalHours int) error {
	_, err := db.Exec(
		`UPDATE backup_verification_configs
		 SET verify_interval_hours = ?, full_verify_interval_hours = ?,
		     updated_at = datetime('now')
		 WHERE server_id = ?`,
		verifyIntervalHours, fullVerifyIntervalHours, serverID,
	)
	return err
}
