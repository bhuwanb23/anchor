CREATE TABLE IF NOT EXISTS custom_domains (
    id              TEXT PRIMARY KEY,
    deployment_id   TEXT NOT NULL,
    domain          TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending',
    verified_at     TEXT,
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at      TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (deployment_id) REFERENCES deployments(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_custom_domains_domain ON custom_domains(domain);
CREATE INDEX IF NOT EXISTS idx_custom_domains_deployment ON custom_domains(deployment_id);
CREATE INDEX IF NOT EXISTS idx_custom_domains_status ON custom_domains(status);
