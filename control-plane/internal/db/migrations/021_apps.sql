-- 021_apps.sql
-- Apps table: current state of each app on each server
CREATE TABLE IF NOT EXISTS apps (
    id                  TEXT PRIMARY KEY,
    server_id           TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    project_name        TEXT NOT NULL,
    status              TEXT NOT NULL DEFAULT 'stopped'
                            CHECK (status IN (
                                'deploying',
                                'running',
                                'stopped',
                                'failed',
                                'removing'
                            )),

    -- Current deployment info
    current_image       TEXT,
    current_container_id TEXT,
    current_host_port   INTEGER,

    -- Routing
    platform_domain     TEXT,
    custom_domains      TEXT,

    -- Resource config
    memory_limit_mb     INTEGER DEFAULT 512,
    cpu_quota_percent   INTEGER DEFAULT 50,
    app_port            INTEGER DEFAULT 3000,

    created_at          TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at          TEXT NOT NULL DEFAULT (datetime('now')),

    UNIQUE (server_id, project_name)
);

CREATE INDEX IF NOT EXISTS idx_apps_server ON apps(server_id);
CREATE INDEX IF NOT EXISTS idx_apps_status ON apps(status);

-- Project databases: databases provisioned for projects
CREATE TABLE IF NOT EXISTS project_databases (
    id              TEXT PRIMARY KEY,
    app_id          TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    server_id       TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    project_name    TEXT NOT NULL,
    db_type         TEXT NOT NULL CHECK (db_type IN ('postgres', 'mysql', 'redis')),
    db_version      TEXT,
    db_name         TEXT,
    status          TEXT NOT NULL DEFAULT 'running',
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),

    UNIQUE (server_id, project_name, db_type)
);

CREATE INDEX IF NOT EXISTS idx_project_databases_app ON project_databases(app_id);
CREATE INDEX IF NOT EXISTS idx_project_databases_server ON project_databases(server_id);

-- Env var keys: store KEYS only, never values
-- Values live on the server in /etc/yourplatform/envs/
CREATE TABLE IF NOT EXISTS env_var_keys (
    id              TEXT PRIMARY KEY,
    app_id          TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    server_id       TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    key_name        TEXT NOT NULL,
    is_auto         INTEGER NOT NULL DEFAULT 0,
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at      TEXT NOT NULL DEFAULT (datetime('now')),

    UNIQUE (app_id, key_name)
);

CREATE INDEX IF NOT EXISTS idx_env_var_keys_app ON env_var_keys(app_id);
