-- Layer 4C: container status (latest per project+role, upserted each health_report)
CREATE TABLE IF NOT EXISTS container_status (
    id            TEXT PRIMARY KEY,
    server_id     TEXT NOT NULL,
    project       TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'app',
    container_id  TEXT NOT NULL,
    status        TEXT NOT NULL,
    health        TEXT,
    cpu_percent   REAL NOT NULL DEFAULT 0,
    ram_used_mb   INTEGER NOT NULL DEFAULT 0,
    ram_limit_mb  INTEGER NOT NULL DEFAULT 0,
    ram_percent   REAL NOT NULL DEFAULT 0,
    restart_count INTEGER NOT NULL DEFAULT 0,
    uptime_secs   INTEGER NOT NULL DEFAULT 0,
    exit_code     INTEGER,
    net_rx_bytes  INTEGER NOT NULL DEFAULT 0,
    net_tx_bytes  INTEGER NOT NULL DEFAULT 0,
    last_seen     TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_container_status_key ON container_status(server_id, project, role);
CREATE INDEX IF NOT EXISTS idx_container_status_server ON container_status(server_id);

-- Layer 4C: health metrics history (raw 30s samples, pruned after 7 days)
CREATE TABLE IF NOT EXISTS metrics_history (
    id              TEXT PRIMARY KEY,
    server_id       TEXT NOT NULL,
    recorded_at     TEXT NOT NULL,
    collected_in_ms INTEGER,

    cpu_percent     REAL,
    ram_used_mb     INTEGER,
    ram_total_mb    INTEGER,
    ram_percent     REAL,
    disk_used_gb    REAL,
    disk_total_gb   REAL,
    disk_percent    REAL,
    load_1min       REAL,
    load_per_core   REAL,

    caddy_running       INTEGER NOT NULL DEFAULT 0,
    caddy_routes_count  INTEGER NOT NULL DEFAULT 0,
    last_backup_age_sec INTEGER,
    container_count     INTEGER NOT NULL DEFAULT 0,

    FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_metrics_history_server_time ON metrics_history(server_id, recorded_at);
