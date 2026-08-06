-- 022_server_metrics_latest.sql
-- Latest metrics snapshot per server for O(1) dashboard load.
-- Updated on every health report via UPSERT.
CREATE TABLE IF NOT EXISTS server_metrics_latest (
    server_id       TEXT PRIMARY KEY REFERENCES servers(id) ON DELETE CASCADE,
    recorded_at     TEXT NOT NULL,
    cpu_percent     REAL,
    ram_used_mb     INTEGER,
    ram_total_mb    INTEGER,
    disk_used_gb    REAL,
    disk_total_gb   REAL,
    load_1min       REAL
);
