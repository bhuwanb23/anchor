# Layer 5C — Database (SQLite): Complete Plan

---

## What Layer 5C Actually Is

```
Layer 5C is the control plane's memory.

Everything that must survive a restart lives here.
Everything the dashboard reads comes from here.
Everything the agent reports gets stored here.

SQLite is the right choice for MVP because:
  → Zero infrastructure (no separate database server)
  → Single file (easy backup, easy migration)
  → Faster than PostgreSQL for single-process read-heavy workloads
  → Go has excellent SQLite support via modernc.org/sqlite (pure Go)
  → When you outgrow it: migrate to PostgreSQL later
    (SQL is SQL — most queries work unchanged)

One database file:
  /var/lib/yourplatform/control-plane/yourplatform.db

That single file contains everything.
```

---

## SQLite Configuration for Production Use

```
Before any schema, configure SQLite correctly.
Default SQLite settings are for embedded use, not server use.

Required PRAGMAs on every connection open:

PRAGMA journal_mode = WAL;
  → Write-Ahead Logging
  → Readers do not block writers
  → Writers do not block readers
  → Multiple concurrent reads with one write = no contention
  → Essential for a web server with concurrent requests
  → Default (DELETE mode) would serialize everything

PRAGMA foreign_keys = ON;
  → SQLite has foreign keys but they are OFF by default
  → Must enable on every connection
  → Without this: orphaned records accumulate silently

PRAGMA busy_timeout = 5000;
  → If database is locked: wait up to 5 seconds before returning error
  → Without this: concurrent writes return SQLITE_BUSY immediately
  → 5 seconds is enough for any write to complete

PRAGMA synchronous = NORMAL;
  → Default is FULL (fsync after every write)
  → NORMAL: fsync less often, much faster, safe with WAL mode
  → Only lose data if OS crashes (not just app crash)
  → Acceptable for this use case

PRAGMA cache_size = -64000;
  → 64MB page cache in memory
  → Negative value = kilobytes
  → Speeds up repeated reads significantly
  → SQLite keeps hot pages in memory

PRAGMA temp_store = MEMORY;
  → Temporary tables in memory instead of disk
  → Faster sorting, joining

Connection pool settings:
  → MaxOpenConns: 1 for writes (SQLite allows one writer)
  → MaxOpenConns: 10 for reads (WAL allows concurrent readers)
  → Separate read and write connection pools
  → Write operations use the write connection (serialized)
  → Read operations use the read pool (concurrent)
```

---

## Step 1 — Database Initialization

### Step 1A — File Location and Permissions

```
Database file: /var/lib/yourplatform/control-plane/yourplatform.db

Directory creation on first start:
  → Create /var/lib/yourplatform/control-plane/ if not exists
  → Permissions: 700 (only the control plane process can read/write)
  → Owner: the user the control plane runs as

File permissions after creation:
  → 600 (owner read/write only)
  → The database contains user passwords (hashed) and agent secrets (hashed)
  → No other process should be able to read it

WAL files (created automatically by SQLite):
  → yourplatform.db-wal (write-ahead log)
  → yourplatform.db-shm (shared memory file)
  → These are normal and expected with WAL mode
  → Do not delete them while the database is in use
  → Include them in backups (back up all three files together)
```

### Step 1B — Connection Pool Setup

```
Two connection pools:

Write pool:
  → Single connection (SQLite allows only one writer at a time)
  → All INSERT, UPDATE, DELETE go through this connection
  → Serialized: no two writes happen simultaneously
  → This is correct and safe

Read pool:
  → Up to 10 connections
  → All SELECT queries go through the read pool
  → Concurrent reads are fine with WAL mode
  → Each HTTP request gets a read connection from the pool

How the query layer uses this:
  → db.Write(func) → acquires write connection, runs function, releases
  → db.Read(func)  → acquires read connection, runs function, releases

Why not just one pool:
  → One pool with MaxOpenConns=1: all reads and writes serialize
  → With a web server: slow read query blocks all writes
  → Separate pools: reads run concurrently, writes are isolated
```

### Step 1C — Migration System

```
Migrations are SQL files embedded in the Go binary.
On startup: run any unapplied migrations in order.

Migration file naming:
  0001_initial_users.sql
  0002_servers.sql
  0003_deployments.sql
  0004_backups.sql
  0005_alerts.sql
  0006_teams.sql
  0007_commands.sql
  0008_metrics.sql

Migration tracking table (created before any migrations run):
CREATE TABLE IF NOT EXISTS schema_migrations (
    version     INTEGER PRIMARY KEY,
    name        TEXT NOT NULL,
    applied_at  TEXT NOT NULL
);

Migration runner on startup:
  1. Create schema_migrations if not exists
  2. Read current highest applied version
  3. Read all migration files from embedded filesystem
  4. For each migration with version > current:
     → Run in a transaction
     → If it fails: rollback, log error, stop (do not apply partial migrations)
     → If it succeeds: insert record into schema_migrations, commit
  5. Log: "Applied N migrations"

Transactions for migrations:
  → Each migration wrapped in BEGIN/COMMIT
  → If migration fails partway: entire migration is rolled back
  → Database never left in a half-migrated state
  → The migration version number is only recorded if it fully succeeds
```

