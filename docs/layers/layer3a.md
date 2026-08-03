# Layer 3A — Docker Management: Complete Plan

---

## What Layer 3A Actually Is

```
Layer 3A is the agent's hands.

Every action that involves a container goes through this layer.
Nothing else in the agent touches Docker directly.
Everything talks to Layer 3A, and Layer 3A talks to Docker.

Layer 3A is a clean interface that hides Docker complexity.
The rest of the agent thinks in terms of:
  "deploy this app"
  "stop this app"  
  "get logs for this app"

Layer 3A translates that into Docker API calls.
```

---

## The Mental Model

```
Rest of the agent (Layer 4B, 4C)
          │
          │  speaks in: apps, deployments, databases
          │
          ▼
    ┌─────────────┐
    │  Layer 3A   │
    │  Docker     │
    │  Manager    │
    └─────────────┘
          │
          │  speaks in: containers, images, networks, volumes
          │
          ▼
    Docker Engine API
    (via /var/run/docker.sock)
```

---

## Everything Layer 3A Owns

```
Section 1 — Docker Client Setup
  How the agent connects to Docker

Section 2 — Image Management
  Pull, cache, verify, clean up images

Section 3 — Network Management
  Create and manage Docker networks
  App-to-database communication
  Isolation between different apps

Section 4 — Volume Management
  Persistent storage for apps and databases
  Named volumes, their lifecycle

Section 5 — Container Lifecycle
  Create, start, stop, restart, remove
  Health checks
  Resource limits

Section 6 — Environment Variables
  Inject env vars into containers
  Secret handling

Section 7 — Log Streaming
  Stream container logs back to the agent
  Which then streams them to the control plane

Section 8 — State Reconciliation
  What happens when the agent restarts
  How it rediscovers what is already running
```

---

## Step 1 — Docker Client Setup

### What This Is

Before anything else, the agent needs a reliable connection to the Docker engine. This is not trivial — Docker can be configured in multiple ways and the connection point varies.

### How the Agent Connects to Docker

```
Primary method: Unix socket
  Location: /var/run/docker.sock
  This is the default on every standard Docker install
  The agent talks HTTP over this Unix socket
  No network port needed, no TCP, just a socket file

Why Unix socket and not TCP:
  TCP Docker API (port 2375/2376) is disabled by default
  Enabling it requires TLS configuration
  Unix socket is simpler, already there, and actually more secure
  (only processes that can read the socket can talk to Docker)
```

### What the Client Setup Does

**On agent startup:**

```
1. Check socket exists at /var/run/docker.sock
     → If not: surface clear error (Layer 2 should have caught this,
       but defense in depth)

2. Check socket is readable by the agent process
     → If not: surface permission error with fix instruction

3. Make a test API call: GET /info
     → Returns Docker version, storage driver, number of containers
     → If this fails: Docker is installed but broken — surface error

4. Store the Docker client as a shared resource
     → Single client instance used by all of Layer 3A
     → Not created fresh for every operation
     → Thread-safe — multiple operations can use it concurrently
```

### Connection Resilience

```
Docker daemon can be restarted independently of the agent.
(User might run: sudo systemctl restart docker)

The agent must handle this:
  → Docker API call fails with "connection refused" or "broken pipe"
  → Agent detects this is a Docker connection error, not an app error
  → Agent waits and retries the connection with backoff
  → Agent does NOT crash — it waits for Docker to come back
  → Agent reports to control plane: "Docker temporarily unavailable"
  → When Docker comes back: agent re-establishes client and continues
```

### Done Condition for Step 1
```
□ Agent connects to Docker socket on startup
□ Test API call succeeds and stores Docker version in system info
□ Clear error if socket does not exist
□ Clear error if socket exists but permission denied
□ Agent survives Docker daemon restart without crashing
□ Agent reconnects automatically when Docker comes back
□ Single shared client instance, not recreated per operation
```

---

## Step 2 — Image Management

### What Images Are in This Context

```
Every app runs from a Docker image.
Images come from:
  - Docker Hub (nginx, postgres, redis, wordpress — public images)
  - GitHub Container Registry (ghcr.io — user's own built images)
  - Any other registry the user configures

Layer 3A handles pulling these images reliably.
```

