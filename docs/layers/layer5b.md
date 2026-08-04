# Layer 5B — WebSocket Hub: Complete Plan

---

## What Layer 5B Actually Is

```
Layer 5B is the central nervous system of the control plane.

Every real-time interaction flows through it:

  User clicks "Deploy" in browser
    → Browser WebSocket → Hub → Agent WebSocket → Agent executes

  Agent collects CPU metrics
    → Agent WebSocket → Hub → Browser WebSocket → Dashboard updates

  App crashes on server
    → Agent WebSocket → Hub → Browser WebSocket → Alert appears

Without Layer 5B:
  → Dashboard cannot show live data
  → Commands take seconds to reach the agent (HTTP polling)
  → Log streaming is impossible
  → The product feels dead and slow

With Layer 5B:
  → Everything is instant
  → Dashboard feels alive
  → The product feels like a real cloud platform
```

---

## The Mental Model

```
                        ┌─────────────────────────────────┐
                        │         WebSocket Hub            │
                        │                                  │
Browser A               │  Agent Registry                  │
(watching srv-1)  ──WS──►  srv-1 ──► conn_agent_1         │
                        │  srv-2 ──► conn_agent_2         │
Browser B         ──WS──►  srv-3 ──► conn_agent_3         │
(watching srv-1)        │                                  │
                        │  Browser Registry                │
Browser C         ──WS──►  usr-abc, watching srv-1         │
(watching srv-2)        │    ──► [conn_browser_A,          │
                        │         conn_browser_B]          │
                        │  usr-def, watching srv-2         │
                        │    ──► [conn_browser_C]          │
                        │                                  │
                        │  Routing Table                   │
                        │  cmd from browser A              │
                        │    → find srv-1 agent            │
                        │    → forward command             │
                        │                                  │
                        │  health report from agent srv-1  │
                        │    → find browsers watching srv-1│
                        │    → forward to A and B          │
                        └─────────────────────────────────┘
                                      │
                               Agent srv-1 ──WS──► Agent
                               Agent srv-2 ──WS──► Agent
                               Agent srv-3 ──WS──► Agent
```

---

## Step 1 — Hub Architecture

### The Hub as a Single Goroutine

```
The Hub is implemented as a single central goroutine.

Why a single goroutine manages all state:
  → No mutex locks needed for the registry maps
  → No race conditions on connection add/remove
  → All state changes are serialized naturally
  → Go's goroutine model is designed for exactly this pattern

How it works:
  → All operations sent to the hub via channels
  → Hub goroutine reads from channels and processes sequentially
  → Each operation: register connection, unregister, route message
  → Connections themselves run in their own goroutines
    (reading from their WebSocket, writing to hub channel)

The hub owns:
  → agent_connections: map of server_id → AgentConnection
  → browser_connections: map of connection_id → BrowserConnection
  → subscriptions: map of server_id → set of browser connection_ids
    (which browsers are watching which server)
  → pending_commands: map of command_id → BrowserConnection
    (which browser is waiting for which command result)

Channel types into the hub:
  → register_agent
  → unregister_agent
  → register_browser
  → unregister_browser
  → subscribe (browser starts watching a server)
  → unsubscribe (browser stops watching)
  → agent_message (message from an agent, needs routing)
  → browser_message (message from a browser, needs routing)
  → send_to_agent (control plane wants to send to an agent)
  → broadcast_to_server (send to all browsers watching a server)
```

### The Hub Struct

```
Hub contains:

  Agent side:
    → agents: map[serverID]AgentConnection
    → Each AgentConnection holds:
        server_id
        user_id (owner of this server)
        websocket connection reference
        send channel (buffered — write to this to send to agent)
        connected_at timestamp
        last_ping_at timestamp
        agent_version string

  Browser side:
    → browsers: map[connectionID]BrowserConnection
    → Each BrowserConnection holds:
        connection_id (random UUID per connection)
        user_id
        websocket connection reference
        send channel (buffered)
        watching_server_id (which server they have open)
        active_streams: set of stream_ids they started

  Subscriptions:
    → subscriptions: map[serverID]set[connectionID]
    → "which browser connections are watching this server"
    → Updated when browser navigates to a server page

  Command tracking:
    → pending_commands: map[commandID]connectionID
    → "which browser connection is waiting for this command result"
    → When result arrives from agent: find the waiting browser, send result
    → Entries cleaned up after result delivered or timeout (5 minutes)
```

