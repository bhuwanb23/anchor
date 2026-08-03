# Layer 2 — Server Environment Detection: Complete Plan

---

## What Layer 2 Actually Is

```
Layer 2 is the agent's self-awareness.

Before the agent does anything useful, it needs to answer:
"Is this server actually capable of running what we need?"

It runs in two situations:

Situation 1 — During install (called by install.sh)
  The install script runs: yourplatform-agent preflight --json
  Agent checks everything, returns results
  Install script either continues or stops and explains why

Situation 2 — Every time the agent starts (called by itself)
  Agent starts via systemd
  Runs pre-flight internally before connecting to control plane
  If something is now broken that wasn't before, it reports it
  and waits rather than connecting in a broken state
```

---

## The Full Check List

```
Layer 2 checks everything in this order:

Group A — System basics (must pass to continue)
  A1. OS and version check
  A2. Architecture check
  A3. Available disk space
  A4. Available RAM
  A5. System clock accuracy

Group B — Network (must pass to continue)
  B1. Outbound internet connectivity
  B2. DNS resolution
  B3. Port 80 availability
  B4. Port 443 availability
  B5. Outbound port 443 to control plane

Group C — Docker (auto-fix attempted before failing)
  C1. Docker installed
  C2. Docker daemon running
  C3. Docker version acceptable
  C4. Agent can talk to Docker socket
  C5. Docker can pull from internet

Group D — Runtime environment
  D1. Systemd available and functional
  D2. Required directories exist and are writable
  D3. No conflicting agent already running
  D4. Config file is readable and valid

Each check has:
  - A name
  - A severity (blocking vs warning)
  - An auto-fix (yes/no)
  - A plain-English failure message
  - A plain-English fix instruction
```

---

## How Results Flow

```
Pre-flight runs all checks
          │
          ▼
Produces a result object:
{
  passed: true/false,
  checks: [
    { name, status, severity, message, fix_instruction },
    { name, status, severity, message, fix_instruction },
    ...
  ],
  auto_fixed: [ list of things the agent fixed automatically ],
  system_info: { os, arch, ram, disk, docker_version, ... }
}
          │
    ┌─────┴─────┐
called by      called by
install.sh     agent startup
    │               │
    ▼               ▼
prints human-   sends result
readable        to control plane
output          via API/WebSocket
```

---

## Step 1 — Result Data Structure

### What to Define First

Before writing any check logic, define what a check result looks like. This structure flows through the entire layer.

**A single check result contains:**
- Name — machine-readable identifier like `docker_installed`
- Display name — human readable like `Docker Installation`
- Status — one of: `pass`, `fail`, `warn`, `fixed`
- Severity — one of: `blocking`, `warning`
- Message — plain English description of what was found
- Fix instruction — plain English of what to do if this failed
- Auto fixed — boolean, whether the agent fixed this automatically

**The overall pre-flight result contains:**
- Whether the overall check passed (all blocking checks passed)
- List of all individual check results
- List of what was auto-fixed
- System info collected during checks (OS, arch, RAM, disk, Docker version)
- Timestamp of when the check ran
- How long the entire check took

**Severity distinction:**
```
Blocking = agent cannot function at all without this
  Examples: Docker not installed, port 80 in use, no disk space

Warning = agent works but something is suboptimal
  Examples: RAM is low but above minimum, Docker version is old but functional
  Agent continues, but warning is shown in dashboard
```

### Done Condition for Step 1
```
□ Check result structure is defined
□ Overall result structure is defined
□ Severity levels are defined
□ Status values are defined
□ Structure can be serialized to JSON (for --json flag output)
□ Structure can be rendered as human-readable text (for install script output)
```

---

## Step 2 — Group A: System Basics

### A1. OS and Version Check

**What it checks:**
Reads `/etc/os-release` and extracts the distribution ID and version number.

**What it validates:**
- Distribution is in the supported list: ubuntu, debian, centos, rhel, fedora, rocky, almalinux
- Version meets the minimum: Ubuntu 20.04+, Debian 11+, CentOS 8+

