Layer 4B — Command Executor: Complete Plan
What Layer 4B Actually Is
text

Layer 4B is the agent's brain for action.

Layer 4A keeps the agent connected.
Layer 4B decides what to do with what arrives over that connection.

Every user action in the dashboard eventually becomes
a command that travels:

  User clicks "Deploy"
       │
       ▼
  Dashboard → Control Plane API
       │
       ▼
  Control Plane → WebSocket → Agent
       │
       ▼
  Layer 4B receives the command
       │
       ▼
  Layer 4B orchestrates:
    Layer 3A (Docker)
    Layer 3B (Caddy)
    Layer 3C (Backup)
       │
       ▼
  Layer 4B reports result back
       │
       ▼
  Control Plane → WebSocket → Dashboard
       │
       ▼
  User sees: "Deploy successful"

Layer 4B is the orchestrator.
It does not do Docker itself.
It does not do Caddy itself.
It coordinates the layers that do.
The Command Lifecycle
text

Every command goes through exactly these stages:

RECEIVED
  Agent receives the command message over WebSocket
       │
       ▼
ACKNOWLEDGED
  Agent immediately sends back: "I got this command"
  Control plane marks command as "in progress"
  Dashboard shows spinner
       │
       ▼
VALIDATED
  Agent checks: is this command valid?
  Can I execute it right now?
  Does the referenced project exist?
       │
    ┌──┴──┐
  valid  invalid
    │      │
    │      ▼
    │    REJECTED
    │    Report why to control plane
    │    Dashboard shows error
    │
    ▼
EXECUTING
  Layer 4B orchestrates the execution
  Progress updates sent during execution
       │
    ┌──┴──┐
 success  failure
    │        │
    ▼        ▼
COMPLETED  FAILED
  Result   Error details
  reported reported
       │        │
       └────────┘
            │
            ▼
  Control plane stores result
  Dashboard updates
Step 1 — Command Structure
What Every Command Looks Like
text

Every command arriving from the control plane has this envelope:

{
  "id": "cmd-a1b2c3d4e5f6",
  "type": "deploy",
  "server_id": "srv-xyz789",
  "issued_by": "usr-abc123",
  "issued_at": "2024-01-15T10:00:00Z",
  "expires_at": "2024-01-15T11:00:00Z",
  "payload": {
    ...command-specific fields...
  }
}

Fields explained:

id:
  Unique identifier for this command
  Used for acknowledgment and result correlation
  Used for idempotency (same command ID = same command, do not re-execute)

type:
  What to do
  Full list defined in Step 2

server_id:
  Which server this command is for
  Agent validates this matches its own server ID
  Prevents commands from being replayed on wrong server

issued_by:
  Which user issued this command
  Used for audit logging
  Not used for permission checking (control plane already did that)

issued_at:
  When the user initiated this action
  Used for staleness detection

expires_at:
  After this time, do not execute this command
  Commands queued while offline: check before executing
  If expired: acknowledge, mark as expired, do not execute

payload:
  Command-specific data
  Different for each command type
Command ID Idempotency
text

Why idempotency matters:

Scenario without idempotency:
  1. Control plane sends deploy command
  2. Agent executes it
  3. Agent sends result
  4. Result message is lost in transit
  5. Control plane thinks command was not received
  6. Control plane resends the command
  7. Agent executes AGAIN
  8. App is deployed twice — second deploy kills first

With idempotency:
  1. Control plane sends deploy command (id: cmd-abc)
  2. Agent executes it
  3. Agent stores: "cmd-abc was executed, result: success"
  4. Result message is lost in transit
  5. Control plane resends the command
  6. Agent checks: have I seen cmd-abc?
  7. Yes → return stored result, do not re-execute

Implementation:
  Store executed command IDs with results
  Location: /var/lib/yourplatform/executed-commands.json
  
  Structure:
  {
    "cmd-abc123": {
      "executed_at": "2024-01-15T10:00:00Z",
      "status": "success",
      "result": { ... }
    }
  }
  
  Retention:
    Keep for 48 hours
    Older entries: remove (too old to be re-sent)
    On startup: clean out entries older than 48 hours
Done Condition for Step 1
text

□ Command envelope structure is defined and parsed correctly
□ server_id validation rejects commands for other servers
□ expires_at is checked before execution
□ Command IDs are stored after execution
□ Duplicate command ID returns stored result without re-executing
□ Old command records are cleaned up after 48 hours
Step 2 — Command Types
The Complete Command List
text

Category 1: Deployment
  deploy          → deploy an app from a Docker image
  rollback        → revert to a previous deployment
  redeploy        → redeploy current version (same image)

Category 2: App Lifecycle
  start           → start a stopped app
  stop            → stop a running app
  restart         → restart a running app

Category 3: Environment
  set_env         → set or update an environment variable
  delete_env      → remove an environment variable

