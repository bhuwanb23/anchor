# Layer 4C — Health and Log Reporter: Complete Plan

---

## What Layer 4C Actually Is

```
Layer 4C is the agent's eyes and voice.

Every other layer does things:
  3A → manages Docker
  3B → manages Caddy
  3C → manages backups
  4A → manages connection
  4B → executes commands

Layer 4C watches everything and reports back.

It answers three questions continuously:
  1. How is this server doing right now?
     (CPU, RAM, disk, container statuses)

  2. What is my app saying?
     (log streaming)

  3. Is anything wrong?
     (anomaly detection, plain-English alerts)

Layer 4C never takes action.
It observes, reports, and alerts.
Layer 4B takes action based on alerts.
The user takes action based on what they see in the dashboard.
```

---

## The Mental Model

```
Server Reality                    Dashboard
─────────────                     ─────────
CPU: 45%          ──────────────► CPU gauge: 45%
RAM: 1.2GB/2GB    ──────────────► RAM bar: 60%
Disk: 18GB/40GB   ──────────────► Disk bar: 45%
                                  
myshop: running   ──────────────► myshop: ● Running
myblog: crashed   ──────────────► myblog: ✗ Crashed
                                  Alert: "myblog crashed"
                  
Log line appears  ──────────────► Log line appears
in container      (under 2 sec)   in dashboard

Disk hits 90%     ──────────────► Alert: "Your disk is almost full"
```

---

## Layer 4C Internal Structure

```
┌──────────────────────────────────────────────────────┐
│                    Layer 4C                          │
│                                                      │
│  ┌─────────────────┐    ┌──────────────────────────┐ │
│  │  Metrics        │    │  Log Streamer            │ │
│  │  Collector      │    │                          │ │
│  │                 │    │  One goroutine per       │ │
│  │  Runs every     │    │  active log stream       │ │
│  │  30 seconds     │    │  Forwards lines to       │ │
│  │                 │    │  control plane           │ │
│  │  Collects:      │    └──────────────────────────┘ │
│  │  CPU, RAM,      │                                  │
│  │  Disk,          │    ┌──────────────────────────┐ │
│  │  Containers     │    │  Anomaly Detector        │ │
│  └────────┬────────┘    │                          │ │
│           │             │  Runs after every        │ │
│           ▼             │  metrics collection      │ │
│  ┌─────────────────┐    │                          │ │
│  │  Report         │    │  Compares current vs     │ │
│  │  Builder        │    │  thresholds              │ │
│  │                 │    │  Generates alerts        │ │
│  │  Formats data   │    └──────────────────────────┘ │
│  │  for control    │                                  │
│  │  plane          │    ┌──────────────────────────┐ │
│  └────────┬────────┘    │  Alert Manager           │ │
│           │             │                          │ │
│           └─────────────► Deduplicates alerts      │ │
│                         │  Rate limits alerts      │ │
│                         │  Sends to control plane  │ │
│                         └──────────────────────────┘ │
└──────────────────────────────────────────────────────┘
```

---

## Step 1 — Metrics Collection

### What Gets Collected and How

```
Layer 4C collects three categories of metrics:

Category 1: Server-level metrics
  → CPU usage (overall server)
  → RAM usage (overall server)
  → Disk usage (overall server)
  → Network I/O (basic — bytes in/out)
  → System load average

Category 2: Container-level metrics
  → Per-container CPU usage
  → Per-container RAM usage
  → Per-container status (running, stopped, crashed)
  → Per-container restart count
  → Per-container uptime

Category 3: Platform-level metrics
  → Caddy process status (running or not)
  → Number of active routes in Caddy
  → Last backup status and age
  → Agent connection status
```

### Step 1A — Server CPU Collection

```
Source: /proc/stat

/proc/stat format (first line):
  cpu  2255 34 2290 22625563 6290 127 456 0 0 0
       user nice system idle  iowait irq softirq ...

How to calculate CPU usage percentage:

  Read /proc/stat at time T1
  Wait 1 second
  Read /proc/stat at time T2

  For each CPU field:
    delta_field = value_at_T2 - value_at_T1

  total_delta = sum of all deltas
  idle_delta = delta of idle field

  cpu_used_percent = ((total_delta - idle_delta) / total_delta) * 100

Why read twice with a gap:
  A single reading of /proc/stat tells you nothing
  It is a cumulative counter since boot
  You need the delta over a time period to get a rate

Collection interval: 30 seconds
  → Read once, store the values
  → 30 seconds later: read again, calculate delta
  → This gives 30-second average CPU usage
  → Smooth enough to avoid noise, responsive enough to catch spikes

What to collect:
  → Overall CPU percentage (all cores combined as a single number)
  → Why not per-core: non-technical users do not understand per-core
  → Overall percentage is what they see on a hosting dashboard
```

### Step 1B — Server RAM Collection

```
Source: /proc/meminfo

Relevant fields:
  MemTotal:       8192000 kB   ← total RAM
  MemFree:        2048000 kB   ← completely free
  MemAvailable:   4096000 kB   ← actually usable (free + reclaimable cache)
  Buffers:         512000 kB
  Cached:         1024000 kB

What to use:
  → Use MemAvailable, not MemFree
  → MemFree excludes cache that Linux would free if needed
  → MemAvailable is what you actually have to work with
  → This is what tools like "free -m" show as "available"

What to calculate:
  total_ram_mb = MemTotal / 1024
  available_ram_mb = MemAvailable / 1024
  used_ram_mb = total_ram_mb - available_ram_mb
  used_percent = (used_ram_mb / total_ram_mb) * 100

What to report:
  → used_ram_mb
  → total_ram_mb
  → used_percent
  → NOT individual process RAM (too granular for this layer)
```

