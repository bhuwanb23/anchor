# Layer 7 — Frontend Dashboard: Complete Plan (No Code)

---

## What Layer 7 Actually Is

```
Layer 7 is the only thing the user ever sees.

Every other layer exists to make Layer 7 work.
The agent, the control plane, the WebSocket hub —
all of it is invisible infrastructure.

Layer 7 is the product.

The user judges the entire system by what they see here.
A technically perfect backend with a confusing dashboard
is a failed product.

Two design mandates that override everything else:

Mandate 1: A non-technical user must be able to
           go from "I have a VPS" to "my app is live"
           without googling anything.

Mandate 2: The day-to-day view must communicate
           "everything is fine" or "here is exactly
           what is wrong and what to do" at a glance.
```

---

## The Two Zones

```
The entire dashboard is divided into exactly two zones.
Every page belongs to one zone or the other.
They have different layouts, different goals, different users.

Zone 1: Onboarding
  Who: a brand new user who just signed up
  Goal: get their first app live without confusion
  Design: linear, one step at a time, no distractions
  Frequency: user goes through this once in their lifetime

Zone 2: Day-to-day
  Who: a returning user managing their server
  Goal: understand what is happening and take action quickly
  Design: overview-first, calm, obvious actions
  Frequency: user visits this regularly
```

---

## Step 1 — Project and Folder Structure

### How the Files Are Organized

```
The project uses Next.js App Router.
Every folder inside app/ becomes a URL segment.

Top-level structure:

dashboard/
├── app/                  ← all pages live here
├── components/           ← reusable UI pieces
├── lib/                  ← API client, WebSocket client, utilities
├── hooks/                ← custom React hooks for data fetching
├── store/                ← global state (Zustand)
├── types/                ← TypeScript type definitions
└── middleware.ts         ← route protection
```

### Page Hierarchy

```
app/
│
├── (auth)/               ← Zone 1 start: auth pages
│   ├── login/
│   ├── register/
│   ├── forgot-password/
│   └── reset-password/
│
├── onboarding/           ← Zone 1: first-time setup wizard
│   ├── connect-server/   ← step 1: connect a VPS
│   └── first-deploy/     ← step 2: deploy first app
│
└── dashboard/            ← Zone 2: everything day-to-day
    ├── page              ← overview of all servers
    └── servers/
        └── [server_id]/
            ├── page          ← single server overview
            ├── apps/
            │   ├── page      ← list of apps
            │   ├── new/      ← deploy a new app
            │   └── [app_id]/
            │       ├── page  ← app detail
            │       └── logs/ ← live log viewer
            ├── backups/
            ├── alerts/
            └── settings/
```

### Component Folder Organization

```
components/
│
├── ui/                   ← base building blocks
│   Button, Input, Card,
│   Badge, Dialog, Toast,
│   Skeleton, Progress
│
├── layout/               ← structural pieces
│   Sidebar, Header,
│   PageHeader
│
├── server/               ← server-specific components
│   ServerCard, ServerStatusBadge,
│   MetricsGauges, ConnectionStatus
│
├── app/                  ← app-specific components
│   AppCard, DeployButton,
│   DeploymentStatus, EnvVarList
│
├── logs/                 ← log viewer components
│   LogViewer, LogLine
│
└── alerts/               ← alert components
    AlertCard, AlertBanner
```

### Done Condition for Step 1
```
□ All folders created with correct names
□ Next.js starts without errors
□ Navigating to /login renders the login page
□ Navigating to /dashboard redirects to login if not authenticated
□ Navigating to /onboarding works after login
□ No 404 pages for routes that should exist
```

---

## Step 2 — Types and Data Contracts

### What Types Define

```
Before writing any UI, define what every piece of data looks like.
Types are the contract between the API and the frontend.
If the API sends a server object, the types define exactly
what fields it has and what type each field is.

This catches bugs at compile time, not at runtime.
A user never sees a "Cannot read property of undefined" error
if the types are correct.
```

### Key Types to Define

```
User:
  → ID, email, name, created_at
  → Never includes password hash (API never sends it)

Server:
  → ID, name, status, public IP, agent version
  → OS info: type, version, architecture
  → Hardware: CPU cores, RAM total, disk total
  → Latest metrics: CPU percent, RAM percent, disk percent
  → Status values: pending, connected, disconnected, updating, error

App:
  → ID, server ID, project name
  → Status: deploying, running, stopped, failed, removing
  → Current image, platform domain, custom domains
  → Memory limit, CPU quota, app port

Deployment:
  → ID, image, status, error message
  → Timing: started at, completed at, duration
  → Who triggered it
  → Domain at time of deploy

Container State:
  → Which project and role (app, postgres, redis)
  → Status, health, CPU percent, RAM used and limit
  → Restart count

Alert:
  → ID, server ID, project name (optional)
  → Severity: warning or critical
  → Type: machine-readable identifier
  → Status: active, resolved, acknowledged
  → Human content: title, message, detail, what to do
  → Timing: when it fired, when resolved

Backup:
  → ID, status, sizes, verification status
  → Per-project results
  → Timing

Log Line:
  → Timestamp, stream (stdout or stderr), text content

WebSocket Message Types:
  → Every type of message the WebSocket can receive
  → server_update, command_progress, command_result
  → initial_logs, log_lines, stream_ended
  → alert, agent_connected, agent_disconnected
```

### Done Condition for Step 2
```
□ All types defined in one central types file
□ Types match exactly what the Layer 6 API returns
□ No use of "any" type anywhere
□ Server status values are a union type (not any string)
□ App status values are a union type
□ WebSocket message types are union types
□ Types imported cleanly across components
```

---

## Step 3 — API Client Design

### What the API Client Does

