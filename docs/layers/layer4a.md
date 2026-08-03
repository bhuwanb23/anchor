# Layer 4A — Agent Lifecycle: Complete Plan

---

## What Layer 4A Actually Is

```
Layer 4A is the agent's nervous system.

Every other layer does a specific job:
  3A → Docker
  3B → Caddy
  3C → Backups

Layer 4A is what keeps the agent alive, connected,
and up to date so all those other layers can do their jobs.

If Layer 4A fails:
  → Agent loses connection to control plane
  → User cannot deploy anything
  → Dashboard shows server as disconnected
  → But: apps keep running (Docker and Caddy run independently)

Layer 4A owns four things:
  1. The persistent WebSocket connection to the control plane
  2. Reconnection logic when that connection drops
  3. Self-update: keeping the agent binary current
  4. Systemd integration: surviving reboots and crashes
```

---

## The Mental Model

```
Control Plane
     │
     │  WebSocket (wss://)
     │  Persistent, bidirectional
     │  Agent initiates, always outbound
     │
     ▼
┌─────────────────────────────────────┐
│           Layer 4A                  │
│                                     │
│  ┌─────────────┐  ┌──────────────┐  │
│  │  Connection │  │   Updater    │  │
│  │  Manager    │  │              │  │
│  │             │  │  polls for   │  │
│  │  connects   │  │  new versions│  │
│  │  reconnects │  │  downloads   │  │
│  │  heartbeats │  │  verifies    │  │
│  │  auth       │  │  swaps       │  │
│  └──────┬──────┘  └──────────────┘  │
│         │                           │
│         │ incoming commands          │
│         ▼                           │
│  ┌─────────────┐                    │
│  │  Dispatcher │                    │
│  │             │                    │
│  │  routes to  │                    │
│  │  4B, 4C     │                    │
│  └─────────────┘                    │
└─────────────────────────────────────┘
     │
     │  systemd manages this entire process
     │  starts it, restarts it, runs it on boot
     ▼
Server stays alive, apps keep running
```

---

## Step 1 — Agent Startup Sequence

### The Full Startup Order

```
systemd starts the agent process
          │
          ▼
1. Parse config file
     /etc/yourplatform/config.yaml
     Validate all required fields present
     If invalid: log error, exit 1
     (systemd will restart, but burst limit prevents infinite loop)
          │
          ▼
2. Initialize logging
     Structured JSON logs to stdout
     systemd captures via journald
     Log level from config (info by default)
          │
          ▼
3. Run Layer 2 pre-flight checks
     Full environment audit
     Auto-fix what can be fixed
     If blocking failures remain: log them, exit 1
     Report results (stored for when connection is established)
          │
          ▼
4. Start Layer 3B (Caddy)
     Start Caddy process
     Wait for admin API to be ready
     Restore all routes from state.json
     If Caddy fails to start: log error, exit 1
          │
          ▼
5. Start Layer 3A (Docker client)
     Connect to Docker socket
     Run state reconciliation
     Discover running containers
     Restart any that should be running but are not
          │
          ▼
6. Start Layer 4A connection manager
     Begin WebSocket connection attempt to control plane
     (Connection happens asynchronously — agent does not
      block here waiting for connection)
          │
          ▼
7. Start Layer 4C health reporter
     Begin collecting metrics
     Start log streaming infrastructure
     (Does not send yet — waits for connection)
          │
          ▼
8. Start Layer 3C backup scheduler
     Calculate next backup time
     Start scheduler goroutine
          │
          ▼
9. Start Layer 4B command executor
     Ready to receive and execute commands
     (No commands until connection is established)
          │
          ▼
10. Enter main loop
      All goroutines running
      Agent is operational
      Connection manager working in background
```

### Why This Specific Order

```
Caddy starts before connection (step 4 before step 6):
  → Apps must be accessible even before control plane connection
  → A network blip should not take down user apps
  → Caddy is independent of the control plane

Docker reconciliation before connection (step 5 before step 6):
  → Know the true state of the server before reporting to control plane
  → Do not report stale state while reconciliation is in progress

Connection is async (step 6 does not block):
  → Everything else starts regardless of connection status
  → Connection establishes in background
  → Once connected: sends queued state, starts receiving commands
```

### Done Condition for Step 1
```
□ Agent starts cleanly on a fresh server after install
□ Agent starts cleanly after a server reboot
□ Each startup step logs its completion
□ Invalid config causes immediate exit with clear message
□ Pre-flight failure causes exit with specific failure reason
□ Caddy failure causes exit (apps cannot run without proxy)
□ Docker failure causes exit (cannot manage containers)
□ All goroutines are running within 15 seconds of process start
```

---

## Step 2 — WebSocket Connection

### Connection Fundamentals

```
The WebSocket connection is the agent's lifeline to the control plane.

Properties:
  → Initiated by the agent (outbound from server)
  → Never initiated by the control plane (inbound to server)
  → This matters: most servers block inbound connections
    but allow all outbound connections
  → Protocol: WSS (WebSocket Secure = WebSocket over TLS)
  → URL: wss://ws.yourplatform.com/agent
  → Port: 443 (same as HTTPS — almost never blocked)
  → Persistent: one connection, kept alive indefinitely
  → Bidirectional: agent sends, control plane sends
```