### Done Condition for Step 1
```
□ Hub goroutine starts on control plane startup
□ Hub has separate maps for agents and browsers
□ Hub has subscription map for server watching
□ Hub has pending command map for result routing
□ All hub operations go through channels (no direct map access from outside)
□ Hub goroutine is the only writer to all maps
□ Hub startup is logged with confirmation message
```

---

## Step 2 — Agent Connection Management

### Step 2A — Agent Connects

```
Flow when an agent establishes a WebSocket connection:

1. HTTP request arrives at /ws/agent
2. Layer 5A validates agent credentials (agent_id + secret)
   (This happens in the HTTP handler before upgrade)
3. If auth passes: upgrade to WebSocket
4. Create AgentConnection struct
5. Send register_agent event to hub channel:
   {
     type: "register_agent",
     server_id: "srv-abc123",
     connection: the_websocket_connection
   }
6. Hub goroutine receives event:
   → Check: is there already an agent for this server_id?
     → If yes: close the old connection (duplicate connection)
       This handles: agent restarted before old connection timed out
     → Register the new connection in agents map
   → Update servers table: status = connected, last_seen = now
   → Check subscriptions: any browsers watching this server?
     → If yes: send "agent_connected" event to those browsers
     → Dashboard updates: server status shows as Connected
7. Start two goroutines for this agent connection:
   → Reader goroutine: reads messages from agent WebSocket
   → Writer goroutine: reads from send channel, writes to WebSocket
```

### Step 2B — Agent Reader Goroutine

```
One goroutine per agent connection.
Runs until connection closes.

Loop:
  1. Read next message from WebSocket
       → If error: connection closed or broken
         → Send unregister_agent to hub
         → Exit goroutine

  2. Parse message type from JSON

  3. Route based on type:

     type: "hello"
       → Send to hub as agent_message
       → Hub processes hello: updates server state in DB
       → Hub sends hello_ack back to agent
         (with pending commands)

     type: "pong"
       → Update last_ping_at on connection
       → No routing needed (just heartbeat response)

     type: "health_report"
       → Send to hub as agent_message
       → Hub:
         → Updates server metrics in DB
         → Forwards to all browsers watching this server
         → Sends to anomaly processor (runs inline or via channel)

     type: "command_ack"
       → Agent confirmed it received a command
       → Update command status in DB: in_progress
       → Find waiting browser (from pending_commands map)
       → Send progress update to browser: "command received by agent"

     type: "command_progress"
       → Agent is reporting progress during a long command
       → Find waiting browser in pending_commands
       → Forward progress to browser (deploy progress, backup progress, etc.)

     type: "command_result"
       → Command completed (success or failure)
       → Find waiting browser in pending_commands
       → Forward result to browser
       → Remove from pending_commands map
       → Update command record in DB with result

     type: "log_line" or "log_lines"
       → Container log output
       → Contains stream_id
       → Find browsers subscribed to this stream_id
       → Forward to those browsers

     type: "alert"
       → Agent detected a problem
       → Store alert in DB
       → Forward to all browsers watching this server
       → Trigger email/WhatsApp delivery via notification service

     type: "state_update"
       → Something changed on the server
       → Forward to all browsers watching this server

     type: "backup_result"
       → Backup completed
       → Store in DB
       → Forward to watching browsers

  4. Repeat
```

### Step 2C — Agent Writer Goroutine

```
One goroutine per agent connection.
Reads from the agent's send channel.
Writes to the WebSocket.

Loop:
  1. Read from send channel (blocks until message available)
       → If channel closed: exit goroutine

  2. Write message to WebSocket
       → If write fails: connection is broken
         → Signal reader goroutine to stop
         → Exit

  3. Repeat

Why a buffered send channel:
  → Hub can put messages in without blocking
  → Writer processes them as fast as it can
  → Buffer size: 256 messages
  → If buffer full: agent is too slow to receive
    → Drop oldest message
    → Log warning: "Agent send buffer full for srv-abc"
    → This should not happen under normal operation
```

### Step 2D — Agent Disconnects