Category 4: Data
  create_database → provision a new database container
  delete_database → remove a database container

Category 5: Backup and Restore
  run_backup      → trigger an immediate backup
  restore         → restore from a specific snapshot

Category 6: Domains
  add_domain      → add a custom domain to an app
  remove_domain   → remove a custom domain from an app
  verify_dns      → check if a domain's DNS is pointing here

Category 7: Diagnostics
  get_logs        → fetch log history (not streaming)
  start_log_stream → begin streaming logs to dashboard
  stop_log_stream  → stop streaming logs

Category 8: Agent Management
  update_agent    → install a specific agent version
  run_preflight   → run Layer 2 checks and return results
  get_state       → return current full agent state
Step 3 — The Command Executor Core
The Dispatcher
text

When a command arrives:

1. Parse the JSON into a command struct
2. Validate the envelope (server_id, expiry)
3. Check idempotency cache
4. Send acknowledgment to control plane
5. Route to the correct handler based on type
6. Execute the handler
7. Send result to control plane
8. Store in idempotency cache

The dispatcher is a single goroutine reading from a channel.
Commands arrive from the WebSocket reader (Layer 4A)
and are placed into a buffered channel.
The dispatcher processes them one at a time.

Why one at a time:
  → Two simultaneous deploys for the same project would conflict
  → One deploy creating a container while another deletes it
  → Simpler: queue everything, process sequentially

Exception: some commands can run concurrently
  → Commands for different projects can run concurrently
  → Log streaming can run alongside deployments
  → Preflight checks can run alongside anything

Concurrency model:
  → One execution slot per project
  → Multiple projects can execute concurrently
  → Global commands (preflight, get_state) run in their own slot
  → Log streaming commands run outside the queue entirely
The Execution Slot System
text

Execution slots:

slot["myshop"]:   currently executing "deploy"
slot["myblog"]:   idle
slot["global"]:   idle

New command arrives: "restart" for "myshop"
  → slot["myshop"] is busy
  → Queue the restart behind the deploy
  → Deploy completes
  → Restart executes next

New command arrives: "deploy" for "myblog"
  → slot["myblog"] is idle
  → Execute immediately, concurrently with myshop's deploy

New command arrives: "run_preflight"
  → slot["global"] is idle
  → Execute immediately

Slot structure stored in memory only (not persisted):
  → On agent restart: all slots are clear
  → Any in-progress commands are interrupted
  → Detected via unclean shutdown in state.json
  → Reported to control plane on reconnect
Progress Reporting During Execution
text

Long-running commands must report progress.
The user should not stare at a spinner for 5 minutes.

Progress update message format:
{
  "id": "msg-xyz",
  "type": "command_progress",
  "command_id": "cmd-abc123",
  "timestamp": "...",
  "payload": {
    "step": "pulling_image",
    "step_number": 2,
    "total_steps": 6,
    "message": "Pulling nginx:latest (layer 3/7)...",
    "percent": 28
  }
}

The control plane forwards these to the dashboard.
Dashboard renders: step indicator + current message + percent bar.

Each command handler is responsible for emitting progress updates.
The dispatcher provides a progress reporter function to each handler.
Handler calls: progress.Report(step, message, percent)
Dispatcher sends the WebSocket message.
Done Condition for Step 3
text

□ Commands are dispatched to correct handlers by type
□ Acknowledgment is sent before execution begins
□ One execution slot per project prevents conflicts
□ Different projects execute concurrently
□ Progress updates are sent during long operations
□ Result is sent after execution completes or fails
□ Unknown command types are rejected with clear error
□ Handler panics are caught and reported as failures
   (a panicking handler must not crash the entire agent)
Step 4 — Deploy Command
This Is The Most Important Command
text

Deploy is what the user does most often.
Every other command is simpler than this one.
Getting deploy right is the core of the product.

Deploy command payload:
{
  "project_name": "myshop",
  "image": "nginx:latest",
  "port": 3000,
  "env_vars": {
    "NODE_ENV": "production",
    "API_KEY": "..."
  },
  "databases": [
    { "type": "postgres", "name": "myshop" }
  ],
  "volumes": [
    { "name": "uploads", "mount_path": "/app/uploads" }
  ],
  "domain": "myshop",
  "custom_domains": [],
  "memory_limit_mb": 512,
  "cpu_quota_percent": 50
}
Step 4A — Pre-Deploy Validation
text

Before touching Docker or Caddy, validate everything:

1. Project name is valid
     → Alphanumeric + hyphens, max 63 chars
     → Does not conflict with another project on this server

2. Image reference is parseable
     → Valid format: [registry/]name[:tag]
     → Tag present (warn if "latest" — acceptable but noted)

3. Port is in valid range
     → 1-65535
     → Not a privileged port (< 1024) — containers run as non-root

4. Memory limit is within bounds
     → Above minimum (64MB — below this nothing will run)
     → Below maximum (available server RAM minus 512MB for system)