**Severity:** Blocking — unsupported OS means the agent cannot guarantee behavior

**Auto-fix:** No — cannot change the OS

**Failure message example:**
```
Your server is running CentOS 7, which is not supported.
Minimum supported version is CentOS 8.
Supported systems: Ubuntu 20.04+, Debian 11+, CentOS 8+, Fedora 36+
```

**What it stores in system_info:**
- `os`: ubuntu
- `os_version`: 22.04
- `os_pretty`: Ubuntu 22.04.3 LTS

---

### A2. Architecture Check

**What it checks:**
Runs `uname -m` and reads the result.

**What it validates:**
- Architecture is `x86_64` (amd64) or `aarch64` (arm64)
- Not 32-bit (i386, i686, armv7l)

**Severity:** Blocking — binaries are only built for 64-bit

**Auto-fix:** No

**Failure message example:**
```
Your server is running 32-bit ARM (armv7l), which is not supported.
YourPlatform requires a 64-bit server (x86_64 or aarch64).
If this is a Raspberry Pi, please flash a 64-bit OS image.
```

**What it stores in system_info:**
- `arch`: amd64

---

### A3. Available Disk Space

**What it checks:**
Reads available disk space on the root filesystem and on `/var` if it is a separate partition.

**Thresholds:**
```
Below 2GB available   → Blocking (cannot pull Docker images)
2GB - 5GB available   → Warning (will fill up quickly)
Above 5GB             → Pass
```

**Severity:** Blocking if below 2GB, Warning if 2-5GB

**Auto-fix:** No — cannot free disk space safely without knowing what to delete

**Failure message example:**
```
Your server only has 800MB of free disk space.
YourPlatform needs at least 2GB free to pull Docker images and store app data.

To free up space, you can:
  - Remove unused Docker images: docker image prune -a
  - Check what is using space: du -sh /* 2>/dev/null | sort -rh | head -20
  - Expand your server's disk through your hosting provider
```

**Warning message example:**
```
Your server has 3.2GB of free disk space.
This is enough to get started, but apps and their data will fill this quickly.
Consider expanding your disk soon.
```

**What it stores in system_info:**
- `disk_total_gb`: 40
- `disk_available_gb`: 23
- `disk_used_percent`: 42

---

### A4. Available RAM

**What it checks:**
Reads `/proc/meminfo` and extracts `MemTotal` and `MemAvailable`.

**Thresholds:**
```
Total RAM below 512MB     → Blocking
Total RAM 512MB - 1GB     → Warning
Total RAM above 1GB       → Pass
```

**Severity:** Blocking if below 512MB, Warning if 512MB-1GB

**Auto-fix:** No

**Why 512MB minimum:**
```
Docker daemon itself uses ~100MB
Caddy uses ~30MB
Agent uses ~50MB
That leaves ~330MB for user apps — barely enough for one small app
Below 512MB total, even the infrastructure cannot run reliably
```

**Failure message example:**
```
Your server has 256MB of RAM, which is below the 512MB minimum.
Running Docker and your apps requires at least 512MB.

Options:
  - Upgrade to a larger server plan (usually $2-5/month more)
  - Your current hosting provider likely offers a RAM upgrade
```

**What it stores in system_info:**
- `ram_total_mb`: 2048
- `ram_available_mb`: 1456

---

### A5. System Clock Accuracy

**What it checks:**
Compares the system clock to the control plane's reported time. If the agent cannot reach the control plane yet, it checks whether `systemd-timesyncd` or `ntpd` is running.

**Why this matters:**
```
TLS certificates fail if the clock is more than a few minutes off
JWT tokens have expiry times — a wrong clock breaks auth
Let's Encrypt ACME challenges fail with clock skew
```

**Threshold:** More than 5 minutes of drift is blocking

**Auto-fix:** Yes — if `systemd-timesyncd` is available but not running, start it