### Step 1C — Disk Usage Collection

```
Source: syscall statfs() or reading /proc/mounts + statfs per mount

What to check:
  → Root filesystem (/) — always checked
  → /var — checked if it is a separate partition
    (/var/lib/docker stores all images and container data)
  → Any other mounted filesystems with yourplatform data

For each filesystem:
  total_bytes = statfs.Blocks * statfs.Bsize
  free_bytes = statfs.Bfree * statfs.Bsize
  used_bytes = total_bytes - free_bytes
  used_percent = (used_bytes / total_bytes) * 100

What to report:
  → For root filesystem:
    total_disk_gb
    used_disk_gb
    available_disk_gb
    used_percent

  → If /var is separate:
    var_total_gb
    var_used_gb
    var_used_percent

Why this matters:
  → Docker images and container logs accumulate in /var/lib/docker
  → If /var is a separate partition: it fills independently
  → Many VPS providers give /var its own partition
  → Must monitor the right filesystem
```

### Step 1D — System Load Average

```
Source: /proc/loadavg

Format:
  0.45 0.32 0.28 2/345 12345
  ↑    ↑    ↑
  1min 5min 15min load average

Load average meaning:
  1.0 on a single-core server = 100% CPU busy
  1.0 on a 4-core server = 25% CPU busy
  2.0 on a single-core server = 200% load (queue building up)

What to collect:
  → load_1min
  → load_5min
  → cpu_cores (from /proc/cpuinfo — count processor: lines)

What to report:
  → load_1min
  → load_per_core = load_1min / cpu_cores
    → 0.0-0.7: low
    → 0.7-1.0: moderate
    → 1.0+: high (work queuing up)

Why load average alongside CPU percent:
  → CPU percent tells you current usage
  → Load average tells you if work is queuing up
  → High load + low CPU = I/O bound (disk or network wait)
  → Both metrics together give a more complete picture
```

### Step 1E — Container Metrics Collection

```
Source: Docker Stats API

The Docker API provides live stats per container.
Two modes:
  → Stream mode: continuous updates (too much data)
  → One-shot mode: single reading per container

We use one-shot mode, triggered by our own 30-second interval.

For each container with label "managed-by=yourplatform":

  1. Call Docker stats API (one-shot, not streaming)
  2. Wait for response (usually < 1 second per container)
  3. Parse the response

Docker stats response includes:
  → CPU usage (in nanoseconds, need to calculate percentage)
  → Memory usage and limit
  → Network I/O (bytes in/out)
  → Block I/O (disk reads/writes)
  → Container status and PID count

CPU percentage calculation for containers:
  Docker gives: cpu_delta and system_cpu_delta
  cpu_percent = (cpu_delta / system_cpu_delta) * num_cpus * 100

Memory for containers:
  → memory_usage: current RSS
  → memory_limit: the limit we set
  → memory_percent = (memory_usage / memory_limit) * 100

Container status:
  → Read from container inspect (not stats)
  → Status: running, exited, paused, restarting
  → ExitCode: 0 = clean exit, 137 = OOM killed, other = crash
  → RestartCount: how many times Docker restarted this container
  → StartedAt: when it last started (uptime calculation)
  → Health: healthy, unhealthy, starting (if health check configured)
```

### Step 1F — Collection Goroutine

```
How collection runs:

Single goroutine, started in Layer 4A startup sequence.

Loop:
  1. Sleep 30 seconds (or configured interval)
  2. Collect server metrics (CPU, RAM, disk, load)
  3. Collect container metrics (for all managed containers)
  4. Collect platform metrics (Caddy status, backup age)
  5. Build metrics snapshot
  6. Send snapshot to anomaly detector (Step 4)
  7. Build report for control plane (Step 2)
  8. Send report via WebSocket (if connected)
  9. If not connected: store in local buffer (Step 2C)
  10. Repeat

Collection timing:
  → Total collection time should be < 5 seconds
  → If collection takes longer: log a warning
  → Do NOT collect continuously — 30 second interval is sufficient
  → More frequent = more CPU overhead from the agent itself

Why 30 seconds:
  → Fast enough to detect problems quickly
  → Slow enough to not overhead the server
  → Standard interval for most monitoring tools
  → Matches what users expect from a "live" dashboard
```

### Done Condition for Step 1
```
□ CPU percentage calculated correctly from /proc/stat delta
□ RAM uses MemAvailable (not MemFree)
□ Disk checks the correct filesystem (/ and /var if separate)
□ Load average collected and per-core value calculated
□ Container stats collected for all managed containers
□ Container status includes exit code and restart count
□ Collection runs every 30 seconds
□ Collection goroutine is started on agent startup
□ Collection goroutine runs independently of other layers
□ Total collection time logged if it exceeds 5 seconds
```

---

## Step 2 — Report Building and Sending

### Step 2A — The Metrics Report Structure