### Step 2A — Pulling Images

**The pull operation:**

```
Input:  image reference (e.g., "nginx:latest", "postgres:16", "myapp:v1.2.3")
Output: image is available locally on the server, or a clear error

Steps:
1. Parse the image reference
     → Extract registry, name, tag
     → If no tag specified, default to "latest"
     → Note: "latest" is a bad practice but users will do it

2. Check if image already exists locally
     → If yes AND the tag is a specific version (not "latest"):
       skip the pull — already have it, saves time and bandwidth
     → If yes AND the tag is "latest":
       still pull — "latest" might have been updated upstream

3. Pull the image
     → Stream pull progress back through the agent
     → Docker pull is multi-layered — each layer downloads separately
     → Report progress: "Pulling layer 3/7..."
     → This is what the user sees in the deployment log

4. Verify pull succeeded
     → Inspect the pulled image to confirm it exists and is healthy
     → Record: image ID, size, creation date
```

**What can go wrong and how to surface it:**

```
Image does not exist:
  "Image 'myapp:v999' was not found on Docker Hub.
   Check the image name and tag in your deployment settings."

Registry authentication required:
  "Image 'ghcr.io/yourname/myapp:latest' requires authentication.
   Add your registry credentials in the deployment settings."

Network error during pull:
  "Download interrupted while pulling 'nginx:latest'.
   This is usually a temporary network issue. Retrying..."

Disk full during pull:
  "Not enough disk space to download 'postgres:16' (requires ~400MB).
   Current available: 300MB.
   Free up space or expand your disk to continue."

Pull timeout:
  "Pulling 'myapp:latest' is taking longer than expected.
   Large images on slow connections can take several minutes.
   Still trying..."
```

### Step 2B — Image Caching Strategy

```
Problem: pulling the same image repeatedly wastes bandwidth and time.
         A $5 VPS often has limited bandwidth.

Strategy:

For tagged images (postgres:16, nginx:1.25.3):
  → Pull once, reuse forever until explicitly asked to update
  → Tag includes version = image content is fixed
  → No need to re-pull on every deploy

For "latest" tag:
  → Pull on every deploy
  → "latest" means "most recent" — must check for updates
  → But: check the image digest first
    → If digest matches what we have locally: skip pull, already current
    → If digest differs: pull the new version

For user's own images (their app image):
  → Always pull on deploy
  → User just built and pushed a new version
  → Must get the latest

Cache metadata to store per image:
  - Image ID (Docker's content hash)
  - Image reference (name:tag)
  - Pull timestamp
  - Image size
  - Last used timestamp (for cleanup decisions)
```

### Step 2C — Image Cleanup

```
Problem: pulled images accumulate and fill the disk.
         Each app might have multiple versions sitting around.

Cleanup rules:

Automatic cleanup (runs weekly):
  → Remove images that have no running containers using them
  → Remove images not used in the last 30 days
  → BUT: keep the current version of every deployed app
  → Never remove an image that a running container depends on

Before cleanup, check available disk:
  → If disk above 70% full: run cleanup immediately
  → If disk above 85% full: run aggressive cleanup AND alert user

What NOT to remove automatically:
  → Images currently used by running containers
  → The immediately previous version of each app
    (needed for one-click rollback — Layer 4B)
  → Images marked as "pinned" by the deployment config
```

### Done Condition for Step 2
```
□ Pull succeeds for a public Docker Hub image
□ Pull succeeds for a specific version tag
□ Pull skips correctly when image already exists locally (versioned tag)
□ Pull re-checks when tag is "latest"
□ Each failure type produces a distinct plain-English error message
□ Pull progress is reported (not a silent wait)
□ Cleanup runs and removes old images correctly
□ Cleanup never removes images used by running containers
□ Disk pressure triggers immediate cleanup
```

---

## Step 3 — Network Management

### Why Docker Networking Matters Here

```
This is the most misunderstood part of Docker for non-technical users.

The problem:
  User deploys a Next.js app and a Postgres database.
  The Next.js app needs to talk to Postgres.
  In Docker, containers are isolated by default.
  They cannot just say "connect to localhost:5432" — that does not work.

The solution:
  Docker networks.
  Put the app and its database on the same Docker network.
  Then the app can reach Postgres at "postgres:5432"
    (using the container name as a hostname).

Layer 3A manages this completely.
The user never knows Docker networks exist.
```

