-- Ensure the rollup unique index from 024 exists.
-- When 024's ALTER TABLE hit "duplicate column" on an already-upgraded DB,
-- the CREATE INDEX in the same file never ran. This migration is idempotent.

CREATE UNIQUE INDEX IF NOT EXISTS idx_metrics_history_rollup
    ON metrics_history(server_id, recorded_at) WHERE granularity != 'raw';