### Done Condition for Step 1
```
□ Database file created with correct permissions on first start
□ WAL mode enabled and verified
□ All PRAGMAs applied on connection open
□ Separate read and write connection pools
□ Migration system runs on startup
□ Applied migrations are recorded in schema_migrations
□ Failed migration rolls back and stops startup
□ Second startup does not re-run already applied migrations
□ Database file survives control plane restart
```

---

## Step 2 — Complete Schema

### Migration 0001 — Users

```sql
-- 0001_initial_users.sql

CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

CREATE INDEX idx_users_email ON users(email);

-- Password reset tokens
CREATE TABLE password_resets (
    id          TEXT PRIMARY KEY,
    token_hash  TEXT NOT NULL UNIQUE,
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TEXT NOT NULL,
    expires_at  TEXT NOT NULL,
    used_at     TEXT
);

CREATE INDEX idx_password_resets_user ON password_resets(user_id);

-- Refresh tokens (browser sessions)
CREATE TABLE refresh_tokens (
    id           TEXT PRIMARY KEY,
    token_hash   TEXT NOT NULL UNIQUE,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at   TEXT NOT NULL,
    expires_at   TEXT NOT NULL,
    last_used_at TEXT,
    user_agent   TEXT,
    ip_address   TEXT,
    revoked_at   TEXT
);

CREATE INDEX idx_refresh_tokens_user    ON refresh_tokens(user_id);
CREATE INDEX idx_refresh_tokens_hash    ON refresh_tokens(token_hash);
CREATE INDEX idx_refresh_tokens_expires ON refresh_tokens(expires_at);
```

### Migration 0002 — Teams

```sql
-- 0002_teams.sql

CREATE TABLE teams (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    owner_id   TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX idx_teams_owner ON teams(owner_id);

CREATE TABLE team_members (
    id         TEXT PRIMARY KEY,
    team_id    TEXT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       TEXT NOT NULL DEFAULT 'member'
                   CHECK (role IN ('owner', 'member')),
    invited_by TEXT REFERENCES users(id),
    joined_at  TEXT NOT NULL,

    UNIQUE (team_id, user_id)
);

CREATE INDEX idx_team_members_team ON team_members(team_id);
CREATE INDEX idx_team_members_user ON team_members(user_id);

CREATE TABLE invitations (
    id          TEXT PRIMARY KEY,
    team_id     TEXT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    email       TEXT NOT NULL,
    role        TEXT NOT NULL DEFAULT 'member'
                    CHECK (role IN ('owner', 'member')),
    token_hash  TEXT NOT NULL UNIQUE,
    invited_by  TEXT NOT NULL REFERENCES users(id),
    created_at  TEXT NOT NULL,
    expires_at  TEXT NOT NULL,
    accepted_at TEXT
);

CREATE INDEX idx_invitations_team  ON invitations(team_id);
CREATE INDEX idx_invitations_email ON invitations(email);
```

### Migration 0003 — Servers

```sql
-- 0003_servers.sql

CREATE TABLE servers (
    id              TEXT PRIMARY KEY,
    team_id         TEXT NOT NULL REFERENCES teams(id) ON DELETE RESTRICT,
    name            TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending'
                        CHECK (status IN (
                            'pending',
                            'connected',
                            'disconnected',
                            'updating',
                            'error'
                        )),

    -- Agent credentials (stored as hashes)
    agent_id        TEXT UNIQUE,
    agent_secret_hash TEXT,

    -- Connection info
    public_ip       TEXT,
    last_seen       TEXT,
    agent_version   TEXT,

    -- Hardware info (populated after registration)
    os              TEXT,
    os_version      TEXT,
    arch            TEXT,
    cpu_cores       INTEGER,
    ram_total_mb    INTEGER,
    disk_total_gb   INTEGER,

    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);

CREATE INDEX idx_servers_team     ON servers(team_id);
CREATE INDEX idx_servers_agent_id ON servers(agent_id);
CREATE INDEX idx_servers_status   ON servers(status);

-- Registration tokens (used once during install)
CREATE TABLE server_registration_tokens (
    id          TEXT PRIMARY KEY,
    token_hash  TEXT NOT NULL UNIQUE,
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    server_name TEXT,
    created_at  TEXT NOT NULL,
    expires_at  TEXT NOT NULL,
    used_at     TEXT,
    used_by_ip  TEXT
);

CREATE INDEX idx_reg_tokens_user    ON server_registration_tokens(user_id);
CREATE INDEX idx_reg_tokens_expires ON server_registration_tokens(expires_at);
```