### Step 2A — Initial Connection

```
Connection sequence:

1. Read agent credentials from config:
     agent_id: agt-abc123
     agent_secret: sec-xyz789

2. Build connection URL with auth parameters:
     wss://ws.yourplatform.com/agent
     Query params:
       ?agent_id=agt-abc123
       &version=1.0.0
       (secret goes in a header, not the URL — URLs are logged)

3. Set connection headers:
     Authorization: Bearer {agent_secret}
     X-Agent-Version: 1.0.0
     X-Agent-ID: agt-abc123

4. Dial the WebSocket connection:
     → TLS handshake
     → WebSocket upgrade handshake
     → Server validates agent_id and agent_secret
     → Server validates agent is not banned/revoked
     → Server confirms: 101 Switching Protocols

5. Connection established

6. Send initial hello message:
     {
       "type": "hello",
       "agent_id": "agt-abc123",
       "agent_version": "1.0.0",
       "server_id": "srv-xyz789",
       "state": {
         "running_containers": [...],
         "caddy_routes": [...],
         "last_backup": "2024-01-14T02:04:23Z",
         "preflight_results": {...}
       }
     }
     
     This gives the control plane immediate full picture of this server
     without needing to query anything separately

7. Control plane responds with hello_ack:
     {
       "type": "hello_ack",
       "server_time": "2024-01-15T10:00:00Z",
       "pending_commands": [...]
     }
     
     pending_commands: commands that were queued while agent was offline
     Agent processes these immediately in order

8. Write agent.connected file
     /var/lib/yourplatform/agent.connected
     (Install script polls for this — confirms registration succeeded)

9. Agent is fully connected and operational
```

### Step 2B — Authentication Failure

```
What happens when credentials are rejected:

Control plane returns 401 or 403 during WebSocket upgrade:

Possible reasons:
  → agent_secret has been revoked (user deleted server from dashboard)
  → agent_secret is wrong (config file corrupted)
  → agent_id does not exist (server record deleted)

Response:
  → Do NOT retry immediately (credentials will not change)
  → Do NOT enter reconnection loop (will never succeed)
  → Log the specific rejection reason
  → Alert via any available channel (email to platform ops)
  → Write to a status file: /var/lib/yourplatform/agent.auth_failed
  → Enter a slow poll: check every 60 minutes
    → Maybe the user re-adds the server and generates new credentials
    → Check for updated credentials in config
    → If credentials changed: attempt connection with new credentials

Apps continue running:
  → Caddy keeps running (started in step 4 of startup)
  → Docker containers keep running
  → Backups cannot run (need connection for reporting)
  → User cannot deploy anything new (no connection = no commands)
  → But: existing apps stay up
```

### Step 2C — Message Protocol

```
All messages are JSON.
Every message has a type field.

Message types the agent RECEIVES from control plane:

  command          → execute something (deploy, restart, backup, etc.)
  ping             → heartbeat check (agent responds with pong)
  config_update    → update agent configuration
  update_available → new agent version ready to install

Message types the agent SENDS to control plane:

  hello            → initial connection with full state
  pong             → response to ping
  command_ack      → "I received command X, starting execution"
  command_result   → "Command X completed, here is the result"
  health_report    → periodic health metrics (Layer 4C)
  log_line         → individual log line for streaming (Layer 4C)
  alert            → something needs user attention
  backup_result    → backup completed with metadata (Layer 3C)
  state_update     → something changed (container status, etc.)

Message envelope structure:
  {
    "id": "msg-abc123",      ← unique per message, for correlation
    "type": "command_ack",   ← message type
    "timestamp": "...",      ← when this was sent
    "payload": { ... }       ← type-specific content
  }
```

### Done Condition for Step 2
```
□ Agent connects to control plane WebSocket on startup
□ Hello message sent with full server state
□ Control plane acknowledges with pending commands
□ Pending commands are processed in order
□ Auth failure stops reconnection loop
□ Auth failure does not kill running apps
□ All messages include id, type, timestamp, payload
□ agent.connected file written after successful hello_ack
```

---

## Step 3 — Reconnection Logic

### Why Reconnection Is Complex

```
The WebSocket connection will drop. This is certain.

Causes:
  → Network blip (30 seconds of packet loss)
  → Control plane deployment (your backend restarts)
  → Server-side idle connection timeout
  → ISP maintenance window
  → Server's network interface restarted
  → The user's hosting provider has an incident

When it drops:
  → Running apps: unaffected (Caddy and Docker keep running)
  → New deployments: impossible until reconnected
  → Log streaming: paused until reconnected
  → Health reports: queued until reconnected

The agent must reconnect automatically, quickly,
without bothering the user unless it takes a very long time.
```

### Step 3A — Detecting Disconnection