```
What gets sent to the control plane every 30 seconds:

{
  "type": "health_report",
  "server_id": "srv-abc123",
  "timestamp": "2024-01-15T10:00:30Z",
  "collected_in_ms": 342,

  "server": {
    "cpu_percent": 45.2,
    "ram_used_mb": 1228,
    "ram_total_mb": 2048,
    "ram_percent": 59.9,
    "disk_used_gb": 18.4,
    "disk_total_gb": 40.0,
    "disk_percent": 46.0,
    "load_1min": 0.82,
    "load_per_core": 0.41
  },

  "containers": [
    {
      "project": "myshop",
      "role": "app",
      "container_id": "abc123",
      "status": "running",
      "health": "healthy",
      "cpu_percent": 12.3,
      "ram_used_mb": 187,
      "ram_limit_mb": 512,
      "ram_percent": 36.5,
      "restart_count": 0,
      "uptime_seconds": 86400,
      "exit_code": null
    },
    {
      "project": "myshop",
      "role": "postgres",
      "container_id": "def456",
      "status": "running",
      "health": "healthy",
      "cpu_percent": 2.1,
      "ram_used_mb": 98,
      "ram_limit_mb": 512,
      "ram_percent": 19.1,
      "restart_count": 0,
      "uptime_seconds": 86400,
      "exit_code": null
    },
    {
      "project": "myblog",
      "role": "app",
      "container_id": "ghi789",
      "status": "exited",
      "health": null,
      "cpu_percent": 0,
      "ram_used_mb": 0,
      "ram_limit_mb": 512,
      "ram_percent": 0,
      "restart_count": 3,
      "uptime_seconds": 0,
      "exit_code": 1
    }
  ],

  "platform": {
    "caddy_running": true,
    "caddy_routes_count": 2,
    "last_backup_at": "2024-01-15T02:04:23Z",
    "last_backup_status": "success",
    "agent_version": "1.0.0",
    "agent_uptime_seconds": 172800
  }
}
```

### Step 2B — What the Control Plane Does With Reports

```
Control plane receives health report:

1. Validate: server_id matches a known server
2. Update server record:
     last_seen = now
     status = connected
3. Upsert container statuses:
     For each container in the report:
       Update or insert into app_state table
4. Store metrics for graphing:
     Insert into metrics_history table
     (only keep last 7 days for MVP)
5. Forward to any browser WebSocket connections watching this server:
     Dashboard gets live updates

Control plane does NOT store every metric permanently.
  → Too much data over time
  → Keep last 7 days of 30-second data
  → Keep last 30 days of hourly averages
  → Keep last 12 months of daily averages

This is enough for:
  → Live dashboard: 30-second data from last few hours
  → Weekly trend: hourly averages
  → Long-term trend: daily averages
```

### Step 2C — Offline Buffering

```
When agent is disconnected from control plane:
  → Cannot send health reports
  → Must not lose data (dashboard wants history when reconnected)

Buffer strategy:
  → Keep last 100 health reports in memory
  → 100 reports × 30 seconds = 50 minutes of history buffered
  → If disconnected for > 50 minutes: oldest reports are dropped
  → On reconnect: send buffered reports as a batch

Batch format on reconnect:
{
  "type": "health_report_batch",
  "server_id": "srv-abc123",
  "reports": [
    { ...report from 50 minutes ago... },
    { ...report from 49 minutes 30 seconds ago... },
    ...
    { ...most recent report... }
  ]
}

Control plane processes the batch:
  → Fills in the gap in the historical data
  → Dashboard can show what happened during offline period
  → Anomalies during offline period appear in history

Memory consideration:
  → Each report is roughly 2KB of JSON
  → 100 reports = ~200KB
  → Acceptable for even a 512MB server
```

### Done Condition for Step 2
```
□ Report structure is correct and complete
□ Report sent every 30 seconds when connected
□ Control plane updates server last_seen on each report
□ Container statuses updated in database on each report
□ Metrics history stored with correct timestamps
□ Offline buffer stores last 100 reports
□ Buffered reports sent as batch on reconnect
□ Dashboard receives live updates from forwarded reports
□ Report includes all managed containers
□ Report collected_in_ms is accurate
```

---

## Step 3 — Log Streaming

### Architecture of Log Streaming

```
Log streaming is separate from health reporting.
It is event-driven (new log line appears → send it)
not interval-driven (every 30 seconds → send metrics).

Log stream lifecycle:
  1. User opens "Logs" view in dashboard for a project
  2. Dashboard opens WebSocket to control plane
  3. Dashboard sends: "start streaming logs for myshop"
  4. Control plane sends: start_log_stream command to agent
  5. Agent (Layer 4B) receives command, calls Layer 4C
  6. Layer 4C starts a log streamer goroutine for that container
  7. Every new log line → forwarded to control plane → to dashboard
  8. User closes logs view
  9. Dashboard sends: "stop streaming logs"
  10. Control plane sends: stop_log_stream command to agent
  11. Agent stops the goroutine
```

### Step 3A — The Log Streamer Goroutine

```
One goroutine per active log stream.

Each goroutine:
  1. Opens Docker log API with follow=true
       → This is a streaming HTTP connection to Docker
       → Docker sends new lines as they appear
       → Connection stays open until we close it

  2. Sends historical lines first (last 200 lines)
       → Fetch tail=200 from Docker logs
       → Send as initial_logs message:
         {
           "type": "initial_logs",
           "stream_id": "stream-abc123",
           "lines": [
             { "timestamp": "...", "stream": "stdout", "text": "..." },
             ...200 lines...
           ]
         }

  3. Enters live streaming loop:
       → Read next line from Docker log stream
       → Parse timestamp and stream type (stdout/stderr)
       → Send as log_line message:
         {
           "type": "log_line",
           "stream_id": "stream-abc123",
           "timestamp": "2024-01-15T10:00:30.123456789Z",
           "stream": "stdout",
           "text": "GET /api/users 200 45ms"
         }
       → Repeat

  4. Handles these stop conditions:
       → Stop signal received (via goroutine cancellation context)
       → Container stops (Docker stream closes)
       → WebSocket disconnects (write fails)
       → Error reading from Docker

  5. On container stop:
       → Stream ends naturally (Docker closes the log stream)
       → Goroutine sends a stream_ended event:
         {
           "type": "stream_ended",
           "stream_id": "stream-abc123",
           "reason": "container_stopped"
         }
       → Dashboard shows: "[Container stopped]"
       → Dashboard does not close the log view automatically
         (user might want to see the last lines before crash)
```