### Migration 0004 — Deployments and Apps

```sql
-- 0004_deployments.sql

-- Current state of each app on each server
CREATE TABLE apps (
    id            TEXT PRIMARY KEY,
    server_id     TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    project_name  TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'stopped'
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
    platform_domain     TEXT,      -- myshop.srv-abc.yourplatform.app
    custom_domains      TEXT,      -- JSON array of custom domains

    -- Resource config
    memory_limit_mb     INTEGER DEFAULT 512,
    cpu_quota_percent   INTEGER DEFAULT 50,

    -- Port the app listens on inside the container
    app_port            INTEGER DEFAULT 3000,

    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL,

    UNIQUE (server_id, project_name)
);

CREATE INDEX idx_apps_server  ON apps(server_id);
CREATE INDEX idx_apps_status  ON apps(status);

-- History of every deployment attempt
CREATE TABLE deployments (
    id            TEXT PRIMARY KEY,
    app_id        TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    server_id     TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    project_name  TEXT NOT NULL,

    -- What was deployed
    image         TEXT NOT NULL,
    image_digest  TEXT,           -- sha256 of the pulled image

    -- Result
    status        TEXT NOT NULL DEFAULT 'pending'
                      CHECK (status IN (
                          'pending',
                          'in_progress',
                          'success',
                          'failed',
                          'rolled_back'
                      )),
    error         TEXT,           -- error message if failed

    -- Timing
    started_at    TEXT,
    completed_at  TEXT,
    duration_ms   INTEGER,

    -- Who triggered it
    triggered_by  TEXT REFERENCES users(id),
    triggered_at  TEXT NOT NULL,

    -- The domain when this was deployed (for reference)
    domain        TEXT,

    created_at    TEXT NOT NULL
);

CREATE INDEX idx_deployments_app     ON deployments(app_id);
CREATE INDEX idx_deployments_server  ON deployments(server_id);
CREATE INDEX idx_deployments_status  ON deployments(status);
CREATE INDEX idx_deployments_created ON deployments(created_at);

-- Container statuses (updated by health reports from agent)
CREATE TABLE container_states (
    id              TEXT PRIMARY KEY,
    server_id       TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    project_name    TEXT NOT NULL,
    role            TEXT NOT NULL,   -- app, postgres, redis, mysql

    container_id    TEXT,
    status          TEXT NOT NULL,   -- running, stopped, exited, restarting
    health          TEXT,            -- healthy, unhealthy, starting, null
    exit_code       INTEGER,
    restart_count   INTEGER DEFAULT 0,
    oom_killed      INTEGER DEFAULT 0,  -- boolean

    -- Resource usage (from last health report)
    cpu_percent     REAL DEFAULT 0,
    ram_used_mb     INTEGER DEFAULT 0,
    ram_limit_mb    INTEGER DEFAULT 0,

    -- Timing
    started_at      TEXT,
    updated_at      TEXT NOT NULL,

    UNIQUE (server_id, project_name, role)
);

CREATE INDEX idx_container_states_server  ON container_states(server_id);
CREATE INDEX idx_container_states_project ON container_states(project_name);

-- Databases provisioned for projects
CREATE TABLE project_databases (
    id           TEXT PRIMARY KEY,
    app_id       TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    server_id    TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    project_name TEXT NOT NULL,
    db_type      TEXT NOT NULL CHECK (db_type IN ('postgres', 'mysql', 'redis')),
    db_version   TEXT,
    db_name      TEXT,
    status       TEXT NOT NULL DEFAULT 'running',
    created_at   TEXT NOT NULL,

    UNIQUE (server_id, project_name, db_type)
);

CREATE INDEX idx_project_databases_app    ON project_databases(app_id);
CREATE INDEX idx_project_databases_server ON project_databases(server_id);
```

### Migration 0005 — Commands

```sql
-- 0005_commands.sql

CREATE TABLE commands (
    id          TEXT PRIMARY KEY,
    server_id   TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,

    -- Command details
    type        TEXT NOT NULL,    -- deploy, restart, rollback, etc.
    payload     TEXT NOT NULL,    -- JSON

    -- Lifecycle
    status      TEXT NOT NULL DEFAULT 'queued'
                    CHECK (status IN (
                        'queued',
                        'sent',
                        'in_progress',
                        'success',
                        'failed',
                        'timeout',
                        'expired'
                    )),

    -- Result
    result      TEXT,             -- JSON result from agent
    error       TEXT,

    -- Timing
    issued_at   TEXT NOT NULL,
    expires_at  TEXT NOT NULL,
    sent_at     TEXT,             -- when hub sent to agent
    completed_at TEXT,

    -- Who issued it
    issued_by   TEXT REFERENCES users(id),

    created_at  TEXT NOT NULL
);

CREATE INDEX idx_commands_server  ON commands(server_id);
CREATE INDEX idx_commands_status  ON commands(status);
CREATE INDEX idx_commands_issued  ON commands(issued_at);
CREATE INDEX idx_commands_type    ON commands(type);
```