```
The API client is a single configured HTTP client
that every component uses to talk to the control plane.

It handles several things automatically so no component has to:

1. Attaches the JWT to every request
   → Every request includes: Authorization: Bearer {token}
   → Component does not need to manually add auth headers

2. Handles token expiry silently
   → If a request gets a 401 with X-Token-Expired header:
     → Pause the request
     → Use the refresh token to get a new access token
     → Retry the original request with the new token
     → Component never knows this happened
   → If refresh fails: redirect to login

3. Handles multiple simultaneous expired requests
   → If 5 requests all get 401 at the same time:
     → Only one refresh call is made
     → The other 4 requests wait for that refresh to complete
     → All 5 retry with the new token
   → Without this: 5 simultaneous refresh calls, race condition

4. Provides a base URL
   → Every component calls /api/v1/servers not the full URL
   → The base URL comes from an environment variable
   → Changing environments (dev to prod) only requires one change
```

### Token Storage Strategy

```
Access token: stored in localStorage
  → Available to JavaScript (needed to attach to requests)
  → Not sent automatically by the browser (unlike cookies)
  → Cleared on logout

Refresh token: stored in localStorage
  → Used only when access token has expired
  → Cleared on logout
  → Revoked on the server when user logs out

Session indicator: stored in a cookie
  → A simple flag: "this browser has a logged-in session"
  → No sensitive data in the cookie
  → Used by Next.js middleware to redirect unauthenticated users
  → Without this: middleware cannot tell if user is logged in
    (localStorage is not readable in Next.js middleware)
  → Set on login, cleared on logout

Why this combination:
  → JWT in Authorization header (not cookie) avoids CSRF
  → Cookie indicator enables server-side route protection
  → Simple, well-understood, easy to debug
```

### Done Condition for Step 3
```
□ API client sends Authorization header on every request
□ Expired token triggers silent refresh
□ Failed refresh redirects to login
□ Multiple simultaneous 401s trigger only one refresh call
□ Base URL comes from environment variable
□ Tokens cleared correctly on logout
□ Cookie indicator set on login, cleared on logout
```

---

## Step 4 — WebSocket Client Design

### What the WebSocket Client Does

```
The WebSocket client is a singleton — one instance shared by the entire app.
It connects when the user logs in.
It disconnects when the user logs out.

It handles:

1. Connection management
   → Connects to /ws/browser with the JWT in the URL
   → JWT in URL because browsers cannot set custom headers on WebSocket connections
   → Single connection for the entire dashboard session

2. Automatic reconnection
   → When the connection drops: reconnect with exponential backoff
   → Wait 1 second, then 2, then 4, then 8... up to 60 seconds max
   → Add random jitter to each wait (prevents all users reconnecting simultaneously)
   → Stop reconnecting only on explicit logout

3. Heartbeat detection
   → Send a ping every 30 seconds
   → Expect a pong within 10 seconds
   → If no pong: connection is silently dead — close it and reconnect
   → This catches "half-open connections" that TCP does not detect

4. Message routing
   → Every message from the server has a type field
   → Components register handlers for specific message types
   → When a message arrives: find all handlers for that type, call them
   → Components register when they mount, deregister when they unmount

5. Server subscription management
   → The dashboard tells the hub which server it is currently viewing
   → Subscribe when opening a server page, unsubscribe when leaving
   → Hub only sends updates for the subscribed server
   → Saves bandwidth, prevents wrong-server updates appearing
```

### How Components Use the WebSocket Client

```
The relationship between components and the WebSocket client:

Component mounts (page opens):
  → Register a handler: "when a server_update arrives, call this function"
  → If viewing a server: subscribe to that server

Message arrives from server:
  → Client finds all registered handlers for that message type
  → Calls each handler with the message data
  → Handler updates state → component re-renders

Component unmounts (page closes):
  → Deregister the handler (prevents memory leaks)
  → If viewing a server: unsubscribe from that server

This pattern:
  → Components are decoupled from the WebSocket client
  → Multiple components can react to the same message
  → Components do not need to manage the WebSocket connection
  → Clean up is automatic on unmount
```

### Done Condition for Step 4
```
□ WebSocket client is a singleton (one instance app-wide)
□ Connects with JWT in query parameter
□ Reconnects automatically after connection loss
□ Backoff increases with each failed attempt (capped at 60 seconds)
□ Jitter applied to each backoff period
□ Heartbeat sends ping every 30 seconds
□ No-pong timeout closes dead connections after 10 seconds
□ Components register and deregister handlers cleanly
□ Unregistered handlers are never called
□ Memory does not grow over time from forgotten handlers
```

---

## Step 5 — Global State Management

### Why Global State Is Needed

```
Some state is needed by many components simultaneously:
  → The logged-in user (needed everywhere)
  → The list of servers (needed in sidebar and main content)
  → Real-time metrics for the selected server (needed in multiple places)
  → Active alerts (needed in header badge and alert list)
  → WebSocket connection status (needed in header)

Without global state: this data gets fetched multiple times,
different components show inconsistent data, prop drilling
(passing props through many layers) becomes unmanageable.

With global state (Zustand stores): one source of truth,
every component reads from the same place,
updates appear everywhere simultaneously.
```

### The Three Stores

```
Store 1: Auth Store
  What it holds:
    → The logged-in user object (ID, email, name)
    → Whether the user is authenticated
    → Whether auth state is still loading (on first page load)

  What it does:
    → Login: stores user, sets authenticated to true
    → Logout: clears user, clears tokens, sets authenticated to false
    → Loading state: prevents flash of login page while checking auth

  Persistence:
    → The user object is persisted to localStorage
    → On page refresh: user object is restored immediately
    → No flash of unauthenticated state

Store 2: Server Store
  What it holds:
    → List of all servers the user has access to
    → Which server is currently selected (being viewed)
    → Real-time metrics for the selected server
    → Container states for the selected server
    → Active alerts for the selected server

  What it does:
    → Update metrics: called by WebSocket handler every 30 seconds
    → Update containers: called by WebSocket handler on status change
    → Add alert: called when new alert arrives
    → Resolve alert: called when alert is resolved
    → Change selected server: clears all real-time data (prevents stale data)

Store 3: WebSocket Store
  What it holds:
    → Current WebSocket connection status (connecting, connected, disconnected)
    → In-progress commands (commands that were sent but not yet completed)
    → Each command's progress: step, message, percentage

  What it does:
    → Set status: called by WebSocket client when connection changes
    → Set command progress: called when command_progress messages arrive
    → Clear command: called when command_result arrives (success or failure)
```