5. Database types are supported
     → postgres, mysql, redis — anything else is rejected

6. Volume mount paths are valid
     → Absolute paths only
     → Not mounting system directories (/etc, /usr, /bin, etc.)
     → These would give the container access to the host system

7. Domain name is valid
     → Will become: {domain}.{server-id}.yourplatform.app
     → Must be a valid DNS label

If any validation fails:
  → Reject the command immediately
  → Report specific validation error
  → Do not proceed with any Docker/Caddy operations
Step 4B — The Deploy Sequence
text

This is the full step-by-step deploy:

Progress: "Starting deployment..."

Step 1: Pull the image (Layer 3A)
  → Check if image already exists locally
  → If not or if "latest": pull from registry
  → Stream pull progress to dashboard
  Progress: "Pulling image nginx:latest (layer 2/5)..."

Step 2: Prepare databases (Layer 3A)
  → For each requested database:
    → Check if database container already exists
    → If not: create and start the database container
    → Wait for database health check to pass
    → Generate connection credentials if new
    → Store credentials in env file
  Progress: "Starting Postgres database..."

Step 3: Prepare network (Layer 3A)
  → Create project network if not exists
  → Network: yourplatform_{project_name}
  Progress: "Configuring networking..."

Step 4: Prepare volumes (Layer 3A)
  → Create persistent volumes if not exist
  → Reuse existing volumes (data is preserved)
  Progress: "Preparing storage..."

Step 5: Prepare environment variables (Layer 4B)
  → Merge: user-provided env vars + auto-generated (DATABASE_URL, PORT)
  → Write to: /etc/yourplatform/envs/{project}.env
  → Do not log values
  Progress: "Configuring environment..."

Step 6: Stop old container if exists (Layer 3A)
  → Find container: yourplatform_{project}_app
  → If running: stop it gracefully (SIGTERM, 30s timeout)
  → Record: this is the previous deployment (for rollback)
  → Store previous container config in state.json
  Progress: "Stopping previous version..."

Step 7: Start new container (Layer 3A)
  → Create container with full config
  → Start it
  → Wait for health check (up to 60 seconds)
  Progress: "Starting your app..."

Step 8: Verify container is healthy (Layer 3A)
  → Poll health check endpoint
  → If healthy within 60 seconds: proceed
  → If not healthy after 60 seconds: trigger rollback (Step 4D)
  Progress: "Waiting for app to be ready..."

Step 9: Update Caddy route (Layer 3B)
  → Add or update route for this project
  → Domain: {project}.{server-id}.yourplatform.app
  → Upstream: 127.0.0.1:{new container host port}
  → If custom domains exist: add them to the route
  Progress: "Configuring routing and HTTPS..."

Step 10: Verify routing works (Layer 3B)
  → Make an HTTP request to the platform subdomain
  → Expect any response (even 404 — the app is reachable)
  → If request fails: check Caddy route was created correctly
  Progress: "Verifying HTTPS is working..."

Step 11: Record deployment (Layer 4B)
  → Write to state.json:
    → Current deployment image
    → Container ID
    → Deployment timestamp
    → Previous deployment details (for rollback)
  → Report to control plane:
    → status: success
    → container_id, host_port
    → domain: myshop.srv-abc.yourplatform.app
    → deployment_id: dep-xyz789

Progress: "Deployment complete!"
Step 4C — Deploy Result
text

Successful deploy result sent to control plane:
{
  "command_id": "cmd-abc123",
  "status": "success",
  "result": {
    "deployment_id": "dep-xyz789",
    "container_id": "abc123def456",
    "image": "nginx:latest",
    "image_digest": "sha256:...",
    "domain": "myshop.srv-abc.yourplatform.app",
    "custom_domains": [],
    "port": 3000,
    "host_port": 32847,
    "started_at": "2024-01-15T10:00:00Z",
    "duration_seconds": 47
  }
}

Dashboard shows:
  ✓ myshop deployed successfully
  Running at: https://myshop.srv-abc.yourplatform.app
  Deploy took: 47 seconds
Step 4D — Automatic Rollback on Failed Deploy
text

If the new container fails its health check:

1. Log: "New deployment failed health check after 60 seconds"

2. Stop the new container
     → It is unhealthy — remove it

3. Check: does a previous deployment exist?
     → Read from state.json: previous_deployment

4. If previous deployment exists:
     → Restart the old container
     → Wait for health check on old container
     → If old container passes: update Caddy route back to old port
     → If old container also fails: this is a bigger problem (Step 4E)

5. Report to control plane:
   {
     "command_id": "cmd-abc123",
     "status": "failed",
     "error": {
       "code": "health_check_failed",
       "message": "App did not become healthy within 60 seconds",
       "detail": "Last 20 log lines from the container: [...]",
       "auto_rolled_back": true,
       "rolled_back_to": "nginx:1.24.0"
     }
   }

