CREATE TABLE IF NOT EXISTS registration_tokens (
    id TEXT PRIMARY KEY,
    token_hash TEXT UNIQUE NOT NULL,
    user_id TEXT NOT NULL,
    server_name TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at TEXT NOT NULL,
    used_at TEXT,
    used_by_ip TEXT,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_registration_tokens_hash ON registration_tokens(token_hash);
CREATE INDEX IF NOT EXISTS idx_registration_tokens_user ON registration_tokens(user_id);
