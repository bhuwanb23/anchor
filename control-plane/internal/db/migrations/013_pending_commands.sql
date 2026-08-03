-- Layer 4B: offline command queue
CREATE TABLE IF NOT EXISTS pending_commands (
    id          TEXT PRIMARY KEY,
    server_id   TEXT NOT NULL,
    command_type TEXT NOT NULL,
    payload     TEXT NOT NULL,
    project_key TEXT,
    expires_at  TEXT,
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_pending_commands_server ON pending_commands(server_id);
CREATE INDEX IF NOT EXISTS idx_pending_commands_created ON pending_commands(created_at);