6. Dashboard shows:
   ✗ Deploy failed — app did not start correctly
   Automatically rolled back to previous version (nginx:1.24.0)
   Your site is back up.
   
   What went wrong (last log lines from the failing app):
   [actual log output from the container]
   
   Common causes:
     → App failed to connect to database
     → Missing environment variable
     → Port mismatch (app listening on wrong port)
Step 4E — Rollback Also Failed
text

Rare but must be handled:
  → New deploy failed
  → Previous version also fails to start

This means something is wrong with the environment,
not the app code.

Response:
  → Report both failures to control plane
  → Alert: "Critical: Both your new deployment and the
     previous version are failing to start.
     
     Your app is currently unreachable.
     
     This usually means:
       - The database is down
       - A required environment variable is missing
       - The server is out of memory
     
     Check your server resources and database status.
     Your data is safe — no data was modified.
     
     Run logs for the failing container:
     [container log output]"
Done Condition for Step 4
text

□ Validation catches invalid image names, ports, memory limits
□ Full deploy sequence completes successfully end to end
□ Progress updates sent at each step
□ New container is healthy before Caddy route is updated
□ Failed health check triggers automatic rollback
□ Rollback restores old container and updates Caddy route
□ Rollback failure produces a critical alert with diagnostics
□ Deployment record stored in state.json for future rollback
□ Dashboard shows domain and HTTPS working after deploy
□ Container log lines included in failure report
Step 5 — Rollback Command
What Rollback Is
text

User says: "Take my app back to how it was before"

Two flavors:

Flavor 1: Rollback to previous deployment
  → The immediately preceding deploy
  → "Undo the last deploy"
  → Most common use case
  → Available: as long as previous deployment info exists in state

Flavor 2: Rollback to a specific deployment
  → User picks from deployment history in dashboard
  → "Take me back to the version from 3 days ago"
  → Requires deployment history to be stored
  → More complex — need the image name from that deployment
Rollback Command Payload
text

{
  "project_name": "myshop",
  "target": "previous",          ← "previous" or a specific deployment_id
  "deployment_id": null          ← filled if target is not "previous"
}
Rollback Sequence
text

For "rollback to previous":

1. Read previous deployment from state.json
     → previous_deployment:
         image: "nginx:1.24.0"
         env_vars_hash: "abc123"  ← hash of env vars at that time
         port: 3000

2. Check: is the previous image still available locally?
     → If yes: no pull needed (fast rollback)
     → If no: pull it (slower — may take minutes)

3. Execute a deploy with the previous deployment's config
     → Same as Step 4B but with previous image and config
     → This re-runs the full deploy sequence
     → Ensures rollback goes through same validation and health checks

4. On success: update state.json
     → Current deployment becomes: the rolled-back version
     → Previous deployment becomes: the one we just rolled back from

5. Report result:
   {
     "status": "success",
     "rolled_back_to": {
       "image": "nginx:1.24.0",
       "deployment_id": "dep-abc123"
     }
   }

For "rollback to specific deployment":
  → Same as above but retrieve image from deployment history
  → Deployment history stored in control plane database
  → Control plane includes the image name in the rollback command payload
Done Condition for Step 5
text

□ Rollback to previous deploys the previous image successfully
□ Rollback goes through the same health check as a normal deploy
□ Rollback state is updated correctly (what is current, what is previous)
□ Previous image not available locally triggers a pull
□ Specific deployment ID rollback retrieves correct image from history
□ Rollback during a rollback is rejected (one operation per project)
Step 6 — App Lifecycle Commands
Start Command
text

Payload: { "project_name": "myshop" }

Sequence:
1. Find the app container: yourplatform_myshop_app
2. Check current status
     → Already running: return "already running" (not an error)
     → Stopped: proceed
     → Does not exist: return error "no deployment found for myshop"
3. Start the container (Layer 3A)
4. Wait for health check (60 seconds)
5. If healthy: ensure Caddy route is active for this project
     → Route might be missing if Caddy was restarted
     → Re-add route if needed
6. Report result

Common reason start is needed:
  → User manually stopped their app to save resources
  → Container stopped due to OOM and restart was disabled
Stop Command
text

Payload:
{
  "project_name": "myshop",
  "remove_domain": false    ← keep Caddy route or remove it
}

Sequence:
1. Find app container
2. Check current status
     → Already stopped: return "already stopped"
     → Running: proceed
3. Stop the container gracefully (Layer 3A)
4. If remove_domain is true:
     → Remove Caddy route (app returns 502/connection refused)
     → Use case: "I want this domain to stop responding"
     If remove_domain is false:
     → Keep Caddy route
     → Caddy returns 502 (app is down but domain exists)
     → Use case: "Temporary stop, will restart soon"
5. Update state.json: status = stopped
6. Report result