```
Three ways disconnection is detected:

Method 1: WebSocket close frame
  → The remote end sends a proper close frame
  → Clean close: usually means control plane is restarting
  → Agent detects immediately
  → Should reconnect quickly (control plane restart is fast)

Method 2: Read/write error
  → Attempt to send a message and get an error
  → Or: receive loop returns an error
  → Unclean close: network dropped
  → Agent detects within milliseconds of the error

Method 3: Heartbeat timeout
  → Control plane sends a ping every 30 seconds
  → Agent must receive a ping within 90 seconds
  → If 90 seconds pass without a ping: connection is silently dead
  → This catches the "half-open connection" problem:
    TCP connection appears open but no data flows
    (common after NAT timeouts on some hosting providers)

The heartbeat is critical:
  → Without it, the agent can think it is connected for hours
    while the control plane thinks it is disconnected
  → 30 second ping interval, 90 second timeout = 3 missed pings before timeout
```

### Step 3B — Reconnection Algorithm

```
When disconnection is detected:

State machine:

CONNECTED
    │
    │  (disconnection detected)
    ▼
RECONNECTING
    │
    │  attempt reconnection with backoff
    │
    ├── success → CONNECTED
    │
    └── still failing after 5 minutes → DEGRADED
                                              │
                                              │ keep trying (slower)
                                              │
                                              ├── success → CONNECTED
                                              │
                                              └── still failing after
                                                  1 hour → ALERT USER

Backoff schedule:

Attempt 1:  wait 1 second,  then try
Attempt 2:  wait 2 seconds, then try
Attempt 3:  wait 4 seconds, then try
Attempt 4:  wait 8 seconds, then try
Attempt 5:  wait 16 seconds, then try
Attempt 6:  wait 30 seconds, then try
Attempt 7+: wait 60 seconds, then try (cap at 60 seconds)

After 5 minutes of failed attempts:
  → Log: "Control plane connection lost for 5 minutes"
  → State: DEGRADED
  → Continue retrying every 60 seconds

After 1 hour of failed attempts:
  → This is unusual — something is seriously wrong
  → Try to send an alert via fallback channel (email direct from server)
  → Log: "Control plane unreachable for 1 hour"
  → Continue retrying every 5 minutes

After 24 hours:
  → Write alert to a local file
    /var/lib/yourplatform/alerts/connection-lost.txt
  → This file could be read by a monitoring cron if user sets one up
  → Continue retrying

Why exponential backoff:
  → Immediate retry is pointless if network is down
  → All agents retrying simultaneously hammers control plane on recovery
  → Backoff spreads out reconnection attempts
  → Cap at 60 seconds: still reconnects within 1 minute of control plane recovery
```

### Step 3C — Jitter

```
Problem without jitter:
  Control plane restarts.
  1000 agents all start reconnecting.
  They all use the same backoff.
  They all try again at exactly t+1, t+2, t+4, etc.
  They create synchronized thundering herd.

Solution: add jitter to the backoff.

Each wait time = base_backoff + random(0, base_backoff * 0.5)

Examples:
  Attempt 1:  1s + random(0, 0.5s)  = 1.0s to 1.5s
  Attempt 2:  2s + random(0, 1.0s)  = 2.0s to 3.0s
  Attempt 3:  4s + random(0, 2.0s)  = 4.0s to 6.0s
  Attempt 6+: 60s + random(0, 30s)  = 60s to 90s

This spreads reconnection attempts over a time window.
Control plane receives a gradual ramp, not a thundering herd.
```

### Step 3D — State During Disconnection

```
While disconnected, the agent:

Continues:
  ✓ Running Docker containers (they run independently)
  ✓ Running Caddy (routes stay active)
  ✓ Health monitoring (Layer 4C still collects data)
  ✓ Log collection (buffers locally)
  ✓ Backup scheduler (runs on schedule, stores results locally)
  ✓ Auto-restart crashed containers (Layer 3A reconciliation)

Pauses:
  ✗ Receiving new deployment commands (no connection = no commands)
  ✗ Sending health reports (queued)
  ✗ Sending log lines (buffered, up to a limit)
  ✗ Reporting backup results (stored, sent on reconnect)

On reconnection:
  → Send a new hello message with current full state
  → Control plane learns current state immediately
  → Flush queued health reports (last N minutes of data)
  → Flush buffered alerts
  → Process any commands that were queued while disconnected
  → Resume log streaming
```

### Step 3E — Offline Command Queue

```
Commands that arrive while disconnected:
  Control plane queues them in its database
  (Agent cannot receive them — no connection)

When agent reconnects:
  hello_ack includes: pending_commands: [list of queued commands]
  
  Agent processes them in order:
    → Most commands: execute as normal
    → Some commands may be stale: check before executing
    
Staleness checks:
  Deploy command queued 10 minutes ago:
    → Execute: the user wanted this, it has not happened yet
    
  Deploy command queued 25 hours ago:
    → Probably still execute: user would rather late than never
    → But: check if a newer deploy command exists for same project
      → If yes: skip old one, execute new one
    
  Restart command queued 2 hours ago:
    → Check: is the container already running?
      → If yes and it was restarted after the command timestamp:
        the restart already happened (agent did it via reconciliation)
        skip the command
      → If no: execute the restart

Command queue is bounded:
  Control plane keeps at most 50 pending commands per server
  Oldest commands are dropped if queue is full
  (A server disconnected for a very long time gets a clean slate)
```