### Done Condition for Step 5
```
□ Auth store persists user across page refresh
□ Auth store correctly represents loading state on first render
□ Server store clears stale metrics when selected server changes
□ Server store correctly handles alerts being added and resolved
□ WebSocket store tracks all in-progress commands
□ Command is removed from store when result arrives
□ No component derives auth state from multiple sources
□ Stores do not depend on each other (no circular dependencies)
```

---

## Step 6 — Route Protection

### How Route Protection Works

```
Some pages require the user to be logged in.
Some pages should redirect logged-in users away (login page).

Next.js Middleware runs on every request before the page loads.
It can redirect the user before they see anything.

The challenge:
  Tokens are in localStorage.
  Middleware runs on the server, cannot read localStorage.
  Need a way to signal "user is logged in" that the server can read.

Solution: a lightweight cookie called has_session.
  → Set when user logs in (alongside localStorage tokens)
  → Cleared when user logs out
  → Contains no sensitive data (no token, no user info)
  → Only purpose: tell middleware "this browser has a session"

Middleware logic:
  → If user is on a public page (login, register) AND has has_session cookie:
    → Redirect to /dashboard (already logged in, no need to see login)
  → If user is on a protected page AND no has_session cookie:
    → Redirect to /login (not logged in)
    → Store where they were going as a query parameter (?next=/dashboard/...)
    → After login: redirect them to where they were trying to go
  → Otherwise: let the request through

This is a first line of defense, not a security mechanism.
The real security is JWT validation in the API.
If someone clears the cookie but keeps the token: the page loads,
API calls work, session is valid. The cookie is just for UX.
If someone fakes the cookie but has no token: the page loads,
API calls return 401, they see an error or get redirected to login.
```

### Done Condition for Step 6
```
□ Unauthenticated users cannot see dashboard pages
□ Authenticated users cannot see login/register pages
□ Middleware redirects preserve the intended destination
□ After login: user is sent to where they were trying to go
□ Cookie is set on login and cleared on logout
□ Cookie contains no sensitive information
□ Middleware does not run on static assets (images, CSS, JS)
```

---

## Step 7 — Zone 1: Onboarding Flow

### Design Principles for Onboarding

```
The onboarding flow has one job: remove every possible obstacle
between "I signed up" and "my app is live."

Design constraints:
  → No sidebar
  → No navigation
  → No settings or options that are not essential right now
  → One decision per screen
  → Every instruction is copy-pasteable, not descriptive
  → Progress is shown so user knows how far they are
  → The system reacts in real time (no manual "check" button)
```

### The Onboarding Layout

```
What the layout contains:
  → Logo at the top left (reassurance this is the right place)
  → Step indicator showing 2 steps (connect server, deploy app)
  → Current step number and name
  → The step content (takes up most of the page)
  → No footer, no help links in the way, no account menu

Visual design goal:
  → Feels focused and calm
  → User is not overwhelmed by options
  → The next action is always obvious
  → There is always exactly one primary button
```

### Step 7A — Connect Server Step

```
Goal: get the install command onto the user's server and connect.

What this page shows:

Section 1: What to do
  → Short sentence: "Run this command on your server."
  → Not an essay. Not a tutorial. One sentence.

Section 2: The install command
  → The full command displayed in a code block
  → Monospace font so it looks like a terminal command
  → One-click copy button next to the command
  → After copying: button changes to a checkmark for 2 seconds
    (confirms to the user that the copy worked)

Section 3: Token expiry
  → Small text below the command: "This command expires in 47 minutes"
  → Countdown timer (minutes and seconds)
  → When it expires: show "Generate a new command" button instead

Section 4: Waiting indicator
  → Below the command block: "Waiting for your server to connect..."
  → Animated indicator (subtle, not distracting)
  → Below that: "This page will update automatically when your server connects."
  → No "Check status" button. No page refresh. Just trust the user that it works.

Section 5: Help link
  → Very small, at the bottom: "How do I open a terminal on my server?"
  → Link to a simple doc page (not opening a tutorial modal in the flow)

What happens when the server connects:
  → The WebSocket delivers an agent_connected event
  → The waiting indicator is replaced with: "✓ Server connected!"
  → Green checkmark, server name shown
  → After 1.5 seconds: automatically move to the next step
  → User does not need to click anything

What happens if the token expires:
  → Countdown reaches zero
  → Command block is replaced with an expiry message
  → Button: "Generate a new install command"
  → Clicking it fetches a fresh token and shows the new command
  → Countdown resets to 60 minutes
```

### Step 7B — Deploy First App Step

```
Goal: get the user's first app running with HTTPS.

What this page shows when first loaded:

Section 1: Template picker
  → Three large clickable cards in a row:
    → Next.js + Postgres
    → WordPress
    → Django + Postgres
  → Each card shows: name, brief description, what is included
  → Clicking one fills in the form below

Section 2: Custom image option
  → Divider: "— or use your own Docker image —"
  → Three fields:
    → Docker image (text input, placeholder: nginx:latest)
    → App name (text input, placeholder: my-app)
    → Port (number input, placeholder: 3000)
  → Template selection fills these fields in automatically

Section 3: Deploy button
  → One large "Deploy →" button
  → Disabled until required fields are filled

What happens when Deploy is clicked:

Phase 1: Sending the command
  → Button shows spinner, text changes to "Sending..."
  → API call made to create the deployment
  → Page transitions to progress view

Phase 2: Progress view (replaces the form)
  → Title: "Deploying {app-name}..."
  → Step list showing each phase of deployment:
      ✓  Pulling image (completed)
      ⏳  Starting your app (in progress)
      ○  Configuring HTTPS (not started yet)
  → Progress bar showing percentage
  → Text below: "This usually takes 30-90 seconds"
  → Each step updates in real time from command_progress WebSocket messages
  → "View Logs" link (opens logs in a new tab, deploy continues)

Phase 3a: Success view
  → Title: "🎉 Your app is live!"
  → App name and the live HTTPS URL
  → "Open →" button that opens the URL in a new tab
  → Three confirmation lines:
      ✓ Daily backups enabled
      ✓ HTTPS automatic
      ✓ Auto-restart enabled
  → One button: "Go to Dashboard →"

Phase 3b: Failure view
  → Title: "Deploy failed"
  → Calm message: "Your app did not start correctly. Your server is fine."
  → The last 10-20 log lines from the failing container
  → These are shown in a code block with monospace font
  → Three common causes listed:
      → Missing or incorrect environment variable
      → App trying to connect to a database that is not ready
      → Port mismatch (app listening on wrong port)
  → Two buttons: "Try Again" and "View Full Logs"
```