### Step 3B — Multiple Simultaneous Streams

```
Multiple streams can be active at once:

  stream-001: myshop app container
  stream-002: myshop postgres container
  stream-003: myblog app container

Each is an independent goroutine.
Each uses a different stream_id.
The dashboard can display all three simultaneously.

Registry of active streams (in memory):
{
  "stream-001": {
    "project": "myshop",
    "container": "yourplatform_myshop_app",
    "started_at": "2024-01-15T10:00:00Z",
    "cancel": [goroutine cancel function]
  },
  "stream-002": { ... },
  "stream-003": { ... }
}

Limits:
  → Maximum 10 active streams per server
  → Prevents runaway stream accumulation
  → Dashboard typically opens 1-3 streams
  → If limit exceeded: return error, user must close other streams

On agent restart:
  → All stream goroutines are stopped (process ends)
  → Registry is cleared
  → Control plane detects loss of streams (WebSocket closes)
  → Dashboard shows "Log streaming disconnected, reconnecting..."
  → On reconnect: control plane re-sends start_log_stream for active views
```

### Step 3C — Log Line Batching

```
High-volume logs can produce thousands of lines per second.
Sending one WebSocket message per line is inefficient.

Batching strategy:
  → Collect log lines in a small buffer
  → Flush buffer every 100ms OR when buffer has 50 lines
  → Whichever comes first

Batched log message:
{
  "type": "log_lines",           ← plural, note the difference
  "stream_id": "stream-abc123",
  "lines": [
    { "timestamp": "...", "stream": "stdout", "text": "..." },
    { "timestamp": "...", "stream": "stdout", "text": "..." },
    ...up to 50 lines...
  ]
}

Why this approach:
  → Low-volume apps (1 line/second): lines appear individually (< 100ms)
  → High-volume apps (1000 lines/second): batched into bursts
  → Prevents flooding the WebSocket with thousands of messages/second
  → Browser can render batches efficiently
  → 100ms latency is imperceptible to humans
```

### Step 3D — Log Streaming During Deployment

```
Special case: log streaming during a deploy.

When a deploy command runs:
  → The old container is stopped (log stream ends)
  → A new container is started
  → New container logs should automatically appear

Stream handoff during deploy:
  1. Deploy command starts
  2. Layer 4B notifies Layer 4C: "myshop is redeploying"
  3. Layer 4C pauses the log stream (sends: "Redeploying...")
  4. Deploy proceeds: old container stopped, new started
  5. Layer 4B notifies Layer 4C: "new container started: {id}"
  6. Layer 4C automatically switches the stream to the new container
  7. New container logs flow without user needing to refresh

The user sees in the log view:
  [Last line from old container]
  --- Redeploying myshop ---
  [First line from new container]
  [Subsequent lines...]

This seamless handoff is a UX differentiator.
Other tools make you refresh. We don't.
```

### Done Condition for Step 3
```
□ Start log stream sends last 200 lines as initial batch
□ Live log lines appear in dashboard within 100ms of appearing in container
□ High-volume logs are batched (50 lines or 100ms, whichever first)
□ Multiple simultaneous streams work independently
□ Maximum 10 streams enforced
□ Stream ends cleanly when container stops
□ Agent restart stops all streams
□ Control plane re-establishes streams on reconnect
□ Log streaming during deploy shows handoff message
□ New container logs flow automatically after redeploy
```

---

## Step 4 — Anomaly Detection

### What Anomaly Detection Does

```
After every metrics collection run:
  → Compare current metrics against thresholds
  → If any threshold is crossed: generate an alert
  → But: do not spam alerts for persistent problems
    → Alert once when problem appears
    → Alert again when problem resolves
    → Alert again only if problem worsens significantly

This is the intelligence layer.
It turns raw numbers into human-readable events.
```

### Step 4A — The Threshold Table

```
Each metric has thresholds for warning and critical states.

SERVER METRICS:
┌──────────────────┬─────────────┬──────────────┬───────────────────┐
│ Metric           │ Warning     │ Critical     │ Resolution        │
├──────────────────┼─────────────┼──────────────┼───────────────────┤
│ CPU usage        │ 80% for 5m  │ 95% for 2m   │ drops below 70%   │
│ RAM usage        │ 80% used    │ 90% used     │ drops below 75%   │
│ Disk usage       │ 75% used    │ 90% used     │ drops below 70%   │
│ Load per core    │ 0.8         │ 1.5          │ drops below 0.6   │
└──────────────────┴─────────────┴──────────────┴───────────────────┘

CPU is time-delayed:
  → Do not alert on a 2-second CPU spike
  → Alert only if sustained for the threshold duration
  → Track: "how many consecutive 30-second samples above threshold"

CONTAINER METRICS:
┌──────────────────────┬──────────────┬──────────────────────────────┐
│ Condition            │ Severity     │ Alert                        │
├──────────────────────┼──────────────┼──────────────────────────────┤
│ Container stopped    │ Critical     │ "myshop has stopped"         │
│ Container OOM killed │ Critical     │ "myshop ran out of memory"   │
│ Container crashing   │ Critical     │ "myshop keeps crashing"      │
│   (restart_count     │              │                              │
│    increased)        │              │                              │
│ Container unhealthy  │ Warning      │ "myshop health check failing"│
│ RAM > 80% of limit   │ Warning      │ "myshop is using a lot of    │
│                      │              │  memory"                     │
│ RAM > 95% of limit   │ Critical     │ "myshop is about to run out  │
│                      │              │  of memory"                  │
└──────────────────────┴──────────────┴──────────────────────────────┘

PLATFORM METRICS:
┌──────────────────────┬──────────────┬──────────────────────────────┐
│ Condition            │ Severity     │ Alert                        │
├──────────────────────┼──────────────┼──────────────────────────────┤
│ Caddy not running    │ Critical     │ "All apps are unreachable"   │
│ No backup in 26 hrs  │ Warning      │ "Backup is overdue"          │
│ No backup in 50 hrs  │ Critical     │ "Backup has not run in 2     │
│                      │              │  days"                       │
│ Backup failed        │ Warning      │ "Last backup failed"         │
└──────────────────────┴──────────────┴──────────────────────────────┘
```

