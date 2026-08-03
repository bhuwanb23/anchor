# Layer 3C — Restic Backups: Complete Plan

---

## What Layer 3C Actually Is

```
Layer 3C is the agent's safety net.

Everything else in the system handles the happy path.
Layer 3C handles the question every non-technical user
actually cares about:

"If something goes wrong, can I get my data back?"

The answer must be yes, always, without the user
having to think about it or set anything up.

Layer 3C owns:
  → Deciding what to back up (and what not to)
  → When to run backups
  → How to run them without disrupting running apps
  → Where to store them
  → Verifying backups are actually good
  → Restoring from a specific point in time
  → Telling the control plane what exists so the dashboard can show it
```

---

## The Mental Model

```
Every night at 2am (user's server time):

  Layer 3C wakes up
       │
       ▼
  For each project on this server:
       │
       ├── Dump the database to a file
       │     (Postgres dump, MySQL dump, Redis dump)
       │
       ├── Identify persistent volumes with user data
       │
       └── Hand everything to restic:
             "Take a snapshot of these files
              and push it to S3"
                    │
                    ▼
              Restic deduplicates
              Restic compresses
              Restic encrypts
              Restic pushes to S3
                    │
                    ▼
              Snapshot ID returned
                    │
                    ▼
  Agent records metadata in control plane:
  "Project myshop backed up at 2am,
   snapshot ID abc123, size 45MB"
                    │
                    ▼
  Dashboard shows:
  "Last backup: today at 2:04am — Restore"
```

---

## What Gets Backed Up and What Does Not

```
Backed up:
  ✓ Postgres database data (via pg_dump — consistent dump)
  ✓ MySQL database data (via mysqldump)
  ✓ Redis data (via BGSAVE + RDB file)
  ✓ Persistent volumes with user-uploaded files
    (WordPress uploads, user-generated content, etc.)
  ✓ Environment variables file for each project
    (/etc/yourplatform/envs/{project}.env)
  ✓ Caddy certificate storage
    (/var/lib/yourplatform/caddy/certificates/)

NOT backed up:
  ✗ Docker images (can be re-pulled from registry)
  ✗ The agent binary (can be re-downloaded)
  ✗ Caddy binary (can be re-downloaded)
  ✗ Container layers (ephemeral, recreated on deploy)
  ✗ Application code (lives in the Docker image, which comes from a registry)
  ✗ Agent config beyond env vars (regenerated on re-registration)

Why this distinction matters:
  Docker images can be gigabytes.
  Backing them up wastes S3 storage and backup time.
  They can always be re-pulled.
  The irreplaceable data is: the database content and uploaded files.
```

---

## Step 1 — Restic Setup

### What Restic Is and Why

```
Restic is a backup tool written in Go.
Single binary, no dependencies.
It does three critical things:

1. Deduplication
   → Only stores data that has changed since the last backup
   → A 1GB database that changes 10MB per day
     costs 1GB for the first backup, ~10MB for each subsequent one
   → Massive storage cost savings

2. Encryption
   → Every backup is encrypted before leaving the server
   → S3 bucket contains encrypted blobs — useless without the key
   → Even if S3 is breached: user data is safe

3. Content-addressable storage
   → Each snapshot references chunks by their hash
   → Restoring to any point in time is reliable
   → No partial or corrupted restores
```

### Step 1A — Restic Binary

```
Where it comes from:
  → Downloaded alongside the agent binary during install
  → Location: /usr/local/bin/yourplatform-restic
  → Version pinned (do not use whatever restic happens to be installed)
  → Checksum verified during install (same as agent binary)

Why not use system restic:
  → User might have an older version installed
  → Version differences change behavior
  → We need predictable, tested behavior
  → Pin the version, ship it ourselves

Minimum restic version: 0.16.0
  → Introduced improved performance and stability
  → Stable REST backend support
  → Good S3 compatibility
```

### Step 1B — Restic Repository

```
A restic repository is where all backups for a server are stored.

One repository per server (not per project):
  → All projects on the same server share one repository
  → Deduplication works across projects (shared Docker base layers, etc.)
  → Simpler management — one set of credentials per server
  → One encryption key per server

Repository location in S3:
  yourplatform-backups/
  └── servers/
      └── {server-id}/
          └── restic-repo/
                ← restic manages everything inside here

Repository structure (restic manages this internally):
  restic-repo/
  ├── config          ← repository metadata + encryption settings
  ├── data/           ← encrypted data chunks
  ├── index/          ← chunk index
  ├── keys/           ← encryption key storage
  ├── locks/          ← prevents concurrent writes
  └── snapshots/      ← snapshot metadata
```

