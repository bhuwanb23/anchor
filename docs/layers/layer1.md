# Layer 1 Implementation — Step by Step

---

## Before You Touch Layer 1 — Confirm Base Is Ready

```
Checklist before starting Layer 1:

□ make dev-backend starts without errors
□ curl http://localhost:8080/health returns ok
□ make dev-frontend starts without errors
□ SQLite database file gets created on first run
□ All three folders exist: agent/ control-plane/ dashboard/
```

If all five pass, Layer 1 starts here.

---

## Layer 1 Has 5 Implementation Steps

```
Step 1 → Token Generation (control plane)
Step 2 → Install Script (shell script)
Step 3 → Binary Build Pipeline (agent compiles and is downloadable)
Step 4 → Systemd Service Setup (agent installs itself as a service)
Step 5 → Registration Handshake (agent calls home, server appears in dashboard)

Each step has a clear done condition.
You do not move to the next step until the current one passes its done condition.
```

---

## Step 1 — Token Generation

### What You Are Building

A user clicks "Add Server" in the dashboard. The backend generates a short-lived token. That token gets embedded in an install command that the user copies and pastes into their server.

### Where This Lives

```
control-plane/internal/db/migrations/     → add token table
control-plane/internal/db/queries/        → token database operations
control-plane/internal/api/handlers/      → the HTTP endpoint
control-plane/internal/api/router.go      → register the route
```

### What to Build — In Order

**1. Database migration for tokens**

Add a new migration file for the registration tokens table. The table needs to store:
- A unique ID for each token record
- The token hash (you store the hash, never the raw token)
- Which user the token belongs to
- An optional server name the user gave it
- When it was created
- When it expires (1 hour after creation)
- When it was used (null until consumed)
- What IP address consumed it

This is the exact schema from the Layer 1 plan. Write it as a new `.sql` file in your migrations folder.

**2. Token database operations**

In your queries layer, write the functions your handler will call:
- Create a new token record (takes user ID, optional name, returns nothing — the handler generates the raw token before calling this)
- Find a token by its hash (used during agent registration in Step 5)
- Mark a token as used (takes token ID and the IP that used it)
- Delete expired tokens (a cleanup utility, run occasionally)

**3. Token generation logic**

This lives in a utility or auth package, not the handler itself. It does two things:
- Generate a cryptographically random raw token with the `reg_` prefix
- Hash that raw token with SHA-256 so you can store the hash

The raw token goes back to the user. The hash goes into the database. You never store the raw token anywhere after returning it to the user.

**4. The HTTP handler**

This is a protected endpoint — the user must be logged in to call it. The handler:
- Reads the optional server name from the request body
- Calls the token generation utility to get a raw token and its hash
- Writes the hash to the database with an expiry 1 hour from now
- Constructs the full install command string with the raw token embedded
- Returns the raw token, the full install command, and the expiry time
- Never logs or stores the raw token — it leaves this function and that is the last your system sees of it

**5. Register the route**

Add the route to your router. It sits under `/api/v1/servers/registration-token`. It requires auth middleware (you may be adding a placeholder for now since auth is Layer 5 — that is fine, protect it properly when auth is built).

### Done Condition for Step 1

```
□ POST /api/v1/servers/registration-token returns a token
□ Token starts with reg_
□ Token is different every time you call the endpoint
□ Database shows the hashed version, not the raw token
□ Calling the endpoint again after 1 hour would produce an expired token
  (you can test this by manually setting expires_at to the past in SQLite)
□ The install command string in the response contains the raw token
```

---

## Step 2 — Install Script

### What You Are Building

A shell script hosted at `get.yourplatform.com` (locally during dev: just a file you can run directly). The user pastes one command and the script handles everything.

### Where This Lives

```
scripts/install.sh          → the actual script
scripts/README.md           → notes on how to test it
```

During MVP development, you do not need a CDN. You serve this file locally or from any static file server. The control plane can even serve it directly at `/install.sh` during development.

### What to Build — In Order

**1. Script skeleton and argument parsing**

The script receives one required argument: `--token=TOKEN`. Before doing anything else, it:
- Checks it received a token
- Validates the token starts with `reg_` (basic format check)
- Exits with a clear message if either check fails, including the URL where the user gets a new token

**2. Root check**

The script needs root to install a systemd service and write to `/usr/local/bin`. Check `id -u` equals 0. If not, print the exact command they should run with sudo and exit.

**3. OS and architecture detection**

Read `/etc/os-release` to get the distribution and version. Read `uname -m` to get the architecture. Map the architecture to your binary naming scheme (`x86_64` becomes `amd64`, `aarch64` becomes `arm64`). If the OS or architecture is unsupported, exit with a message that names what was detected and what is supported.