**Failure message example:**
```
Your server's clock is 23 minutes behind real time.
This will cause HTTPS certificate errors and login failures.

YourPlatform attempted to fix this automatically but could not.
Please enable NTP time synchronization:
  sudo timedatectl set-ntp true
```

### Done Condition for Step 2
```
□ A1 correctly identifies Ubuntu, Debian, CentOS and their versions
□ A1 fails correctly on an unsupported OS with a clear message
□ A2 correctly identifies amd64 and arm64
□ A3 reads actual available disk and applies correct thresholds
□ A3 warning fires correctly at 3GB, blocking fires correctly at 1GB
□ A4 reads actual RAM from /proc/meminfo
□ A5 detects whether time sync is running
□ All checks populate system_info fields
```

---

## Step 3 — Group B: Network Checks

### B1. Outbound Internet Connectivity

**What it checks:**
Attempts to make an HTTP GET request to a reliable external host. Does not use your own servers for this — use something like `1.1.1.1` (Cloudflare DNS over HTTP) or `https://connectivity-check.ubuntu.com`. Your own servers might be down, which should not block the connectivity check.

**Severity:** Blocking — without internet, the agent cannot pull Docker images or connect to the control plane

**Auto-fix:** No

**Failure message example:**
```
Your server cannot reach the internet.
This is required to pull app images and connect to the YourPlatform control plane.

Common causes:
  - Your hosting provider has a firewall blocking outbound traffic
  - The server has no default route configured
  - DNS is not resolving

Try from your server:
  curl -I https://1.1.1.1
  ping 8.8.8.8
Contact your hosting provider if these fail.
```

---

### B2. DNS Resolution

**What it checks:**
Attempts to resolve a known hostname. Tests two things separately:
- Can it resolve public DNS (`google.com`)
- Can it resolve your control plane hostname (`api.yourplatform.com`)

**Why test both separately:**
```
If public DNS works but your domain fails:
  → Your domain might not be configured correctly
  → Or the user's DNS provider blocks unknown domains

If neither works:
  → DNS is completely broken on this server
```

**Severity:** Blocking

**Auto-fix:** No — DNS configuration is outside the agent's control

**Failure messages:**
```
DNS not working at all:
"Your server cannot resolve domain names.
Check /etc/resolv.conf and ensure a nameserver is configured."

Your domain specifically failing:
"Your server can reach the internet but cannot resolve api.yourplatform.com.
This may be a temporary DNS issue. Wait 60 seconds and try again.
If this persists, contact your hosting provider about DNS filtering."
```

---

### B3. Port 80 Availability

**What it checks:**
Attempts to bind to port 80 on all interfaces. If binding fails, tries to identify which process is currently holding port 80.

**How to identify the occupying process:**
Read `/proc/net/tcp` and cross-reference with `/proc/*/net/tcp` to find which PID has the socket. Then read `/proc/PID/cmdline` to get the process name. This works without installing any tools.

**Common occupants:**
```
apache2     → Common on Ubuntu default installs
nginx       → Common if someone set up a web server before
another caddy → Agent was previously installed
lighttpd    → Less common but exists
```

**Severity:** Blocking — Caddy needs port 80 for Let's Encrypt HTTP challenges

**Auto-fix:**
```
If the occupant is apache2 or nginx with no active sites configured:
  → Offer to stop and disable it automatically
  → Only do this if there are no active virtual hosts (safe to stop)

If the occupant has active sites:
  → Do NOT auto-fix — would break existing websites
  → Tell the user exactly what is running and what to do
```

**Failure message example:**
```
Port 80 is in use by apache2 (PID 1234).

If you are not using Apache for anything, we can stop it for you.
Run the install command again with --disable-apache to do this automatically.

If Apache is serving other websites, you will need to either:
  - Move those sites to YourPlatform first
  - Or configure Apache to proxy to YourPlatform (advanced — see docs)
```

---

### B4. Port 443 Availability

**What it checks:**
Same approach as B3 but for port 443.

**Severity:** Blocking — Caddy serves HTTPS on 443

**Auto-fix:** Same logic as B3

---

