-- Layer 5B Step 4: command routing status/audit table. Every browser-issued
-- command gets a row here with a full status lifecycle:
--   queued -> in_progress -> success | failed | timeout
-- pending_commands (013) remains the offline delivery queue handed to the
-- agent via hello_ack; this table records history, enables deduplication
-- (Step 4C) and issuer-based result routing (Step 4A).
CREATE TABLE IF NOT EXISTS commands (
    id           TEXT PRIMARY KEY,
    server_id    TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    command_type TEXT NOT NULL,
    project_key  TEXT NOT NULL DEFAULT '',
    payload      TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'queued',
    issued_by    TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    started_at   TEXT,
    completed_at TEXT,
    result       TEXT
);
CREATE INDEX IF NOT EXISTS idx_commands_server ON commands(server_id);
CREATE INDEX IF NOT EXISTS idx_commands_server_status ON commands(server_id, status);
CREATE INDEX IF NOT EXISTS idx_commands_dedup ON commands(server_id, command_type, project_key, status);