### Network Architecture Design

```
One network per "project" (an app + its databases + its services)

Example: User deploys "myshop" with Postgres and Redis

Network: yourplatform_myshop

Containers on this network:
  yourplatform_myshop_app       → the Next.js app
  yourplatform_myshop_postgres  → the Postgres database
  yourplatform_myshop_redis     → Redis cache

Inside this network:
  App reaches Postgres at: postgres:5432
  App reaches Redis at:    redis:6379
  Nothing outside this network can reach Postgres or Redis directly
  Only Caddy can reach the app (on the app's exposed port)

Isolation:
  "myblog" project has its own separate network
  myblog's database cannot be reached from myshop's app
  Complete isolation between projects
```

### Step 3A — Creating a Project Network

```
When a new project is deployed:

1. Generate network name: yourplatform_{project_name}
     → Sanitize project name: lowercase, alphanumeric + hyphens only
     → Example: "My Shop!" becomes "my-shop"
     → Full name: "yourplatform_my-shop"

2. Check if network already exists
     → If yes: reuse it (idempotent — safe to call multiple times)
     → If no: create it

3. Create with these settings:
     → Driver: bridge (standard, works everywhere)
     → Internal: false (containers can still reach internet for npm install etc.)
     → Labels: mark it as owned by yourplatform, include project name
       (important for cleanup — know which networks belong to us)

4. Store network ID alongside the project record
```

### Step 3B — Connecting Containers to Networks

```
When a container is created:
  → Attach it to its project network at creation time
  → Do not create the container and then attach — do it in one step
  → This ensures the container is never briefly networkless

When a database container is created for a project:
  → It joins the project network
  → Its hostname on the network = its container name
    (e.g., "yourplatform_myshop_postgres")
  → BUT: we give the user a simpler alias

Container aliases:
  The container "yourplatform_myshop_postgres" gets alias "postgres"
  on the project network.
  
  So the app connects to "postgres:5432" not the full container name.
  Clean, simple, predictable.
  
  Standard aliases we always set:
    postgres container → alias: "postgres" and "db"
    mysql container    → alias: "mysql" and "db"  
    redis container    → alias: "redis" and "cache"
```

### Step 3C — Port Exposure Strategy

```
What gets exposed to the host and what does not:

App containers (Next.js, Django, WordPress):
  → Expose their port to the host on a random high port
  → Example: app runs on 3000 inside container
             exposed as 127.0.0.1:32847:3000 on the host
  → Only binds to 127.0.0.1 — NOT to 0.0.0.0
  → Caddy talks to 127.0.0.1:32847 to reach the app
  → Users cannot reach the app directly — must go through Caddy
  → This is why HTTPS works — Caddy is the only entry point

Database containers (Postgres, MySQL, Redis):
  → Do NOT expose any port to the host by default
  → They are only reachable from within the project network
  → This is correct security behavior
  → A Postgres database should never be reachable from the internet

Exception — database management port (optional):
  → If user explicitly enables it in dashboard
  → Expose on 127.0.0.1 only, not 0.0.0.0
  → Still requires SSH tunnel to reach from the user's laptop
  → We make this very clear in the UI
```

### Step 3D — Network Cleanup

```
When a project is deleted:
  1. Stop and remove all containers in the project
  2. Remove the project network
  3. Remove associated volumes (if user confirmed data deletion)

Order matters:
  → Must remove containers before removing the network
  → Cannot remove a network that has connected containers

Orphan network detection:
  → On agent startup, list all networks with yourplatform labels
  → Check if any have zero connected containers
  → If a network is empty (no containers), it is an orphan
  → Remove orphan networks automatically
  → Log what was removed and report to control plane
```

### Done Condition for Step 3
```
□ Creating a project creates a network with the correct name
□ Network name is sanitized correctly from any project name
□ App container and database container can reach each other by alias
□ App container can reach the internet (for package downloads etc.)
□ Database container is NOT reachable from outside the project network
□ App port is exposed only on 127.0.0.1, not 0.0.0.0
□ Deleting a project removes its network
□ Orphan networks are cleaned up on agent startup
□ Creating the same project twice does not create duplicate networks
```