Note: databases are NOT stopped when app is stopped
  → User might just want to stop the web app
  → Database stopping would lose in-memory data (bad for Redis)
  → Separate command: stop_database (future enhancement)
Restart Command
text

Payload: { "project_name": "myshop" }

Sequence:
  → Stop (graceful)
  → Start
  → Health check
  → Report result

Restart is exactly stop + start.
Not Docker's "restart" — we implement it ourselves for consistent behavior.

Why not Docker restart:
  → We need the same health check behavior as start
  → We need to handle the case where it does not come back up
  → Docker restart is a black box from our perspective
Redeploy Command
text

Payload:
{
  "project_name": "myshop",
  "pull_latest": true    ← re-pull the image before deploying
}

Redeploy = deploy the same image again.
Used when:
  → User pushed a new version to the same image tag
  → User wants to clear any container runtime state
  → User just wants a clean restart

If pull_latest is true:
  → Force a pull even if image exists locally
  → Gets the latest version of that tag
  → Important for "latest" tags that change upstream

If pull_latest is false:
  → Use cached image (faster)
  → Same bytes as current running version

Sequence:
  → Read current image from state.json
  → Execute deploy with that image (and pull_latest flag)
  → Same as full deploy sequence
Done Condition for Step 6
text

□ Start on already-running app returns graceful "already running"
□ Start on non-existent project returns clear error
□ Stop removes Caddy route when remove_domain is true
□ Stop keeps Caddy route when remove_domain is false (returns 502)
□ Restart is functionally stop then start with health check
□ Redeploy with pull_latest forces image re-pull
□ Redeploy without pull_latest uses cached image
□ All lifecycle commands update state.json correctly
Step 7 — Environment Variable Commands
Set Env Command
text

Payload:
{
  "project_name": "myshop",
  "key": "STRIPE_KEY",
  "value": "sk_live_...",
  "restart_after": false    ← whether to restart app to apply
}

Sequence:
1. Validate key
     → Valid env var name: uppercase, underscores, numbers
     → Not starting with a number
     → Not a reserved key (PORT, YOURPLATFORM)

2. Read current env file for this project
     /etc/yourplatform/envs/myshop.env

3. Update or add the key-value pair
     → If key exists: update value
     → If key does not exist: add it
     → Write the file back (atomic write pattern)

4. Do NOT log the value anywhere
     → The value is a secret
     → Acknowledge receipt without including the value in the ack

5. If restart_after is true:
     → Execute a restart command for this project
     → Return combined result (env updated + restart result)

6. Report result:
   {
     "status": "success",
     "key": "STRIPE_KEY",
     "action": "updated",    ← "updated" or "created"
     "restart_required": true   ← always true, env needs restart to apply
   }
Delete Env Command
text

Payload:
{
  "project_name": "myshop",
  "key": "OLD_API_KEY",
  "restart_after": false
}

Sequence:
1. Validate key is not a protected key (DATABASE_URL, PORT)
     → These are auto-managed, cannot be deleted manually

2. Read env file

3. Remove the key-value pair
     → If key does not exist: return "key not found" (not an error)

4. Write env file back

5. If restart_after: trigger restart

6. Report result
Done Condition for Step 7
text

□ Set env writes to file correctly
□ Update existing key replaces the value
□ Add new key appends to file
□ Delete removes only the specified key
□ Values are never logged or included in responses
□ Protected keys (PORT, DATABASE_URL) cannot be deleted
□ restart_after triggers a restart and reports combined result
□ Atomic file write prevents corrupted env file
Step 8 — Database Commands
Create Database Command
text

Payload:
{
  "project_name": "myshop",
  "db_type": "postgres",      ← postgres, mysql, redis
  "db_name": "myshop",        ← database name inside Postgres
  "version": "16"             ← Postgres version
}

Sequence:
1. Check: does a database of this type already exist for this project?
     → If yes: return "database already exists"
     → Idempotent behavior

2. Generate credentials (for postgres/mysql):
     → Username: yourplatform
     → Password: 32 random bytes, base64 encoded
     → Database name: from payload

3. Create the database container (Layer 3A):
     → Image: postgres:16-alpine (smaller than full postgres:16)
     → Container name: yourplatform_{project}_postgres
     → Network: yourplatform_{project} (project network)
     → Volume: yourplatform_{project}_postgres_data
     → Environment variables:
         POSTGRES_USER=yourplatform
         POSTGRES_PASSWORD={generated}
         POSTGRES_DB={db_name}
     → No port exposed to host (internal network only)
     → Memory limit: 512MB soft, 1GB hard

4. Wait for database to be healthy:
     → Poll pg_isready inside container
     → Up to 120 seconds (databases can be slow to initialize)
     → On first run: Postgres initializes its data directory
       This takes 10-30 seconds on first start

5. Generate DATABASE_URL:
     → postgres://yourplatform:{password}@postgres:5432/{db_name}
     → "postgres" is the network alias (not localhost)

