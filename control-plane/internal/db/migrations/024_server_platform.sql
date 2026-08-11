-- 024_server_platform.sql
-- Stores the platform detection result per server.
-- Populated by the agent on connect (platform_report message)
-- and on-demand via the detect_platform command.

CREATE TABLE IF NOT EXISTS server_platform (
    server_id               TEXT PRIMARY KEY REFERENCES servers(id) ON DELETE CASCADE,
    is_arm64                INTEGER NOT NULL DEFAULT 0,
    cpu_model_name          TEXT,
    cpu_vendor_id           TEXT,
    cpu_microarchitecture   TEXT,
    cpu_part_code           TEXT,
    cpu_cloud_provider_hint TEXT,
    cpu_detection_confidence TEXT,
    cpu_cores               INTEGER,
    cpu_mhz                 REAL,
    feature_dotprod         INTEGER NOT NULL DEFAULT 0,
    feature_i8mm            INTEGER NOT NULL DEFAULT 0,
    feature_sve             INTEGER NOT NULL DEFAULT 0,
    feature_sve2            INTEGER NOT NULL DEFAULT 0,
    feature_bf16            INTEGER NOT NULL DEFAULT 0,
    memory_total_mb         INTEGER,
    memory_available_mb     INTEGER,
    memory_recommended_model TEXT,
    disk_total_gb           REAL,
    disk_available_gb       REAL,
    recommended_build       TEXT,
    recommended_quantization TEXT,
    detected_at             TEXT NOT NULL
);