### Done Condition for Step 7
```
□ Onboarding layout shows no sidebar or navigation
□ Step indicator shows current position
□ Install command displays with one-click copy
□ Copy button confirms copy with checkmark
□ Token countdown timer counts down accurately
□ Page updates automatically when server connects (no button)
□ Auto-advance to deploy step after server connects
□ Token expiry shows regenerate option
□ Template selection fills in the deploy form
□ Deploy progress updates step by step from WebSocket
□ Success view shows the live HTTPS URL
□ Failure view shows log lines and common causes
□ "Try Again" works without going back to the beginning
```

---

## Step 8 — Zone 2: Day-to-Day Dashboard Layout

### The Dashboard Shell

```
The shell wraps every page in Zone 2.
It stays the same while the main content changes.

Left sidebar:
  → Logo at the top
  → List of servers, each showing:
      Name of the server
      Status dot: green (connected), yellow (updating), grey (disconnected), red (error)
  → Currently selected server is highlighted
  → "+ Add Server" button at the bottom of the server list
  → Divider
  → Account link
  → Support link

Top header:
  → Page title (changes per page: "Production Server", "Logs", etc.)
  → Notification bell with count of unread alerts
  → User name with dropdown: Account Settings, Logout
  → WebSocket status indicator: a small dot
    → Green: connected to control plane
    → Pulsing yellow: reconnecting
    → Grey: disconnected

Main content area:
  → Takes up the rest of the screen
  → Changes completely per page
  → Has its own scroll (sidebar does not scroll with content)

Responsive behavior:
  → On small screens: sidebar collapses to an icon bar
  → Hamburger menu opens full sidebar as an overlay
  → All functionality accessible on mobile (not optimal but functional)
```

### Done Condition for Step 8
```
□ Sidebar shows all servers with correct status dots
□ Status dot updates in real time when agent connects/disconnects
□ Selecting a server navigates to that server's page
□ Header bell shows count of unread active alerts
□ Bell count updates when new alerts arrive without page refresh
□ WebSocket status dot shows correct connection state
□ Dashboard shell does not re-mount between page navigations
□ Sidebar collapses on small screens
```

---

## Step 9 — Server Overview Page

### What This Page Must Communicate

```
The server overview is the most important page in the product.
It is what the user sees every time they open the dashboard.
In 5 seconds, they must know:
  → Is my server connected?
  → Is everything running?
  → Do I need to do anything right now?

If the answer to all three is positive, the page should feel calm.
If the answer to any is negative, the problem must be obvious.
```

### Page Sections

```
Section 1: Server Status Header
  → Server name (large)
  → Status badge: "Connected" (green) or "Disconnected" (grey) or "Error" (red)
  → Agent version (small, below name)
  → "Deploy New App" button (prominent, top right)

Section 2: Resource Gauges (three side by side)

  CPU gauge:
    → Circular or bar gauge
    → Current percentage large and centered
    → No label clutter — just the number and unit
    → Color: green below 70%, yellow 70-85%, red above 85%
    → Updates every 30 seconds from WebSocket

  RAM gauge:
    → Same design as CPU
    → Shows: percentage AND "1.2GB / 2GB" below
    → Color thresholds: same as CPU

  Disk gauge:
    → Same design
    → Shows: percentage AND "18GB / 40GB" below
    → Color thresholds: 75% = yellow, 90% = red

  Design note: do NOT show a graph here
    → Graphs are for the metrics history page
    → The overview shows current state only
    → A graph of the last 24 hours is not what someone needs
      when they open the dashboard to check if everything is fine

Section 3: Apps List

  Heading: "Apps" with count (Apps · 3) and "+ New App" button

  Each app shown as a card:
    → App name (large)
    → Status badge: Running (green), Stopped (grey), Crashed (red), Deploying (yellow)
    → The live URL (clickable, opens in new tab)
    → Current image and when it was deployed
    → Quick action buttons: Logs, Restart, Deploy, More (...)

  When an app is crashed:
    → Card has a red left border (visually distinct)
    → Below status: "Crashed 5 minutes ago"
    → The alert message is shown inline on the card:
      "Your app myblog stopped unexpectedly. Check logs for the reason."
    → "View Logs" and "Restart" buttons are more prominent

  When an app is deploying:
    → Card shows a progress bar
    → Current step text: "Starting your app..."
    → No action buttons while deploying (prevents conflicting actions)

Section 4: Alert Summary (only shown if there are active alerts)

  Heading: "⚠ {N} Active Alert{s}"

  Shows the most recent critical alert inline:
    → Alert title
    → One-line summary
    → "View All Alerts →" link

  Design note: do NOT show all alerts here
    → The alerts page is for that
    → The overview shows the most urgent one only
    → If no alerts: this section is completely hidden (calm state)

Section 5: Backup Status (always shown, small)

  One line at the bottom of the page:
    → If last backup was today: "✓ Last backup: today at 2:04am (47MB)"
    → If last backup was yesterday: "✓ Last backup: yesterday at 2:01am"
    → If backup is overdue: "⚠ Backup overdue — last backup was 3 days ago"
    → "Backups →" link that goes to the backups page
```

