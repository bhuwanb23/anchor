-- Layer 5A Step 7: Password reset tokens.
-- A single-use, 1-hour token (stored as its SHA-256 hash, never the raw
-- value) lets a user who forgot their password set a new one. used_at marks
-- a consumed token; expired-but-unused rows are garbage collected weekly.

CREATE TABLE IF NOT EXISTS password_resets (
    id          TEXT PRIMARY KEY,
    token_hash  TEXT NOT NULL UNIQUE,
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TEXT NOT NULL,
    expires_at  TEXT NOT NULL,
    used_at     TEXT
);

CREATE INDEX IF NOT EXISTS idx_password_resets_user ON password_resets(user_id);
CREATE INDEX IF NOT EXISTS idx_password_resets_hash ON password_resets(token_hash);