### Step 1C — Repository Initialization

```
First-time setup for a new server:

1. Generate a repository encryption password
     → 32 random bytes, base64 encoded
     → This is the restic repository password
     → Stored encrypted in the agent config
     → Also stored in the control plane
       (so a server can be restored even if lost)
     → NEVER stored in plaintext in S3

2. Initialize the restic repository
     → Run: restic init
     → This creates the repository structure in S3
     → Repository is now ready to accept snapshots

3. Store the password securely:
     On the server:
       /etc/yourplatform/backup-password
       Permissions: 600 (root only)
     
     On the control plane:
       Encrypted with a platform-level key
       Stored in the backups table
       Required for restore operations from the dashboard

4. Verify repository is accessible:
     → Run: restic snapshots (lists existing snapshots)
     → Should return empty list on fresh repo
     → If this fails: S3 credentials or network issue
```

### Step 1D — S3 Destination Configuration

```
For MVP: single S3-compatible destination per server.

S3 credentials the agent needs:
  → S3 endpoint URL (for non-AWS: Cloudflare R2, Backblaze B2, etc.)
  → Access key ID
  → Secret access key
  → Bucket name
  → Region (for AWS S3) or left empty (for R2/B2)

Where these credentials come from:

Option A: Platform-provided storage (recommended for MVP)
  → You create one S3 bucket (Cloudflare R2 — zero egress fees)
  → You create per-server credentials (scoped to that server's prefix)
  → User does not need any S3 account
  → Included in the subscription

Option B: User-provided S3
  → User enters their own S3 credentials in the dashboard
  → Dashboard sends credentials to agent via WebSocket command
  → Agent stores them encrypted locally
  → More complex but user controls their storage

For MVP: implement both but default to platform-provided.
  → User gets zero-config backups immediately
  → Can switch to their own S3 in settings if they want

Credential storage on the server:
  /etc/yourplatform/backup-credentials
  Contents: KEY=VALUE format, like .env
  Permissions: 600
  Never logged, never sent back to control plane
```

### Done Condition for Step 1
```
□ Restic binary exists at expected path and correct version
□ Repository initialized successfully for a new server
□ Repository password generated, stored locally and in control plane
□ S3 credentials stored securely with 600 permissions
□ restic snapshots runs successfully and returns empty list
□ Repository inaccessible scenario produces clear error
□ Restic binary version is verified on agent startup
```

---

## Step 2 — What to Back Up: Data Collection

### The Backup Manifest

```
Before running restic, the agent builds a backup manifest:
A list of exactly what should be in this snapshot.

The manifest for one backup run looks like:

{
  "server_id": "srv-abc123",
  "timestamp": "2024-01-15T02:00:00Z",
  "projects": [
    {
      "name": "myshop",
      "components": [
        {
          "type": "postgres_dump",
          "container": "yourplatform_myshop_postgres",
          "dump_path": "/tmp/yourplatform/backups/myshop/postgres.dump",
          "database": "myshop"
        },
        {
          "type": "volume",
          "volume_name": "yourplatform_myshop_uploads",
          "mount_path": "/var/lib/docker/volumes/yourplatform_myshop_uploads/_data"
        },
        {
          "type": "env_file",
          "path": "/etc/yourplatform/envs/myshop.env"
        }
      ]
    },
    {
      "name": "myblog",
      "components": [...]
    }
  ],
  "platform_data": [
    {
      "type": "caddy_certificates",
      "path": "/var/lib/yourplatform/caddy/certificates/"
    }
  ]
}
```

### Step 2A — Database Dumps

```
Why dump instead of backing up the raw data files directly?

Raw Postgres data files:
  → Cannot be copied while Postgres is running (files are locked)
  → Even if copied, might be in an inconsistent state
  → Restoring requires exact same Postgres version

pg_dump output:
  → Consistent snapshot — taken at a single point in time
  → Postgres continues running during the dump
  → Restores on any compatible Postgres version
  → Human-readable (plain SQL format option)
  → Smaller than raw data files for most databases

Dump process for Postgres:
  1. Identify the Postgres container for this project
       → Container name: yourplatform_{project}_postgres
  2. Run pg_dump inside the container:
       → docker exec yourplatform_myshop_postgres
           pg_dump -U yourplatform -Fc myshop
           > /tmp/yourplatform/backups/myshop/postgres.dump
       → -Fc = custom format (compressed, fast restore)
       → Output goes to a temp file on the host
  3. Verify dump file is not empty and is valid:
       → Check file size > 0
       → Run pg_restore --list on the dump to verify it parses
  4. This temp file is what restic will include in the snapshot

Dump process for MySQL:
  → mysqldump --single-transaction --routines --triggers
  → --single-transaction = consistent dump without locking tables
  → Output to temp file same as Postgres

Dump process for Redis:
  → Send BGSAVE command to Redis: docker exec ... redis-cli BGSAVE
  → Wait for BGSAVE to complete: LASTSAVE returns new timestamp
  → Copy the RDB file from the container:
      docker cp yourplatform_{project}_redis:/data/dump.rdb
               /tmp/yourplatform/backups/{project}/redis.rdb
  → Verify file exists and has size > 0

Temp file location:
  /tmp/yourplatform/backups/{project}/{component}.dump
  → Created before backup, deleted after backup completes
  → If backup fails: files remain for debugging, cleaned up next run
```