### Done Condition for Step 9
```
□ Resource gauges show real numbers from the latest health report
□ Gauges change color at correct thresholds
□ Gauges update every 30 seconds without page refresh
□ App cards show correct status for each app
□ Crashed app card is visually distinct and shows the reason
□ Deploying app card shows progress and no action buttons
□ Status badges update in real time when containers change
□ Alert summary only appears when there are active alerts
□ Alert count is accurate and updates in real time
□ Backup status is accurate and updates after each backup
□ "Deploy New App" button navigates to the deploy page
```

---

## Step 10 — App Detail Page

### What This Page Contains

```
The app detail page is where the user manages a single app.
It has four tabs: Overview, Logs, Deployments, Settings.

Page header (always visible regardless of tab):
  → Back link to the server page
  → App name (large)
  → Status badge (real-time)
  → Primary actions across the top:
    "Deploy New Version" (most common action — most prominent)
    "Rollback" (second most common)
    "Restart" "Stop" "Delete" (in a less prominent row or "More" dropdown)
  → The live URL with an open icon

Tab 1: Overview

  Container Health section:
    → For each container in this project (app, postgres, redis):
      → Container role name
      → Status badge
      → CPU and RAM usage bars
      → Restart count (if > 0, shown in orange)
    → Updates in real time from health reports

  Environment Variables section:
    → List of all env var keys (not values — values are masked)
    → Each row: key name, masked value (••••••), Edit button
    → "(auto)" label on auto-managed variables (DATABASE_URL, PORT)
    → Auto-managed variables have no Edit button (they cannot be changed)
    → "+ Add Variable" button at the bottom of the list
    → Edit opens a dialog with a single input for the new value
    → After saving: shows "Restart your app to apply changes"
    → "Restart Now" button inline

  Custom Domains section:
    → List of any custom domains currently configured
    → Each domain: the domain name, verification status, Remove button
    → "+ Add Custom Domain" button
    → Adding a domain opens a dialog with:
      → Input for the domain name
      → After entering: shows DNS instructions specific to their server IP
        "Point shop.yourdomain.com to 1.2.3.4 (your server IP)"
        Shows the record type (A), name (shop), value (IP), TTL (3600)
      → "I've made the DNS change" button
      → Starts checking DNS propagation (shows a spinner)
      → When propagated: "Domain verified and active ✓"
      → If not propagated after 2 minutes: "Still waiting for DNS..."
        with an explanation about propagation time

Tab 2: Logs
  → Full log viewer (described in Step 12)

Tab 3: Deployments

  A chronological list of all deployments for this app:
  → Most recent at the top
  → Each entry shows:
    → Image that was deployed
    → Status: Success, Failed, Rolled Back
    → Who deployed it (user name)
    → When (relative: "2 hours ago") with exact time on hover
    → Duration (took 47 seconds)
  → Failed deployments show the failure reason (first line of error)
  → "Rollback to this version" button on past successful deployments
  → Current deployment is marked "Current"

Tab 4: Settings

  App configuration:
    → Memory limit: a slider or input (64MB to 2048MB)
    → CPU quota: a slider (10% to 100%)
    → App port: the port the container listens on
    → "Save Changes" button
    → Changing resource limits does NOT require a redeploy
      (agent updates the container limits in place)

  Danger Zone:
    → Clearly separated section, visually distinct (light red background)
    → "Delete App" button
    → Clicking shows a confirmation dialog:
      "Type the app name to confirm deletion"
      Text input, Delete button only enables when name matches exactly
```

### Done Condition for Step 10
```
□ Tab navigation works without page reload
□ Container health updates in real time on Overview tab
□ Env var list shows keys only (no values)
□ Edit env var opens a dialog and sends the update
□ After env var update: restart suggestion appears
□ Add domain shows specific DNS instructions with server IP
□ Domain verification polls automatically and updates when propagated
□ Deployment list is ordered most-recent first
□ Rollback button uses the correct image from that deployment
□ Memory and CPU settings save without requiring redeploy
□ Delete confirmation requires typing the exact app name
□ Deletion is irreversible and this is made clear before confirmation
```

---

## Step 11 — Deploy Flow

### The Deploy Dialog

```
Deploy is triggered by clicking "Deploy New Version" from anywhere.
It opens as a dialog (modal) over the current page.
The page behind stays visible but dimmed.
No navigation away from the current page.

Why a dialog not a separate page:
  → User can see the current state of their app while deploying
  → Less disorienting than navigating away
  → Progress appears in the same visual context

Dialog State 1: Input

  Title: "Deploy {app-name}"
  
  One field: Docker image
    → Pre-filled with the current image
    → User edits the tag (e.g., change :1.24.0 to :latest)
    → Helper text: "Current version: nginx:1.24.0"
  
  Advanced section (collapsed by default):
    → Memory limit override
    → Port override
    → Only shown if user clicks "Advanced settings ▼"
    → Collapsed by default to reduce cognitive load
  
  Two buttons:
    → "Cancel" (left, secondary)
    → "Deploy →" (right, primary)

Dialog State 2: In Progress

  Title: "Deploying {app-name}..."
  
  Step list (updates live from WebSocket):
    ✓  Pulling image (step complete, green checkmark)
    ✓  Preparing databases
    ⏳  Starting your app (spinner, currently executing)
    ○  Configuring HTTPS (not started, grey circle)
  
  Progress bar below the steps: fills as percent increases
  
  Estimated time: updates based on how long current step is taking
  
  "View Logs" link: opens logs in a new tab without closing this dialog
  
  No cancel button: cancelling a running deploy is dangerous
    (would leave the container in a half-started state)
    The user can wait or close the tab — the deploy continues on the server

Dialog State 3: Success

  Title: "✓ Deploy successful"
  
  Live URL shown prominently with "Open →" button
  Deploy duration shown: "Took 47 seconds"
  
  One button: "Done"
  Closing the dialog or clicking Done: returns to the page behind

Dialog State 4: Failure

  Title: "✗ Deploy failed"
  
  Calm reassurance: "Your previous version is still running. Your site is up."
  
  What went wrong — the last log lines from the failing container:
    → Shown in a code block (monospace, dark background)
    → Maximum 20 lines
    → Most recent at the bottom
  
  Three most common causes listed as suggestions:
    → Incorrect environment variable
    → Database not available yet
    → App listening on wrong port
  
  Two buttons:
    → "View Full Logs" — opens logs tab of this app
    → "Try Again" — resets dialog to State 1 with same image pre-filled
```