### Step 4B — Alert State Machine

```
Each monitored metric has its own state machine:

States:
  NORMAL     → metric is within acceptable range
  WARNING    → metric crossed warning threshold
  CRITICAL   → metric crossed critical threshold
  RESOLVED   → metric returned to normal (was previously abnormal)

Transitions trigger alerts:

NORMAL → WARNING:     generate warning alert
NORMAL → CRITICAL:    generate critical alert (skip warning)
WARNING → CRITICAL:   generate critical alert (escalation)
CRITICAL → RESOLVED:  generate resolved alert
WARNING → RESOLVED:   generate resolved alert
RESOLVED → NORMAL:    no alert (just internal state update)

Why RESOLVED is its own state:
  → User needs to know when problems fix themselves
  → "Good news: myshop's disk usage is back to normal"
  → Without this: user sees warning, fixes nothing, still worried
  → With this: user sees "resolved" and can relax

State is stored in memory:
  → One state machine per monitored metric
  → Lost on agent restart (that is acceptable)
  → On restart: current state is evaluated fresh
  → If problem persists: alert fires again (correct)
  → If problem resolved during restart: no alert (correct)
```

### Step 4C — Sustained Duration Detection for CPU

```
CPU alerts must be sustained, not instantaneous.

Implementation:
  → Track consecutive samples above threshold
  → Warning threshold: 80% for 5 minutes = 10 consecutive 30-second samples
  → Critical threshold: 95% for 2 minutes = 4 consecutive 30-second samples

Sample counter approach:
  warning_sample_count = 0
  critical_sample_count = 0

  On each collection:
    if cpu_percent > 95:
      critical_sample_count++
      warning_sample_count = 0  ← reset warning (critical is worse)
      if critical_sample_count >= 4:
        trigger CRITICAL alert (once — state machine prevents duplicates)

    elif cpu_percent > 80:
      warning_sample_count++
      critical_sample_count = 0  ← reset critical (no longer that high)
      if warning_sample_count >= 10:
        trigger WARNING alert (once)

    else:
      if warning_sample_count > 0 or critical_sample_count > 0:
        trigger RESOLVED alert
      warning_sample_count = 0
      critical_sample_count = 0

This prevents alerts on:
  → A single web request causing a CPU spike
  → A one-time backup run causing high CPU
  → Normal startup CPU spike

This catches:
  → A runaway process consuming CPU continuously
  → A memory leak causing CPU thrashing
  → An infinite loop in application code
```

### Step 4D — Container Crash Detection

```
Crash detection is more nuanced than just "container stopped."

Three patterns of container failure:

Pattern 1: Clean stop
  → Container exits with code 0
  → User or command explicitly stopped it
  → Not a crash — no alert needed
  → Detection: exit_code == 0 AND no restart count increase

Pattern 2: Single crash
  → Container exits with non-zero code
  → Docker restarts it (restart policy: always)
  → Container comes back up
  → Generate a warning alert: "myshop crashed and was automatically restarted"
  → Detection: restart_count increased by 1 in this collection interval

Pattern 3: Crash loop
  → Container keeps crashing and restarting
  → restart_count increases by 3+ in a short window
  → Generate a critical alert: "myshop is repeatedly crashing"
  → Detection: restart_count increased by 3+ within 5 minutes

OOM detection:
  → Container exits with code 137 (128 + SIGKILL = OOM kill)
  → Check: /sys/fs/cgroup/memory/docker/{container_id}/memory.oom_control
  → Or: Docker events stream shows OOMKilled: true
  → Generate specific OOM alert (different from generic crash)
  → OOM alert includes current memory limit and usage

Pattern detection timing:
  → Collect restart_count at each 30-second interval
  → Store previous restart_count
  → Delta = current - previous
  → Delta > 0: at least one crash in last 30 seconds
  → Delta > 3 across 5 minutes: crash loop
```

### Done Condition for Step 4
```
□ CPU warning fires after 10 consecutive samples above 80%
□ CPU critical fires after 4 consecutive samples above 95%
□ CPU resolved fires when CPU drops below 70%
□ RAM warning fires immediately when above 80%
□ Disk warning fires immediately when above 75%
□ Container stopped (non-zero exit) generates warning alert
□ Container OOM killed generates specific OOM alert
□ Crash loop (3+ restarts in 5 min) generates critical alert
□ Alert state machine prevents duplicate alerts
□ Resolved alert fires when metric returns to normal
□ Caddy not running generates immediate critical alert
□ Backup overdue generates warning after 26 hours
```

---