### Done Condition for Step 3
```
□ Disconnection detected via close frame, error, or heartbeat timeout
□ Reconnection starts immediately after disconnection detected
□ Backoff schedule is correct (1s, 2s, 4s, 8s... capped at 60s)
□ Jitter is applied to each backoff period
□ Running apps unaffected during disconnection
□ Health data is queued and sent on reconnect
□ Backup results are stored and sent on reconnect
□ hello message on reconnect includes current full state
□ Pending commands from queue are processed in order
□ Stale commands are detected and handled correctly
□ Alert after 1 hour of failed reconnection
□ Exponential backoff tested: does not hammer control plane
```

---

## Step 4 — Heartbeat System

### Why Heartbeats Exist

```
TCP connections can appear open while being functionally dead.

Scenario:
  Agent connects to control plane.
  Router between them reboots.
  TCP connection appears open on both ends.
  No data can actually flow.
  Neither side knows this.
  
  Without heartbeats:
    Agent thinks it is connected — it is not
    Control plane thinks agent is connected — it is not
    User sees server as "Connected" — it is not
    Commands sent to agent: lost silently

  With heartbeats:
    Ping sent every 30 seconds
    If no pong received within 90 seconds: dead connection detected
    Reconnection starts
    Connection restored within minutes
```

### Step 4A — Ping/Pong Protocol

```
Control plane side:
  → Every 30 seconds: send ping to each connected agent
  → {type: "ping", id: "ping-abc123", timestamp: "..."}
  → Record: when ping was sent, to which agent

Agent side:
  → Receive ping
  → Immediately respond: {type: "pong", ping_id: "ping-abc123", timestamp: "..."}
  → pong must be sent within 10 seconds of ping receipt
  → Record: last ping received at

Timeout detection:
  Agent:
    → If no ping received for 90 seconds: connection is dead
    → Close the connection and begin reconnection

  Control plane:
    → If no pong received within 15 seconds of sending ping: mark agent as suspect
    → If no pong received within 45 seconds: mark agent as disconnected
    → Update server status in database: disconnected
    → Dashboard updates: server shows as disconnected
```

### Step 4B — Application-Level vs TCP-Level Heartbeat

```
Two levels of keepalive:

TCP keepalive (lower level):
  → TCP protocol feature
  → The operating system sends small packets to keep the connection open
  → Prevents NAT timeout (many NATs drop connections idle for > 5 minutes)
  → Set TCP keepalive on the WebSocket connection:
      Idle time before first keepalive: 60 seconds
      Interval between keepalives: 30 seconds
      Number of failed keepalives before giving up: 3

Application ping/pong (higher level, Step 4A):
  → Our own heartbeat at the application layer
  → Catches cases TCP keepalive misses
  → Provides round-trip timing data (useful for diagnostics)

Use both:
  → TCP keepalive prevents NAT from killing the connection
  → Application ping/pong detects dead connections TCP keepalive misses
```

### Done Condition for Step 4
```
□ Agent responds to pings within 10 seconds
□ Agent detects missing ping within 90 seconds
□ Agent initiates reconnection on ping timeout
□ Control plane marks agent as disconnected after missed pongs
□ Dashboard updates to "Disconnected" status correctly
□ TCP keepalive is configured on the WebSocket connection
□ Round-trip time of pings is logged (useful for diagnostics)
```

---

## Step 5 — Self-Update Mechanism

### Why This Is Critical

```
You will release new agent versions.
Users will have agents running on their servers.
You cannot SSH into users' servers to update agents.
Users will not manually update agents (they are non-technical).

The agent must update itself.

But: a bad update that bricks the agent is a disaster.
     The user loses all management capability.
     You cannot push a fix because the agent is broken.
     You have to tell a non-technical user to SSH in.

Therefore: self-update must be extremely safe.
           It must roll back on any failure.
           It must never leave the agent in a broken state.
```

### Step 5A — Update Check

```
How the agent learns about updates:

Method 1: Polling (primary for MVP)
  → Every 6 hours, agent checks for updates
  → GET https://releases.yourplatform.com/agent/latest.json
  → Response: { "version": "1.1.0", "released_at": "...", "channel": "stable" }
  → Compare to current version
  → If newer: begin update process

Method 2: Push notification (enhancement)
  → Control plane sends update_available message via WebSocket
  → { type: "update_available", version: "1.1.0" }
  → Agent begins update process immediately
  → Faster than waiting for the poll cycle

For MVP: implement both
  → Polling as the safety net (works even if WebSocket message is missed)
  → Push as the primary for speed

Update channels:
  → stable: production-ready, all agents should run this
  → beta: opt-in testing channel (future — not for MVP)
  → For MVP: only stable channel exists
```

### Step 5B — Update Download and Verification