### Done Condition for Step 11
```
□ Deploy dialog opens over current page without navigation
□ Image field pre-filled with current version
□ Advanced settings collapsed by default
□ Progress updates appear step by step from WebSocket
□ Progress bar percentage is accurate
□ "View Logs" link works without closing the dialog
□ Success state shows the live URL and deploy duration
□ Failure state shows the specific log lines that caused the failure
□ Failure state confirms previous version is still running
□ "Try Again" resets to input state without closing dialog
□ Dialog is keyboard accessible (Escape closes it, Tab navigates)
```

---

## Step 12 — Log Viewer

### Log Viewer Design

```
The log viewer is a dedicated page AND can be opened from other contexts.

Access paths:
  → Tab in app detail page (most common)
  → Link from a crash alert
  → Link from a failed deploy dialog
  → Direct URL: /dashboard/servers/{id}/apps/{id}/logs

What the page contains:

Top bar:
  → Container selector: "App ▼" (dropdown: App, Postgres, Redis)
    → Changing container: fetches logs for the new container
  → Connection indicator: "● Live" (green) or "● Reconnecting" (yellow)
  → No other controls for MVP (search and filter are future features)

Log area (takes up the rest of the page):
  → Dark background (like a terminal)
  → Monospace font
  → Each line shows:
    → Timestamp (formatted to user's local timezone, not UTC)
    → The log line text
  → stdout lines: white or light grey text
  → stderr lines: red/orange text (visually distinct — these are errors)
  → Lines are selectable (user can copy individual lines)

Scrolling behavior:
  → Auto-scrolls to the bottom as new lines arrive
  → If user scrolls up: auto-scroll pauses immediately
  → A "↓ Jump to latest" button appears at the bottom right
  → Clicking it: scrolls to bottom and resumes auto-scroll
  → This is the most important UX detail in the log viewer
    Without it: user cannot read logs while new ones stream in
    With it: user can scroll up to investigate, then jump back

Initial load behavior:
  → Shows the last 200 lines immediately (from initial_logs WebSocket message)
  → Then continues with live streaming
  → No loading spinner before the first lines appear
    (the WebSocket delivers the history almost instantly)

Container stop behavior:
  → When the container stops: the streaming connection closes
  → The last line in the viewer shows: "--- Container stopped ---"
  → The connection indicator changes to "○ Stopped"
  → The log area is not cleared — the existing logs stay visible
  → User can still read everything that happened before the stop

Memory management:
  → Only the last 2000 lines are kept in memory
  → When the 2001st line arrives: the oldest line is removed from display
  → Users with very chatty apps will not see the browser slow down
```

### Done Condition for Step 12
```
□ Log viewer opens and shows last 200 lines within 2 seconds
□ New lines appear within 100ms of arriving in the container
□ stdout and stderr have visually distinct styles
□ Timestamps are in the user's local timezone
□ Auto-scroll works correctly
□ Auto-scroll pauses when user scrolls up
□ "Jump to latest" button appears when scrolled up
□ Clicking "Jump to latest" resumes auto-scroll
□ Container selector switches to correct container's logs
□ "Container stopped" message appears when appropriate
□ Existing logs are not cleared after container stop
□ Memory is bounded at 2000 lines
□ Log viewer works on mobile (horizontal scroll acceptable)
```

---

## Step 13 — Alerts Page

### Alerts Page Design

```
URL: /dashboard/servers/{server_id}/alerts

This page shows all alerts for a server, current and historical.

Filter tabs at the top:
  → "Active" (default): alerts that are currently happening
  → "Resolved": alerts that have been resolved
  → "All": everything

Filter does not navigate to a new page — it filters the current list.

Each alert card shows:
  → Severity indicator: a colored left border (red = critical, yellow = warning)
  → Alert title (bold)
  → Which project it affects (or "Server" for server-level alerts)
  → When it fired (relative time: "5 minutes ago")
  → The full message (plain English, written for non-technical users)
  → What to do (the "action" field from Layer 4C alert templates)
  → For active alerts: "Acknowledge" button
  → For resolved alerts: when it resolved

Critical alerts are shown before warning alerts within each time period.

Acknowledging an alert:
  → Does not fix the underlying problem
  → Removes it from the "needs attention" state
  → Moves it from "Active" to "Acknowledged" (still visible, just not alarming)
  → Use case: user is aware of the problem and is working on it
  → "I know about this, stop highlighting it"

Empty states:
  → No active alerts: a green checkmark and "All clear — no active alerts"
  → This is a positive state and should feel reassuring, not empty
  → Do NOT show a blank area or a generic "No data" message
```

### Alert Banner in Dashboard Layout

```
When there are active critical alerts, show a banner in the header.
Not a modal, not a popup — a subtle but visible bar.

The banner:
  → Appears below the header, above the page content
  → Red background (critical) or yellow (warning)
  → Shows: alert title and which server/app
  → "View" link that goes to the alerts page
  → "×" to dismiss it for this browser session
    (it comes back if new alerts arrive)

The banner does not appear for:
  → Resolved alerts
  → Acknowledged alerts
  → Warning-level alerts on the overview page (only on alerts page)
```

