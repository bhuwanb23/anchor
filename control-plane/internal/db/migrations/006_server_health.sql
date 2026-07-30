-- Add richer system info columns from preflight checks
ALTER TABLE servers ADD COLUMN os_version TEXT;
ALTER TABLE servers ADD COLUMN os_pretty TEXT;
ALTER TABLE servers ADD COLUMN ram_available_mb INTEGER;
ALTER TABLE servers ADD COLUMN disk_total_gb INTEGER;
ALTER TABLE servers ADD COLUMN disk_available_gb INTEGER;
ALTER TABLE servers ADD COLUMN disk_used_percent REAL;
ALTER TABLE servers ADD COLUMN docker_version TEXT;

-- Store health events: warnings, auto-fixes, and alerts
CREATE TABLE IF NOT EXISTS server_events (
    id TEXT PRIMARY KEY,
    server_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    check_name TEXT,
    message TEXT,
    details TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_server_events_server ON server_events(server_id);
CREATE INDEX IF NOT EXISTS idx_server_events_type ON server_events(event_type);
