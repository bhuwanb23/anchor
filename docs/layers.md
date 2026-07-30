# System Layer Analysis — Complete Breakdown

Let me first decompose the entire system into its layers, map every dependency, then we'll work through each one systematically.

---

## The Complete Layer Map

```
┌─────────────────────────────────────────────────────────────┐
│                    LAYER 7: FRONTEND                        │
│              Next.js + TypeScript + Tailwind                │
│   Dashboard UI, onboarding wizard, real-time log viewer     │
└─────────────────────────┬───────────────────────────────────┘
                          │ HTTP / WebSocket
┌─────────────────────────▼───────────────────────────────────┐
│                 LAYER 6: CONTROL PLANE API                  │
│                      Go HTTP Server                         │
│   Auth, routing, command dispatch, subscription enforcement  │
└──────┬──────────────────┬──────────────────┬────────────────┘
       │                  │                  │
┌──────▼──────┐  ┌────────▼───────┐  ┌───────▼────────┐
│  LAYER 5A   │  │   LAYER 5B     │  │   LAYER 5C     │
│    Auth &   │  │  WebSocket     │  │   Database     │
│   Sessions  │  │     Hub        │  │   (Postgres)   │
│  JWT + RBAC │  │ Agent ↔ Browser│  │  State Store   │
└─────────────┘  └────────┬───────┘  └────────────────┘
                          │ Persistent WS tunnel
┌─────────────────────────▼───────────────────────────────────┐
│                    LAYER 4: AGENT CORE                      │
│                   Go Static Binary                          │
│        Runs on user's server as a systemd service           │
│                                                             │
│  ┌──────────────┐  ┌─────────────┐  ┌──────────────────┐   │
│  │  LAYER 4A    │  │  LAYER 4B   │  │    LAYER 4C      │   │
│  │  Agent       │  │  Command    │  │   Health &       │   │
│  │  Lifecycle   │  │  Executor   │  │   Log Reporter   │   │
│  │  install,    │  │  deploy,    │  │   streams back   │   │
│  │  update,     │  │  restart,   │  │   to control     │   │
│  │  reconnect   │  │  rollback   │  │   plane          │   │
│  └──────────────┘  └─────────────┘  └──────────────────┘   │
└──────┬──────────────────┬──────────────────┬────────────────┘
       │                  │                  │
┌──────▼──────┐  ┌────────▼───────┐  ┌───────▼────────┐
│  LAYER 3A   │  │   LAYER 3B     │  │   LAYER 3C     │
│   Docker    │  │     Caddy      │  │    Restic      │
│  Management │  │  Reverse Proxy │  │    Backups     │
│  containers,│  │  + Auto HTTPS  │  │  scheduled,    │
│  images,    │  │  + Routing     │  │  to S3/B2      │
│  networks   │  │                │  │                │
└─────────────┘  └────────────────┘  └────────────────┘
                          │
┌─────────────────────────▼───────────────────────────────────┐
│                 LAYER 2: SERVER ENVIRONMENT                  │
│                                                             │
│   OS detection, port checking, firewall detection,          │
│   disk space, Docker install/verify, dependency audit        │
└─────────────────────────────────────────────────────────────┘
                          │
┌─────────────────────────▼───────────────────────────────────┐
│                  LAYER 1: INSTALL LAYER                     │
│                                                             │
│       One-line curl script, binary download,                │
│       systemd service creation, first-time setup            │
└─────────────────────────────────────────────────────────────┘
```

---

## Every Layer Defined

### Layer 1 — Install Layer
The entry point. A shell script the user pastes into their terminal. Downloads the agent binary, detects the OS, creates a systemd service, runs first-time environment checks, registers the server with the control plane using a token, and exits. The user never touches this server again through SSH.

**Owns:** `install.sh`, binary download, systemd unit file, initial registration handshake

---

### Layer 2 — Server Environment
The agent's first action after install: audit the server it just landed on. Check OS version, architecture, available ports, existing processes on 80/443, Docker presence and version, available disk, available RAM. Either fix problems automatically or surface them as plain-English errors before anything else runs.

**Owns:** Environment detection, pre-flight checks, Docker installation if missing, problem reporting to control plane

---

### Layer 3A — Docker Management
The agent's interface to Docker. Pull images, create containers with correct networking, manage environment variables, handle container lifecycle (start, stop, restart, remove), set resource limits, manage Docker networks so apps can talk to their databases, stream container logs.

**Owns:** All Docker API calls, container state management, image cache management, inter-container networking

---

### Layer 3B — Caddy Management
The reverse proxy layer. When an app deploys on port 3000, Caddy gets told: route `myapp.yourdomain.com` to `localhost:3000` and handle HTTPS automatically. When an app is removed, that route is deleted. Caddy config is managed programmatically by the agent via Caddy's admin API.

**Owns:** Caddy process management, dynamic route configuration, HTTPS certificate lifecycle, custom domain routing

---