### B5. Outbound Port 443 to Control Plane

**What it checks:**
Attempts a TCP connection to `api.yourplatform.com:443`. This is separate from B1 (general internet) because some hosting providers block outbound traffic to specific ports or hosts.

**Why this is its own check:**
```
Some VPS providers (especially cheaper ones) have default outbound firewall rules
Some corporate networks block outbound 443 to non-whitelisted hosts
Some providers require you to explicitly open outbound ports in a dashboard
```

**Severity:** Blocking — the agent communicates with the control plane over 443

**Failure message example:**
```
Your server cannot reach api.yourplatform.com on port 443.
The agent needs this connection to receive deployment commands.

Your server can reach the internet generally, so this is likely
a firewall rule blocking this specific connection.

Check your hosting provider's firewall settings and ensure
outbound TCP port 443 is allowed to all destinations.

Providers where this is a common issue:
  - AWS EC2: check Security Group outbound rules
  - Google Cloud: check VPC firewall rules
  - Hetzner Cloud: check Firewall rules in the dashboard
```

### Done Condition for Step 3
```
□ B1 correctly detects internet connectivity (test on a server with no network)
□ B2 correctly detects DNS failure vs working DNS
□ B3 detects what process is on port 80 and names it
□ B3 auto-fix stops apache2 when it has no active sites
□ B3 does NOT auto-fix when apache2 has active virtual hosts
□ B4 same as B3 for port 443
□ B5 specifically tests the control plane connection
□ All failures produce messages that name the specific problem and fix
```

---

## Step 4 — Group C: Docker Checks

### C1. Docker Installed

**What it checks:**
Checks whether the `docker` binary exists in PATH. Secondary check: whether `/var/run/docker.sock` exists.

**Severity:** Blocking — everything runs in Docker

**Auto-fix:** Yes — this is the most important auto-fix in the entire layer

**Auto-fix logic:**
```
Detect OS (already done in A1)

Ubuntu/Debian:
  1. Update apt package index
  2. Install prerequisites (apt-transport-https, ca-certificates, curl, gnupg)
  3. Add Docker's official GPG key
  4. Add Docker's apt repository
  5. Install docker-ce, docker-ce-cli, containerd.io
  6. Start and enable docker service

CentOS/RHEL/Rocky/AlmaLinux:
  1. Install yum-utils
  2. Add Docker's yum repository
  3. Install docker-ce, docker-ce-cli, containerd.io
  4. Start and enable docker service

Fedora:
  Same as CentOS but using dnf
```

**Why auto-install Docker:**
```
This is the single most common reason install fails for non-technical users.
"I need to install Docker first" is a blocker that loses users.
Auto-installing Docker is the difference between foolproof and requires-a-sysadmin.
```

**What to show while installing:**
```
[INFO] Docker is not installed. Installing Docker automatically...
[INFO] Adding Docker repository...
[INFO] Installing Docker packages (this may take a minute)...
[OK]   Docker installed successfully (version 25.0.3)
```

**Failure message (if auto-install fails):**
```
Docker is not installed and automatic installation failed.

Error: [specific error from package manager]

To install manually:
  Ubuntu/Debian: https://docs.docker.com/engine/install/ubuntu/
  CentOS:        https://docs.docker.com/engine/install/centos/

After installing Docker, run the install command again.
```

---

### C2. Docker Daemon Running

**What it checks:**
Even if Docker is installed, the daemon might not be running. Check via `systemctl is-active docker`.

**Severity:** Blocking

**Auto-fix:** Yes — run `systemctl start docker`

**Sequence:**
```
Check if docker daemon is running
  → Not running
    → Attempt: systemctl start docker
    → Wait up to 10 seconds
    → Check again
      → Now running: mark as "fixed", continue
      → Still not running: read docker daemon logs, surface error
```