### Migration 0006 — Metrics

```sql
-- 0006_metrics.sql

-- Server metrics history (from health reports)
-- Kept for 7 days at 30-second granularity
-- Kept for 30 days at hourly granularity (rolled up)
-- Kept for 12 months at daily granularity (rolled up)

CREATE TABLE server_metrics (
    id            TEXT PRIMARY KEY,
    server_id     TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    recorded_at   TEXT NOT NULL,

    -- Server level
    cpu_percent   REAL,
    ram_used_mb   INTEGER,
    ram_total_mb  INTEGER,
    disk_used_gb  REAL,
    disk_total_gb REAL,
    load_1min     REAL,

    granularity   TEXT NOT NULL DEFAULT 'raw'
                      CHECK (granularity IN ('raw', 'hourly', 'daily'))
);

CREATE INDEX idx_server_metrics_server  ON server_metrics(server_id);
CREATE INDEX idx_server_metrics_time    ON server_metrics(recorded_at);
CREATE INDEX idx_server_metrics_gran    ON server_metrics(server_id, granularity, recorded_at);

-- Latest metrics snapshot per server (for instant dashboard load)
-- Updated on every health report
-- SELECT is O(1) with this table
CREATE TABLE server_metrics_latest (
    server_id     TEXT PRIMARY KEY REFERENCES servers(id) ON DELETE CASCADE,
    recorded_at   TEXT NOT NULL,
    cpu_percent   REAL,
    ram_used_mb   INTEGER,
    ram_total_mb  INTEGER,
    disk_used_gb  REAL,
    disk_total_gb REAL,
    load_1min     REAL
);
```

### Migration 0007 — Backups

```sql
-- 0007_backups.sql

CREATE TABLE backups (
    id                  TEXT PRIMARY KEY,
    server_id           TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    restic_snapshot_id  TEXT NOT NULL,

    status              TEXT NOT NULL DEFAULT 'running'
                            CHECK (status IN (
                                'running',
                                'success',
                                'partial',
                                'failed'
                            )),

    -- Size stats
    size_new_bytes      INTEGER,
    size_total_bytes    INTEGER,

    -- Per-project results
    project_results     TEXT,     -- JSON

    -- Verification
    verified            INTEGER DEFAULT 0,  -- boolean
    verified_at         TEXT,

    -- Error info
    error               TEXT,

    -- Timing
    started_at          TEXT NOT NULL,
    completed_at        TEXT,
    duration_seconds    INTEGER,

    -- Who triggered it (null = scheduled)
    triggered_by        TEXT REFERENCES users(id),

    created_at          TEXT NOT NULL
);

CREATE INDEX idx_backups_server  ON backups(server_id);
CREATE INDEX idx_backups_status  ON backups(status);
CREATE INDEX idx_backups_created ON backups(created_at);

-- Restore history
CREATE TABLE restores (
    id           TEXT PRIMARY KEY,
    server_id    TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    backup_id    TEXT NOT NULL REFERENCES backups(id),
    project_name TEXT NOT NULL,
    snapshot_id  TEXT NOT NULL,

    status       TEXT NOT NULL DEFAULT 'running'
                     CHECK (status IN ('running', 'success', 'failed')),
    error        TEXT,

    started_at   TEXT NOT NULL,
    completed_at TEXT,
    duration_seconds INTEGER,

    triggered_by TEXT REFERENCES users(id),
    created_at   TEXT NOT NULL
);

CREATE INDEX idx_restores_server ON restores(server_id);
CREATE INDEX idx_restores_backup ON restores(backup_id);

-- Backup schedule per server
CREATE TABLE backup_schedules (
    server_id       TEXT PRIMARY KEY REFERENCES servers(id) ON DELETE CASCADE,
    enabled         INTEGER NOT NULL DEFAULT 1,  -- boolean
    hour_utc        INTEGER NOT NULL DEFAULT 2,
    last_backup_at  TEXT,
    next_backup_at  TEXT,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);
```

### Migration 0008 — Alerts