```
When a new version is available:

1. Determine the correct binary
     → Same OS and architecture as current install
     → URL: https://releases.yourplatform.com/agent/v1.1.0/
              yourplatform-agent-linux-amd64

2. Check disk space
     → Need at least 200MB free for the download
     → If insufficient: abort update, log warning
     → Alert: "Agent update available but insufficient disk space to install"

3. Download to temp location
     → /tmp/yourplatform-agent-update-{version}
     → Not to the install directory yet — never replace the running binary in place

4. Verify checksum
     → Download: yourplatform-agent-linux-amd64.sha256
     → Compute SHA-256 of downloaded binary
     → Compare — must match exactly
     → If mismatch: delete download, abort, log security warning

5. Verify GPG signature (production security)
     → Download: yourplatform-agent-linux-amd64.sig
     → Verify signature against platform's public key
     → Public key bundled in the current agent binary at build time
     → If signature invalid: delete download, abort
     → Alert: "Update aborted: signature verification failed.
        This may indicate a security issue. Contact support."

6. Make executable
     → chmod +x /tmp/yourplatform-agent-update-{version}

7. Proceed to smoke test (Step 5C)
```

### Step 5C — Smoke Test

```
Before replacing the running binary, verify the new binary works.

Run the new binary with a test flag:
  /tmp/yourplatform-agent-update-1.1.0 --smoke-test

The --smoke-test mode:
  → Does NOT start any services
  → Does NOT connect to control plane
  → Does NOT modify any files
  → Does:
    → Load and validate the config file
    → Verify it can talk to Docker (one API call)
    → Verify it can talk to Caddy admin API (one API call)
    → Print: {"smoke_test": "passed", "version": "1.1.0"}
    → Exit 0

If smoke test exits non-zero or times out (15 second timeout):
  → Delete the downloaded binary
  → Abort update
  → Log: "Update to v1.1.0 aborted: smoke test failed"
  → Send alert to control plane: "Auto-update failed — smoke test failed"
  → Continue running current version
  → This is the correct behavior: do nothing, stay safe

If smoke test passes:
  → Proceed to atomic swap (Step 5D)
```

### Step 5D — Atomic Binary Swap

```
This is the most critical part of the update.
It must be impossible for the agent to end up with no working binary.

The atomic swap technique:

Current state:
  /usr/local/bin/yourplatform-agent  ← currently running (v1.0.0)
  /tmp/yourplatform-agent-update-1.1.0  ← new binary (smoke tested)

Step 1: Back up current binary
  cp /usr/local/bin/yourplatform-agent
     /usr/local/bin/yourplatform-agent.backup

Step 2: Move new binary to a staging location near the target
  mv /tmp/yourplatform-agent-update-1.1.0
     /usr/local/bin/yourplatform-agent.new
     
  Why move, not copy:
    mv within the same filesystem is atomic at OS level
    It is a metadata operation — either succeeds or fails, never partial

Step 3: Atomic rename
  mv /usr/local/bin/yourplatform-agent.new
     /usr/local/bin/yourplatform-agent

  This is the "atomic swap"
  The kernel does this in one operation
  No moment where the binary is half-written or missing

Step 4: Verify the new binary is in place
  /usr/local/bin/yourplatform-agent --version
  → Must return the new version string
  → If this fails: the swap failed somehow → restore backup immediately

At any point if something goes wrong:
  mv /usr/local/bin/yourplatform-agent.backup
     /usr/local/bin/yourplatform-agent
  → Restore the original binary immediately
```

### Step 5E — Restart After Update

```
After the binary is swapped, the agent needs to restart to run the new code.

The current process is still running the old code.
The file on disk is now the new binary.
systemd will run the new binary on next start.

Restart sequence:

1. Finish any in-progress operations
     → Wait for active deployment to complete (max 5 minutes)
     → Do not interrupt a running backup (max 10 minutes wait)
     → Do not restart during an active restore

2. Notify control plane
     → { type: "state_update", update: "restarting_for_update",
         new_version: "1.1.0" }
     → Control plane marks server as "updating"
     → Dashboard shows: "Server is updating agent..."

3. Clean shutdown of managed processes
     → Signal Caddy: do NOT stop it
       Caddy should keep running through the agent restart
       (apps stay accessible during agent restart)
     → Flush any pending state writes
     → Close WebSocket connection cleanly

4. Exit the process
     → os.Exit(0)
     → systemd detects the exit
     → systemd starts the new binary (because restart policy is "always")

5. New agent starts
     → Runs the new version
     → Goes through full startup sequence (Step 1)
     → Reconnects to control plane
     → Sends hello with: agent_version: "1.1.0"
     → Control plane confirms update success
     → Dashboard updates: "Agent updated to v1.1.0"

6. Cleanup
     → New agent deletes the backup binary:
       rm /usr/local/bin/yourplatform-agent.backup
     → Backup is no longer needed once new version is confirmed running
```

### Step 5F — Update Failure Recovery