**4. Dependency check**

The script needs either `curl` or `wget` to download the binary. It needs `systemctl` to create the service. It needs `sha256sum` or `shasum` to verify the download. Check each one and exit with installation instructions if missing.

**5. Binary download**

Construct the download URL from the base URL, version, OS, and architecture. Download the binary to a temp directory. Download the corresponding `.sha256` checksum file from the same location.

**6. Checksum verification**

Before installing anything, compute the SHA-256 of the downloaded binary and compare it to the downloaded checksum file. If they do not match, delete the temp files and exit loudly — never install an unverified binary. The error message should explain what the mismatch means and suggest either retrying or contacting support.

**7. Binary installation**

Make the binary executable. Move it from the temp directory to `/usr/local/bin/yourplatform-agent`. Create the config directory at `/etc/yourplatform/`. Create the data directory at `/var/lib/yourplatform/`. Write a config file at `/etc/yourplatform/config.yaml` containing the control plane URL, the registration token, and the agent version.

**8. Pre-flight integration point**

At this point, run the agent binary itself with a `preflight` subcommand. The agent binary checks the environment (Layer 2) and returns either success or a list of problems. If it returns problems, display them in plain English and exit. The install script does not implement Layer 2 logic itself — it delegates to the binary.

**9. Systemd service creation**

Write a systemd unit file to `/etc/systemd/system/yourplatform-agent.service`. Run `systemctl daemon-reload`, `systemctl enable yourplatform-agent`, and `systemctl start yourplatform-agent`.

**10. Wait for connection**

After starting the service, poll for a file that the agent writes to disk when it successfully connects to the control plane (`/var/lib/yourplatform/agent.connected`). Check every 2 seconds for up to 60 seconds. If the file appears, the install succeeded. If 60 seconds pass without the file appearing, check whether the service is still running. If the service crashed, show the last 20 lines of its journal log and exit with a troubleshooting message. If the service is running but not connected, suggest checking firewall rules and DNS.

**11. Success message**

Print a clear, friendly success message. Tell the user their server is connected, direct them back to the browser, and show them the two commands they would ever need to check on the agent manually (`systemctl status` and `journalctl`).

### Testing the Script During Development

```
You do not need a real server to test this.
You can test it in a Docker container:

- Run a plain Ubuntu container locally
- Mount the install script into it
- Run the script with a test token
- Watch what happens at each step

This lets you iterate on the script without renting servers.
```

### Done Condition for Step 2

```
□ Script runs on a fresh Ubuntu 22.04 machine without errors
□ Script fails clearly when --token is missing
□ Script fails clearly when running without sudo
□ Script fails clearly when the OS is unsupported
□ Script fails loudly if checksum does not match
□ Script creates the binary at /usr/local/bin/yourplatform-agent
□ Script creates /etc/yourplatform/config.yaml with the token inside
□ Script creates the systemd service and it shows as active
□ Script waits for connection and either succeeds or explains why it failed
```

---

## Step 3 — Binary Build Pipeline

### What You Are Building

The agent needs to be compiled and available for download before the install script can fetch it. During MVP, this is a manual process. You build the binary, put it somewhere downloadable, and point the install script at that location.

### Where This Lives

```
Makefile                    → build commands
scripts/release.sh          → optional helper script for cutting a release
bin/                        → compiled binaries (gitignored)
```

### What to Build — In Order

**1. Build targets in the Makefile**

Add build commands that cross-compile the agent for each supported target:
- Linux amd64
- Linux arm64

Go cross-compilation is built in. You set `GOOS=linux` and `GOARCH=amd64` (or `arm64`) as environment variables before running `go build`. The output is a static binary that runs on that target.

**2. Checksum generation**

After building each binary, generate a `.sha256` file for it. The install script downloads both the binary and this file to verify integrity.

**3. Where to host binaries during MVP development**

You have several zero-cost options during MVP:
- GitHub Releases — tag a release on GitHub and upload binary files as release assets. They get a stable public URL. This is the recommended approach for MVP.
- Your control plane server — add a static file route that serves files from a directory. Simple but means your API server also serves binaries.
- Any S3-compatible storage — Cloudflare R2 has a free tier with zero egress fees. Worth setting up even for MVP if GitHub Releases feels limiting.

For MVP, GitHub Releases is the simplest. The URLs look like `github.com/yourname/yourplatform/releases/download/v0.1.0/yourplatform-agent-linux-amd64` and they are stable and fast.

**4. Version coordination**

The install script hardcodes an agent version. The binary at that URL must match that version. The checksum file must match that binary. When you cut a new release, you update all three together. During MVP, this is a manual process.