### Step 2B — Volume Backup

```
Volumes that contain user-uploaded files need to be backed up directly.

How to identify which volumes need backup:
  → All volumes with label "yourplatform-backup=true"
  → This label is set by Layer 3A when creating volumes
    that are expected to contain user data
  → Database data volumes are NOT labeled for direct backup
    (they are backed up via dumps, not direct volume copy)
  → Uploaded file volumes ARE labeled for direct backup

Volume data location on the host:
  /var/lib/docker/volumes/{volume_name}/_data/

Restic can back this up directly — no need to copy to temp location.
Just point restic at the volume data directory.

Volumes that get direct backup:
  → WordPress uploads volume
  → Any generic persistent storage volume
  → User-defined persistent directories

Volumes that are excluded (backed up via dump instead):
  → yourplatform_{project}_postgres_data
  → yourplatform_{project}_mysql_data
  → yourplatform_{project}_redis_data
```

### Step 2C — Consistency During Backup

```
The challenge: backing up a live database and its app simultaneously.

If the database is being written to during backup:
  → The dump captures a consistent point-in-time snapshot
  → No problem — pg_dump handles this via MVCC (Postgres)
  → mysqldump handles this via --single-transaction

If files are being written to a volume during backup:
  → Restic captures what is there at the moment of each file read
  → For most file types (uploads, static content): this is fine
  → A partially-written file might be captured mid-write
  → For MVP: accept this limitation
  → Future: use filesystem snapshots (LVM, btrfs) for true consistency

The app does NOT need to be stopped during backup.
This is essential — stopping the app for backup is not acceptable.
```

### Done Condition for Step 2
```
□ pg_dump runs successfully for a live Postgres container
□ Dump file is verified (not empty, parseable)
□ mysqldump runs with --single-transaction
□ Redis BGSAVE completes and RDB file is captured
□ Volume data directories are identified correctly
□ Env files are included in backup manifest
□ Caddy certificates directory is included
□ Temp dump files are cleaned up after backup completes
□ App continues running throughout the entire backup process
```

---

## Step 3 — Running the Backup

### Step 3A — Backup Execution Flow

```
Full backup run sequence:

1. Acquire backup lock
     → Create /var/lib/yourplatform/backup.lock
     → If lock already exists: another backup is running, skip this run
     → Prevents overlapping backups

2. Notify control plane: backup starting
     → Status: "running"
     → Timestamp: now
     → Control plane updates dashboard: "Backup in progress..."

3. Build backup manifest (Step 2)
     → Identify all projects
     → Plan database dumps
     → Plan volume inclusions

4. Run database dumps (Step 2A)
     → For each project, dump each database
     → If a dump fails: record the failure, continue with other projects
     → Do not abort entire backup if one database dump fails

5. Assemble the include list for restic
     → Temp dump files
     → Volume data directories
     → Env files
     → Certificate directory

6. Run restic backup
     → Command: restic backup [list of paths] --tag [project tags]
     → Environment variables set for this run:
         RESTIC_REPOSITORY: s3:endpoint/bucket/servers/server-id/restic-repo
         RESTIC_PASSWORD: the repository password
         AWS_ACCESS_KEY_ID: S3 access key
         AWS_SECRET_ACCESS_KEY: S3 secret key
     → Restic streams progress as it runs
     → Agent captures progress output

7. Capture restic output
     → Restic prints: files changed, bytes added, snapshot ID
     → Parse the snapshot ID from the output
     → Parse the statistics (new data size, total size)

8. Verify the snapshot
     → Run: restic check --read-data-subset=1%
     → Reads 1% of snapshot data and verifies checksums
     → Catches corruption at S3 level
     → Quick: takes seconds not minutes

9. Clean up temp dump files
     → Delete all files in /tmp/yourplatform/backups/

10. Apply retention policy (Step 3B)

11. Report results to control plane
      → Snapshot ID
      → Size of new data
      → Total repository size
      → Duration
      → Per-project status
      → Any failures

12. Release backup lock
      → Delete /var/lib/yourplatform/backup.lock
```