### Done Condition for Step 13
```
□ Alerts page shows correct filter tabs
□ Switching tabs filters without page reload
□ Critical alerts appear before warning alerts
□ Alert cards show the full plain-English message and action
□ Acknowledge button updates the alert status
□ Acknowledged alerts move to the correct category
□ Resolved alerts show when they were resolved
□ Empty "Active" state shows reassuring "All clear" message
□ Critical alert banner appears in header when active
□ Banner can be dismissed for the session
□ Banner reappears if new critical alert arrives after dismissal
```

---

## Step 14 — Backups Page

### Backups Page Design

```
URL: /dashboard/servers/{server_id}/backups

This page shows backup history and allows manual backup and restore.

Header section:
  → Title: "Backups"
  → Current storage usage: "892MB used in backup storage"
  → Schedule: "Daily at 2:00am UTC" with an "Edit" link
  → "Back Up Now" button (right side, triggers immediate backup)

When "Back Up Now" is clicked:
  → Button becomes disabled, shows spinner
  → A progress card appears at the top of the list
  → Shows which project is currently being backed up
  → Updates in real time from command_progress messages
  → When done: shows the new backup at the top of the list

Backup list:
  Each backup is a card showing:
    → Date and time (relative: "Today at 2:04am")
    → Status: Success (green), Partial (yellow), Failed (red), Running (spinner)
    → Size added to storage (new deduplicated data: "47MB added")
    → Total repository size ("892MB total")
    → Verification badge: "✓ Verified" or "⚠ Unverified"
    → Per-project results:
        myshop ✓   myblog ✓   (for full success)
        myshop ✗   myblog ✓   (for partial, with error reason on hover)
    → "Restore" button (only on successful backups)

Restore flow:
  → Clicking "Restore" on a backup opens a dialog
  
  Dialog shows:
    → Which backup this is (date, time, size)
    → What will be restored (list of projects)
    → Serious warning:
      "This will replace your current database and files
       with the versions from this backup.
       Data created after this backup will be lost.
       This cannot be undone."
    → A text confirmation input: "Type RESTORE to confirm"
    → The Restore button is disabled until the user types exactly "RESTORE"
    → Cancel button

  During restore:
    → Dialog content changes to progress view
    → Steps: Downloading backup, Restoring database, Restoring files, Restarting app
    → Warning banner: "Do not close this window during restore"
    → If the browser is closed: restore continues on the server

  After restore:
    → Success: "Restore complete. Your app has been restarted."
    → Failure: specific error message explaining what failed and what to do

Storage section at the bottom:
  → Simple storage usage display
  → "Using 892MB of your 5GB included storage"
  → Progress bar
  → Link to upgrade storage if approaching limit
```

### Done Condition for Step 14
```
□ Backup list shows all backups in reverse chronological order
□ Running backup shows progress in real time
□ Per-project status shown for each backup
□ Verification status clearly visible
□ Restore dialog shows correct backup details
□ Restore requires typing "RESTORE" to confirm
□ Restore progress shown step by step
□ Restore failure shows specific error with guidance
□ "Back Up Now" triggers immediate backup and shows progress
□ Storage usage is accurate and updates after each backup
```

---

## Step 15 — Error and Loading States

### Loading States

```
Every page that fetches data must show a skeleton while loading.
Never show a blank page, never show a spinner in the middle of content.

Skeleton principles:
  → Match the shape of the actual content exactly
  → Use light grey blocks where text will appear
  → Use rounded rectangles for badges and buttons
  → Animate subtly (gentle shimmer or pulse)
  → Do NOT show placeholder text ("Loading..." or "---")
    The shape communicates what is loading

Server overview skeleton:
  → Three grey circles (where the gauges will be)
  → Three grey rectangles below (where app cards will be)
  → A thin grey line at the bottom (where backup status will be)

Transition from skeleton to content:
  → Content fades in (opacity 0 to 1 over 150ms)
  → No layout shift — skeleton occupies exactly the same space as content
  → If content loads in under 200ms: skip the skeleton entirely
    (showing a skeleton for 50ms then immediately hiding it is jarring)
```

### Error States

```
When a page fails to load its data:

Error state principles:
  → Never show a technical error message to the user
  → Always show what went wrong in plain English
  → Always offer something to do (Retry, Go home, Contact support)
  → Always show the request ID (small, at the bottom, for support)

Standard error component:
  → Icon: a simple warning sign or cloud-with-slash
  → Heading: "Something went wrong"
  → Message: plain English explanation
    Examples:
      → "We could not load your server information. Try again in a moment."
      → "Your session has expired. Please log in again."
      → "This server was not found, or you do not have access to it."
  → Primary button: "Try again" (retries the failed request)
  → Secondary link: "Go to dashboard" (escape hatch)
  → Fine print: "If this keeps happening, contact support.
     Request ID: req-abc123"

Server disconnected state (not an error, but an unusual state):
  → Shown on the server overview when agent is not connected
  → Calm tone: "Your server is not connected right now."
  → Reassurance: "Your apps are still running."
  → Explanation: brief, plain English reasons this happens
  → Automatic: "We will reconnect automatically"
  → Last seen time: "Last connected 3 minutes ago"
  → No "Reconnect" button (the agent reconnects itself)

Empty states:
  → No servers yet: welcome message + "Add your first server" button
  → No apps deployed: "No apps yet" + "Deploy your first app" button
  → No active alerts: "✓ All clear" with a brief reassuring message
  → No backups yet: "Your first backup will run tonight at 2am"
    (sets expectation, not an error)
```

### Toast Notifications

```
For actions that happen quickly and do not need a full page:

Toast notifications appear in the bottom right corner.
They disappear automatically after 4 seconds.
User can dismiss them with an X.

When to use toasts:
  → Env var saved: "Variable updated. Restart your app to apply."
  → Alert acknowledged: "Alert acknowledged"
  → Domain removed: "Domain removed"
  → Settings saved: "Settings saved"

When NOT to use toasts:
  → Deployment result (too important — stays in the dialog)
  → Errors that require action (use inline errors or error states)
  → Multi-step processes (use progress indicators)

Toast types:
  → Success: green left border, ✓ icon
  → Warning: yellow left border, ⚠ icon
  → Error: red left border, ✗ icon
  → Info: blue left border, ℹ icon
```