```
Scenario: the new binary was swapped in, agent restarted,
but the new version fails to start or connect.

How this is detected:
  → systemd restart count increases rapidly
  → New version crashes, systemd restarts it
  → It crashes again
  → systemd burst limit: 5 crashes in 200 seconds → stop restarting

At this point:
  → Server is without agent management
  → Apps are still running (Caddy and Docker independent)
  → But: user cannot deploy, logs are not streaming

Recovery mechanism:
  → The backup binary is still at: /usr/local/bin/yourplatform-agent.backup
  → We need something to restore it

Option A: Watchdog script (MVP approach)
  → A simple shell script installed alongside the agent
  → Location: /usr/local/bin/yourplatform-watchdog
  → Run by a separate systemd service: yourplatform-watchdog.service
  → The watchdog service only does one thing:
    → Check if the agent has been stopped for > 60 seconds
    → If backup binary exists: restore it and restart agent
    → /usr/local/bin/yourplatform-agent.backup → yourplatform-agent
    → systemctl start yourplatform-agent
  → Simple, reliable, independent of the agent itself

Option B: ExecStartPre in systemd unit (simpler)
  → Before starting the agent, systemd runs a check script
  → Script: if agent fails --smoke-test:
              and backup exists:
              restore backup
  → Then systemd starts whatever binary is at the path

Recommendation: Option B for MVP
  → Fewer moving parts
  → No separate watchdog service to maintain
  → Runs as part of the normal systemd start sequence
```

### Done Condition for Step 5
```
□ Update check runs every 6 hours
□ Push notification from control plane triggers immediate check
□ Download verifies checksum before proceeding
□ Smoke test runs in isolation (no side effects)
□ Smoke test timeout of 15 seconds enforced
□ Failed smoke test aborts update and keeps current version
□ Atomic binary swap completes without any window of missing binary
□ Agent restarts after swap without disrupting Caddy or Docker
□ Dashboard shows "updating" during restart
□ Dashboard shows new version after successful update
□ Update failure recovery restores backup binary
□ Backup binary cleaned up after confirmed successful update
```

---

## Step 6 — Systemd Integration

### What Systemd Does for the Agent

```
systemd is the process supervisor for the agent.

It provides:
  → Start on boot: agent runs after every reboot without user action
  → Restart on crash: if agent exits unexpectedly, systemd restarts it
  → Dependency management: agent starts after Docker and network are ready
  → Resource limits: agent cannot starve user apps
  → Log capture: agent stdout/stderr goes to journald
  → Clean shutdown: systemd sends SIGTERM before SIGKILL
```

### Step 6A — The Systemd Unit File (Complete Design)

```
The unit file was written by the install script.
Layer 4A defines exactly what it must contain and why.

[Unit] section:
  Description=YourPlatform Agent
  Documentation=https://docs.yourplatform.com/agent

  After=network-online.target docker.service
    → network-online: true network connectivity, not just interface up
    → docker.service: Docker must be running
    → Without this: agent starts, cannot reach control plane,
      cannot talk to Docker, fails, retries infinitely

  Wants=network-online.target
    → "I want this but it is not hard required"
    → network-online is best-effort on some systems

  Requires=docker.service
    → "I require Docker — if Docker dies, restart me too"
    → When Docker restarts: agent restarts, runs reconciliation,
      rediscovers containers

[Service] section:
  Type=simple
    → Process starts and stays running
    → Not forking, not oneshot
    → systemd watches the main process

  User=root
    → Required: needs root to manage Docker socket,
      write to /usr/local/bin, manage systemd services
    → Future: could use a dedicated user with specific capabilities
      (docker group, specific file permissions)
      but root is simpler for MVP

  ExecStartPre=/usr/local/bin/yourplatform-check-update
    → Run before starting the agent
    → This script checks if the backup binary needs to be restored
    → Implements the Option B recovery from Step 5F

  ExecStart=/usr/local/bin/yourplatform-agent run
             --config /etc/yourplatform/config.yaml
    → The actual agent command

  Restart=always
    → Restart on any exit: crash, OOM kill, explicit exit
    → Exception: we exit 0 for updates (systemd still restarts — correct)

  RestartSec=10
    → Wait 10 seconds before restarting
    → Prevents tight crash loops
    → If agent crashes immediately: 10 second pause before retry

  StartLimitInterval=200
  StartLimitBurst=5
    → If agent crashes 5 times in 200 seconds: give up restarting
    → systemd sends failure notification
    → This prevents burn loops (crashing binary that hammers resources)
    → A human (or watchdog) must intervene

  TimeoutStartSec=90
    → If agent does not signal ready within 90 seconds: systemd kills it
    → With Type=simple: this is how long systemd waits for the process
      to at least start without exiting

  TimeoutStopSec=60
    → When systemd stops the agent: give it 60 seconds for clean shutdown
    → After 60 seconds: SIGKILL

  KillMode=mixed
    → Send SIGTERM to the main process
    → After timeout: SIGKILL the entire process group
    → This ensures child processes (any subprocesses) are cleaned up

  StandardOutput=journal
  StandardError=journal
  SyslogIdentifier=yourplatform-agent
    → All output goes to journald
    → Identified as "yourplatform-agent" in logs
    → User can view with: journalctl -u yourplatform-agent -f

  MemoryMax=256M
    → Hard memory limit
    → Agent is killed (and restarted) if it exceeds this
    → Prevents agent memory leak from impacting apps

  CPUQuota=20%
    → Agent uses at most 20% of one CPU core
    → Leaves 80% for user apps on single-core servers

  PrivateTmp=true
    → Agent gets its own /tmp namespace
    → Prevents temp file conflicts with other processes
    → Agent's /tmp is actually /tmp/systemd-private-.../tmp/

  NoNewPrivileges=false
    → Agent can gain new privileges (needed for some Docker operations)
    → Set to false = allow privilege operations

[Install] section:
  WantedBy=multi-user.target
    → Start in normal multi-user mode (standard server operation)
    → Not in rescue mode, not in single-user mode
```