6. Write DATABASE_URL to project env file:
     → Add: DATABASE_URL=postgres://yourplatform:...

7. Report result:
   {
     "status": "success",
     "db_type": "postgres",
     "container": "yourplatform_myshop_postgres",
     "database_name": "myshop",
     "env_var_added": "DATABASE_URL",
     "note": "Restart your app to connect to the database"
   }
   
   Note: do NOT include the password in the result
   The password is in the env file on the server, not sent to control plane
Delete Database Command
text

Payload:
{
  "project_name": "myshop",
  "db_type": "postgres",
  "confirm_data_deletion": true   ← must be explicitly true
}

Safety check:
  → confirm_data_deletion must be exactly true
  → If false or missing: reject with error
  → "Database deletion is irreversible. Set confirm_data_deletion
     to true to proceed."

Sequence:
1. Stop the app container first
     → Cannot delete a database while the app is connected to it
     → Well, you can, but app will throw connection errors

2. Stop and remove the database container

3. Remove the database volume
     → ONLY if confirm_data_deletion is true
     → This is what actually deletes the data

4. Remove DATABASE_URL from the project env file

5. Report result

6. Do NOT restart the app automatically
     → App will fail to connect to database on next start
     → User must either:
       → Create a new database and redeploy
       → Redeploy without a database
     → Dashboard should warn about this
Done Condition for Step 8
text

□ Create postgres container with correct credentials
□ Wait up to 120 seconds for healthy status
□ DATABASE_URL automatically added to env file
□ Password not included in result sent to control plane
□ Database already exists returns graceful message
□ Delete requires confirm_data_deletion to be true
□ Delete removes container AND volume
□ Delete removes DATABASE_URL from env file
□ App is stopped before database deletion
Step 9 — Backup and Restore Commands
Run Backup Command
text

Payload:
{
  "project_name": null    ← null means all projects
  OR
  "project_name": "myshop"  ← specific project only
}

Sequence:
1. Check: is a backup already running?
     → Check backup lock file
     → If locked: respond "Backup already in progress"

2. Delegate entirely to Layer 3C
     → Layer 4B calls Layer 3C's backup runner
     → Passes the project filter (all or specific)
     → Layer 3C handles everything

3. Stream progress updates from Layer 3C to control plane
     → Layer 3C emits progress events
     → Layer 4B forwards them as command_progress messages

4. When Layer 3C completes:
     → Layer 4B sends command result
     → Result includes backup metadata from Layer 3C
Restore Command
text

Payload:
{
  "project_name": "myshop",
  "snapshot_id": "a1b2c3d4",   ← restic snapshot ID
  "confirmed": true             ← must be true (dashboard forces confirmation)
}

Safety check:
  → confirmed must be true
  → If not: reject immediately

Sequence:
1. Validate project exists on this server

2. Validate snapshot_id is a known snapshot
     → Run: restic snapshots --json and check ID exists
     → If not found: error "Snapshot not found"

3. Check: is a deployment in progress for this project?
     → If yes: reject "Cannot restore while a deployment is in progress"

4. Delegate to Layer 3C restore sequence
     → Layer 4B calls Layer 3C's restore runner
     → Layer 3C handles the full restore (as defined in Layer 3C plan)

5. Stream progress updates from Layer 3C

6. On completion: report result
Done Condition for Step 9
text

□ Run backup delegates to Layer 3C correctly
□ Backup in progress returns clear message
□ Restore requires confirmed to be true
□ Restore validates snapshot ID exists before starting
□ Restore blocked if deployment in progress
□ Progress from Layer 3C forwarded to control plane
□ Backup and restore results include Layer 3C metadata
Step 10 — Domain Commands
Add Domain Command
text

Payload:
{
  "project_name": "myshop",
  "domain": "shop.clientbusiness.com"
}

Sequence:
1. Validate domain format
     → Valid hostname format
     → Not already used by another project on this server

2. Trigger DNS verification (Layer 3B)
     → Check if domain resolves to this server's IP
     → If not: do not add to Caddy yet

3. DNS not propagated yet:
     → Respond to control plane:
       { "status": "pending_dns",
         "message": "DNS not yet pointing to this server",
         "expected_ip": "1.2.3.4",
         "current_resolution": "5.6.7.8" }
     → Schedule periodic recheck: every 5 minutes for 2 hours
       (handled by a background goroutine, not the command executor)
     → When DNS propagates: automatically add the domain and notify

4. DNS already propagated:
     → Add domain to Caddy route (Layer 3B)
     → Caddy gets certificate on first request automatically
     → Respond: success

Background DNS recheck goroutine:
  → Separate from the command executor
  → Runs independently
  → On success: calls Layer 3B to add the domain
  → Sends a state_update to control plane: domain is now active
  → Stops checking after 2 hours (user needs to fix DNS first)
Verify DNS Command
text