**Failure message (if auto-start fails):**
```
Docker is installed but the Docker daemon is not running and could not be started.

Docker error output:
[last 10 lines of journalctl -u docker]

Common causes:
  - Kernel too old to support Docker's requirements
  - Missing kernel modules (overlay filesystem)
  - Conflicting container runtime already installed

Try manually: sudo systemctl start docker
Then check: sudo journalctl -u docker -n 50
```

---

### C3. Docker Version Acceptable

**What it checks:**
Runs `docker version --format json` and parses the version number.

**Minimum version:** Docker 20.10.0

**Why 20.10.0:**
```
20.10 introduced rootless mode improvements and BuildKit by default
Older versions have known security issues
Most Linux distros with Docker have at least 20.10 by now
```

**Severity:** Warning if 20.10-23.x, pass if 24+

**Auto-fix:** No — upgrading Docker is risky mid-operation

**Warning message:**
```
Docker version 20.10.7 is installed. Version 24+ is recommended.
YourPlatform will work, but you may be missing security patches.
To upgrade: [link to Docker upgrade docs for their specific OS]
```

---

### C4. Agent Can Talk to Docker Socket

**What it checks:**
Attempts to make a real Docker API call — specifically `GET /info` via the Docker socket at `/var/run/docker.sock`. This confirms the agent process has permission to use Docker, not just that Docker exists.

**Why this is separate from C1 and C2:**
```
Docker can be installed and running but:
  - Socket permissions might deny non-root access
  - Socket path might be non-standard
  - Docker in rootless mode has a different socket path
  - SELinux/AppArmor might block socket access
```

**Severity:** Blocking

**Auto-fix:** Limited — if the socket exists but has wrong permissions, fix them

**Failure message:**
```
The agent cannot communicate with Docker.
Docker is installed and running, but access to the Docker socket was denied.

Socket location: /var/run/docker.sock
Socket permissions: [actual permissions]

This is usually a permissions issue. Try:
  sudo chmod 660 /var/run/docker.sock

If you are running Docker in rootless mode, please see:
https://docs.yourplatform.com/docker-rootless
```

---

### C5. Docker Can Pull from Internet

**What it checks:**
Attempts to pull a tiny Docker image to verify Docker can reach Docker Hub. Use the smallest possible image — `hello-world` is 13KB.

**Why this check exists:**
```
Some servers have Docker installed but:
  - Outbound traffic to registry-1.docker.io is blocked
  - Corporate proxy required for Docker pulls
  - DNS resolves docker.io but TCP connection is blocked
  - Registry rate limits (less common but exists)
```

**Severity:** Blocking — cannot deploy anything without pulling images

**Auto-fix:** No

**Important:** Delete the pulled image immediately after the check. Do not leave `hello-world` on the user's server.

**Failure message:**
```
Docker is running but cannot pull images from Docker Hub.
This means apps cannot be deployed until this is resolved.

Error: [docker pull error message]

Common causes:
  - Firewall blocking outbound traffic to registry-1.docker.io
  - Docker Hub rate limit (rare on fresh servers)
  - Incorrect proxy settings

Test manually: sudo docker pull hello-world
```

### Done Condition for Step 4
```
□ C1 detects Docker not installed on a fresh Ubuntu server
□ C1 auto-installs Docker successfully on Ubuntu 20.04, 22.04, Debian 11
□ C1 auto-installs Docker successfully on CentOS 8, Rocky Linux
□ C2 detects stopped Docker daemon and restarts it
□ C3 correctly parses Docker version and applies warning threshold
□ C4 detects socket permission issues separately from daemon issues
□ C5 pulls hello-world, confirms success, deletes the image
□ C5 fails clearly when outbound Docker Hub traffic is blocked
```

---

## Step 5 — Group D: Runtime Environment

### D1. Systemd Available and Functional

**What it checks:**
Confirms `systemctl` exists and can communicate with the systemd daemon. Runs `systemctl is-system-running` and accepts `running` or `degraded` as acceptable states.

**Why `degraded` is acceptable:**
```
Degraded means some services have failed but systemd itself is functional.
This is common on minimal VPS installs where some optional services fail.
The agent can still be managed by systemd in degraded state.
```