```
When an agent connection closes (either end):

1. Reader goroutine detects error or clean close
2. Sends unregister_agent to hub:
   { type: "unregister_agent", server_id: "srv-abc123" }
3. Hub goroutine receives:
   → Remove from agents map
   → Update servers table: status = disconnected, last_seen = now
   → Any browsers watching this server?
     → Send "agent_disconnected" event to those browsers
     → Dashboard updates: server shows as Disconnected
   → Any pending commands for this server?
     → Mark them as failed: "Server disconnected before command completed"
     → Send failure to waiting browsers

Agent reconnection (handled automatically):
  → Agent reconnects (Step 2A)
  → Hub processes hello message
  → Pending commands delivered in hello_ack
  → Browsers watching: receive "agent_connected" again
  → Dashboard updates back to Connected
```

### Done Condition for Step 2
```
□ Agent connection registered in hub on WebSocket upgrade
□ Duplicate agent connection: old one closed, new one registered
□ Server status updated to connected in database
□ Browsers watching server notified of agent connection
□ All message types from agent are routed correctly
□ Reader goroutine exits cleanly on connection close
□ Writer goroutine uses buffered send channel
□ Agent disconnect updates server status in database
□ Browsers watching server notified of agent disconnect
□ Pending commands marked failed on agent disconnect
```

---

## Step 3 — Browser Connection Management

### Step 3A — Browser Connects

```
Flow when a browser (dashboard) opens a WebSocket connection:

1. HTTP request arrives at /ws/browser
2. Layer 5A validates the JWT in the Authorization header
   (or from ?token= query parameter for WebSocket)
3. If auth passes: upgrade to WebSocket
4. Create BrowserConnection struct
5. Send register_browser to hub:
   {
     type: "register_browser",
     connection_id: "conn-abc123",  ← new UUID for this connection
     user_id: "usr-xyz789",
     connection: the_websocket_connection
   }
6. Hub goroutine receives:
   → Add to browsers map
   → No server subscription yet (browser connected but not viewing any server)
7. Start reader and writer goroutines for this browser connection
8. Send welcome message to browser:
   {
     type: "connected",
     connection_id: "conn-abc123"
   }
```

### Step 3B — Browser Subscribes to a Server

```
When user navigates to a server's page in the dashboard:

Dashboard sends:
{
  "type": "subscribe",
  "server_id": "srv-abc123"
}

Hub processes:
1. Validate: does this user have access to this server?
   → Check team membership (Layer 5A permission model)
   → If no access: send error, do not subscribe

2. If user was watching another server: unsubscribe from that server

3. Add browser to subscriptions[server_id]:
   subscriptions["srv-abc123"] = set of connection IDs watching it

4. Send current server state to the browser:
   {
     type: "server_state",
     server: {
       id: "srv-abc123",
       status: "connected",        ← current connection status
       containers: [...],          ← current container statuses
       metrics: { cpu, ram, disk },← latest metrics
       alerts: [...],              ← active alerts
       routes: [...]               ← active Caddy routes
     }
   }
   This gives the browser an immediate snapshot.
   After this: real-time updates flow as things change.

5. Track: browser_connection.watching_server_id = "srv-abc123"
```

### Step 3C — Browser Unsubscribes

```
Three ways this happens:

Way 1: Browser navigates away
  → Dashboard sends: { "type": "unsubscribe", "server_id": "srv-abc123" }
  → Hub removes connection from subscriptions map

Way 2: Browser closes tab or navigates to different site
  → WebSocket connection closes
  → Reader goroutine detects close
  → Hub unregisters connection
  → Hub removes from subscriptions map
  → Hub cancels any active log streams this connection started

Way 3: Server becomes the active server for another subscription
  → User was watching srv-1, navigates to srv-2
  → Subscribe to srv-2 automatically unsubscribes from srv-1
```

### Step 3D — Browser Message Handling

```
Messages arriving from the browser:

type: "subscribe"
  → Subscribe to server updates (Step 3B)

type: "unsubscribe"
  → Unsubscribe from server updates (Step 3C)

type: "command"
  → User wants to do something (deploy, restart, etc.)
  → Handle in Step 4 (command routing)

type: "ping"
  → Browser sends heartbeat
  → Hub responds: { "type": "pong" }

type: "start_log_stream"
  → Browser wants to see logs
  → Hub validates browser has access to the server
  → Hub forwards start_log_stream command to agent
  → Registers: stream_id → connection_id
    (so log lines can be routed back to this browser)

type: "stop_log_stream"
  → Browser done viewing logs
  → Hub forwards stop_log_stream command to agent
  → Removes stream_id from routing table
```