```sql
-- 0008_alerts.sql

CREATE TABLE alerts (
    id           TEXT PRIMARY KEY,
    server_id    TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    project_name TEXT,            -- null for server-level alerts

    severity     TEXT NOT NULL CHECK (severity IN ('warning', 'critical')),
    type         TEXT NOT NULL,   -- disk_warning, container_crash, etc.
    status       TEXT NOT NULL DEFAULT 'active'
                     CHECK (status IN ('active', 'resolved', 'acknowledged')),

    -- Human-readable content
    title        TEXT NOT NULL,
    message      TEXT NOT NULL,
    detail       TEXT,
    action       TEXT,

    -- Relevant metrics at time of alert
    metrics      TEXT,            -- JSON

    -- Timing
    fired_at     TEXT NOT NULL,
    resolved_at  TEXT,
    acknowledged_at TEXT,
    acknowledged_by TEXT REFERENCES users(id),

    created_at   TEXT NOT NULL
);

CREATE INDEX idx_alerts_server   ON alerts(server_id);
CREATE INDEX idx_alerts_status   ON alerts(status);
CREATE INDEX idx_alerts_severity ON alerts(severity);
CREATE INDEX idx_alerts_fired    ON alerts(fired_at);

-- Alert delivery log (what was sent where)
CREATE TABLE alert_deliveries (
    id         TEXT PRIMARY KEY,
    alert_id   TEXT NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
    channel    TEXT NOT NULL,     -- email, whatsapp, dashboard
    status     TEXT NOT NULL,     -- sent, failed, suppressed
    sent_at    TEXT,
    error      TEXT,
    created_at TEXT NOT NULL
);

CREATE INDEX idx_alert_deliveries_alert ON alert_deliveries(alert_id);
```

### Migration 0009 — Server Events

```sql
-- 0009_server_events.sql

-- Audit log and timeline of everything that happened on a server
CREATE TABLE server_events (
    id           TEXT PRIMARY KEY,
    server_id    TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    project_name TEXT,

    event_type   TEXT NOT NULL,
    -- Examples:
    -- agent_connected, agent_disconnected
    -- deploy_started, deploy_success, deploy_failed
    -- rollback_started, rollback_success
    -- backup_started, backup_success, backup_failed
    -- restore_started, restore_success, restore_failed
    -- alert_fired, alert_resolved
    -- container_crashed, container_oom
    -- docker_auto_installed
    -- domain_added, domain_verified
    -- agent_updated

    title        TEXT NOT NULL,   -- "Deploy of myshop started"
    detail       TEXT,            -- additional context

    -- Who triggered it (null for automatic events)
    actor_id     TEXT REFERENCES users(id),
    actor_type   TEXT,            -- user, agent, system

    -- Related records
    related_id   TEXT,            -- deployment_id, backup_id, etc.
    related_type TEXT,            -- deployment, backup, restore, alert

    occurred_at  TEXT NOT NULL,
    created_at   TEXT NOT NULL
);

CREATE INDEX idx_server_events_server  ON server_events(server_id);
CREATE INDEX idx_server_events_type    ON server_events(event_type);
CREATE INDEX idx_server_events_time    ON server_events(occurred_at);
CREATE INDEX idx_server_events_project ON server_events(project_name);
```

### Migration 0010 — Env Var Keys (not values)

```sql
-- 0010_env_var_keys.sql

-- Store KEYS only, never values
-- Values live on the server in /etc/yourplatform/envs/
CREATE TABLE env_var_keys (
    id           TEXT PRIMARY KEY,
    app_id       TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    server_id    TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    key_name     TEXT NOT NULL,
    is_auto      INTEGER NOT NULL DEFAULT 0,  -- auto-generated (DATABASE_URL)
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,

    UNIQUE (app_id, key_name)
);

CREATE INDEX idx_env_var_keys_app ON env_var_keys(app_id);
```

---

## Step 3 — Query Layer

### How the Query Layer Is Organized

```
One file per domain area:

control-plane/internal/db/queries/
├── users.go
├── teams.go
├── servers.go
├── apps.go
├── deployments.go
├── commands.go
├── metrics.go
├── backups.go
├── alerts.go
└── events.go

Each file contains:
  → Struct definitions for that domain
  → Functions to create, read, update, delete records
  → No business logic — pure database operations
  → No HTTP logic — just data access
```

### Step 3A — Query Patterns