Payload:
{
  "project_name": "myshop",
  "domain": "shop.clientbusiness.com"
}

Sequence:
1. Look up the domain's A record
2. Get this server's public IP
3. Compare
4. Report result immediately:
   {
     "domain": "shop.clientbusiness.com",
     "expected_ip": "1.2.3.4",
     "current_ip": "1.2.3.4",
     "propagated": true
   }
   
Used by dashboard to let user check DNS status on demand.
Separate from the automatic recheck.
Remove Domain Command
text

Payload:
{
  "project_name": "myshop",
  "domain": "shop.clientbusiness.com"
}

Sequence:
1. Remove domain from Caddy route (Layer 3B)
2. Platform subdomain remains (always kept)
3. Cancel any pending DNS recheck goroutine for this domain
4. Report result
Done Condition for Step 10
text

□ Add domain triggers DNS verification before adding to Caddy
□ Pending DNS state is reported to control plane
□ Background recheck runs every 5 minutes for 2 hours
□ Domain automatically activates when DNS propagates
□ Verify DNS command returns immediate result
□ Remove domain removes from Caddy but keeps platform subdomain
□ Background recheck is cancelled when domain is removed
Step 11 — Diagnostic Commands
Get Logs Command
text

Payload:
{
  "project_name": "myshop",
  "container": "app",      ← app, postgres, redis
  "lines": 200,
  "since": null            ← ISO timestamp or null for latest N lines
}

Sequence:
1. Find the container
2. Fetch last N lines from Docker log API
3. Return as result:
   {
     "lines": [
       { "timestamp": "...", "stream": "stdout", "text": "Server started on port 3000" },
       { "timestamp": "...", "stream": "stderr", "text": "Warning: no DATABASE_URL set" }
     ],
     "container": "yourplatform_myshop_app",
     "total_lines": 200
   }

This is a one-shot fetch, not streaming.
For streaming: use start_log_stream command.
Start Log Stream Command
text

Payload:
{
  "project_name": "myshop",
  "container": "app",
  "stream_id": "stream-abc123"   ← ID to correlate stream messages
}

Sequence:
1. Check: is this stream_id already active?
     → If yes: return existing stream (idempotent)

2. Start Docker log stream (Layer 3A/4C integration)
     → Open follow=true stream on the container
     → Each log line: send as log_line message
       {
         "type": "log_line",
         "stream_id": "stream-abc123",
         "timestamp": "...",
         "stream": "stdout",
         "text": "..."
       }

3. Register the active stream
     → Store: stream_id → container, goroutine handle

4. Return acknowledgment (stream is starting)
Stop Log Stream Command
text

Payload: { "stream_id": "stream-abc123" }

Sequence:
1. Find the active stream by ID
2. Signal the goroutine to stop
3. Clean up stream registration
4. Return acknowledgment
Run Preflight Command
text

Payload: {} (no payload — run all checks)

Sequence:
1. Delegate to Layer 2 preflight runner
2. Run all checks (this may take 30-60 seconds)
3. Return full preflight result
4. Control plane stores result
5. Dashboard shows current server health

Used when:
  → User reports a problem and support wants to diagnose
  → Periodic health verification
  → After server changes (disk upgrade, RAM upgrade)
Get State Command
text

Payload: {} (no payload)

Returns the full current agent state:
{
  "agent_version": "1.0.0",
  "connection_status": "connected",
  "projects": [...],
  "caddy_routes": [...],
  "docker_containers": [...],
  "last_backup": "...",
  "disk_usage": {...},
  "ram_usage": {...}
}

Used by control plane to sync state after reconnection.
Also useful for debugging discrepancies between
what dashboard shows and what the server actually has.
Done Condition for Step 11
text

□ Get logs returns correct lines for each container type
□ Get logs with since timestamp returns only lines after that time
□ Start log stream begins sending log_line messages
□ Two start_log_stream commands for same stream_id are idempotent
□ Stop log stream stops the goroutine and cleans up
□ Run preflight returns structured results from Layer 2
□ Get state returns accurate current snapshot of all server state
Step 12 — Offline Command Queue
The Problem
text

Agent is disconnected from control plane.
User deploys an app from the dashboard.
Control plane has no agent to send the command to.

Options:
  A: Fail immediately — "Server is offline, try again later"
  B: Queue the command — execute when agent reconnects

We choose B for most commands.
The user should not have to babysit the connection.
What Gets Queued vs What Does Not
text

Queued (can wait):
  ✓ deploy          → user wants this, execute when back online
  ✓ rollback        → same
  ✓ set_env         → same
  ✓ add_domain      → same
  ✓ create_database → same
  ✓ run_backup      → same

Not queued (must be real-time):
  ✗ start_log_stream  → meaningless when agent reconnects
  ✗ stop_log_stream   → same
  ✗ get_logs          → user wants current logs, stale answer is useless
  ✗ get_state         → stale state is worse than no state
  ✗ verify_dns        → must run now