### Done Condition for Step 3
```
□ Browser connection registered in hub on WebSocket upgrade
□ JWT validated before upgrade
□ Subscribe sends immediate current server state snapshot
□ Subscribe validates user has access to the server
□ Unsubscribe removes from subscriptions map
□ Tab close unregisters connection and cleans up subscriptions
□ Log stream routing table updated on start/stop
□ Browser ping receives pong response
□ Navigating to different server auto-unsubscribes from previous
```

---

## Step 4 — Command Routing

### The Full Command Journey

```
User clicks "Deploy" in dashboard

Step 1: Browser sends command to hub
{
  "type": "command",
  "command_id": "cmd-abc123",
  "server_id": "srv-xyz789",
  "command_type": "deploy",
  "payload": { "project": "myshop", "image": "nginx:latest" }
}

Step 2: Hub validates
  → Is this browser subscribed to this server?
  → Does user have permission for this command type?
  → Is this server currently connected (agent online)?

Step 3: Hub stores pending command
  → pending_commands["cmd-abc123"] = browser connection_id
  → This maps the command to the browser waiting for results

Step 4: Hub stores command in database
  → INSERT INTO commands (id, server_id, type, payload, status, issued_by)
  → Status: queued

Step 5: Hub forwards to agent
  → Find agent connection for srv-xyz789
  → Put command on agent's send channel
  → Agent writer goroutine sends it

Step 6: Hub sends immediate response to browser
  → "cmd-abc123 received and sent to your server"
  → Browser shows: spinner, "Sending to server..."

Step 7: Agent receives command, sends command_ack
  → Hub receives ack
  → Update command status: in_progress
  → Find browser in pending_commands
  → Forward ack to browser
  → Browser shows: "Server received command, executing..."

Step 8: Agent sends command_progress (multiple times)
  → Hub receives each progress update
  → Forward to browser
  → Browser updates progress display

Step 9: Agent sends command_result
  → Hub receives result
  → Update command status in DB: success or failed
  → Find browser in pending_commands
  → Forward result to browser
  → Remove from pending_commands
  → Browser shows: success or error message
```

### Step 4A — Agent Offline Handling

```
What if the agent is not connected when command arrives?

Hub checks: is srv-xyz789 in agents map?
  → If no (agent offline):
    → Do NOT put in pending_commands
    → Store command in database: status = queued
    → Send to browser:
      {
        "type": "command_queued",
        "command_id": "cmd-abc123",
        "message": "Your server is currently offline. This command
                    will execute automatically when it reconnects."
      }
    → Browser shows: "Server offline — command queued"

When agent reconnects:
  → Hub processes hello from agent
  → Fetches queued commands from DB for this server
  → Includes in hello_ack to agent
  → Agent processes them
  → Results come back through normal flow
  → But: which browser is waiting?
    → The browser that issued the command may have closed
    → pending_commands map does not have this command_id
    → Hub checks DB for who issued the command
    → If that user has an active browser connection: send result there
    → If no active connection: result stored in DB only
      → User sees it in command history when they next open the dashboard
```

### Step 4B — Command Timeout

```
What if agent receives command but never responds?

Hub starts a timer when command is sent to agent:
  → Timeout: 10 minutes (most operations finish well under this)
  → Exception: restore command gets 30 minutes

If timer fires before command_result arrives:
  → Hub sends failure to waiting browser:
    {
      "type": "command_result",
      "command_id": "cmd-abc123",
      "status": "timeout",
      "error": "Command did not complete within 10 minutes.
                Your server may be experiencing issues.
                Check the server status and try again."
    }
  → Remove from pending_commands
  → Update command in DB: status = timeout
  → Agent may still be executing (unaware of timeout)
    → When agent eventually sends result: hub ignores it
      (command_id no longer in pending_commands)
    → Update DB with late result if it arrives (for audit purposes)
```

### Step 4C — Command Deduplication at Hub Level

```
Prevent double-submission from the browser.

Scenario:
  → User clicks "Deploy"
  → Browser sends command (cmd-abc123)
  → Network is slow, user thinks it did not work
  → User clicks "Deploy" again
  → Browser sends same type of command (cmd-def456)
  → Both arrive at hub

Hub handling:
  → Check: is there already a command of type "deploy" for this
    project currently in-progress (in pending_commands)?
  → If yes:
    → Reject the new command
    → Respond to browser:
      "A deploy for myshop is already in progress.
       Wait for it to complete before starting another."
  → If no: proceed normally

This prevents:
  → Two simultaneous deploys for the same project
  → Race conditions in container management
  → Confusing double-deploy behavior
```