### Step 3B — Retention Policy

```
Old snapshots must be pruned or storage costs grow forever.

Retention policy:
  → Keep all snapshots from the last 7 days
  → Keep one snapshot per week for the last 4 weeks
  → Keep one snapshot per month for the last 3 months
  → Delete everything older

This translates to restic's forget command:
  restic forget
    --keep-daily 7
    --keep-weekly 4
    --keep-monthly 3
    --prune

What --prune does:
  → forget marks old snapshots for deletion
  → prune actually frees the storage in S3
  → Without prune: snapshots are deleted from index
    but data chunks remain (storage still used)
  → With prune: actual S3 storage is reclaimed

When to run retention:
  → After every successful backup run
  → Not before — always have at least one new snapshot before pruning

User-visible result:
  → Dashboard shows backups going back 7 days (daily)
  → Plus weekly snapshots for the last month
  → Plus monthly snapshots for the last 3 months
  → User can restore from any of these points
```

### Step 3C — Backup Progress Reporting

```
Restic outputs progress as it runs.
The agent captures and forwards this to the control plane.

Restic progress output (JSON mode):
  → Run restic with --json flag
  → Each status update is a JSON line
  → Contains: percent complete, files processed, bytes processed

Agent translates this to dashboard-friendly progress:

What dashboard shows while backup runs:
  "Backing up myshop...
   Database: done (12MB)
   Files: 45% (230MB of 512MB)
   Estimated time remaining: 2 minutes"

Not the raw restic output — a translated, friendly version.

If backup is running when user opens dashboard:
  → Dashboard shows progress in real time via WebSocket
  → Not "backup started 5 minutes ago" — actual current progress
```

### Step 3D — Handling Backup Failures

```
Failure scenarios and responses:

S3 not reachable:
  → Restic fails immediately with connection error
  → Agent records failure
  → Retries once after 5 minutes
  → If second attempt fails: send alert
  → "Backup failed: could not reach backup storage.
     Your apps are running normally, but no backup was created tonight.
     We will try again in 24 hours."

Disk full during dump:
  → pg_dump fails mid-dump
  → Temp file may be partial
  → Agent detects disk full error
  → Alert: "Backup failed: not enough disk space for the database dump.
     Free up space or expand your disk."

Database container not running:
  → pg_dump fails because container is stopped
  → Agent records which project failed
  → Continues backing up other projects
  → Reports partial success: "myblog backed up successfully.
     myshop backup failed: database is not running."

Restic repository lock conflict:
  → Another restic process (leftover from a crashed backup) holds the lock
  → Restic refuses to run
  → Agent checks if the locking process is still alive
  → If not alive (stale lock): run restic unlock, then retry
  → If alive: skip this run, try again next scheduled time

Partial backup (some projects succeeded, some failed):
  → Still create a snapshot with what was collected
  → Tag failed projects in the snapshot metadata
  → Report partial success to control plane
  → Dashboard shows: "Partial backup — myshop not included (see reason)"
```

### Done Condition for Step 3
```
□ Full backup completes successfully end to end
□ Snapshot ID is captured from restic output
□ Backup lock prevents simultaneous runs
□ Retention policy removes old snapshots correctly
□ S3 storage is actually reclaimed after prune
□ Dashboard shows progress in real time during backup
□ S3 unreachable: retry once, then alert
□ Disk full: clear alert with fix instruction
□ Partial backup creates snapshot with failed projects marked
□ Stale restic lock is detected and cleared automatically
□ Temp files are cleaned up even if backup fails
```

---

## Step 4 — Backup Scheduling

### When Backups Run

```
Default schedule: daily at 2:00am server local time

Why 2am:
  → Low traffic for most small apps
  → Disk I/O from backup does not impact users
  → Database dumps do not compete with peak writes
  → Universal "off-hours" time

User can change the schedule from the dashboard:
  → Choose time of day (hour granularity)
  → Cannot change frequency for MVP (always daily)
  → Future: weekly-only option for very low-change apps
```

### How Scheduling Works

