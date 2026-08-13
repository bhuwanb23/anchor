-- Fix benchmark_results FK column types to match servers.id / deployments.id (TEXT).
-- SQLite cannot ALTER COLUMN type; recreate the table.

CREATE TABLE IF NOT EXISTS benchmark_results_new (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    server_id       TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    deployment_id   TEXT REFERENCES deployments(id) ON DELETE SET NULL,
    template_id     TEXT NOT NULL,
    build_label     TEXT NOT NULL,
    image_tag       TEXT NOT NULL,
    quantization    TEXT NOT NULL,
    arm_features    TEXT DEFAULT '',

    median_tokens_per_second  REAL NOT NULL DEFAULT 0,
    median_ttft_ms            INTEGER NOT NULL DEFAULT 0,
    peak_memory_bytes         INTEGER NOT NULL DEFAULT 0,
    total_duration_ms         INTEGER NOT NULL DEFAULT 0,

    prompt_results TEXT NOT NULL DEFAULT '[]',

    tokens_sec_range_min REAL DEFAULT 0,
    tokens_sec_range_max REAL DEFAULT 0,
    ttft_range_min_ms INTEGER DEFAULT 0,
    ttft_range_max_ms INTEGER DEFAULT 0,
    variance_detected INTEGER DEFAULT 0,
    actual_runs INTEGER DEFAULT 2,

    performix_tokens_per_second REAL DEFAULT 0,
    performix_ttft_ms INTEGER DEFAULT 0,
    performix_peak_memory_bytes INTEGER DEFAULT 0,
    performix_raw_output TEXT DEFAULT '',

    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO benchmark_results_new (
    id, server_id, deployment_id, template_id, build_label, image_tag, quantization,
    arm_features, median_tokens_per_second, median_ttft_ms, peak_memory_bytes, total_duration_ms,
    prompt_results, tokens_sec_range_min, tokens_sec_range_max, ttft_range_min_ms, ttft_range_max_ms,
    variance_detected, actual_runs,
    performix_tokens_per_second, performix_ttft_ms, performix_peak_memory_bytes, performix_raw_output,
    created_at, updated_at
)
SELECT
    id, CAST(server_id AS TEXT), CAST(deployment_id AS TEXT), template_id, build_label, image_tag, quantization,
    COALESCE(arm_features, ''), median_tokens_per_second, median_ttft_ms, peak_memory_bytes, total_duration_ms,
    prompt_results,
    COALESCE(tokens_sec_range_min, 0), COALESCE(tokens_sec_range_max, 0),
    COALESCE(ttft_range_min_ms, 0), COALESCE(ttft_range_max_ms, 0),
    COALESCE(variance_detected, 0), COALESCE(actual_runs, 2),
    COALESCE(performix_tokens_per_second, 0), COALESCE(performix_ttft_ms, 0),
    COALESCE(performix_peak_memory_bytes, 0), COALESCE(performix_raw_output, ''),
    created_at, updated_at
FROM benchmark_results;

DROP TABLE benchmark_results;
ALTER TABLE benchmark_results_new RENAME TO benchmark_results;

CREATE INDEX IF NOT EXISTS idx_benchmark_results_server_id ON benchmark_results(server_id);
CREATE INDEX IF NOT EXISTS idx_benchmark_results_template_id ON benchmark_results(template_id);
CREATE INDEX IF NOT EXISTS idx_benchmark_results_created_at ON benchmark_results(created_at);
