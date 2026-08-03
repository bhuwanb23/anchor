-- Step 6: Restore Operations
-- Add job_type column to distinguish backup vs restore jobs

-- Add job_type column (default 'backup' for existing rows)
ALTER TABLE backup_jobs ADD COLUMN job_type TEXT NOT NULL DEFAULT 'backup';

-- Add restore-specific columns
ALTER TABLE backup_jobs ADD COLUMN project_name TEXT;
ALTER TABLE backup_jobs ADD COLUMN snapshot_id TEXT;

-- Index for querying restore jobs efficiently
CREATE INDEX idx_backup_jobs_job_type ON backup_jobs(job_type);
CREATE INDEX idx_backup_jobs_server_job_type ON backup_jobs(server_id, job_type);