**5. The agent must handle the version subcommand**

The install script might call `yourplatform-agent --version` to confirm what is installed. Make sure the agent binary responds to this with the version string.

### Done Condition for Step 3

```
□ make build-agent produces a binary for linux/amd64
□ make build-agent produces a binary for linux/arm64
□ Checksums are generated alongside each binary
□ Binaries are uploaded somewhere publicly downloadable
□ The install script's download URL points to that location
□ Running the binary with --version returns the version string
□ The downloaded binary checksum matches what the install script verifies
```

---

## Step 4 — Systemd Service Setup

### What You Are Building

The agent needs to survive reboots, restart on crashes, and run in the background. Systemd handles all of this. The install script writes the service file, but the agent itself needs to behave correctly when started by systemd.

### What to Understand About Systemd and the Agent

The agent will be started by the install script via `systemctl start`. It will be configured to restart automatically if it crashes. It will start on every reboot. It must not crash during normal operation — it should handle errors internally and keep running.

The agent runs as a long-lived process. Its job when started by systemd:
- Load the config file from `/etc/yourplatform/config.yaml`
- Run pre-flight checks (Layer 2)
- Connect to the control plane via WebSocket
- Write the `agent.connected` file when connected
- Enter a loop: process commands, report health, reconnect if dropped
- Never exit except on a fatal unrecoverable error

### What to Build — In Order

**1. The systemd unit file content**

The install script writes this file, but you need to design what it contains. Key decisions:
- `After=network-online.target docker.service` — do not start until network and Docker are ready
- `Requires=docker.service` — if Docker dies, the agent restarts too (it cannot function without Docker)
- `Restart=always` — always restart on any exit
- `RestartSec=10` — wait 10 seconds between restart attempts
- `StartLimitInterval=200` and `StartLimitBurst=5` — if it crashes 5 times in 200 seconds, give up and alert the system administrator (the user in this case)
- `MemoryMax=256M` — the agent must never starve user apps of RAM
- `CPUQuota=20%` — same for CPU

**2. The agent startup sequence**

When systemd starts the agent, it runs `yourplatform-agent run --config /etc/yourplatform/config.yaml`. The agent's `run` subcommand:
- Parses the config file
- Sets up logging (to stdout so systemd captures it via journald)
- Runs pre-flight checks — if they fail, log the failures and exit (systemd will restart it, but if pre-flight keeps failing, the burst limit kicks in and stops the restart loop)
- Attempts to connect to the control plane
- On successful connection, writes `/var/lib/yourplatform/agent.connected`
- Enters the main loop

**3. Graceful shutdown**

When systemd stops the agent (during updates or intentional stops), it sends SIGTERM first, then SIGKILL after a timeout. The agent should catch SIGTERM and:
- Stop accepting new commands
- Finish any in-progress operation (do not kill a running deploy)
- Close the WebSocket connection cleanly
- Remove the `agent.connected` file
- Exit with code 0

**4. Journal logging**

Systemd captures everything the agent writes to stdout and stderr into the journal. Users can view this with `journalctl -u yourplatform-agent -f`. Make sure the agent writes useful log lines — not too noisy but enough to diagnose problems. Every significant action should have a log line.

### Done Condition for Step 4

```
□ systemctl status yourplatform-agent shows active (running)
□ Rebooting the test server brings the agent back up automatically
□ Killing the agent process with kill -9 causes systemd to restart it
□ journalctl -u yourplatform-agent shows readable log output
□ systemctl stop yourplatform-agent shuts down cleanly
□ Agent respects MemoryMax and CPUQuota limits
```

---

## Step 5 — Registration Handshake

### What You Are Building

The first thing the agent does after starting is register itself with the control plane. It uses the token from the config file to prove it belongs to a specific user account. The control plane validates the token, creates a server record, issues permanent credentials, and the dashboard updates to show the server as connected.

### Where This Lives

```
agent/internal/ws/client.go           → WebSocket connection + registration
control-plane/internal/api/handlers/agent.go  → registration endpoint
control-plane/internal/db/queries/servers.go  → create server record
```

### What to Build — In Order

**1. The registration endpoint on the control plane**

This endpoint lives at `POST /api/v1/agent/register`. It does not require a user JWT — the token itself is the authentication. The endpoint:
- Reads the token from the request body
- Hashes it with SHA-256
- Looks up the hash in the registration tokens table
- Validates: exists, not used, not expired
- Reads the server info from the request body (OS, arch, RAM, disk, IP)
- Creates a new server record in the servers table linked to the token's user
- Generates a permanent agent ID and agent secret (the secret is hashed before storing)
- Marks the registration token as used with the current timestamp and requesting IP
- Returns the agent ID, raw agent secret, WebSocket URL, and server ID to the agent
- Never issues credentials again for the same token — single use