```
Standard patterns used throughout the query layer:

Pattern 1: Get by ID
  func GetUserByID(db *sql.DB, id string) (*User, error)
  → SELECT * FROM users WHERE id = ?
  → Returns nil, nil if not found
  → Returns nil, err if database error
  → Returns user, nil if found

Pattern 2: Get by unique field
  func GetUserByEmail(db *sql.DB, email string) (*User, error)
  → SELECT * FROM users WHERE email = LOWER(?)
  → Same return conventions as Get by ID

Pattern 3: List with filters
  func ListServersByTeam(db *sql.DB, teamID string) ([]*Server, error)
  → SELECT * FROM servers WHERE team_id = ? ORDER BY created_at DESC
  → Returns empty slice (not nil) if no results
  → Never returns nil slice

Pattern 4: Create
  func CreateUser(db *sql.DB, user *User) error
  → INSERT INTO users (id, email, ...) VALUES (?, ?, ...)
  → Returns nil on success
  → Returns error on constraint violation or db error

Pattern 5: Update
  func UpdateServerStatus(db *sql.DB, serverID, status string) error
  → UPDATE servers SET status = ?, updated_at = ? WHERE id = ?
  → Returns nil if updated (even if no rows changed)
  → Returns error only on database error

Pattern 6: Upsert
  func UpsertContainerState(db *sql.DB, state *ContainerState) error
  → INSERT INTO container_states (...) VALUES (...)
    ON CONFLICT (server_id, project_name, role) DO UPDATE SET ...
  → Used for metrics and state that are updated frequently

Pattern 7: Delete
  func DeleteRefreshToken(db *sql.DB, tokenHash string) error
  → DELETE FROM refresh_tokens WHERE token_hash = ?

Pattern 8: Exists check
  func EmailExists(db *sql.DB, email string) (bool, error)
  → SELECT COUNT(*) FROM users WHERE email = ?
  → Returns true if count > 0
```

### Step 3B — Transaction Helper

```
For operations that need multiple writes to be atomic:

func WithTransaction(db *sql.DB, fn func(*sql.Tx) error) error
  → BEGIN
  → Call fn with the transaction
  → If fn returns nil: COMMIT
  → If fn returns error: ROLLBACK, return error

Usage example:
  err := db.WithTransaction(db, func(tx *sql.Tx) error {
    if err := createUser(tx, user); err != nil {
      return err  // triggers rollback
    }
    if err := createTeam(tx, team); err != nil {
      return err  // triggers rollback
    }
    if err := createTeamMember(tx, member); err != nil {
      return err  // triggers rollback
    }
    return nil  // triggers commit
  })

Transactions used for:
  → User registration (create user + create team + create team_member)
  → Server registration (create server + mark token used)
  → Deployment record creation (create deployment + update app)
  → Any operation touching more than one table
```

### Step 3C — Specific Query Designs

```
Queries that need careful design:

1. Get current server state (for dashboard load)
   → Needs: server info + latest metrics + container states + active alerts
   → Option A: 4 separate queries, assemble in Go
   → Option B: one query with JOINs
   → Choice: Option A (simpler, easier to read, fast enough for SQLite)

2. List deployments for an app (most recent first, limit 20)
   SELECT * FROM deployments
   WHERE app_id = ?
   ORDER BY triggered_at DESC
   LIMIT 20

3. Get active alerts for a server
   SELECT * FROM alerts
   WHERE server_id = ?
   AND status = 'active'
   ORDER BY fired_at DESC

4. Upsert server metrics latest (runs every 30 seconds per server)
   INSERT INTO server_metrics_latest
     (server_id, recorded_at, cpu_percent, ram_used_mb, ...)
   VALUES (?, ?, ?, ?, ...)
   ON CONFLICT (server_id) DO UPDATE SET
     recorded_at = excluded.recorded_at,
     cpu_percent = excluded.cpu_percent,
     ram_used_mb = excluded.ram_used_mb,
     ...

5. Insert raw metric + check if rollup needed
   → Insert into server_metrics with granularity='raw'
   → Rollup runs separately (Step 4)

6. Get commands queued for a server (for hello_ack)
   SELECT * FROM commands
   WHERE server_id = ?
   AND status = 'queued'
   AND expires_at > datetime('now')
   ORDER BY issued_at ASC

7. Check team membership for a user + server
   SELECT tm.role FROM team_members tm
   JOIN servers s ON s.team_id = tm.team_id
   WHERE s.id = ? AND tm.user_id = ?
   → Returns role if member, empty if not
   → Used in permission checks
```

### Done Condition for Step 3
```
□ All query functions follow consistent return conventions
□ Not found returns nil, nil (not an error)
□ Empty list returns empty slice, not nil
□ Transaction helper correctly commits and rolls back
□ Upsert queries use correct SQLite ON CONFLICT syntax
□ Queries use parameterized statements (no string concatenation)
□ No business logic in query layer
□ Each query file handles one domain
□ All queries tested with actual SQLite (not mocked)
```

---

## Step 4 — Data Lifecycle Management

### Step 4A — Metrics Rollup