---

## Step 4 — Volume Management

### What Volumes Are For

```
Problem: containers are stateless.
  If you stop a Postgres container and start a new one,
  all the data is gone.
  
Solution: Docker volumes.
  A volume is storage that exists outside the container.
  The container mounts the volume and reads/writes there.
  You can remove the container, create a new one,
  mount the same volume, and all data is intact.

What needs volumes:
  → Postgres data directory (/var/lib/postgresql/data)
  → MySQL data directory (/var/lib/mysql)
  → Redis data (if persistence is enabled)
  → WordPress uploads (/var/www/html/wp-content/uploads)
  → Any app that writes files that must survive restarts
```

### Step 4A — Volume Naming

```
Consistent, predictable naming:

yourplatform_{project_name}_{purpose}

Examples:
  yourplatform_myshop_postgres_data
  yourplatform_myshop_redis_data
  yourplatform_myblog_uploads
  yourplatform_myblog_postgres_data

Rules:
  → Always prefixed with yourplatform_ (we know what belongs to us)
  → Includes project name (we know which project it belongs to)
  → Includes purpose (we know what data is inside)
  → Lowercase, alphanumeric, hyphens and underscores only
```

### Step 4B — Volume Creation

```
When creating a volume:

1. Check if volume already exists (by name)
     → If yes: reuse it — this is correct behavior
     → Reusing a volume = preserving existing data
     → This is what we want on redeploy

2. If no: create it
     → Driver: local (default, uses server's filesystem)
     → Labels: project name, purpose, creation timestamp
     
3. Store volume name in the deployment/database record
     → So we know which volume belongs to which deployment

On redeploy:
  → Old container is removed
  → New container is created
  → Same volume is attached
  → Data is preserved automatically
  → User never thinks about this — it just works
```

### Step 4C — Volume Backup Integration Point

```
Volumes are what Layer 3C (Restic backups) backs up.

Layer 3A must provide Layer 3C with:
  → The volume name
  → The mount path inside the container
  → The project it belongs to

Layer 3A must also provide a way to:
  → Pause writes to a volume temporarily (for consistent backups)
    → For Postgres: trigger a checkpoint before backup
    → For MySQL: flush tables with read lock
    → For Redis: trigger BGSAVE
    → For generic volumes: no pause needed

This is an interface Layer 3A exposes.
Layer 3C calls it. Layer 3A knows how to do it per database type.
```

### Step 4D — Volume Cleanup

```
When a project is deleted:
  → Volumes are NOT deleted by default
  → This is intentional safety — data deletion is irreversible
  → User must explicitly confirm data deletion in dashboard
  
When user confirms:
  → Verify no container is currently mounted to the volume
  → Delete the volume
  → Record deletion event with timestamp

Orphan volume detection:
  → On agent startup, list all volumes with yourplatform labels
  → Check if any are not mounted by any container
  → List them in the dashboard as "unmounted volumes"
  → User can review and delete from dashboard
  → Agent never auto-deletes volumes — too risky
```

### Done Condition for Step 4
```
□ Volume created with correct naming convention
□ Redeploying an app reuses existing volume (data preserved)
□ Postgres data survives container removal and recreation
□ Volume names are stored in deployment records
□ Deleting a project does NOT delete volumes by default
□ Explicit user confirmation required before volume deletion
□ Orphan volumes listed in dashboard, not auto-deleted
□ Layer 3C can get volume info from Layer 3A
```

---

## Step 5 — Container Lifecycle

### The Container Model

```
Each deployed app or database = one container.

Container naming convention:
  yourplatform_{project_name}_{role}
  
  Examples:
    yourplatform_myshop_app         → the app itself
    yourplatform_myshop_postgres    → its database
    yourplatform_myshop_redis       → its cache
    yourplatform_myblog_app
    yourplatform_myblog_postgres

This naming lets the agent:
  → Find all containers belonging to a project
  → Find all containers it manages (all start with yourplatform_)
  → Know what role a container plays
```

### Step 5A — Container Creation