Queued with expiry:
  All queued commands have an expires_at
  Default: 1 hour for most commands
  If agent is offline for > 1 hour and command arrives:
    Execute it on reconnect if not expired
    Mark as expired if past expires_at
Where the Queue Lives
text

The queue is managed by the control plane, not the agent.

Control plane:
  → Stores pending commands in database
  → Persists through control plane restarts
  → Sends all pending commands in hello_ack when agent reconnects

Agent:
  → Receives pending commands in hello_ack
  → Processes them in order (oldest first)
  → Checks expiry before each command
  → Sends result for each

The agent does NOT maintain its own offline queue.
If the agent is offline, it cannot receive commands.
When it reconnects, the control plane delivers queued commands.
This is simpler and more reliable than a local queue.
Processing Queued Commands on Reconnect
text

Agent reconnects.
hello_ack received with pending_commands: [list of commands]

Process sequence:
  For each command in order:
    1. Check expires_at — if expired: skip, report expired
    2. Check idempotency cache — if already executed: skip, return cached result
    3. Validate the command is still applicable
         → Does the project still exist?
         → Is a newer deploy command for this project in the queue?
           If yes: skip this one, execute the newest
    4. Execute the command
    5. Report result
    6. Continue to next

Finding the newest deploy for a project:
  → Scan pending_commands for all "deploy" commands for the same project
  → Keep only the newest (by issued_at timestamp)
  → Skip older ones (user kept clicking deploy — execute latest only)

After processing queue:
  → Normal operation resumes
  → Scheduler resumes (backup, etc.)
  → Health reporting resumes
Done Condition for Step 12
text

□ Non-queueable commands fail immediately with clear message when offline
□ Control plane queues commands when agent is offline
□ On reconnect, pending commands are received in hello_ack
□ Commands are processed oldest-first
□ Expired commands are skipped with expired status
□ Duplicate deploys for same project: only newest executed
□ Idempotency cache prevents re-execution of already-done commands
□ Results for queued commands are sent to control plane after execution
Layer 4B Overall Done Condition
text

The full test sequence:

Test 1 — Deploy a new app:
  □ Deploy command received
  □ Acknowledgment sent immediately
  □ Progress updates visible in dashboard during deploy
  □ App container running and healthy after deploy
  □ Domain accessible over HTTPS
  □ Success result with domain sent to control plane

Test 2 — Deploy with health check failure:
  □ New container fails health check
  □ Automatic rollback to previous version
  □ Dashboard shows failure with log lines
  □ Previous version is running and accessible
  □ Rollback status visible in dashboard

Test 3 — Environment variable update:
  □ Set env updates the file
  □ Value not logged or sent to control plane
  □ Restart_after triggers restart and combined result
  □ App has new env var after restart

Test 4 — Commands while offline:
  □ Agent goes offline (disconnect WebSocket)
  □ User sends a deploy command from dashboard
  □ Control plane queues the command
  □ Agent reconnects
  □ Command received in hello_ack
  □ Command executes automatically
  □ Result sent to control plane
  □ Dashboard shows deployment result

Test 5 — Concurrent commands different projects:
  □ Deploy myshop starts
  □ Deploy myblog command arrives
  □ Both execute concurrently (no blocking between projects)
  □ Both complete successfully

Test 6 — Concurrent commands same project:
  □ Deploy myshop starts
  □ Restart myshop command arrives during deploy
  □ Restart is queued behind the deploy
  □ Deploy completes
  □ Restart executes after

Test 7 — Rollback:
  □ Deploy v2 (creates previous_deployment record)
  □ Rollback command received
  □ v1 is deployed (same full sequence as deploy)
  □ Dashboard shows rolled back to v1

Test 8 — Database creation:
  □ Create postgres command received
  □ Container created and healthy
  □ DATABASE_URL added to env file
  □ App can connect to database using DATABASE_URL

Test 9 — Expired command:
  □ Agent offline for 2 hours
  □ Deploy command issued 90 minutes ago (expires_at: 1 hour)
  □ Agent reconnects
  □ Expired command is skipped
  □ Dashboard shows: "Deploy expired while server was offline"

When all 9 tests pass, Layer 4B is done.
Move to Layer 4C — Health and Log Reporter.
What Layer 4B Does NOT Do
text

Layer 4B does not:
├── Talk to Docker directly (Layer 3A does)
├── Talk to Caddy directly (Layer 3B does)
├── Run backup jobs directly (Layer 3C does)
├── Manage the WebSocket connection (Layer 4A does)
├── Collect health metrics (Layer 4C does)
├── Authenticate users (Layer 5A does)
└── Make decisions about what should be deployed
    (it executes what the control plane tells it to)

Layer 4B is a clean orchestration layer.
It receives intent from the control plane.
It coordinates the execution layers.
It reports results faithfully.