```
The agent implements its own scheduler.
No cron, no external scheduler, no systemd timers.

Why no cron:
  → Cron is not available on all systems
  → We would need to write/remove cron entries = messy
  → Our agent is always running — it can schedule itself
  → More control over retry behavior and failure handling

How the scheduler works:
  
  On agent startup:
    → Calculate next backup time
    → If last backup was today: next backup is tomorrow at 2am
    → If last backup was yesterday or earlier: run immediately
      (catch up on missed backup)
    → Start a timer goroutine

  Timer goroutine:
    → Sleeps until next backup time
    → Wakes up, runs backup
    → Calculates next run time
    → Sleeps again
    → Loops forever

  On schedule change:
    → User changes time via dashboard
    → Command sent to agent
    → Agent cancels current timer
    → Calculates new next run time
    → Starts new timer
    → Saves new schedule to config

Missed backup detection:
  → On startup, agent reads last backup timestamp from state
  → If last backup > 25 hours ago: run backup now, then schedule normally
  → This handles: server was off, agent was down, previous backup failed
  → "25 hours" not "24 hours" — gives a 1 hour buffer for timing variance
```

### Manual Backup Trigger

```
User can trigger a backup manually from the dashboard at any time.

Flow:
  1. User clicks "Back Up Now"
  2. Dashboard sends command to control plane
  3. Control plane forwards to agent via WebSocket
  4. Agent checks: is a backup already running?
     → If yes: respond "Backup already in progress"
     → If no: start backup immediately
  5. Agent runs full backup (same as scheduled)
  6. Dashboard shows progress in real time
  7. Completion shown: "Backup complete — 47MB backed up in 3 minutes"

Manual backup does not reset the schedule:
  → Scheduled backup still runs at 2am regardless
  → Manual backup is in addition to, not instead of
```

### Done Condition for Step 4
```
□ Backup runs automatically at configured time
□ Backup runs on agent startup if last backup was > 25 hours ago
□ Schedule change from dashboard takes effect immediately
□ Manual backup trigger works from dashboard
□ Manual trigger during a running backup returns "already in progress"
□ Timer survives agent restart (next run recalculated correctly)
□ Backup time stored in state so it survives restarts
```

---

## Step 5 — Backup Metadata and Control Plane Reporting

### What the Control Plane Needs to Know

```
The control plane never touches S3 directly.
It only knows what the agent tells it.

After every backup, agent reports:

{
  "server_id": "srv-abc123",
  "backup_id": "bkp-xyz789",        ← our generated ID
  "restic_snapshot_id": "a1b2c3d4", ← restic's ID (40 char hex)
  "status": "success",               ← success / partial / failed
  "started_at": "2024-01-15T02:00:00Z",
  "completed_at": "2024-01-15T02:04:23Z",
  "duration_seconds": 263,
  "size_new_bytes": 47185920,        ← new data added (deduplicated)
  "size_total_bytes": 892416000,     ← total repo size
  "projects": [
    {
      "name": "myshop",
      "status": "success",
      "components": [
        { "type": "postgres_dump", "size_bytes": 12582912, "status": "success" },
        { "type": "volume", "name": "uploads", "size_bytes": 34603008, "status": "success" }
      ]
    },
    {
      "name": "myblog",
      "status": "failed",
      "error": "Database container not running"
    }
  ],
  "retention_applied": true,
  "snapshots_pruned": 2
}
```

### Database Schema for Backups

```
On the control plane (SQLite for MVP):

Table: backups
  id                TEXT PRIMARY KEY
  server_id         TEXT NOT NULL REFERENCES servers(id)
  restic_snapshot_id TEXT NOT NULL
  status            TEXT NOT NULL  -- success, partial, failed
  started_at        TEXT NOT NULL
  completed_at      TEXT
  duration_seconds  INTEGER
  size_new_bytes    INTEGER
  size_total_bytes  INTEGER
  project_results   TEXT           -- JSON blob of per-project status
  error             TEXT           -- top-level error if fully failed
  created_at        TEXT NOT NULL

Table: backup_schedules
  id                TEXT PRIMARY KEY
  server_id         TEXT NOT NULL REFERENCES servers(id) UNIQUE
  hour_utc          INTEGER NOT NULL DEFAULT 2  -- 2am UTC
  enabled           BOOLEAN NOT NULL DEFAULT true
  last_backup_at    TEXT
  next_backup_at    TEXT
  created_at        TEXT NOT NULL
  updated_at        TEXT NOT NULL
```

### What the Dashboard Shows

```
Backups page for a server:

Header:
  "Backups — Last backup: Today at 2:04am (47MB added)"

Backup history list:
  ┌─────────────────────────────────────────────────────┐
  │ Today, Jan 15 — 2:04am        47MB    ✓ Success    │
  │   myshop ✓   myblog ✗ (db not running)   [Restore] │
  ├─────────────────────────────────────────────────────┤
  │ Yesterday, Jan 14 — 2:01am    52MB    ✓ Success    │
  │   myshop ✓   myblog ✓                    [Restore] │
  ├─────────────────────────────────────────────────────┤
  │ Jan 13 — 2:03am               49MB    ✓ Success    │
  │   myshop ✓   myblog ✓                    [Restore] │
  └─────────────────────────────────────────────────────┘

Storage usage:
  "892MB used in backup storage"

Schedule:
  "Daily at 2:00am"  [Change]

Actions:
  [Back Up Now]
```