**Severity:** Blocking — the agent runs as a systemd service

**Auto-fix:** No

---

### D2. Required Directories Exist and Are Writable

**What it checks:**
Verifies these directories exist and the agent can write to them:
- `/etc/yourplatform/` — config directory
- `/var/lib/yourplatform/` — data directory
- `/var/log/yourplatform/` — log directory
- `/tmp/` — temp directory for downloads

**Severity:** Blocking

**Auto-fix:** Yes — create the directories if they do not exist. They should exist after the install script ran, but this is a safety net.

---

### D3. No Conflicting Agent Already Running

**What it checks:**
Looks for another instance of `yourplatform-agent` already running as a process. This catches:
- Install script run twice simultaneously
- Previous failed install that left a process running
- Manual start while systemd service is also running

**How to check:**
Read `/proc/*/cmdline` for all processes, look for ones that match the agent binary name, exclude the current process's own PID.

**Severity:** Blocking

**Auto-fix:** No — two running agents would cause chaos, safer to stop and let the user sort it out

**Failure message:**
```
Another YourPlatform agent is already running (PID 4521).

If this is an old agent from a previous install attempt:
  sudo systemctl stop yourplatform-agent
  sudo kill 4521

Then run the install command again.
```

---

### D4. Config File Readable and Valid

**What it checks:**
This only runs when the agent is starting via systemd (not during the initial install pre-flight). Verifies:
- Config file exists at `/etc/yourplatform/config.yaml`
- File is readable by the agent process
- File parses correctly as YAML
- Required fields are present: either `registration_token` OR (`agent_id` AND `agent_secret`)
- `control_plane_url` is present and is a valid URL

**Severity:** Blocking — agent cannot do anything without valid config

**Auto-fix:** No — corrupted config requires user intervention

**Failure message:**
```
The agent configuration file is invalid or missing.

File location: /etc/yourplatform/config.yaml
Problem: [specific parse error or missing field]

If this file was accidentally edited or deleted, you can reconnect
this server by running a new install command from your dashboard.
Your deployed apps will continue running — only management is affected.
```

### Done Condition for Step 5
```
□ D1 passes on a normal systemd system, fails clearly if systemd is missing
□ D2 creates missing directories automatically
□ D3 detects a duplicate agent process correctly
□ D4 catches missing fields in config with specific error messages
□ D4 catches malformed YAML with a useful error
```

---

## Step 6 — The Pre-flight Runner

### What This Is

The orchestrator that runs all checks in the right order, handles auto-fixes, and produces the final result. This is what gets called either by the install script or by the agent on startup.

### Execution Order and Short-Circuit Logic

```
Run Group A (system basics)
  │
  ├── A1 fails (blocking) → stop immediately
  │   Report: cannot determine OS, nothing else will work
  │
  ├── A1 passes → continue
  │
  [continue through A2, A3, A4, A5]
  │
  All of Group A passes or has only warnings
  │
  ▼
Run Group B (network)
  │
  ├── B1 fails (blocking) → stop immediately
  │   No point checking DNS or specific ports without basic internet
  │
  ├── B1 passes → continue through B2, B3, B4, B5
  │
  ▼
Run Group C (Docker)
  │
  ├── C1 fails → attempt auto-fix (install Docker)
  │   ├── Fix succeeds → mark as "fixed", continue to C2
  │   └── Fix fails → stop, report
  │
  ├── C2 fails → attempt auto-fix (start daemon)
  │   ├── Fix succeeds → continue to C3
  │   └── Fix fails → stop, report
  │
  [C3, C4, C5 — no short circuit between them, collect all results]
  │
  ▼
Run Group D (runtime environment)
  [all checks, collect all results]
  │
  ▼
Produce final result object
```

### Auto-Fix Transparency

When the agent fixes something automatically, it must record exactly what it did. This gets included in the result:

