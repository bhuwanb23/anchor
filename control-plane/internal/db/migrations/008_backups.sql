CREATE TABLE IF NOT EXISTS backup_configs (
    id              TEXT PRIMARY KEY,
    server_id       TEXT NOT NULL,
    enabled         INTEGER NOT NULL DEFAULT 0,
    schedule        TEXT NOT NULL DEFAULT '0 2 * * *',
    retention_daily INTEGER NOT NULL DEFAULT 7,
    retention_weekly INTEGER NOT NULL DEFAULT 4,
    retention_monthly INTEGER NOT NULL DEFAULT 12,
    s3_endpoint     TEXT,
    s3_access_key   TEXT,
    s3_secret_key   TEXT,
    s3_bucket       TEXT,
    s3_region       TEXT,
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at      TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_backup_configs_server ON backup_configs(server_id);
CREATE INDEX IF NOT EXISTS idx_backup_configs_enabled ON backup_configs(enabled);

CREATE TABLE IF NOT EXISTS backup_snapshots (
    id              TEXT PRIMARY KEY,
    server_id       TEXT NOT NULL,
    snapshot_id     TEXT NOT NULL,
    paths           TEXT NOT NULL,
    size_bytes      INTEGER NOT NULL DEFAULT 0,
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_backup_snapshots_server ON backup_snapshots(server_id);
CREATE INDEX IF NOT EXISTS idx_backup_snapshots_created ON backup_snapshots(created_at);

CREATE TABLE IF NOT EXISTS backup_jobs (
    id              TEXT PRIMARY KEY,
    server_id       TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending',
    started_at      TEXT,
    completed_at    TEXT,
    error_message   TEXT,
    snapshot_id     TEXT,
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_backup_jobs_server ON backup_jobs(server_id);
CREATE INDEX IF NOT EXISTS idx_backup_jobs_status ON backup_jobs(status);
CREATE INDEX IF NOT EXISTS idx_backup_jobs_created ON backup_jobs(created_at);