### Done Condition for Step 5
```
□ Agent reports backup results to control plane after every run
□ Control plane stores backup record in database
□ Dashboard shows backup history with per-project status
□ Failed projects shown with specific failure reason
□ Storage size displayed correctly
□ Schedule shown and changeable from dashboard
□ Back Up Now triggers immediate backup and shows progress
```

---

## Step 6 — Restore Operations

### The Restore Mental Model

```
User sees in dashboard:
"Yesterday, Jan 14 — 2:01am    [Restore]"

They click Restore.

What they expect:
  → Their data goes back to how it was yesterday at 2am
  → Their app keeps running (or restarts with restored data)
  → This works without them understanding restic, S3, or Docker volumes

What actually happens:
  → Agent downloads the snapshot from S3
  → Restores the database from the dump in that snapshot
  → Restores uploaded files from that snapshot
  → Restarts the app with the restored data
```

### Step 6A — What Can Be Restored

```
Three levels of restore:

Level 1: Restore a specific project's database only
  → User selects: "Restore myshop database from Jan 14"
  → Only the Postgres dump is restored
  → App is briefly restarted after restore
  → No other projects affected
  → Use case: accidental data deletion, bad migration

Level 2: Restore a specific project's files only
  → User selects: "Restore myshop uploads from Jan 14"
  → Only the volume data is restored
  → App is briefly restarted
  → Use case: accidental file deletion

Level 3: Restore entire project (database + files)
  → User selects: "Restore entire myshop from Jan 14"
  → Database and files restored together
  → App restarted
  → Use case: serious corruption, bad deployment

For MVP: implement Level 3 (entire project restore) only.
Level 1 and 2 are enhancements for later.
They require more granular restore logic and more UI work.
```

### Step 6B — Restore Flow

```
Full restore sequence for Level 3:

1. User clicks "Restore" for a backup in the dashboard

2. Dashboard shows confirmation:
   "Restoring myshop to Jan 14 at 2:01am will:
    → Replace the current database with the Jan 14 version
    → Replace uploaded files with the Jan 14 versions
    → Restart the app
    
    Data created after Jan 14 at 2:01am will be lost.
    
    Are you sure? This cannot be undone.
    Type 'restore' to confirm: [          ]"

3. User confirms

4. Control plane sends restore command to agent:
   {
     "command": "restore",
     "project": "myshop",
     "snapshot_id": "a1b2c3d4",  ← restic snapshot ID
     "components": ["database", "files"]
   }

5. Agent acknowledges command, begins restore

6. Restore sequence:
   a. Stop the app container
        → The app must be stopped before its database is modified
        → Database restore into a live database = corruption risk
   
   b. Download snapshot from S3 to temp directory
        → restic restore {snapshot_id}
                --target /tmp/yourplatform/restore/{project}/
                --include {paths for this project only}
        → Restic downloads, decrypts, decompresses
        → Places files at the target path
   
   c. Restore the database
        → Drop and recreate the database:
          docker exec yourplatform_{project}_postgres
            dropdb -U yourplatform myshop
          docker exec yourplatform_{project}_postgres
            createdb -U yourplatform myshop
        → Restore from dump:
          docker exec -i yourplatform_{project}_postgres
            pg_restore -U yourplatform -d myshop
            < /tmp/yourplatform/restore/{project}/postgres.dump
        → Verify restore:
          docker exec yourplatform_{project}_postgres
            psql -U yourplatform -c "SELECT COUNT(*) FROM information_schema.tables"
          → At least some tables must exist

   d. Restore the volume data
        → Stop Docker volume access:
          App container is already stopped (step a)
          Postgres container keeps running (database is a separate container)
        → Clear current volume data:
          rm -rf /var/lib/docker/volumes/{volume_name}/_data/*
        → Copy restored files:
          cp -r /tmp/yourplatform/restore/{project}/volumes/uploads/*
                /var/lib/docker/volumes/{volume_name}/_data/
        → Fix permissions:
          Ensure www-data or the app's user owns the files

   e. Start the app container
        → Same container config as before
        → Same volume mounts (now containing restored data)
        → Health check must pass within 60 seconds

   f. Clean up temp restore files
        → rm -rf /tmp/yourplatform/restore/{project}/

   g. Report completion to control plane
        → Status: success or failed with specific step that failed
        → Duration

7. Dashboard shows:
   "myshop restored to Jan 14 at 2:01am
    Restore took 4 minutes 32 seconds
    App is running and healthy"
```