**2. The agent registration logic**

When the agent starts for the first time (registration token exists in config, no agent ID yet), it:
- Collects server information: reads `/etc/os-release`, runs `uname -m`, reads `/proc/meminfo`, reads `df` output, counts CPU cores
- Makes an HTTP POST to the registration endpoint with the token and server info
- If the request fails: log the error and exit (systemd will retry — if the token is expired, the restart burst limit will kick in and stop retrying, which is the correct behavior)
- If the request succeeds: save the returned agent ID and agent secret to the config file, delete the registration token from the config file (it has been used), write the `agent.connected` file

**3. Credential storage after registration**

After registration, the config file changes:
- `registration_token` field is removed
- `agent_id` field is added
- `agent_secret` field is added (the raw secret — this is the agent's permanent password)

The config file must have permissions set to `600` (readable only by root) because it now contains a permanent credential.

**4. Subsequent startups (already registered)**

On every start after the first, the agent sees no registration token in config — instead it sees an agent ID and secret. It skips the registration endpoint and goes directly to opening the WebSocket connection, authenticating with the agent ID and secret.

**5. WebSocket connection with credentials**

When the agent opens its WebSocket connection to the control plane, it sends its agent ID and secret as part of the connection handshake. The control plane validates these credentials, looks up the server record, marks the server as connected in the database, and confirms the connection. The dashboard then shows the server status as connected.

**6. Dashboard reaction**

When the server status changes to connected in the database, the dashboard needs to show this. During MVP, the simplest approach is polling — the dashboard checks the server status every few seconds. Later this becomes a real-time WebSocket push. For Layer 1, polling is fine.

**7. The agent.connected file**

This file at `/var/lib/yourplatform/agent.connected` is what the install script polls for. The agent writes this file after:
- First-time: after the registration request succeeds AND the WebSocket connects
- Subsequent starts: after the WebSocket connects and the control plane confirms the server

### The Full Registration Flow in Order

```
Agent starts (first time)
        │
        ▼
Reads config — sees registration_token, no agent_id
        │
        ▼
Collects server info (OS, RAM, disk, arch, IP)
        │
        ▼
POST /api/v1/agent/register
  { token, server_info }
        │
        ▼
Control plane validates token
        │
   ┌────┴────┐
invalid    valid
   │          │
   ▼          ▼
exit 1    Create server record
(systemd  Generate agent_id + agent_secret
retries)  Mark token as used
          Return credentials
               │
               ▼
Agent saves agent_id + agent_secret to config
Agent removes registration_token from config
               │
               ▼
Agent opens WebSocket to control plane
Authenticates with agent_id + agent_secret
               │
               ▼
Control plane marks server status = connected
               │
               ▼
Agent writes /var/lib/yourplatform/agent.connected
               │
               ▼
Install script detects the file
Prints success message
               │
               ▼
Dashboard shows server as Connected ✓
```

### Done Condition for Step 5

```
□ Running install script on a test server creates a server record in the DB
□ The server record contains the correct OS, arch, RAM, disk info
□ The registration token is marked as used in the DB after registration
□ Running the install script with the same token a second time fails clearly
□ Running the install script with an expired token fails clearly
□ The agent.connected file appears within 10 seconds of registration
□ The install script detects the file and prints the success message
□ The dashboard (or direct DB query for now) shows the server as connected
□ Rebooting the server reconnects the agent without needing a new token
□ The config file after registration has agent_id, no registration_token
```

---

## Layer 1 Overall Done Condition

```
The full test:

1. Open your dashboard (localhost:3000)
2. Click "Add Server"
3. See an install command with a token
4. Copy that command
5. Paste it into a fresh Ubuntu 22.04 machine (or Docker container)
6. Watch it run without errors
7. See the server appear as Connected in the dashboard
8. Reboot that machine
9. See the server reconnect automatically within 30 seconds

When all 9 steps work end to end, Layer 1 is done.
Move to Layer 2.
```

---

## What Layer 1 Does NOT Do

```
Layer 1 does not:
├── Deploy any apps (Layer 3A, 4B)
├── Set up Caddy or HTTPS (Layer 3B)
├── Check for port conflicts or firewall issues (Layer 2)
├── Stream logs (Layer 4C)
├── Handle auth for the dashboard (Layer 5A)
└── Show anything useful in the dashboard beyond "Connected" (Layer 7)

All of those come later.
Layer 1 does exactly one thing: server goes in, appears as connected, stays connected.
```

---

**Ready for Layer 2 — Server Environment Detection?**