```
Creating a container is the most complex operation in Layer 3A.
It involves coordinating: image, network, volume, env vars, ports, limits.

Order of operations for creating a container:

1. Ensure image is pulled (Step 2A)
2. Ensure network exists (Step 3A)
3. Ensure volumes exist (Step 4B)
4. Determine host port for exposure (Step 3C)
5. Assemble the container configuration:
     → Name: yourplatform_{project}_{role}
     → Image: the pulled image
     → Network: the project network
     → Network aliases: "app", "postgres", etc.
     → Volumes: mount each volume at the correct path
     → Environment variables: injected at runtime (Step 6)
     → Port bindings: app port → 127.0.0.1:random_port
     → Resource limits (Step 5E)
     → Health check (Step 5D)
     → Restart policy: always (container restarts if it crashes)
     → Labels: project name, role, managed-by: yourplatform
6. Create the container (do not start yet)
7. Verify container was created correctly
8. Start the container
9. Wait for health check to pass (or timeout)
10. Record the container ID, host port, and status
```

### Step 5B — Starting and Stopping

```
Start:
  → Check container exists and is not already running
  → Start it
  → Wait up to 30 seconds for it to reach running state
  → If it exits immediately: read exit code and last log lines
    → Surface as: "App crashed on startup. Last output: [log lines]"

Stop (graceful):
  → Send SIGTERM to the container's main process
  → Give it 30 seconds to shut down cleanly
  → If still running after 30 seconds: send SIGKILL
  → Mark container as stopped in state records

Stop (immediate):
  → Send SIGKILL directly
  → Used only when graceful stop is explicitly bypassed
  → Or when graceful stop has already been attempted and failed

Restart:
  → Stop (graceful)
  → Start
  → Wait for health check
  → Same as a stop + start, not a Docker restart command
  → Reason: gives us more control and better error handling
```

### Step 5C — Removal

```
Remove a container:
  → Container must be stopped first
  → If container is running: stop it, then remove
  → Remove the container
  → Do NOT remove the volume (Step 4D — volumes are separate lifecycle)
  → Do NOT remove the network (may have other containers on it)
  → Record removal with timestamp

Remove all containers for a project:
  → Get all containers with this project's label
  → Stop each one (parallel is fine, they do not depend on stop order)
  → Remove each one
  → Then remove the network (Step 3D)
  → Volumes remain unless user explicitly confirmed deletion
```

### Step 5D — Health Checks

```
Health checks tell the agent whether a container is actually working,
not just running.

"Running" means the process started.
"Healthy" means the app inside is responding correctly.

These are different:
  → A Node.js app might start the process but crash during init
  → It is "running" but not "healthy"
  → Without a health check, the agent thinks it is fine

Health check types by app type:

Web apps (HTTP):
  → Check: HTTP GET to http://localhost:{app_port}/
  → Healthy: response status < 500
  → Interval: every 30 seconds
  → Timeout: 10 seconds
  → Start period: 60 seconds (give the app time to boot)
  → Retries: 3 (must fail 3 times in a row to be considered unhealthy)

Postgres:
  → Check: pg_isready -U postgres
  → This is the official Postgres health check tool
  → Healthy: exit code 0

MySQL:
  → Check: mysqladmin ping
  → Healthy: exit code 0

Redis:
  → Check: redis-cli ping
  → Healthy: responds with "PONG"

Generic (unknown app type):
  → Check: is the process still running (PID 1 in container alive)
  → Less informative but better than nothing

Health check results:
  → Reported to Layer 4C (health reporter) every check interval
  → If unhealthy: Layer 4C decides whether to restart and alert
  → Layer 3A provides the health data, Layer 4C decides what to do with it
```

### Step 5E — Resource Limits