### Step 6C — Restore Failure Handling

```
Restore is the most critical operation — failure must be handled carefully.

Failure at step b (download from S3):
  → App is still stopped
  → Database is untouched (we failed before touching it)
  → Clean up temp files
  → Restart the app with current data (rollback is trivial: nothing changed)
  → Alert: "Restore failed: could not download backup from storage.
     Your app has been restarted with its existing data. Nothing was changed."

Failure at step c (database restore):
  → App is stopped
  → Database may be in a dropped state (after dropdb, before pg_restore)
  → This is the most dangerous failure point
  → Recovery:
    → If database was dropped: recreate it and re-run pg_restore
    → If pg_restore partially completed: drop and restore from scratch
    → If restore keeps failing: leave database empty, alert immediately
  → Alert: "Restore failed during database restoration.
     Your database may be in an inconsistent state.
     Please contact support immediately with backup ID {id}."

Failure at step d (file restore):
  → App is stopped
  → Database is restored (might be ahead of files)
  → Volume might be partially cleared
  → Alert: "Restore partially completed. Database was restored but
     file restore failed. Your app has been restarted.
     Contact support with backup ID {id} for manual file restore."

Failure at step e (app won't start after restore):
  → Database and files are restored
  → But app is not healthy
  → Alert: "Restore completed but your app is not starting correctly.
     The restored data is in place. Check your app logs for startup errors."
```

### Step 6D — Restore to a Different Project (Future)

```
Not in MVP. Document for future:

Use case: user wants to preview the restored data without
destroying the current live data.

How it would work:
  → Create a new project with a "-restored" suffix
  → Restore the backup into that project
  → User can inspect the data
  → User manually copies what they need
  → User deletes the "-restored" project when done

This requires cloning the project config, which is complex.
Defer to post-MVP.
```

### Done Condition for Step 6
```
□ Restore confirmation dialog shows exactly what will happen
□ Restore confirmation requires typing a word (prevents accidents)
□ App container is stopped before any data is touched
□ Database is dropped and restored from dump
□ Volume data is cleared and restored from snapshot
□ App is restarted after restore with health check verification
□ Temp restore files are cleaned up after completion
□ Failure at each step produces a specific, actionable alert
□ Failure at download step: app restarted unchanged, clear alert
□ Failure at database step: immediate critical alert, support contact
□ Restore duration reported to dashboard
□ Restore history visible in dashboard (who restored, when, from which snapshot)
```

---

## Step 7 — Backup Verification

### Why Verification Matters

```
A backup that cannot be restored is not a backup.
It is a false sense of security.

Three things can make a backup unrestorable:
  1. Encryption key lost (cannot decrypt)
  2. S3 data corrupted (bits flipped, partial upload)
  3. Dump file corrupted (pg_dump wrote garbage)

Layer 3C verifies backups so the user never discovers
a problem at the worst possible time (during a crisis restore).
```

### Step 7A — Post-Backup Verification

```
After every backup run:
  1. Run restic check --read-data-subset=5%
       → Downloads and verifies 5% of snapshot data
       → Checks that encrypted chunks decrypt correctly
       → Checks that decompressed data matches stored hashes
       → Fast: a few seconds to a minute
       
  2. Attempt to list the snapshot contents
       → restic ls {snapshot_id}
       → Verifies the snapshot index is readable
       → Confirms the snapshot metadata is intact

  3. Record verification result with backup metadata
       → "verified: true" or "verified: false + reason"
```

### Step 7B — Weekly Deep Verification

```
Once per week (Sunday at 3am):
  → Run restic check --read-data-subset=25%
  → More thorough than post-backup check
  → Still not 100% (too slow) but catches most corruption

Once per month:
  → Run restic check --read-data-subset=100%
  → Full verification of entire repository
  → May take several minutes depending on repository size
  → Run during lowest-traffic time
  → Alert if this fails — critical issue

Verification failure alert:
  "Warning: Your backup storage appears to have data integrity issues.
   Backups are continuing but some snapshots may not be fully restorable.
   Please contact support immediately.
   Backup ID: {id}
   Error: {restic error message}"
```

### Step 7C — Restore Drill (Future Enhancement)