### Done Condition for Step 4
```
□ Command arrives from browser and is routed to correct agent
□ Command stored in database with queued status
□ Agent offline: command queued in DB, browser informed
□ command_ack from agent forwarded to correct browser
□ command_progress messages forwarded to correct browser
□ command_result forwarded to correct browser and DB updated
□ Command timeout fires after 10 minutes
□ Timeout message sent to browser, command marked in DB
□ Duplicate in-progress commands rejected with clear message
□ Queued command delivered on agent reconnect
□ Result delivered to browser even if it reconnects after command completes
```

---

## Step 5 — Broadcast Routing

### What Broadcast Routing Is

```
Some messages from agents go to ALL browsers watching that server.
Not just the browser that issued a command.

These are events, not command results:
  → health_report: every browser watching sees updated metrics
  → alert: every browser watching sees the new alert
  → state_update: every browser watching sees container status change
  → agent_connected/disconnected: every browser watching sees status

This is what makes the dashboard feel live:
  → Two team members both have the server page open
  → One deploys an app
  → The other sees the container status change in real time
  → Without polling, without page refresh
```

### Step 5A — Broadcast Implementation

```
Hub broadcasts to all browsers watching a server:

Function: broadcastToServer(server_id, message)

Implementation:
  1. Look up subscriptions[server_id]
     → Returns set of connection_ids watching this server
  
  2. For each connection_id:
     → Look up browser connection in browsers map
     → If found and connection is alive:
       → Put message on browser's send channel
     → If not found or connection dead:
       → Remove from subscriptions set
       → Clean up stale entry

  3. Done

Why put on send channel instead of writing directly:
  → Direct write would block the hub goroutine
  → Hub goroutine must never block
  → Send channel is buffered: hub puts it there and moves on
  → Writer goroutine handles the actual WebSocket write
```

### Step 5B — Health Report Broadcast Flow

```
Agent sends health_report every 30 seconds.
Hub routes it to all browsers watching that server.

Full flow:

1. Agent sends health_report JSON over WebSocket

2. Hub reader goroutine receives it

3. Hub goroutine processes:
   a. Store metrics in database (update server record)
   b. Run anomaly detection inline:
      → Compare new metrics against thresholds
      → If anomaly detected: generate alert
        → Store alert in DB
        → Add alert to the broadcast message
   c. Prepare broadcast message:
      {
        "type": "server_update",
        "server_id": "srv-abc123",
        "timestamp": "...",
        "metrics": {
          "cpu_percent": 45.2,
          "ram_percent": 59.9,
          "disk_percent": 46.0
        },
        "containers": [
          { "project": "myshop", "status": "running", ... }
        ],
        "alerts": [
          { new alert if any }
        ]
      }
   d. Call broadcastToServer(server_id, message)

4. All browsers watching this server receive the update

5. Browser/dashboard:
   → Updates CPU/RAM/disk gauges
   → Updates container status indicators
   → Shows any new alerts
   → All without any API call or polling
```

### Step 5C — Log Line Routing

```
Log lines are NOT broadcast to all browsers watching a server.
They go only to browsers that requested that specific log stream.

Stream routing table (maintained by hub):
  stream_routes: map[stream_id]set[connection_id]

When browser starts a log stream:
  → Hub adds: stream_routes[stream_id] = {connection_id}

When another browser starts the same stream:
  → Hub adds: stream_routes[stream_id] = {conn_A, conn_B}

When agent sends a log line:
  {
    "type": "log_line",
    "stream_id": "stream-abc123",
    "timestamp": "...",
    "text": "..."
  }

Hub routes:
  → Look up stream_routes[stream_id]
  → Send to each connection_id in the set
  → If a connection is gone: remove from set

Multiple browsers can watch the same log stream:
  → Example: two team members both have the logs view open
  → Both get every log line
  → Neither knows the other is watching
```

