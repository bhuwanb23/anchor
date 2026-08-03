-- Layer 3C Step 8: backup storage usage tracking and plan limits
ALTER TABLE backup_configs ADD COLUMN storage_limit_bytes INTEGER NOT NULL DEFAULT 1073741824;
ALTER TABLE backup_configs ADD COLUMN repository_size_bytes INTEGER;
ALTER TABLE backup_configs ADD COLUMN storage_alert_level TEXT;

CREATE TABLE IF NOT EXISTS backup_storage_history (
    id          TEXT PRIMARY KEY,
    server_id   TEXT NOT NULL,
    size_bytes  INTEGER NOT NULL,
    recorded_at TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_backup_storage_history_server ON backup_storage_history(server_id);
CREATE INDEX IF NOT EXISTS idx_backup_storage_history_recorded ON backup_storage_history(recorded_at);