```
Why resource limits matter:
  One misbehaving app on a $5 VPS can kill everything else.
  An app with a memory leak will eat all RAM and crash the server.
  Resource limits contain the damage.

Default limits applied to every app container:

Memory:
  → Soft limit (memory reservation): 256MB
    → Docker tries to keep the container under this
    → Other containers can still get memory if needed
  → Hard limit (memory limit): 512MB
    → Container is killed if it exceeds this
    → OOM (out of memory) kill
  → Memory swap: 0
    → Disable swap for containers
    → Swap on a $5 VPS is deadly slow
    → Better to OOM kill than to swap

CPU:
  → CPU shares: 512 (default is 1024 — gives app half priority)
  → No hard CPU limit — allow bursting during low load
  → Reason: CPU is time-shared, less dangerous than RAM runaway

For database containers:
  → Higher memory limits (databases legitimately use more RAM)
  → Postgres default: 512MB soft, 1GB hard
  → MySQL default: 512MB soft, 1GB hard
  → Redis default: 128MB soft, 256MB hard

User-configurable limits:
  → Dashboard lets user adjust within bounds
  → Cannot set limits higher than available server RAM
  → Cannot set limits that would leave less than 512MB for the system
  → Agent validates limits before applying

What happens when a container hits its memory limit:
  → Container is OOM-killed by the kernel
  → Docker restarts it (because restart policy is "always")
  → Agent detects the OOM kill (exit code 137)
  → Creates an alert: "Your app ran out of memory and was restarted.
     Current limit: 512MB. Consider increasing the memory limit
     or investigating memory usage in your app."
```

### Done Condition for Step 5
```
□ Container is created with all settings in one operation
□ Container naming convention is consistent
□ Starting a crashed container surfaces the crash reason and log lines
□ Graceful stop works (SIGTERM then SIGKILL after timeout)
□ Removal stops the container first if running
□ Health check passes for a running Postgres container
□ Health check passes for a running web app on its HTTP port
□ Health check failure is detected and reported to Layer 4C
□ Memory limit of 512MB is enforced (test: run a memory hog container)
□ OOM kill produces a plain-English alert
□ Resource limits cannot be set above available server RAM
```

---

## Step 6 — Environment Variables

### Why This Needs Careful Handling

```
Environment variables for apps often contain secrets:
  DATABASE_URL=postgres://user:password@postgres/mydb
  STRIPE_SECRET_KEY=sk_live_...
  JWT_SECRET=...
  SENDGRID_API_KEY=...

These must:
  → Never appear in logs
  → Never be sent to the control plane in plaintext
  → Be injected into containers at runtime
  → Be stored encrypted at rest on the server
```

### Step 6A — Storage

```
Where env vars are stored:
  /etc/yourplatform/envs/{project_name}.env
  
  File permissions: 600 (root read/write only)
  
  Format: standard .env format
    KEY=value
    ANOTHER_KEY=another value
    
  This file lives on the user's server only.
  The control plane stores only the key names, not the values.
  The values never leave the server.

Why store on the server, not the control plane:
  → User owns their secrets
  → If your control plane is breached, no secrets are exposed
  → Agent reads the file locally — no network request for secrets
  → This is the correct model for a "bring your own server" product
```

### Step 6B — What the Control Plane Knows

```
Control plane stores:
  → List of env var key names for each deployment
  → NOT the values

Example:
  Control plane knows: ["DATABASE_URL", "STRIPE_SECRET_KEY", "JWT_SECRET"]
  Control plane does NOT know: the actual values

Dashboard shows:
  → The key names (so user knows what they have configured)
  → Values are masked: DATABASE_URL = ••••••••••••
  → User can update a value — new value is sent encrypted to the agent
  → Agent stores it locally, acknowledges receipt
  → Value never sits in the control plane database
```

### Step 6C — Injecting into Containers

```
At container creation time:
  1. Read the env file for this project
  2. Parse each KEY=VALUE pair
  3. Pass the full list to Docker's container creation API
     as environment variables
  4. Docker injects them into the container at startup

Automatic env vars always added by the agent:
  → YOURPLATFORM=true
    (app can detect it is running on YourPlatform)
  → PORT={the port the app should listen on}
    (standardized — app always knows which port to use)

Database connection env vars (auto-generated):
  When a Postgres database is added to a project:
  → Agent auto-generates DATABASE_URL
    = postgres://yourplatform:{random_password}@postgres:5432/{dbname}
  → Injects it into the app container
  → User does not need to figure out the connection string
  → Password is random, stored in the project env file
```

### Step 6D — Updating Env Vars