### Done Condition for Step 5
```
□ Health report broadcast reaches all browsers watching the server
□ Anomaly detection runs on each health report
□ Anomaly alert included in broadcast immediately
□ Two browsers watching same server both receive all updates
□ Log lines route only to browsers that requested that stream
□ Multiple browsers can watch the same log stream simultaneously
□ Dead browser connections are cleaned up from subscriptions
□ Dead browser connections are cleaned up from stream_routes
□ Broadcast does not block the hub goroutine
```

---

## Step 6 — Connection Health Monitoring

### Step 6A — Browser Connection Heartbeat

```
Browser WebSocket connections can go silently dead:
  → User's laptop sleeps
  → Network switches (WiFi → mobile data)
  → Browser tab hidden for extended time
  → Proxy timeout between browser and server

Hub sends ping to browser every 30 seconds:
  {
    "type": "ping",
    "timestamp": "2024-01-15T10:00:30Z"
  }

Browser must respond within 10 seconds:
  {
    "type": "pong",
    "timestamp": "2024-01-15T10:00:30Z"  ← echo the same timestamp
  }

If no pong received within 10 seconds:
  → Hub closes the WebSocket connection
  → Browser reader goroutine detects close
  → Unregister connection from hub
  → Browser dashboard detects WebSocket close
  → Dashboard shows: "Connection lost, reconnecting..."
  → Dashboard automatically tries to reconnect
```

### Step 6B — Agent Connection Heartbeat

```
Same concept as browser but different timings (defined in Layer 4A):
  → Control plane sends ping to agent every 30 seconds
  → Agent must respond with pong within 15 seconds
  → If no pong: hub considers agent disconnected
  → Hub processes as if agent sent an unregister event

Hub's ping sender goroutine:
  → Runs independently of hub main goroutine
  → Every 30 seconds: for each agent connection, send ping
  → Records ping sent time on the connection
  → Hub main goroutine checks for pong responses
  → If pong not received within 15 seconds of ping: disconnect agent

Important: ping goroutine runs separately from hub goroutine
  → Hub goroutine must never sleep or block
  → Ping goroutine sends "send_ping" event to hub channel
  → Hub goroutine puts ping on agent's send channel (non-blocking)
```

### Step 6C — Connection Metrics

```
Hub tracks connection metrics:

  → Total agent connections: currently connected servers
  → Total browser connections: currently open dashboard tabs
  → Messages routed per second (rolling average)
  → Average broadcast fan-out (messages per broadcast)
  → Failed sends (browser buffer full, dropped messages)

These metrics are:
  → Logged every 5 minutes
  → Available at an internal endpoint: GET /internal/hub/stats
  → Useful for: debugging, capacity planning, detecting issues

Example stats:
  {
    "agent_connections": 12,
    "browser_connections": 8,
    "subscriptions": 6,
    "messages_per_second": 24.3,
    "average_broadcast_fanout": 1.4,
    "pending_commands": 2,
    "active_log_streams": 3
  }
```

### Done Condition for Step 6
```
□ Hub pings browsers every 30 seconds
□ Browser with no pong response: connection closed after 10 seconds
□ Browser dashboard detects close and attempts reconnect
□ Hub pings agents every 30 seconds
□ Agent with no pong response: treated as disconnected
□ Agent disconnect processing runs (status update, browser notification)
□ Ping goroutine runs separately from hub main goroutine
□ Hub metrics logged every 5 minutes
□ Internal stats endpoint returns accurate current counts
```

---

## Step 7 — Reconnection Handling

### Step 7A — Browser Reconnection

```
Browser loses connection and automatically reconnects.

Dashboard WebSocket client behavior:
  → On close: wait 1 second, attempt reconnect
  → Reconnect with same JWT
  → If JWT expired: use refresh token to get new JWT first
  → On successful reconnect: send subscribe again

Hub side:
  → New browser connection arrives
  → Auth validated
  → Registered as new connection (new connection_id)
  → Browser sends subscribe for the server it was watching
  → Hub sends current server state snapshot
  → Browser re-renders with fresh state
  → All updates resume

What the user sees:
  → Brief "Reconnecting..." indicator
  → Metrics and status may briefly show as loading
  → Within 1-2 seconds: everything is live again
  → No page refresh needed
```

### Step 7B — Recovering Missed Updates