```
Not in MVP. Document for awareness:

Monthly restore drill:
  → Take the most recent snapshot
  → Restore it to an isolated environment
  → Verify the app starts correctly
  → Confirm database has expected table count
  → Report: "Restore drill: success — your backup from Jan 14 is confirmed restorable"

This is the gold standard of backup verification.
Complex to implement (needs isolated environment).
High value for user confidence.
Post-MVP roadmap item.
```

### Done Condition for Step 7
```
□ Post-backup verification runs after every backup
□ Verification result stored with backup record
□ Weekly deep verification runs on schedule
□ Monthly full verification runs on schedule
□ Verification failure produces immediate alert
□ Dashboard shows verification status per backup
□ Unverified backups are clearly marked in the UI
```

---

## Step 8 — Storage Management

### Monitoring Storage Usage

```
The agent monitors backup storage usage:

After every backup:
  → Run: restic stats --mode raw-data
  → Returns total repository size
  → Report to control plane

Control plane tracks this over time:
  → Storage usage graph in dashboard
  → Current usage vs plan limit

Storage usage alert thresholds:
  → 80% of plan limit: warning
  → 95% of plan limit: urgent alert
  
Alert: "Your backup storage is 82% full (820MB of 1GB used).
  At your current backup rate, storage will be full in approximately 12 days.
  Options:
    - Upgrade your plan for more storage
    - Reduce retention period (currently keeping 7 daily, 4 weekly, 3 monthly)
    - Add your own S3 storage in settings"
```

### Repository Maintenance

```
Restic repositories need occasional maintenance beyond pruning.

restic rebuild-index:
  → Run monthly
  → Rebuilds the index from pack files
  → Faster snapshot operations after rebuild
  → Run during backup window (low traffic time)

restic cache --cleanup:
  → Restic caches data locally for performance
  → Cache location: /var/lib/yourplatform/restic-cache/
  → Cleanup removes stale cache entries
  → Run weekly

These are maintenance operations, not backup operations.
Run them on a separate schedule, not alongside backups.
```

### Done Condition for Step 8
```
□ Storage usage reported after every backup
□ Storage usage visible in dashboard
□ Alert fires at 80% and 95% of plan limit
□ Index rebuild runs monthly without disrupting backups
□ Cache cleanup runs weekly
□ Repository size tracked over time in control plane
```

---

## Layer 3C Overall Done Condition

```
The full test sequence:

Test 1 — First backup on a new server:
  □ Repository initialized automatically
  □ Backup runs at scheduled time
  □ Postgres dump captured
  □ Volume data captured
  □ Snapshot created in S3
  □ Metadata reported to control plane
  □ Dashboard shows backup with correct size and timestamp

Test 2 — Second backup (deduplication):
  □ Only changed data uploaded (much smaller than first backup)
  □ Dashboard shows smaller "new data" size
  □ Both snapshots visible in dashboard

Test 3 — Backup with a stopped database:
  □ Database dump fails for that project
  □ Other projects backed up successfully
  □ Partial success reported clearly
  □ Alert sent to user

Test 4 — Restore from yesterday's backup:
  □ Confirmation dialog shown with correct details
  □ User confirms
  □ App stopped
  □ Database restored from dump
  □ Files restored from volume snapshot
  □ App restarted and healthy
  □ Dashboard shows restore completion

Test 5 — S3 unreachable during backup:
  □ Backup fails clearly
  □ Alert sent
  □ No temp files left behind
  □ Next scheduled backup runs normally

Test 6 — Manual backup trigger:
  □ Backup starts immediately
  □ Progress shown in real time
  □ Completion shown in dashboard

Test 7 — Retention policy:
  □ After 8 daily backups: oldest daily is removed
  □ S3 storage is actually reduced after prune
  □ Weekly snapshots preserved correctly

Test 8 — Agent restart during backup:
  □ Lock file detected on restart
  □ Previous backup run determined to be dead (process gone)
  □ Stale lock cleared
  □ New backup runs at next scheduled time

When all 8 tests pass, Layer 3C is done.
Move to Layer 4A — Agent Lifecycle.
```

---

## What Layer 3C Does NOT Do

```
Layer 3C does not:
├── Back up Docker images (not needed — re-pullable)
├── Back up the agent binary (re-downloadable)
├── Back up other servers (one agent = one server)
├── Provide real-time replication (point-in-time snapshots only)
├── Provide cross-region redundancy (single S3 destination for MVP)
├── Manage S3 bucket creation (user or platform creates the bucket)
├── Encrypt S3 bucket itself (restic encrypts the content — bucket
│   encryption is an S3 configuration, not our responsibility)
└── Handle database replication or streaming backups
    (pg_dump is sufficient for the target scale)
```

---

**Ready for Layer 4A — Agent Lifecycle?**