## Step 5 — Alert Generation

### What an Alert Is

```
An alert is a structured event that represents:
  → What happened (in plain English)
  → How severe it is
  → What the user can do about it
  → When it happened
  → When it resolved (if it has)

Alerts are NOT:
  → Raw metric values ("CPU is 87.3%")
  → Technical jargon ("OOMKilled: true")
  → Vague messages ("Something went wrong")
```

### Step 5A — Alert Structure

```
{
  "id": "alert-abc123",
  "server_id": "srv-xyz789",
  "project": "myshop",          ← null for server-level alerts
  "severity": "critical",        ← warning, critical
  "type": "container_oom",       ← machine-readable type
  "status": "active",            ← active, resolved
  "title": "myshop ran out of memory",
  "message": "Your app myshop was stopped because it used more memory
               than its 512MB limit. It has been automatically restarted
               and is running again.",
  "detail": "Memory usage reached 511MB of 512MB limit before being killed.",
  "action": "Consider increasing the memory limit for myshop in your
              deployment settings, or investigate memory usage in your app.",
  "fired_at": "2024-01-15T10:00:30Z",
  "resolved_at": null,
  "metrics": {
    "ram_used_mb": 511,
    "ram_limit_mb": 512,
    "exit_code": 137
  }
}
```

### Step 5B — Plain-English Alert Messages

```
Every alert type has a template.
The template is filled with actual values.
The result is human-readable by someone who does not know what Docker is.

Alert templates:

container_stopped:
  Title: "{project} has stopped unexpectedly"
  Message: "Your app {project} stopped running and could not be
             automatically restarted. Your site is currently unreachable."
  Detail: "The app exited with error code {exit_code}."
  Action: "Check the logs for {project} to see what caused the crash.
            Common causes: missing environment variable, cannot connect
            to database, or a bug in your application code."

container_oom:
  Title: "{project} ran out of memory and was restarted"
  Message: "Your app {project} used more memory than its {limit}MB limit.
             It was automatically stopped and restarted.
             Your site should be accessible again."
  Detail: "Memory usage: {used}MB of {limit}MB limit."
  Action: "If this happens often, increase the memory limit in your
            app settings. You can also check if your app has a memory leak."

container_crash_loop:
  Title: "{project} keeps crashing"
  Message: "Your app {project} has crashed {count} times in the last
             5 minutes. This usually means there is a problem that
             prevents it from starting correctly."
  Detail: "Restart count: {count}. Last exit code: {exit_code}."
  Action: "Check the logs for {project} to see the startup error.
            Common causes: wrong environment variable, database not
            available, or a bug introduced in the latest deployment.
            Consider rolling back to the previous version."

disk_warning:
  Title: "Your server disk is getting full"
  Message: "Your server's disk is {percent}% full ({used}GB of {total}GB used).
             At the current rate, it will be full in approximately {days} days."
  Detail: "Available: {available}GB remaining."
  Action: "You can free up space by:
            - Removing unused Docker images (we can do this automatically)
            - Deleting old deployment logs
            - Upgrading your server's disk size with your hosting provider"

disk_critical:
  Title: "Your server disk is almost full — action required"
  Message: "Your server's disk is {percent}% full. Apps may stop working
             soon if this is not resolved."
  Detail: "Only {available}GB remaining of {total}GB total."
  Action: "Immediate action needed. We have automatically cleaned up
            unused Docker images. If the disk is still full, please
            upgrade your server's disk or contact support."

ram_warning:
  Title: "{project} is using a lot of memory"
  Message: "Your app {project} is using {used}MB of its {limit}MB memory
             limit ({percent}% used)."
  Detail: "If usage reaches 100%, the app will be automatically restarted."
  Action: "Monitor if this continues to increase. If it does, consider
            increasing the memory limit or investigating memory usage."

backup_overdue:
  Title: "Backup is overdue"
  Message: "Your last successful backup was {hours} hours ago.
             Backups normally run every 24 hours."
  Detail: "Last backup: {last_backup_at}. Status: {status}."
  Action: "You can run a backup manually from the Backups tab.
            If backups keep failing, check that your backup storage
            is accessible and has enough space."

caddy_down:
  Title: "All your apps are unreachable"
  Message: "The component that handles web traffic (the reverse proxy)
             has stopped. None of your apps are accessible from the internet."
  Detail: "Attempting to restart automatically."
  Action: "This is being handled automatically. If your apps do not
            come back online within 2 minutes, please contact support."
```

### Step 5C — Alert Deduplication

```
Problem without deduplication:
  → Disk is at 91% full
  → Every 30 seconds: new alert generated
  → User gets 2 alerts per minute
  → 120 alerts per hour
  → User turns off alerts

Solution: alert deduplication

Rules:
  1. Same alert type for same resource: only fire once
       → disk_critical for server srv-abc: fire once
       → Do not fire again until it resolves and re-triggers

  2. Alert state is tracked:
       active_alerts = {
         "server_disk": {
           "type": "disk_critical",
           "fired_at": "...",
           "status": "active"
         },
         "myshop_container": {
           "type": "container_crash_loop",
           "fired_at": "...",
           "status": "active"
         }
       }

  3. When metrics are collected:
       → Check: is there already an active alert for this exact condition?
       → If yes: do not fire another alert
       → If no: fire the alert, add to active_alerts

  4. When condition clears:
       → Remove from active_alerts
       → Fire a resolved alert
       → Next time condition appears: fresh alert

  5. Escalation (warning → critical):
       → active alert at WARNING level exists
       → Metric crosses CRITICAL threshold
       → Update existing alert to CRITICAL
       → Fire escalation alert: "myshop memory usage is now critical"
       → This is a new alert, not a duplicate
```