### Done Condition for Step 15
```
□ Every data-fetching page shows a skeleton while loading
□ Skeleton matches content layout exactly
□ No layout shift when content replaces skeleton
□ Error states are shown for failed requests
□ Error states are in plain English (no technical messages)
□ Error states include request ID for support
□ Retry button works correctly on error states
□ Server disconnected state is calm and reassuring
□ Empty states have helpful prompts
□ Toast notifications appear for quick actions
□ Toasts disappear after 4 seconds
□ Critical actions use dialogs, not toasts
```

---

## Layer 7 Overall Done Condition

```
The complete user experience test:

Test 1 — New user onboarding:
  □ Sign up with email and password
  □ Directed to onboarding immediately after signup
  □ See install command within 2 seconds of page load
  □ Copy button works and confirms copy
  □ Paste command on a fresh Ubuntu server
  □ Page updates to "Server connected" automatically (no refresh)
  □ Advance to deploy step automatically
  □ Select a template (Next.js template)
  □ Click deploy
  □ Watch step-by-step progress
  □ See live URL on success
  □ Click URL: app is accessible over HTTPS

Test 2 — Returning user daily check:
  □ Log in
  □ Redirected to dashboard overview
  □ See all servers with status dots
  □ Click a server
  □ See resource gauges with live data
  □ See all apps with their status
  □ All looks healthy: page feels calm (no alarming elements)
  □ Session stays active during a full workday

Test 3 — Incident handling:
  □ App crashes (simulate by stopping the container)
  □ App card changes to "Crashed" status in real time
  □ Alert banner appears in header
  □ Alert card appears in the server overview
  □ Alert message is in plain English
  □ Alert shows what to do
  □ Click "View Logs" from the alert
  □ Log viewer shows logs including the crash reason
  □ Click "Restart" from the app card
  □ App restarts successfully
  □ Status badge changes back to "Running" in real time
  □ Alert resolves automatically

Test 4 — Deploy update:
  □ Navigate to an existing app
  □ Click "Deploy New Version"
  □ Change the image tag
  □ Click Deploy
  □ Watch progress in the dialog
  □ Success state shows the live URL
  □ Close the dialog
  □ App card shows the new image and "just now" timestamp

Test 5 — Rollback:
  □ Navigate to an app's Deployments tab
  □ See the list of past deployments
  □ Click "Rollback to this version" on an older deployment
  □ Confirm the rollback
  □ Progress shown
  □ App running on the older version

Test 6 — Log streaming:
  □ Navigate to an app's Logs tab
  □ See last 200 lines immediately
  □ Generate some app traffic (make HTTP requests)
  □ New log lines appear in real time
  □ Scroll up to read old logs: auto-scroll pauses
  □ "Jump to latest" button appears
  □ Click it: returns to bottom and resumes live streaming

Test 7 — Environment variable update:
  □ Navigate to app Overview tab
  □ Click Edit on an environment variable
  □ Enter a new value
  □ Save
  □ Dialog closes
  □ Toast confirms "Variable updated"
  □ "Restart your app to apply changes" suggestion appears
  □ Click "Restart Now"
  □ App restarts
  □ App picks up the new value

Test 8 — Team member access:
  □ Owner invites a team member
  □ Member logs in
  □ Member sees the shared server
  □ Member successfully deploys an app
  □ Member tries to delete the server: sees "403 Forbidden" in plain English
  □ Member cannot see owner-only settings

Test 9 — Browser reconnection:
  □ Open dashboard
  □ Disconnect internet for 30 seconds
  □ WebSocket indicator shows "Reconnecting" (yellow)
  □ Reconnect internet
  □ WebSocket indicator returns to green
  □ Dashboard shows current data without page refresh
  □ No stale data visible after reconnection

Test 10 — Mobile browser:
  □ Open dashboard on a mobile phone
  □ Sidebar is collapsed to icons or hamburger
  □ Server overview is readable
  □ App status is visible
  □ Can navigate to logs page
  □ Logs are readable (with horizontal scroll if needed)
  □ Deploy button is accessible and tappable

When all 10 tests pass, Layer 7 is done.
The MVP is complete and ready for validation users.
```

---

## What Layer 7 Does NOT Do

```
Layer 7 does not:
├── Store server-side state
│   (Next.js pages are stateless — state lives in client stores and the API)
│
├── Communicate directly with agents
│   (all agent communication goes through Layer 6 API and Layer 5B hub)
│
├── Process raw WebSocket messages from agents
│   (Layer 5B processes them and sends dashboard-ready messages)
│
├── Perform any infrastructure operations
│   (deploy, backup, restart — all sent as commands through the API)
│
├── Show technical details to non-technical users
│   (every visible message is translated to plain English)
│
├── Make security decisions
│   (JWT validation happens in the API, routing protection in middleware)
│
└── Implement business logic
    (which user can do what is enforced in Layer 5A and Layer 6)

Layer 7 is presentation and interaction only.
It shows what the system knows.
It sends what the user wants to do.
All intelligence is in the layers below.
```

---

## Summary of All Design Decisions

```
Every design decision in Layer 7 was made with one user in mind:

A non-technical freelancer or small business owner who:
  → Does not know what Docker is
  → Does not know what a reverse proxy is
  → Does not want to learn Linux
  → Has one cheap VPS and wants their app to run reliably
  → Will panic if they see a cryptic error message
  → Will trust the product if it communicates clearly

Every error message was written for that user.
Every empty state was written for that user.
Every loading state was designed so they do not feel lost.
Every action confirmation was designed so they do not feel scared.

The goal is not to hide complexity.
The goal is to handle the complexity on their behalf
and only surface what they need to know to make a decision.
```