### Step 6B — Systemd Notify (Optional Enhancement)

```
systemd has a "ready notification" mechanism.
With Type=notify, the agent can tell systemd "I am ready"
instead of systemd just assuming the process is ready when it starts.

Change Type=simple to Type=notify
Add to the agent startup sequence (after step 9 in startup):
  → Write to the systemd notify socket: READY=1
  → systemd then marks the service as started
  → systemctl start waits for this notification

Benefits:
  → systemctl start blocks until agent is actually ready
  → systemctl status shows accurate started vs starting state
  → Dependent services (if any) wait for agent to be ready

This requires using the sd_notify mechanism from Go.
Available via: github.com/coreos/go-systemd/daemon

For MVP: Type=simple is sufficient
Enhancement: add sd_notify for accurate status reporting
```

### Step 6C — Graceful Shutdown

```
When systemd stops the agent (sudo systemctl stop yourplatform-agent):
  → systemd sends SIGTERM to the agent process

Agent must handle SIGTERM:

1. Catch the signal (in Go: signal.Notify for syscall.SIGTERM)

2. Enter shutdown mode:
     → Stop accepting new commands from WebSocket
     → Stop the update checker (do not start an update during shutdown)

3. Wait for in-progress operations:
     → Active deployment: wait up to 5 minutes for it to complete
     → Active backup: wait up to 10 minutes for it to complete
     → Active restore: wait up to 15 minutes (do not interrupt a restore)
     → If none of the above: proceed immediately

4. Notify control plane (if connected):
     → { type: "state_update", update: "shutting_down" }
     → Control plane marks server as "agent_stopped"
     → Dashboard: server shows as offline

5. Close WebSocket connection cleanly
     → Send WebSocket close frame
     → Wait for close frame from server (max 5 seconds)

6. Stop health reporter (Layer 4C)
     → Flush any pending health data

7. Write final state to state.json
     → Current container statuses
     → Current backup status
     → Shutdown timestamp

8. Do NOT stop Caddy
     → Caddy keeps running
     → Apps remain accessible during agent downtime
     → This is essential: agent restart ≠ app downtime

9. Do NOT stop Docker containers
     → Same reason

10. Exit with code 0
      → systemd sees clean exit
      → If Restart=always: systemd will restart (correct for updates)
      → If manually stopped: stays stopped (also correct)

Total shutdown time: 
  → Without in-progress operations: under 10 seconds
  → With active deployment: up to 5 minutes (+ 10s cleanup)
  → With active backup: up to 10 minutes (+ 10s cleanup)
```

### Step 6D — Detecting Unclean Exits

```
When the agent exits uncleanly (crash, OOM kill, SIGKILL):
  → state.json may be in a partially written state
  → The agent.connected file may still exist (stale)
  → In-progress operations were interrupted

On next startup (after systemd restarts):
  → Read state.json carefully
    → Check for a "shutdown_clean" flag
    → If missing: last shutdown was unclean
  → Remove stale agent.connected file
  → Check each container that was "deploying" or "restoring"
    → These operations were interrupted
    → Determine current actual state from Docker
    → Report to control plane: "Deploy of myshop was interrupted by agent crash"
  → Run full reconciliation (Layer 3A Step 8)
  → Proceed normally

The key insight:
  → An unclean exit is unusual but must be handled gracefully
  → The agent recovers by reconciling with Docker reality
  → Docker containers are the ground truth — always
```

### Done Condition for Step 6
```
□ Agent starts automatically after server reboot
□ Agent restarts after crash (test: kill -9 the process)
□ Agent waits for Docker before starting (test: start agent with Docker stopped)
□ Agent restarts when Docker restarts (test: systemctl restart docker)
□ Memory limit of 256MB is enforced
□ CPU quota of 20% is enforced
□ SIGTERM causes graceful shutdown (not immediate kill)
□ Graceful shutdown waits for in-progress deployments
□ Caddy keeps running when agent shuts down
□ Docker containers keep running when agent shuts down
□ Unclean exit is detected on next startup
□ state.json is readable after unclean exit (partial write handled)
□ journalctl -u yourplatform-agent shows readable logs
```

---

## Step 7 — State Persistence

### What State the Agent Persists

