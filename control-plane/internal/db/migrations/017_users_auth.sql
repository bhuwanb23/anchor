-- Layer 5A — Auth and Sessions (Step 1D).
-- Extends the users table with the fields required by the registration
-- flow: a display name and an updated_at timestamp.
--
-- SQLite does not support ADD COLUMN IF NOT EXISTS. Re-runs are safe because
-- the migration runner tracks applied migrations in schema_migrations and
-- tolerates duplicate-column errors on databases created by the old runner.

ALTER TABLE users ADD COLUMN name TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN updated_at TEXT;
