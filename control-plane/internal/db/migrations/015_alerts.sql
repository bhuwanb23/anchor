-- Layer 4C Step 5 — Alert persistence.
-- Rich alerts emitted by the agent (anomaly_alert) are stored here so the
-- dashboard can show an alert list with history, severity, and resolution.

CREATE TABLE IF NOT EXISTS alerts (
    id TEXT PRIMARY KEY,
    server_id TEXT NOT NULL,
    project TEXT,
    container TEXT,
    severity TEXT NOT NULL,
    type TEXT NOT NULL,
    status TEXT NOT NULL,
    title TEXT,
    message TEXT,
    detail TEXT,
    action TEXT,
    metrics TEXT,
    fired_at TEXT,
    resolved_at TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_alerts_server ON alerts(server_id);
CREATE INDEX IF NOT EXISTS idx_alerts_status ON alerts(status);
CREATE INDEX IF NOT EXISTS idx_alerts_fired ON alerts(fired_at);
