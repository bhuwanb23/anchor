-- Stores benchmark results for inference deployments.
-- Each deployment can have multiple benchmark runs (baseline + optimized).
CREATE TABLE IF NOT EXISTS benchmark_results (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    server_id       INTEGER NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    deployment_id   INTEGER REFERENCES deployments(id) ON DELETE SET NULL,
    template_id     TEXT NOT NULL,
    build_label     TEXT NOT NULL,
    image_tag       TEXT NOT NULL,
    quantization    TEXT NOT NULL,

    -- Aggregate metrics
    median_tokens_per_second  REAL NOT NULL,
    median_ttft_ms            INTEGER NOT NULL,
    peak_memory_bytes         INTEGER NOT NULL,
    total_duration_ms         INTEGER NOT NULL,

    -- Per-prompt breakdown (stored as JSON)
    prompt_results TEXT NOT NULL DEFAULT '[]',

    -- Metadata
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_benchmark_results_server_id ON benchmark_results(server_id);
CREATE INDEX IF NOT EXISTS idx_benchmark_results_template_id ON benchmark_results(template_id);
CREATE INDEX IF NOT EXISTS idx_benchmark_results_created_at ON benchmark_results(created_at);