### Step 5D — Alert Rate Limiting

```
Even with deduplication, some conditions might flip rapidly:
  → Container crashes, restarts, crashes again, restarts...
  → Each transition could trigger an alert

Rate limiting:
  → Maximum 3 alerts per project per hour
  → Maximum 5 server-level alerts per hour
  → If rate limit hit: suppress alerts, send one
    "multiple alerts suppressed" notification per hour

The suppression message:
  "Multiple issues detected with myshop in the last hour.
   Some alerts were suppressed to avoid overwhelming you.
   Current status: [crash loop — 8 restarts in 60 minutes]
   Check the logs for details."
```

### Done Condition for Step 5
```
□ Every alert type has a plain-English template
□ Alert messages do not contain technical jargon
□ Alert structure includes all required fields
□ Deduplication prevents duplicate alerts for same condition
□ Escalation generates a new alert when severity increases
□ Resolved alert fires when condition clears
□ Rate limiting prevents more than 3 alerts per project per hour
□ Suppression message sent when rate limit hit
□ Alerts stored in control plane database
□ Alerts visible in dashboard as a list
```

---

## Step 6 — Alert Delivery

### Where Alerts Go

```
Three delivery paths:

Path 1: Dashboard (always)
  → Alert appears in the dashboard notification center
  → Bell icon shows unread count
  → Alert list shows all recent alerts
  → Alert can be marked as "acknowledged"

Path 2: Email (configurable)
  → User sets their email in account settings
  → Control plane sends email via its email provider
  → Agent does NOT send email directly
  → Agent sends alert to control plane via WebSocket
  → Control plane handles delivery

Path 3: WhatsApp (configurable, differentiator)
  → User connects their WhatsApp number in settings
  → Control plane sends via WhatsApp Business API
  → Agent sends alert to control plane
  → Control plane handles delivery

For MVP:
  → Dashboard: always implemented
  → Email: implement in MVP
  → WhatsApp: post-MVP (requires WhatsApp Business API setup)
```

### Step 6A — Alert Severity and Delivery Rules

```
Not every alert needs to wake someone up.

Delivery rules:

CRITICAL alerts:
  → Dashboard: immediately
  → Email: immediately (even at 3am)
  → WhatsApp: immediately (high urgency)
  → These are: app down, disk full, Caddy down, crash loop

WARNING alerts:
  → Dashboard: immediately
  → Email: batched — send once per hour with all warnings
  → WhatsApp: not sent (too noisy)
  → These are: high memory, disk at 75%, single crash, backup overdue

RESOLVED alerts:
  → Dashboard: immediately
  → Email: included in next hourly batch if within working hours
  → WhatsApp: send resolved for critical alerts only
    ("Good news: myshop is running again")
```

### Step 6B — Alert Sending Flow

```
Alert generated by anomaly detector (Step 4)
           │
           ▼
Alert Manager receives it
           │
           ▼
Check deduplication: is this already active?
  → If yes: skip (already sent)
  → If no: proceed
           │
           ▼
Apply rate limiting: is project over limit?
  → If yes: suppress, update suppression state
  → If no: proceed
           │
           ▼
Send to control plane via WebSocket:
{
  "type": "alert",
  "alert": { ...full alert structure... }
}
           │
           ▼
Control plane receives alert:
  → Stores in alerts table
  → Marks as unread for this user
  → Pushes to dashboard via WebSocket (if open)
  → Queues email delivery (if severity warrants)
  → Queues WhatsApp delivery (if configured and severity warrants)
           │
           ▼
Dashboard updates:
  → Bell icon shows new count
  → Alert appears in notification list
  → If critical: banner shown on relevant project page
```

### Step 6C — Alert History

```
Control plane stores all alerts:

alerts table:
  id
  server_id
  project_name      ← null for server-level
  severity          ← warning, critical
  type              ← machine-readable type
  status            ← active, resolved, acknowledged
  title
  message
  detail
  action
  metrics           ← JSON blob of relevant metrics at time of alert
  fired_at
  resolved_at
  acknowledged_at
  acknowledged_by   ← user ID

Retention:
  → Keep all alerts forever (they are small text records)
  → Useful for: support, pattern detection, user history

Dashboard displays:
  → Active alerts: shown prominently
  → Recent resolved: shown in history
  → All alerts: available in alerts history page
```

### Done Condition for Step 6
```
□ Alerts appear in dashboard within 2 seconds of being generated
□ Critical alerts trigger immediate email
□ Warning alerts are batched hourly for email
□ Alert history stored in control plane database
□ Alerts can be acknowledged from dashboard
□ Resolved alerts update the dashboard
□ WhatsApp delivery architecture is designed (even if not built for MVP)
□ Alert delivery does not block the metrics collection loop
```

---

## Step 7 — Auto-Remediation

### What Auto-Remediation Is

```
For some problems, Layer 4C does not just alert.
It also takes automatic corrective action.

Philosophy:
  → Alert the user AND try to fix it
  → Never take destructive action automatically
  → Always tell the user what was done automatically
  → User can see: "We detected X and automatically did Y"

This is the "peace of mind" differentiator.
Most monitoring tools just alert.
We alert AND try to help.
```

### Step 7A — Auto-Remediation Cases

