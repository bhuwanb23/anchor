CREATE TABLE IF NOT EXISTS deployments (
    id TEXT PRIMARY KEY,
    server_id TEXT NOT NULL,
    app_name TEXT NOT NULL,
    image TEXT NOT NULL,
    port INTEGER NOT NULL,
    domain TEXT,
    status TEXT NOT NULL DEFAULT 'deploying',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_deployments_server_id ON deployments(server_id);