```
During reconnection, the browser missed some updates.
How to handle the gap:

Option A: Ignore the gap (MVP approach)
  → On reconnect: hub sends current state snapshot
  → Browser renders current state
  → Historical gap is just not shown
  → Simple, works well for metrics (current state is what matters)

Option B: Send missed updates (post-MVP)
  → Hub keeps a short buffer of recent broadcasts per server
  → On browser reconnect: send buffered updates
  → Browser catches up on what was missed
  → Useful for: missed log lines, missed alerts during gap

For MVP: Option A
  → The current state snapshot covers most needs
  → Missed log lines: user can scroll back in history
  → Missed alerts: alerts are stored in DB, visible in alert history
  → Metrics gap: small gap in chart is acceptable
```

### Step 7C — State Consistency After Reconnection

```
When browser reconnects and subscribes:

Hub builds the current state snapshot from:
  1. Server record from database (status, agent_version)
  2. Latest health metrics from database (last health_report values)
  3. Container statuses from database (last known state)
  4. Active alerts from database (currently active)
  5. Active Caddy routes from database (current domains)

This snapshot is always accurate because:
  → Every update from the agent is stored in the database
  → The database is the source of truth
  → Hub does not need to cache state itself
  → Hub asks the database on each reconnect

Result:
  → Browser reconnects and immediately has accurate current state
  → No stale data, no blank screens
  → Feels seamless to the user
```

### Done Condition for Step 7
```
□ Browser reconnects automatically after connection close
□ Browser sends subscribe after reconnect
□ Hub sends current state snapshot on subscribe
□ State snapshot is built from database (always accurate)
□ User sees reconnection indicator briefly then normal view
□ No page refresh required after reconnect
□ Missed updates during gap are handled (MVP: not replayed, snapshot covers it)
□ JWT refresh happens before reconnect if token expired
```

---

## Step 8 — Hub Startup and Shutdown

### Step 8A — Hub Startup

```
Hub starts when the control plane starts.
Order matters: hub must start before the HTTP server
that accepts WebSocket connections.

Startup sequence:
  1. Create hub struct with all maps initialized (empty)
  2. Create all channels with appropriate buffer sizes
  3. Start hub main goroutine
  4. Start ping sender goroutine
  5. Start cleanup goroutine (removes stale data periodically)
  6. Log: "WebSocket hub started"
  7. Return hub reference to HTTP server
  8. HTTP server starts accepting connections
     (Hub is ready to handle them)

Channel buffer sizes:
  → register_agent: 10
    (rare event, small buffer is fine)
  → register_browser: 100
    (can spike on redeploy when users refresh)
  → agent_message: 1000
    (frequent — health reports every 30s × many servers)
  → browser_message: 500
    (user actions — less frequent than agent messages)
  → broadcast_to_server: 500
    (frequent — same rate as agent messages)

If a channel fills up:
  → Hub is processing too slowly
  → Log error: "Hub channel full: [channel_name]"
  → Consider: is the hub goroutine blocked on something?
  → For MVP: this indicates a bug, not expected in normal operation
```

### Step 8B — Hub Shutdown

```
When the control plane shuts down:

1. Stop accepting new WebSocket connections
   (HTTP server stops first)

2. Signal hub to stop:
   → Close the hub's stop channel
   → Hub goroutine detects stop signal

3. Hub goroutine shutdown:
   → Stop ping sender goroutine
   → Close all browser WebSocket connections cleanly:
     → Send close frame to each browser
     → Browser dashboard shows "Server shutting down"
   → Close all agent WebSocket connections cleanly:
     → Send close frame to each agent
     → Agent detects close, begins reconnection loop
   → Wait for all reader/writer goroutines to exit (max 10 seconds)
   → Log: "WebSocket hub shut down. Connections closed: [N agents, M browsers]"

4. Control plane exits

Agent behavior after control plane shutdown:
  → Agent detects WebSocket close (clean close frame)
  → Agent begins reconnection with exponential backoff
  → Control plane restarts (typically < 5 seconds)
  → Agent reconnects on first or second retry
  → From user perspective: brief "Disconnected" then "Connected"
```

### Step 8C — Cleanup Goroutine

```
Runs every 5 minutes.
Cleans up stale state that might accumulate.

What it cleans:
  → Stale pending_commands (older than 15 minutes with no result)
    → These are commands where the agent never responded
    → Mark as timeout in database
    → Remove from pending_commands map

  → Empty subscription sets
    → subscriptions["srv-abc"] = empty set
    → Remove the key entirely (saves memory)

  → Stale stream_routes
    → stream_id with no active connections
    → Remove from stream_routes

  → Expired command records in database
    → DELETE FROM commands WHERE status = 'queued'
      AND expires_at < now() AND created_at < now() - INTERVAL '7 days'

This goroutine prevents memory accumulation over long uptimes.
The hub can run for months without restart.
Without cleanup: maps grow indefinitely.
```