```
Case 1: Disk space cleanup

Trigger: Disk above 75% full

Auto-action:
  → Run: docker image prune -f
    → Removes all unused Docker images
    → Safe: only removes images not used by any container
    → Can free significant space (old app versions, pulled images)
  → Run: docker container prune -f
    → Removes stopped containers
    → Safe: running containers are not touched
  → Check disk after cleanup
  → Report to control plane: "Automatically freed {freed}GB by removing
     unused Docker images"

Do NOT automatically:
  → Delete user data or volumes
  → Delete log files (user might need them)
  → Delete backups

Case 2: Container single crash + restart

Trigger: Container crashes, Docker restarts it, it comes back up

Auto-action:
  → Docker's restart policy already handles the restart
  → Layer 4C's action: verify the restart succeeded
  → After detecting crash (restart_count increased):
    → Wait 30 seconds (next collection cycle)
    → Check: is container now running and healthy?
    → If yes: send alert that includes "automatically recovered"
    → If no: escalate to crash loop alert

Case 3: Caddy not running

Trigger: Caddy process is not found

Auto-action:
  → Immediately attempt to restart Caddy (Layer 3B Step 1D)
  → This happens in Layer 3B but Layer 4C triggers it
  → Layer 4C detects: caddy_running = false
  → Layer 4C calls: Layer 3B restart Caddy
  → Layer 3B restarts and restores all routes
  → Layer 4C on next collection: caddy_running = true
  → Send alert: "Reverse proxy stopped and was automatically restarted.
     Your apps are accessible again."

Case 4: Agent memory approaching limit

Trigger: Agent itself using > 200MB (of its 256MB limit)

Auto-action:
  → Flush log stream buffers (largest memory consumers)
  → Run Go garbage collection explicitly
  → If memory still above 200MB: log warning
  → Do NOT restart the agent (too disruptive)
  → Alert: "Agent memory usage is high — monitoring stability"
```

### Done Condition for Step 7
```
□ Disk above 75%: docker image prune runs automatically
□ Amount freed is reported to control plane and dashboard
□ Caddy not running: restart attempted immediately
□ Caddy restart triggers route restoration
□ Alert includes "automatically resolved" when auto-remediation works
□ Auto-remediation never deletes user data
□ All auto-remediation actions are logged as server events
□ User can see history of automatic actions in dashboard
```

---

## Layer 4C Overall Done Condition

```
The full test sequence:

Test 1 — Normal operation metrics:
  □ Deploy an app, let it run for 5 minutes
  □ Dashboard shows CPU, RAM, disk updating every 30 seconds
  □ Container shows as running with correct memory usage
  □ No alerts generated for healthy system

Test 2 — Container crash:
  □ Manually crash the app container (kill -9 PID inside container)
  □ Docker restarts it automatically
  □ Within 60 seconds: alert appears "myshop crashed and was restarted"
  □ Alert includes last log lines before crash
  □ Second alert (resolved) appears when container is healthy again

Test 3 — Crash loop:
  □ Configure app to crash immediately on start
  □ Docker restarts it repeatedly
  □ Within 5 minutes: critical alert "myshop keeps crashing"
  □ Alert includes restart count and last log lines

Test 4 — OOM kill:
  □ Deploy an app with very low memory limit (64MB)
  □ Run a workload that uses more than 64MB
  □ Container is OOM killed
  □ Alert: "myshop ran out of memory" with specific memory values
  □ Alert is distinct from generic crash alert

Test 5 — Disk filling up:
  □ Fill disk to 76% (create large files)
  □ Warning alert fires
  □ Auto-cleanup runs (docker image prune)
  □ Alert shows: "Automatically freed X GB"
  □ If disk is still above threshold: alert remains active

Test 6 — Log streaming:
  □ Open logs view for myshop in dashboard
  □ Last 200 lines appear immediately
  □ Generate log output in app (make HTTP requests)
  □ New lines appear in dashboard within 100ms
  □ Close log view: streaming stops
  □ Reopen: fresh 200 lines + new streaming starts

Test 7 — Offline metrics buffer:
  □ Disconnect agent from control plane for 10 minutes
  □ Reconnect
  □ Dashboard shows the 10 minutes of history that was buffered
  □ No gap in the metrics graph

Test 8 — Alert deduplication:
  □ Fill disk to 91%
  □ Wait 5 minutes (10 collection cycles)
  □ Exactly ONE disk_critical alert in dashboard (not 10)
  □ Resolve by freeing disk space
  □ One "resolved" alert appears

Test 9 — Caddy crash recovery:
  □ Kill Caddy process manually
  □ Layer 4C detects caddy_running = false within 30 seconds
  □ Layer 4C triggers Caddy restart
  □ Caddy restarts, routes restored
  □ Alert: "Reverse proxy stopped and automatically restarted"
  □ Apps accessible again within 30 seconds of Caddy death

When all 9 tests pass, Layer 4C is done.
Move to Layer 5A — Auth and Sessions.
```

---

## What Layer 4C Does NOT Do

```
Layer 4C does not:
├── Execute deployment commands (Layer 4B)
├── Manage Docker containers (Layer 3A)
├── Configure Caddy routes (Layer 3B)
├── Run backup jobs (Layer 3C)
├── Manage WebSocket connection (Layer 4A)
├── Send emails directly (control plane handles delivery)
├── Make deployment decisions (Layer 4B)
├── Store metrics permanently (control plane stores them)
└── Monitor other servers (each agent monitors only its own server)

Layer 4C watches, reports, and alerts.
It is the observer, not the actor.
When it detects a problem it cannot fix:
  it tells the user clearly what is wrong and what to do.
When it detects a problem it can safely fix:
  it fixes it, tells the user what happened, and keeps watching.
```

---

**Ready for Layer 5A — Auth and Sessions?**