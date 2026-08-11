-- Layer 5C Step 4A — metrics retention tiers.
--
-- Adds a granularity marker (raw | hourly | daily) to metrics_history so the
-- cleanup jobs can apply the plan's retention policy:
--   raw   kept 7 days
--   hourly kept 30 days
--   daily  kept 12 months
--
-- The partial unique index (server_id, recorded_at) over non-raw rows makes
-- rollups idempotent: re-running a rollup for the same hour/day bucket hits
-- INSERT OR IGNORE and updates nothing instead of duplicating rows. Raw rows
-- are excluded from the index because their recorded_at is a 30-second sample
-- timestamp (never a bucket boundary).

ALTER TABLE metrics_history ADD COLUMN granularity TEXT NOT NULL DEFAULT 'raw';

CREATE UNIQUE INDEX IF NOT EXISTS idx_metrics_history_rollup
    ON metrics_history(server_id, recorded_at) WHERE granularity != 'raw';