```
Raw metrics every 30 seconds = 2880 rows per server per day.
After 7 days = 20160 rows per server.
With 100 servers = 2 million rows.
SQLite handles this fine but queries get slow.

Rollup strategy:
  → Keep raw (30-second) data for 7 days
  → Aggregate into hourly averages, keep for 30 days
  → Aggregate into daily averages, keep for 12 months

Rollup runs once per hour (not continuous).

Hourly rollup query:
  INSERT INTO server_metrics
    (id, server_id, recorded_at, cpu_percent, ram_used_mb,
     disk_used_gb, load_1min, granularity)
  SELECT
    lower(hex(randomblob(16))),
    server_id,
    strftime('%Y-%m-%dT%H:00:00Z', recorded_at) as hour,
    AVG(cpu_percent),
    AVG(ram_used_mb),
    AVG(disk_used_gb),
    AVG(load_1min),
    'hourly'
  FROM server_metrics
  WHERE granularity = 'raw'
    AND recorded_at < datetime('now', '-1 hour')
    AND recorded_at >= datetime('now', '-8 days')
  GROUP BY server_id, hour
  ON CONFLICT DO NOTHING;

Delete old raw data:
  DELETE FROM server_metrics
  WHERE granularity = 'raw'
  AND recorded_at < datetime('now', '-7 days');

Daily rollup (same pattern but grouping by day from hourly data):
  → Runs once per day
  → Groups hourly data into daily averages
  → Deletes hourly data older than 30 days
```

### Step 4B — Cleanup Jobs

```
Scheduled cleanup queries that run periodically:

Every hour:
  → Delete expired registration tokens:
    DELETE FROM server_registration_tokens
    WHERE expires_at < datetime('now')
    AND used_at IS NULL;

  → Delete expired password resets:
    DELETE FROM password_resets
    WHERE expires_at < datetime('now')
    AND used_at IS NULL;

  → Delete expired refresh tokens:
    DELETE FROM refresh_tokens
    WHERE expires_at < datetime('now');

  → Delete old queued commands that have expired:
    UPDATE commands SET status = 'expired'
    WHERE status = 'queued'
    AND expires_at < datetime('now');

  → Run metrics rollup

Every day:
  → Delete commands older than 30 days:
    DELETE FROM commands
    WHERE created_at < datetime('now', '-30 days');

  → Delete server events older than 90 days:
    DELETE FROM server_events
    WHERE occurred_at < datetime('now', '-90 days');

  → Delete alert deliveries older than 30 days:
    DELETE FROM alert_deliveries
    WHERE created_at < datetime('now', '-30 days');

  → VACUUM (reclaim space after deletions):
    PRAGMA incremental_vacuum(1000);
    → Incremental vacuum: reclaims 1000 pages at a time
    → Does not lock the database for long periods
    → Run daily to keep file size manageable

Cleanup goroutine:
  → Runs in background
  → Wakes up every hour
  → Runs the hourly queries
  → At midnight: runs the daily queries
  → Logs how many rows were deleted in each run
```

### Step 4C — Database File Size Management

```
Monitor the database file size:

After each daily cleanup:
  → Check file size: stat /var/lib/yourplatform/control-plane/yourplatform.db
  → Log: "Database size: 45MB"
  → If size > 500MB: log warning

What contributes to size:
  → server_metrics: largest table by far
  → server_events: grows continuously
  → commands: large payloads (deployment configs)
  → Cleanup jobs handle all of these

Expected sizes at MVP scale (10 servers):
  → After 1 week: ~50MB
  → After 1 month: ~100MB (rollup compresses old metrics)
  → After 1 year: ~200MB
  → SQLite handles all of this comfortably
```

### Done Condition for Step 4
```
□ Raw metrics older than 7 days are rolled up and deleted
□ Hourly metrics older than 30 days are rolled up and deleted
□ Daily metrics older than 12 months are deleted
□ Expired tokens cleaned up every hour
□ Old commands cleaned up every day
□ Old server events cleaned up every day
□ Incremental VACUUM runs daily
□ Database file size is logged after daily cleanup
□ Cleanup goroutine starts with control plane and runs on schedule
□ Cleanup failure logs error but does not stop the control plane
```

---

## Step 5 — Backup of the Database

### The Database IS the Platform

```
If the database file is lost:
  → All user accounts gone
  → All server records gone
  → All deployment history gone
  → Users cannot log in
  → Agents cannot reconnect (agent_id references deleted server records)

The database must be backed up.
This is different from user data backups (Layer 3C).
This is backing up the platform itself.
```

### Step 5A — SQLite Backup Approach