```
Flow when user updates an env var from the dashboard:

1. User types new value in dashboard
2. Dashboard sends to control plane:
   { key: "STRIPE_SECRET_KEY", value: "sk_live_newvalue" }
   over HTTPS (encrypted in transit)
3. Control plane forwards command to agent via WebSocket
4. Agent receives it, writes new value to the env file
5. Agent acknowledges receipt to control plane
6. Control plane marks the command as completed
7. Value is NOT stored on the control plane — only passed through

The app does not automatically get the new value:
  → Env vars are set at container creation time in Docker
  → A running container does not see env var changes
  → Agent must restart the container for new values to take effect
  → Dashboard shows: "Env var updated. Restart your app to apply changes."
  → Or: option to restart immediately
```

### Done Condition for Step 6
```
□ Env file is created with 600 permissions
□ Values are never logged anywhere
□ Control plane only receives key names, not values
□ Container starts with correct env vars injected
□ PORT env var is always present
□ DATABASE_URL is auto-generated when Postgres is added
□ Updating an env var does not automatically restart the container
□ Dashboard shows key names with masked values
□ New values are passed through control plane without being stored
```

---

## Step 7 — Log Streaming

### What This Is

```
User deploys an app and wants to see what it is doing.
They click "Logs" in the dashboard.
They see the app's stdout and stderr output in real time.

This is the same as:
  docker logs -f container_name

But delivered to the browser through:
  Container → Agent → WebSocket → Control Plane → Browser
```

### Step 7A — Attaching to Container Logs

```
The Docker API provides a log streaming endpoint.
Given a container ID, it returns a stream of log lines.
Each line has:
  → Timestamp
  → Stream type (stdout or stderr)
  → The log line content

The agent:
  1. Receives a command from the control plane:
     "Start streaming logs for project myshop"
  2. Identifies the container: yourplatform_myshop_app
  3. Opens a log stream from Docker for that container
  4. For each log line received:
     → Formats it: { timestamp, stream, line }
     → Sends it over the WebSocket to the control plane
     → Control plane forwards to the browser
  5. Continues until:
     → The browser disconnects (user closed the logs view)
     → The container stops
     → The agent receives a "stop streaming" command
```

### Step 7B — Historical Logs

```
When a user opens the logs view, they need to see:
  → Recent historical logs (last N lines) immediately
  → Then new logs as they arrive in real time

Historical logs:
  → Docker keeps logs in its own log driver
  → Default log driver: json-file
  → Location: /var/lib/docker/containers/{id}/{id}-json.log
  → The agent requests the last 200 lines from Docker's API
  → These are sent first as a "history" batch
  → Then live streaming begins

Log rotation:
  → Configure Docker logging with size limits
  → Max log file size: 10MB per container
  → Max log files: 3 (30MB total per container)
  → Without this, logs fill the disk on chatty apps
  → This is set at container creation time in the Docker config
```

### Step 7C — Multi-Container Logs

```
A project has multiple containers:
  → The app
  → Postgres
  → Redis

Dashboard can show:
  → All logs interleaved (with a prefix showing which container)
  → Or logs for just one container

When streaming all containers:
  → Open separate log streams for each container
  → Prefix each line with the container role: [app], [postgres], [redis]
  → Merge streams and send together
  → Order by timestamp
```

### Step 7D — Log Filtering

```
For the MVP: no filtering, just raw logs.

Future enhancement (not MVP):
  → Filter by log level (ERROR, WARN, INFO)
  → Filter by keyword search
  → Time range selection

For MVP, streaming all lines is sufficient.
```

### Done Condition for Step 7
```
□ Opening logs view shows last 200 lines immediately
□ New log lines appear in real time (under 2 second delay)
□ Both stdout and stderr are captured
□ Timestamps are correct and in a readable format
□ Closing the logs view stops the streaming (no resource leak)
□ Log rotation is configured (10MB max, 3 files)
□ Container restart does not break the log stream
   (agent detects container restart and re-attaches)
□ Multi-container logs are prefixed with container role
```

---

## Step 8 — State Reconciliation

### Why This Is Critical

```
The agent restarts. It could be because:
  → Server was rebooted
  → Agent crashed and systemd restarted it
  → Agent was updated to a new version
  → Docker was restarted and agent restarted with it

When the agent comes back up, it has no memory.
But Docker kept running (or also restarted and kept containers running).

The agent must rediscover reality:
  "What is actually running on this server right now?"
  "Does it match what I think should be running?"
  "What do I need to fix?"
```

### The Reconciliation Process

