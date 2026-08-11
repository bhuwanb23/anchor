-- +migrate Up

-- Add Arm Performix results and arm_features to benchmark_results.
ALTER TABLE benchmark_results ADD COLUMN arm_features TEXT DEFAULT '';
ALTER TABLE benchmark_results ADD COLUMN performix_tokens_per_second REAL DEFAULT 0;
ALTER TABLE benchmark_results ADD COLUMN performix_ttft_ms INTEGER DEFAULT 0;
ALTER TABLE benchmark_results ADD COLUMN performix_peak_memory_bytes INTEGER DEFAULT 0;
ALTER TABLE benchmark_results ADD COLUMN performix_raw_output TEXT DEFAULT '';
ALTER TABLE benchmark_results ADD COLUMN tokens_sec_range_min REAL DEFAULT 0;
ALTER TABLE benchmark_results ADD COLUMN tokens_sec_range_max REAL DEFAULT 0;
ALTER TABLE benchmark_results ADD COLUMN ttft_range_min_ms INTEGER DEFAULT 0;
ALTER TABLE benchmark_results ADD COLUMN ttft_range_max_ms INTEGER DEFAULT 0;
ALTER TABLE benchmark_results ADD COLUMN variance_detected INTEGER DEFAULT 0;
ALTER TABLE benchmark_results ADD COLUMN actual_runs INTEGER DEFAULT 2;

-- +migrate Down
-- SQLite does not support DROP COLUMN; recreate the table.
CREATE TABLE IF NOT EXISTS benchmark_results_backup (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    server_id       INTEGER NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    deployment_id   INTEGER REFERENCES deployments(id) ON DELETE SET NULL,
    template_id     TEXT NOT NULL,
    build_label     TEXT NOT NULL,
    image_tag       TEXT NOT NULL,
    quantization    TEXT NOT NULL,
    median_tokens_per_second  REAL NOT NULL,
    median_ttft_ms            INTEGER NOT NULL,
    peak_memory_bytes         INTEGER NOT NULL,
    total_duration_ms         INTEGER NOT NULL,
    prompt_results TEXT NOT NULL DEFAULT '[]',
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO benchmark_results_backup SELECT id, server_id, deployment_id, template_id, build_label, image_tag, quantization, median_tokens_per_second, median_ttft_ms, peak_memory_bytes, total_duration_ms, prompt_results, created_at, updated_at FROM benchmark_results;
DROP TABLE benchmark_results;
ALTER TABLE benchmark_results_backup RENAME TO benchmark_results;