```
The agent's memory lives in state.json.
This is how the agent knows what it was doing before it restarted.

Location: /var/lib/yourplatform/state.json

Contents:
{
  "version": 1,                          ← schema version for migrations
  "agent_version": "1.0.0",
  "last_updated": "2024-01-15T10:00:00Z",
  "shutdown_clean": false,               ← set to true on clean shutdown only

  "connection": {
    "last_connected": "2024-01-15T09:55:00Z",
    "last_disconnected": null,
    "reconnect_attempts": 0
  },

  "projects": {
    "myshop": {
      "status": "running",
      "containers": { ... },             ← from Layer 3A
      "routes": { ... },                 ← from Layer 3B
      "last_deployment": "2024-01-15T09:00:00Z",
      "last_deployment_id": "dep-abc123"
    }
  },

  "backup": {
    "last_run": "2024-01-15T02:04:23Z",
    "last_snapshot_id": "a1b2c3d4",
    "schedule_hour_utc": 2,
    "status": "success"
  },

  "update": {
    "current_version": "1.0.0",
    "last_check": "2024-01-15T08:00:00Z",
    "available_version": null
  }
}
```

### Writing State Safely

```
The state file must never be in a corrupted state.
A half-written JSON file is worse than no file.

Safe write pattern:
  1. Marshal the state to JSON
  2. Write to a temp file: state.json.tmp
  3. Verify the temp file is valid JSON
     (attempt to parse it back — catch marshal bugs)
  4. Atomically rename: state.json.tmp → state.json
     (rename is atomic on Linux)
  5. If any step fails: old state.json is untouched

When to write state:
  → After every successful deployment
  → After every backup
  → After every container status change
  → After WebSocket reconnection
  → On clean shutdown (with shutdown_clean: true)
  → NOT on every health report (too frequent, wears on disk)
```

### Done Condition for Step 7
```
□ state.json is written after every significant operation
□ Atomic write pattern prevents corrupted state file
□ state.json is read correctly on agent startup
□ Unclean shutdown detected via shutdown_clean flag
□ Schema version field enables future state migrations
□ state.json is backed up as part of Layer 3C backup
□ Missing state.json on startup is handled (fresh state created)
□ Corrupted state.json on startup is handled (fresh state created, warning logged)
```

---

## Layer 4A Overall Done Condition

```
The full test sequence:

Test 1 — Normal startup after reboot:
  □ Server reboots
  □ Docker starts
  □ Agent starts (after Docker)
  □ Caddy starts
  □ Routes restored from state.json
  □ Agent connects to control plane within 30 seconds
  □ Dashboard shows server as connected
  □ All previously running apps still running

Test 2 — Network interruption:
  □ Block outbound traffic from server for 2 minutes
  □ Agent detects disconnection within 90 seconds
  □ Reconnection attempts with backoff
  □ Restore network
  □ Agent reconnects within 5 attempts
  □ Dashboard shows connected
  □ Health data from offline period is sent
  □ Apps were accessible throughout (Caddy kept running)

Test 3 — Control plane restart:
  □ Restart the control plane service
  □ All agents disconnect (clean close frame)
  □ Agents begin reconnection with backoff
  □ Control plane comes back up (typically 5-10 seconds)
  □ All agents reconnect
  □ Dashboard shows all servers as connected
  □ No commands were lost

Test 4 — Agent crash:
  □ Kill agent with kill -9
  □ systemd detects exit
  □ systemd waits 10 seconds
  □ systemd restarts agent
  □ Agent runs startup sequence
  □ Detects unclean shutdown from state.json
  □ Runs reconciliation
  □ Reconnects to control plane
  □ Dashboard shows connected within 30 seconds of crash

Test 5 — Agent update:
  □ Release a new version (bump version in manifest)
  □ Agent polls and finds new version
  □ Downloads and verifies
  □ Smoke test passes
  □ Atomic swap completes
  □ Agent restarts
  □ New version connects and reports new version
  □ Dashboard shows updated version
  □ Apps never went down

Test 6 — Bad update (smoke test failure):
  □ Release a broken version (intentionally)
  □ Agent downloads it
  □ Smoke test fails
  □ Current version continues running
  □ Alert appears in dashboard: update failed
  □ Server remains connected and functional

Test 7 — Graceful shutdown:
  □ systemctl stop yourplatform-agent
  □ Agent receives SIGTERM
  □ In-progress deployment waits to complete
  □ Agent notifies control plane of shutdown
  □ Agent exits cleanly
  □ Caddy still running (apps still accessible)
  □ Docker containers still running

When all 7 tests pass, Layer 4A is done.
Move to Layer 4B — Command Executor.
```

---

## What Layer 4A Does NOT Do

```
Layer 4A does not:
├── Execute deployment commands (Layer 4B)
├── Collect health metrics (Layer 4C)
├── Talk to Docker directly (Layer 3A)
├── Configure Caddy routes (Layer 3B)
├── Run backup jobs (Layer 3C)
├── Handle user authentication (Layer 5A)
└── Serve the dashboard (Layer 7)

Layer 4A is purely the agent's lifecycle and connectivity.
It keeps the agent alive and connected.
Everything else does the actual work.
```

---

**Ready for Layer 4B — Command Executor?**