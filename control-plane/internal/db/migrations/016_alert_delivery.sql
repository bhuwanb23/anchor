-- Layer 4C Step 6 — Alert Delivery.
-- Adds acknowledgment / read tracking to alerts and introduces the
-- alert_emails queue used for email delivery (immediate for critical,
-- hourly digest for warning/resolved).

ALTER TABLE alerts ADD COLUMN read_at TEXT;
ALTER TABLE alerts ADD COLUMN acknowledged_at TEXT;
ALTER TABLE alerts ADD COLUMN acknowledged_by TEXT;

CREATE TABLE IF NOT EXISTS alert_emails (
    id TEXT PRIMARY KEY,
    alert_id TEXT NOT NULL,
    server_id TEXT NOT NULL,
    severity TEXT NOT NULL,
    type TEXT NOT NULL,
    project TEXT,
    to_email TEXT NOT NULL,
    subject TEXT NOT NULL,
    body TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued',        -- queued | sent | failed
    is_batch INTEGER NOT NULL DEFAULT 0,          -- 1 = hourly batch, 0 = immediate
    attempts INTEGER NOT NULL DEFAULT 0,
    error TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    sent_at TEXT,
    FOREIGN KEY (alert_id) REFERENCES alerts(id) ON DELETE CASCADE,
    FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE CASCADE
);

-- One queued job per alert condition: escalations (severity change) and
-- resolutions (status change) insert a fresh row so the user is re-notified;
-- identical re-fires update the existing pending job instead of duplicating.
CREATE UNIQUE INDEX IF NOT EXISTS idx_alert_emails_dedup
    ON alert_emails(alert_id, severity, status);

CREATE INDEX IF NOT EXISTS idx_alert_emails_status ON alert_emails(status, is_batch);
CREATE INDEX IF NOT EXISTS idx_alert_emails_server ON alert_emails(server_id);