```
auto_fixed: [
  {
    check: "docker_installed",
    action: "Installed Docker CE 25.0.3 via apt",
    timestamp: "2024-01-15T10:23:45Z"
  },
  {
    check: "docker_daemon_running",
    action: "Started Docker daemon via systemctl",
    timestamp: "2024-01-15T10:24:12Z"
  }
]
```

This gets sent to the control plane and shown in the dashboard. The user should see: "We automatically installed Docker on your server."

### The --json Flag

When called by the install script with `--json`, output the entire result as a single JSON object to stdout. No pretty printing, no color codes, no progress lines. Just the JSON.

The install script parses this JSON and renders it in its own format.

When called without `--json` (future: direct user invocation), render human-readable output with colors, icons, and grouped sections.

### Done Condition for Step 6
```
□ Runner executes groups in correct order
□ Short-circuits correctly on blocking failures
□ Continues through non-blocking failures to collect all warnings
□ Auto-fix results are recorded in the output
□ --json flag produces valid parseable JSON
□ Human output is readable with clear pass/fail/warn per check
□ Total execution time is under 60 seconds in the worst case
   (Docker auto-install can take 30-40 seconds — that is acceptable)
```

---

## Step 7 — Reporting to Control Plane

### When the Agent Sends Pre-flight Results

```
Two moments:

1. During registration (Step 5 of Layer 1)
   Pre-flight results go with the registration payload
   Control plane stores system_info in the server record
   Any warnings are stored and shown in dashboard

2. On every subsequent startup
   Agent runs pre-flight again
   Sends results to control plane via WebSocket
   Dashboard updates to reflect current server health
   Any new problems trigger an alert
```

### What the Control Plane Does with Results

```
Stores:
  - system_info → updates the server record in the database
  - warnings → stored as server health events
  - auto_fixed items → stored as server events with type "auto_fixed"

Triggers:
  - If a check that previously passed now fails → create an alert
  - If Docker was auto-installed → create a notification for the user
  - If disk space dropped into warning territory → create a warning alert
```

### Done Condition for Step 7
```
□ Pre-flight results are included in the registration request
□ Server record in database has correct OS, arch, RAM, disk after registration
□ Warnings appear in the server detail view in the dashboard
□ Auto-fix events appear in server history
□ Running pre-flight on subsequent startup updates the server record
```

---

## Layer 2 Overall Done Condition

```
The full test matrix:

Scenario 1 — Perfect server (Ubuntu 22.04, 2GB RAM, 40GB disk, nothing on 80/443)
  □ All checks pass
  □ Docker is auto-installed if missing
  □ Registration completes
  □ Dashboard shows server info correctly

Scenario 2 — Port 80 in use by Apache with no active sites
  □ B3 detects Apache on port 80
  □ Auto-fix stops Apache
  □ Marked as fixed in output
  □ Install continues

Scenario 3 — Port 80 in use by Apache with active virtual hosts
  □ B3 detects Apache with active sites
  □ No auto-fix attempted
  □ Clear message names Apache, explains the conflict, gives options
  □ Install stops

Scenario 4 — Low disk (1.5GB available)
  □ A3 detects low disk
  □ Blocking failure
  □ Message explains minimum and how to check what is using space

Scenario 5 — No internet
  □ B1 fails
  □ Short circuit stops remaining checks
  □ Clear message with diagnostic commands

Scenario 6 — Docker installed, daemon stopped
  □ C1 passes
  □ C2 detects stopped daemon
  □ Auto-fix starts daemon
  □ Continue with C3+

Scenario 7 — CentOS 7 (unsupported)
  □ A1 fails immediately
  □ Lists supported versions
  □ No further checks run

When all 7 scenarios behave correctly, Layer 2 is done.
Move to Layer 3A — Docker Management.
```

---

## What Layer 2 Does NOT Do

```
Layer 2 does not:
├── Deploy anything
├── Configure Caddy
├── Set up networking for apps
├── Monitor ongoing health (that is Layer 4C)
├── Check application-level health
└── Run continuously — it runs once at startup then hands off to Layer 4C
    for ongoing monitoring
```

---

**Ready for Layer 3A — Docker Management?**