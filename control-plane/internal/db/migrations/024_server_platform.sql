-- 024_server_platform.sql
-- Stores the full platform detection and readiness result per server.
-- Populated by the agent on connect (platform_report message)
-- and on-demand via the detect_platform command.

CREATE TABLE IF NOT EXISTS server_platform (
    server_id                TEXT PRIMARY KEY REFERENCES servers(id) ON DELETE CASCADE,
    -- CPU identity
    is_arm64                 INTEGER NOT NULL DEFAULT 0,
    cpu_model_name           TEXT,
    cpu_vendor_id            TEXT,
    cpu_microarchitecture    TEXT,
    cpu_part_code            TEXT,
    cpu_cloud_provider_hint  TEXT,
    cpu_detection_confidence TEXT,
    cpu_cores                INTEGER,
    cpu_mhz                  REAL,
    -- CPU features
    feature_dotprod          INTEGER NOT NULL DEFAULT 0,
    feature_i8mm             INTEGER NOT NULL DEFAULT 0,
    feature_sve              INTEGER NOT NULL DEFAULT 0,
    feature_sve2             INTEGER NOT NULL DEFAULT 0,
    feature_bf16             INTEGER NOT NULL DEFAULT 0,
    -- Build selection (Step 3)
    image_tag                TEXT,
    optimization_label       TEXT,
    expected_hardware        TEXT,
    -- Memory assessment (Step 4)
    memory_total_mb          INTEGER,
    memory_available_mb      INTEGER,
    memory_available_gb      REAL,
    memory_recommended_model TEXT,
    memory_recommended_quant TEXT,
    memory_sufficient        INTEGER NOT NULL DEFAULT 1,
    memory_note              TEXT,
    -- Disk assessment (Step 4)
    disk_total_gb            REAL,
    disk_available_gb        REAL,
    disk_model_required_gb   REAL,
    disk_sufficient          INTEGER NOT NULL DEFAULT 1,
    disk_note                TEXT,
    -- Readiness summary (Step 5)
    can_run_inference        INTEGER NOT NULL DEFAULT 0,
    block_reason             TEXT,
    readiness_notes          TEXT, -- JSON array of strings
    -- Metadata
    detected_at              TEXT NOT NULL
);