```
On agent startup, after Layer 2 pre-flight:

1. Query Docker for all running containers
   → Filter by label "managed-by=yourplatform"
   → These are all containers we created

2. For each discovered container:
   → Read its labels to determine project and role
   → Read its current status (running, stopped, exited)
   → Read its health check status if applicable
   → Record its host port assignments

3. Compare against deployment records
   → Agent maintains a local record of what should be running
   → Stored in /var/lib/yourplatform/state.json

4. Identify discrepancies:

   Container running but not in our records:
     → Was created by a previous agent version or manually
     → If it has our labels: adopt it, add to records
     → If it does not have our labels: ignore it (not ours)

   Container in records but not running:
     → Container crashed or was stopped
     → Check exit code to understand why
     → If restart policy is "always": Docker should restart it
     → If Docker did not restart it: try to start it
     → If it keeps failing: mark as failed, alert user

   Container in records, running, but wrong version:
     → This should not happen normally
     → Log it as a warning, report to control plane

5. Report reconciliation results to control plane
   → "3 containers running, all healthy"
   → "1 container found crashed, attempted restart, succeeded"
   → "1 container found crashed, restart failed: [reason]"

6. Update the dashboard with current real state
```

### The State File

```
/var/lib/yourplatform/state.json

What it stores:
{
  "projects": {
    "myshop": {
      "containers": {
        "app": {
          "container_id": "abc123",
          "image": "myshop:v1.2.3",
          "status": "running",
          "host_port": 32847,
          "network": "yourplatform_myshop",
          "volumes": ["yourplatform_myshop_postgres_data"],
          "started_at": "2024-01-15T10:00:00Z"
        },
        "postgres": {
          "container_id": "def456",
          ...
        }
      }
    }
  }
}

This file is the agent's memory.
It is updated after every operation that changes container state.
On startup, it is the reference for reconciliation.
```

### Done Condition for Step 8
```
□ Agent restarts and correctly discovers all running containers
□ Running containers are matched to deployment records via labels
□ Crashed containers are detected and restart is attempted
□ Persistent crash (3 attempts failed) creates an alert
□ State file is updated after every container operation
□ State file survives agent restart and is correctly read on startup
□ Control plane receives accurate state after every agent restart
□ Containers not created by the agent are correctly ignored
```

---

## Layer 3A Overall Done Condition

```
The full integration test:

Test 1 — Deploy a web app with Postgres:
  □ Image pulled successfully with progress shown
  □ Project network created
  □ Postgres volume created
  □ Postgres container starts and passes health check
  □ App container starts, can reach postgres at "postgres:5432"
  □ App port exposed only on 127.0.0.1
  □ Env vars injected including DATABASE_URL
  □ Logs stream to the dashboard

Test 2 — Redeploy with a new image version:
  □ New image pulled
  □ Old container stopped gracefully
  □ New container started with same volume (data preserved)
  □ Same network reused
  □ Health check passes on new container

Test 3 — App runs out of memory:
  □ Container is OOM killed
  □ Docker restarts it (restart policy)
  □ Alert created: plain English memory limit message
  □ Container is running again within 30 seconds

Test 4 — Agent restarts:
  □ Agent comes back up
  □ Reconciliation finds all running containers
  □ State file matches Docker reality
  □ Dashboard shows correct state within 10 seconds

Test 5 — Delete a project:
  □ App container stopped and removed
  □ Postgres container stopped and removed
  □ Network removed
  □ Volumes remain (not deleted)
  □ Orphan cleanup runs and finds nothing to clean

When all 5 tests pass, Layer 3A is done.
Move to Layer 3B — Caddy Management.
```

---

## What Layer 3A Does NOT Do

```
Layer 3A does not:
├── Decide WHEN to deploy (that is Layer 4B — Command Executor)
├── Decide WHAT to deploy (that is Layer 4B)
├── Route HTTP traffic to containers (that is Layer 3B — Caddy)
├── Schedule backups (that is Layer 3C)
├── Monitor health over time (that is Layer 4C)
├── Communicate with the control plane (that is Layer 4A)
└── Handle git-based deployments (that is a Layer 4B extension)

Layer 3A is purely the Docker interface.
It does what it is told, and tells the truth about what it found.
```

---

**Ready for Layer 3B — Caddy Management?**