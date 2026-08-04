-- Layer 5A — Auth and Sessions (Step 2C).
-- Refresh tokens enable revocable sessions: the access token is a short-lived
-- JWT (stateless), while this table stores the SHA-256 hash of each refresh
-- token so individual sessions can be revoked and rotated.

CREATE TABLE IF NOT EXISTS refresh_tokens (
    id           TEXT PRIMARY KEY,
    token_hash   TEXT NOT NULL UNIQUE,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at   TEXT NOT NULL,
    last_used_at TEXT,
    user_agent   TEXT,
    ip_address   TEXT,
    revoked_at   TEXT
);

CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user ON refresh_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_hash ON refresh_tokens(token_hash);
