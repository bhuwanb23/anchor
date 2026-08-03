-- Step 5: Backup Metadata and Control Plane Reporting
-- Add columns for detailed backup status reporting

-- Extend backup_jobs with metadata fields
ALTER TABLE backup_jobs ADD COLUMN duration_seconds INTEGER;
ALTER TABLE backup_jobs ADD COLUMN size_new_bytes INTEGER;
ALTER TABLE backup_jobs ADD COLUMN size_total_bytes INTEGER;
ALTER TABLE backup_jobs ADD COLUMN project_results TEXT;
ALTER TABLE backup_jobs ADD COLUMN retention_applied INTEGER DEFAULT 0;
ALTER TABLE backup_jobs ADD COLUMN snapshots_pruned INTEGER DEFAULT 0;

-- Extend backup_configs with schedule tracking
ALTER TABLE backup_configs ADD COLUMN hour_utc INTEGER NOT NULL DEFAULT 2;
ALTER TABLE backup_configs ADD COLUMN last_backup_at TEXT;
ALTER TABLE backup_configs ADD COLUMN next_backup_at TEXT;