### Done Condition for Step 8
```
□ Hub starts before HTTP server accepts connections
□ All channel buffers are sized appropriately
□ Channel full condition is logged as error
□ Hub shutdown sends clean close frame to all connections
□ Agents reconnect after control plane restart
□ Cleanup goroutine runs every 5 minutes
□ Stale pending commands are cleaned up
□ Empty subscription sets are removed
□ Stale stream routes are removed
□ Hub uptime can be verified by checking last startup log line
```

---

## Layer 5B Overall Done Condition

```
The full test sequence:

Test 1 — Basic agent and browser connection:
  □ Start agent on a server: agent appears connected in hub
  □ Open dashboard to server page: browser subscribes
  □ Health report arrives from agent: dashboard metrics update
  □ Close browser tab: browser unregistered from hub cleanly

Test 2 — Command round trip:
  □ Click Deploy in dashboard
  □ Command appears in hub as pending
  □ Agent receives command
  □ command_ack arrives: browser shows "executing"
  □ Progress updates arrive: browser shows deploy steps
  □ command_result arrives: browser shows success
  □ Command removed from pending_commands

Test 3 — Multiple browsers watching same server:
  □ Open server page in two browser tabs (same user)
  □ Deploy from one tab
  □ Both tabs show deployment progress
  □ Both tabs show updated container status after deploy

Test 4 — Agent disconnects and reconnects:
  □ Kill agent process on server
  □ Hub detects disconnect within 90 seconds
  □ Both browsers watching show "Disconnected"
  □ Agent restarts and reconnects
  □ Both browsers show "Connected"
  □ Metrics resume flowing

Test 5 — Log streaming:
  □ Browser opens log view for myshop
  □ Hub registers stream_id → browser_connection
  □ Log lines from agent routed only to this browser
  □ Second browser opens same log view
  □ Both browsers receive all log lines
  □ First browser closes log view
  □ Only second browser receives subsequent lines
  □ Second browser closes: stream_id removed from routing table

Test 6 — Agent offline command queuing:
  □ Disconnect agent
  □ Browser sends deploy command
  □ Hub stores command in DB as queued
  □ Browser receives "command queued" notification
  □ Agent reconnects
  □ Hub sends queued command in hello_ack
  □ Agent executes and sends result
  □ Result reaches browser (if still open) or stored in DB

Test 7 — Browser reconnection:
  □ Browser WebSocket closes (simulate network drop)
  □ Dashboard shows "Reconnecting..."
  □ Dashboard reconnects within 2 seconds
  □ Subscribe sent again
  □ Current server state received
  □ Dashboard shows correct state without page refresh

Test 8 — Heartbeat timeout:
  □ Browser stops responding to pings (simulate frozen tab)
  □ Hub waits 10 seconds after ping
  □ Hub closes the connection
  □ Connection removed from subscriptions
  □ No zombie connections in hub after timeout

Test 9 — Control plane restart:
  □ All agents connected, browsers watching
  □ Restart the control plane process
  □ Agents detect close frame, begin reconnecting
  □ Control plane comes back up (< 5 seconds)
  □ All agents reconnect within 30 seconds
  □ Browsers reconnect within 5 seconds
  □ No commands lost (queued in DB)
  □ All metrics and status resume

When all 9 tests pass, Layer 5B is done.
Move to Layer 5C — Database.
```

---

## What Layer 5B Does NOT Do

```
Layer 5B does not:
├── Authenticate connections (Layer 5A does)
├── Execute commands (agent's Layer 4B does)
├── Store metrics permanently (Layer 5C does)
├── Send emails or WhatsApp messages (notification service does)
├── Make decisions about what the agent should do
├── Validate command payloads (Layer 6 handlers do)
├── Run anomaly detection logic (Layer 4C on agent does)
└── Serve the dashboard HTML (Layer 7 / Next.js does)

Layer 5B does exactly one thing:
  Move the right message to the right place at the right time.
  Nothing more. Nothing less.
```

---

**Ready for Layer 5C — Database?**