### Layer 3C — Restic Backups
Scheduled backup jobs. Restic snapshots the right data (database dumps, persistent volumes) and pushes them to the configured destination (S3-compatible storage). Stores backup metadata in the control plane so the user can browse and restore from the dashboard.

**Owns:** Backup scheduling, restic execution, backup metadata reporting, restore orchestration

---

### Layer 4A — Agent Lifecycle
How the agent manages itself. Persistent WebSocket connection to the control plane with automatic reconnection and exponential backoff. Self-update mechanism: polls for new agent versions, downloads, checksums, smoke-tests, atomically swaps binary. Survives server reboots via systemd.

**Owns:** Control plane connection, reconnection logic, self-update, systemd integration

---

### Layer 4B — Command Executor
The agent receives commands from the control plane and executes them. Deploy, rollback, restart, stop, run backup, fetch logs. Each command is acknowledged, executed, and its result is reported back. Commands received while disconnected are queued and replayed on reconnect.

**Owns:** Command parsing, execution, acknowledgment, offline command queue

---

### Layer 4C — Health and Log Reporter
Continuous background reporting. Every N seconds, collect CPU usage, RAM usage, disk usage, container statuses, and push to control plane. Stream container logs in real time over the WebSocket. Detect anomalies (app crashed, disk 90% full) and emit plain-English alert events.

**Owns:** Metrics collection, log streaming, anomaly detection, alert generation

---

### Layer 5A — Auth and Sessions
User registration, login, JWT issuance and refresh, team membership, role-based access control (owner vs member), session management. Subscription tier enforcement — a free tier user cannot connect a second server.

**Owns:** User accounts, JWT, teams, RBAC, subscription gating

---

### Layer 5B — WebSocket Hub
The bridge between the browser and the agent. Manages all persistent WebSocket connections: one from each connected agent, one from each open browser session. Routes commands from browser to the correct agent. Routes log streams and health updates from agent to the correct browser sessions watching that server.

**Owns:** Connection registry, message routing, agent↔browser bridging, connection health monitoring

---

### Layer 5C — Database (PostgreSQL)
The source of truth for all platform state. Users, servers, deployments, app state, backup records, alert history, team memberships, billing state.

**Owns:** All persistent state, migrations, query layer

---

### Layer 6 — Control Plane API
The HTTP API the frontend talks to. REST endpoints for all CRUD operations plus WebSocket upgrade endpoint for the browser real-time connection. Validates auth on every request, enforces subscription limits, coordinates between layers 5A/5B/5C.

**Owns:** HTTP routing, request validation, response shaping, API versioning

---

### Layer 7 — Frontend
The dashboard. Two major zones: onboarding flow (wizard-style, linear) and the day-to-day view (calm overview with obvious actions). Talks to the control plane API over HTTP and WebSocket. Renders real-time log streams, health metrics, deployment history.

**Owns:** All UI, client-side state, WebSocket client, user-facing error messages

---

## Build Order Map

```
Build sequence (each layer unblocks the next):

WEEK 1-2    Layer 1    Install script + binary delivery
WEEK 1-2    Layer 2    Server environment detection
WEEK 3-4    Layer 3A   Docker management
WEEK 3-4    Layer 3B   Caddy management  
WEEK 5-6    Layer 4A   Agent lifecycle + WS connection
WEEK 5-6    Layer 4B   Command executor
WEEK 5-6    Layer 4C   Health + log reporter
            ↑ At this point: agent works end-to-end, no dashboard yet
            
WEEK 7-8    Layer 5C   Database schema
WEEK 7-8    Layer 5A   Auth
WEEK 7-8    Layer 5B   WebSocket hub
WEEK 9-10   Layer 6    Control plane API
WEEK 11-14  Layer 7    Frontend dashboard
            ↑ At this point: MVP complete
            
WEEK 15+    Layer 3C   Backups (restic)
            Extend 4B  Backup commands
            Extend 7   Backup UI
```

---

## Dependencies Between Layers

```
Layer 7  needs  Layer 6
Layer 6  needs  Layer 5A, 5B, 5C
Layer 5B needs  Layer 4A (agents connect to it)
Layer 4B needs  Layer 3A, 3B (executes via them)
Layer 4C needs  Layer 3A (reads Docker stats and logs)
Layer 4A needs  Layer 1, 2 (agent is installed and env is clean)
Layer 3B needs  Layer 3A (routes to Docker container ports)
Layer 3C needs  Layer 3A (backs up container volumes)
Layer 1  needs  Layer 2 (runs environment checks immediately after install)
```

---

## Which Layer Do We Start With?

Based on the build order and the fact that everything downstream depends on it, **we start with Layer 1 — the install layer**, then move immediately to **Layer 2 — server environment detection**.

These two together are what makes the promise of "paste one command, your server is connected" real or fake.

---

**Ready to start Layer 1 in full technical detail — the install script, binary delivery, systemd service creation, and first-time registration flow?**

Or tell me if you want to reorder and start somewhere else.