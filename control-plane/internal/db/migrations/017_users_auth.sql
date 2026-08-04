-- Layer 5A — Auth and Sessions (Step 1D).
-- Extends the users table with the fields required by the registration
-- flow: a display name and an updated_at timestamp.
--
-- These use ADD COLUMN IF NOT EXISTS so the migration is safe to re-run
-- on every control-plane startup (migrations have no tracking table).

ALTER TABLE users ADD COLUMN IF NOT EXISTS name TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS updated_at TEXT;