```
SQLite has a built-in backup API.
It creates a consistent copy of the database
while it is running (no need to stop writes).

How it works:
  → SQLite backup API reads pages from source to destination
  → If a write happens during backup: that page is re-read
  → Result: a consistent snapshot at the end of the backup
  → No locks held during backup (writes continue normally)

Backup destination:
  → Primary: local file backup (second copy on same server)
    /var/lib/yourplatform/control-plane/yourplatform.db.backup
    → Protects against: file corruption, accidental deletion
    → Does NOT protect against: server loss

  → Secondary: S3-compatible storage
    → Same bucket used for server backups
    → Path: yourplatform-backups/control-plane/yourplatform-{date}.db
    → Protects against: server loss

Backup schedule:
  → Every 6 hours: local backup
  → Every 24 hours: upload to S3

Local backup process:
  1. Use SQLite backup API to create consistent copy
  2. Compress with gzip: yourplatform.db.backup.gz
  3. Keep last 4 local backups (24 hours of history locally)
  4. Delete older ones

S3 backup process:
  1. Create consistent copy via SQLite backup API
  2. Compress with gzip
  3. Encrypt (same restic approach or simple AES-256)
  4. Upload to S3
  5. Keep last 30 daily backups in S3
```

### Step 5B — Recovery from Database Loss

```
If the database file is lost or corrupted:

Step 1: Stop the control plane

Step 2: Restore from backup
  → From local backup: cp yourplatform.db.backup.gz → decompress → done
  → From S3: download latest, decrypt, decompress

Step 3: Verify database is readable
  → sqlite3 yourplatform.db "SELECT COUNT(*) FROM users;"

Step 4: Start control plane

Step 5: Agents reconnect (they still have their agent_id and secret)
  → Server records still exist in restored database
  → Authentication works immediately
  → No re-registration needed

Step 6: Dashboard works normally
  → Users log in normally
  → Some recent data may be missing (up to 6 hours)
  → Deployment history may be incomplete
  → Server metrics history will have a gap
```

### Done Condition for Step 5
```
□ SQLite backup API used (not file copy while running)
□ Local backup created every 6 hours
□ Local backup is compressed
□ 4 local backups retained (older deleted)
□ S3 backup created every 24 hours
□ S3 backup is compressed and encrypted
□ 30 S3 backups retained
□ Recovery process documented and tested
□ After restore: agents reconnect successfully
□ After restore: users can log in normally
```

---

## Layer 5C Overall Done Condition

```
The full test sequence:

Test 1 — Fresh start:
  □ Control plane starts with no database file
  □ Database file created with correct permissions
  □ All migrations run in order
  □ schema_migrations table shows all migrations applied
  □ Control plane ready to accept requests

Test 2 — Second start (no re-migration):
  □ Control plane restarts
  □ Migrations check runs
  □ No migrations re-applied
  □ Startup is fast (< 1 second for migration check)

Test 3 — Data integrity:
  □ Create a user
  □ Create a server linked to a team
  □ Delete the user
  □ Server deletion is blocked (RESTRICT on owner_id)
    OR cascades correctly depending on your design choice
  □ Foreign key violations are caught
  □ Orphaned records cannot be created

Test 4 — Concurrent access:
  □ 10 simultaneous read requests complete successfully
  □ Simultaneous read and write complete without SQLITE_BUSY error
  □ WAL mode confirmed active (PRAGMA journal_mode returns 'wal')

Test 5 — Metrics lifecycle:
  □ Insert 100 raw metric rows
  □ Rollup runs
  □ Hourly averages created correctly
  □ Raw rows older than 7 days deleted
  □ Hourly rows older than 30 days deleted
  □ Latest metrics table always has most recent values

Test 6 — Cleanup jobs:
  □ Insert expired registration token
  □ Run cleanup
  □ Token is deleted
  □ Insert expired refresh token
  □ Run cleanup
  □ Token is deleted

Test 7 — Database backup:
  □ Create some data
  □ Run local backup while control plane is writing
  □ Backup completes without error
  □ Backup file is a valid SQLite database
  □ sqlite3 backup.db "SELECT COUNT(*) FROM users" returns correct count

Test 8 — Full recovery:
  □ Note current data (user count, server count)
  □ Delete the database file
  □ Restore from backup
  □ Start control plane
  □ User count and server count match what was noted
  □ Agents reconnect without re-registration
  □ Users log in successfully

When all 8 tests pass, Layer 5C is done.
Move to Layer 6 — Control Plane API.
```

---

## What Layer 5C Does NOT Do

```
Layer 5C does not:
├── Validate business rules (Layer 6 handlers do)
├── Enforce permissions (Layer 5A does)
├── Cache data in memory (queries always read from SQLite)
│   SQLite's own page cache handles performance
├── Stream data to clients (Layer 5B does)
├── Send notifications (Layer 6 / notification service does)
├── Back up user data on servers (Layer 3C does)
└── Manage multiple database files
    One file. One database. Everything in it.

Layer 5C is pure data.
It stores, it retrieves, it cleans up.
All decisions about what to store and when are made by other layers.
```

---

**Ready for Layer 6 — Control Plane API?**