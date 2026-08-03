# Layer 3B — Caddy Management: Complete Plan

---

## What Layer 3B Actually Is

```
Layer 3B is the agent's front door.

Every HTTP and HTTPS request to any app on this server
passes through Caddy first.

Caddy's jobs:
  1. Listen on ports 80 and 443 (the only process that does)
  2. Route requests to the correct container based on the domain
  3. Handle TLS certificates automatically (Let's Encrypt)
  4. Terminate HTTPS — containers only deal with plain HTTP internally
  5. Update routing instantly when apps are deployed or removed

The user never configures any of this.
They deploy an app, they get a domain, HTTPS just works.
```

---

## The Mental Model

```
Internet
    │
    │  HTTPS request to myapp.yourdomain.com
    ▼
┌─────────────────────────────────────┐
│           Caddy (port 443)          │
│                                     │
│  myapp.yourdomain.com               │
│    → http://127.0.0.1:32847         │
│                                     │
│  myblog.yourdomain.com              │
│    → http://127.0.0.1:32901         │
│                                     │
│  custom.clientdomain.com            │
│    → http://127.0.0.1:32847         │
└─────────────────────────────────────┘
    │                    │
    ▼                    ▼
App Container A     App Container B
(port 32847         (port 32901
 on host)            on host)
```

---

## How Caddy Is Managed — The Key Decision

```
Two ways to manage Caddy programmatically:

Option A: Config file approach
  → Write a Caddyfile or JSON config to disk
  → Signal Caddy to reload: caddy reload
  → Caddy reads the new config and applies it

Option B: Admin API approach
  → Caddy runs with its admin API enabled (localhost:2019)
  → Send HTTP requests to the admin API to add/remove routes
  → Changes apply immediately — no reload, no downtime
  → Zero config file management

We use Option B — the Admin API.

Why:
  → No file locking issues (two simultaneous deploys writing the same file)
  → Instant updates — route appears in milliseconds
  → No Caddy reload — zero downtime for other apps when one is added
  → Cleaner code — HTTP calls instead of file manipulation
  → Caddy's API is designed exactly for this use case
```

---

## Caddy's Configuration Model (What to Understand)

```
Caddy thinks in terms of:

Apps
  └── HTTP app
        └── Servers
              └── Routes
                    ├── Match (which domain/path triggers this route)
                    └── Handle (what to do — proxy, redirect, static files)

In JSON form, a route looks like:

{
  "match": [{ "host": ["myapp.yourdomain.com"] }],
  "handle": [{
    "handler": "reverse_proxy",
    "upstreams": [{ "dial": "127.0.0.1:32847" }]
  }]
}

This is what Layer 3B creates and destroys
for each app deployment and removal.
```

---

## Step 1 — Caddy Process Management

### How Caddy Runs

```
Caddy runs as a child process managed by the agent.
Not as a separate systemd service.
Not embedded as a Go library (we decided: managed child process).

Why managed child process over systemd service:
  → Agent controls Caddy's lifecycle
  → Agent knows immediately if Caddy crashes
  → Agent can restart Caddy with updated config
  → No systemd dependency for Caddy specifically
  → Simpler to manage admin API lifecycle

Why managed child process over embedded library:
  → Simpler to reason about and debug
  → Caddy crash does not crash the agent
  → Can update Caddy binary independently of agent
  → Standard Caddy binary = standard behavior, no surprises
```

### Step 1A — Caddy Binary

```
Where Caddy comes from:

Option A: Downloaded by the install script alongside the agent binary
Option B: Bundled inside the agent binary as an embedded file
Option C: Downloaded by the agent on first start

Recommendation for MVP: Option A
  → Install script downloads two binaries: agent + caddy
  → Both are verified with checksums
  → Agent knows where to find Caddy: /usr/local/bin/yourplatform-caddy
  → Simpler than embedding, more transparent than runtime download

Binary location: /usr/local/bin/yourplatform-caddy
Data directory:  /var/lib/yourplatform/caddy/
Config file:     /var/lib/yourplatform/caddy/config.json
  (initial config — Caddy persists its own state here)
Certificates:    /var/lib/yourplatform/caddy/certificates/
```

### Step 1B — Starting Caddy

```
When the agent starts:

1. Check Caddy binary exists at expected path
     → If not: fatal error with install instructions

2. Check Caddy version meets minimum
     → Minimum: Caddy 2.7.0
     → Why: stable admin API, good ACME handling

3. Check if Caddy is already running
     → Read PID file at /var/lib/yourplatform/caddy/caddy.pid
     → If PID exists: check if process is actually alive
     → If alive: use it (do not start a second instance)
     → If dead PID: stale PID file, start fresh

4. Start Caddy process:
     → Run: yourplatform-caddy run
             --config /var/lib/yourplatform/caddy/config.json
             --pidfile /var/lib/yourplatform/caddy/caddy.pid
     → Start with initial config (explained in Step 2)
     → Capture Caddy's stdout and stderr
     → Forward to agent's logging system

5. Wait for admin API to be ready
     → Poll GET http://localhost:2019/config/
     → Retry every 500ms for up to 15 seconds
     → If admin API does not respond: Caddy failed to start
     → Read captured stderr to surface the error

6. Store Caddy process handle
     → Agent holds reference to the running process
     → Monitors it continuously
```

### Step 1C — Monitoring Caddy

```
Agent monitors Caddy continuously:

Every 5 seconds:
  → Check if Caddy process is still alive
  → If dead: attempt restart (Step 1D)
  → If alive: optionally ping admin API for health

What makes Caddy die:
  → Out of memory (unlikely but possible)
  → Unhandled panic (Caddy is stable but not impossible)
  → Someone manually kills it
  → Port conflict resolved to Caddy losing (rare)

When Caddy crashes:
  → Agent detects it within 5 seconds
  → Logs the crash with exit code
  → Alerts control plane: "Reverse proxy crashed — HTTPS is down"
  → Attempts restart immediately
  → All apps are unreachable until Caddy restarts
  → Restart usually takes 2-3 seconds
```

### Step 1D — Restarting Caddy

```
When Caddy needs to restart:

1. Attempt graceful shutdown first:
     → Send SIGTERM to Caddy process
     → Wait 5 seconds
     → If still running: SIGKILL

2. Start fresh (Step 1B from step 4)

3. After restart: re-apply all routes
     → Read current deployment state from state.json
     → Re-register every active app's route with Caddy
     → This is why routes are stored in state — we can rebuild Caddy state

4. Verify all routes are back:
     → Query Caddy admin API for route list
     → Compare with expected routes from state
     → Alert if any route failed to re-register

Recovery time target: under 10 seconds from crash to all routes restored
```

### Step 1E — Stopping Caddy

```
When the agent shuts down gracefully:

1. Stop accepting new deploy commands
2. Send SIGTERM to Caddy
3. Wait for Caddy to finish in-flight requests (up to 30 seconds)
4. If not stopped: SIGKILL
5. Remove PID file

Apps are unreachable during agent shutdown and restart.
This is acceptable — the agent restarts quickly via systemd.
```

### Done Condition for Step 1
```
□ Caddy starts when agent starts
□ Admin API responds at localhost:2019 within 5 seconds of start
□ Agent detects Caddy crash within 5 seconds
□ Caddy is restarted automatically after crash
□ All routes are restored after Caddy restart
□ Agent shuts down Caddy cleanly on its own shutdown
□ Stale PID files are handled correctly
□ Caddy stderr is captured and visible in agent logs
```

---

## Step 2 — Initial Caddy Configuration

### What the Initial Config Must Contain

```
When Caddy first starts, it needs a base configuration.
This config does three things:

1. Enables the admin API on localhost:2019
2. Configures global TLS settings (ACME email, staging vs production)
3. Sets up the HTTP→HTTPS redirect for all domains

Everything else (app-specific routes) is added dynamically.
```

### The Initial Config Structure

```
The initial config is a JSON file.
It is written by the agent on first start.
If it already exists, it is used as-is.

Top-level structure:

{
  "admin": {
    ...admin API config...
  },
  "apps": {
    "http": {
      "servers": {
        "main": {
          ...HTTPS server config...
        },
        "redirect": {
          ...HTTP to HTTPS redirect server...
        }
      }
    },
    "tls": {
      ...global TLS / ACME config...
    }
  }
}
```

### Admin API Config

```
The admin API must:
  → Listen on localhost:2019 only (not publicly exposed)
  → Not require authentication for MVP (it is only accessible locally)
  → Enable the /config/ endpoint for config management

Why localhost only:
  The admin API has no auth in the default configuration.
  Exposing it publicly would let anyone reconfigure the proxy.
  Only the agent (running on the same server) should talk to it.

Future enhancement: mutual TLS between agent and Caddy admin API
(Not needed for MVP — localhost binding is sufficient)
```

### TLS Configuration

```
Caddy handles Let's Encrypt automatically.
But we need to configure:

ACME email address:
  → Required by Let's Encrypt for certificate expiry notices
  → Use a platform email: certs@yourplatform.com
  → This email gets notices if ANY user's certificate is about to expire
  → Not the user's email — the platform's ops email

ACME directory (staging vs production):
  → Development/testing: use Let's Encrypt staging
    → Staging certs are not trusted by browsers
    → But: staging has much higher rate limits
    → Use staging during development to avoid hitting production rate limits
  → Production: use Let's Encrypt production
    → Trusted by all browsers
    → Rate limit: 5 certificates per domain per week
    → Only switch to production when the system is working correctly

Certificate storage:
  → Caddy stores certificates automatically
  → Location: /var/lib/yourplatform/caddy/certificates/
  → Caddy handles renewal automatically (renews 30 days before expiry)
  → Agent does not need to manage certificate renewal at all

On-demand TLS:
  → Enable this for custom domains
  → Caddy will automatically get a certificate for any domain
    that points to this server, on the first request
  → This is what makes custom domains work without pre-configuration
  → We add a decision function that checks if the domain is registered
    with us before issuing a certificate
    (prevents certificate issuance for random domains)
```

### HTTP to HTTPS Redirect Server

```
When someone visits http://myapp.yourdomain.com:
  → They should be redirected to https://myapp.yourdomain.com
  → This is standard web behavior

Caddy handles this with a separate server block:
  → Listens on port 80
  → Matches all requests
  → Returns 308 Permanent Redirect to the https:// version

Exception: Let's Encrypt HTTP challenges
  → Let's Encrypt verifies domain ownership via HTTP (port 80)
  → Caddy handles this automatically — it intercepts /.well-known/acme-challenge/
  → The redirect does NOT intercept these paths
  → Caddy knows this — it is built in
```

### Done Condition for Step 2
```
□ Initial config file is written correctly on first start
□ Admin API responds at localhost:2019
□ HTTP request to port 80 redirects to HTTPS
□ Caddy uses Let's Encrypt staging during development
□ Certificate storage directory is correct
□ Config file survives agent restart (Caddy reads it on startup)
```

---

## Step 3 — Route Management

### What a Route Is

```
A route in Caddy = a rule:
"When a request arrives for domain X, proxy it to host:port Y"

Layer 3A tells Layer 3B the host port when a container is created.
Example: app container is exposed on 127.0.0.1:32847

Layer 3B creates a route:
  myapp.yourdomain.com → 127.0.0.1:32847

When app is removed:
  Layer 3B deletes that route
```

### Step 3A — Adding a Route

```
Trigger: Layer 4B (command executor) calls Layer 3B after a successful deploy

Input:
  → Project name: myshop
  → Domain: myshop.yourdomain.com
  → Upstream: 127.0.0.1:32847
  → Custom domains (if any): shop.clientdomain.com

Process:

1. Build the route object
     → Match: the domain(s) this route handles
     → Handle: reverse proxy to the upstream

2. Add route via Caddy admin API
     → PUT /config/apps/http/servers/main/routes/{route_id}
     → Route ID is derived from project name (stable, predictable)
     → PUT is idempotent — adding the same route twice is safe

3. Verify route was accepted
     → GET /config/apps/http/servers/main/routes/{route_id}
     → Confirm it matches what was sent

4. Store route in state.json
     → So routes can be restored after agent/Caddy restart

5. Return result to Layer 4B
     → Success: route is live, domain is accessible
     → Failure: specific error (Caddy rejected config, why)
```

### The Route Object Structure

```
For a standard app deployment:

Match section:
  → List of domains this route handles
  → ["myshop.yourdomain.com"]
  → Multiple domains if custom domains are added:
    ["myshop.yourdomain.com", "shop.clientdomain.com"]

Handle section — what happens to matched requests:
  
  Layer 1 — Headers:
    → Add X-Real-IP header (the real client IP)
    → Add X-Forwarded-Proto: https (app knows it is behind HTTPS)
    → Add X-Forwarded-Host: the original host
    → Remove any X-Forwarded-For spoofing from client
    
    Why these headers matter:
      Apps need to know the real client IP for rate limiting, logging
      Apps need to know it is HTTPS to generate correct URLs
      Without X-Forwarded-Proto, a Django app generates http:// URLs
      even when accessed over HTTPS — breaks mixed content warnings

  Layer 2 — Reverse proxy:
    → Upstream: 127.0.0.1:32847
    → Health check on the upstream (optional for MVP)
    → Timeout settings:
        Dial timeout: 30 seconds (connecting to the container)
        Response header timeout: 60 seconds (app can be slow to start)
        Keep-alive: enabled

Route ID:
  → Derived from project name
  → Example: "yourplatform-myshop"
  → Stable — same project always gets same ID
  → This makes updates idempotent (re-deploying replaces the route)
```

### Step 3B — Updating a Route

```
Trigger: app is redeployed with a new image (host port might change)

The new container might get a different host port.
(The old container was on 32847, new container is on 32901)

Process:
  1. Stop old container (Layer 3A)
  2. Start new container (Layer 3A) — gets new port
  3. Layer 4B tells Layer 3B: update route for myshop
     → New upstream: 127.0.0.1:32901
  4. Layer 3B sends PUT to Caddy admin API with same route ID
     → Same route ID = replaces the existing route
     → Zero downtime: old route is replaced atomically
  5. Update state.json with new port

Timing concern:
  There is a brief window where the old container is stopped
  and the new one is not yet started.
  During this window, requests to the domain get a 502 Bad Gateway.
  
  For MVP: this is acceptable (redeploy = brief downtime)
  Future: blue-green deployment eliminates this window
```

### Step 3C — Removing a Route

```
Trigger: project is deleted, or app is stopped permanently

Process:
  1. Identify route ID for this project
  2. DELETE /config/apps/http/servers/main/routes/{route_id}
  3. Verify route is gone
     → GET the route ID and expect a 404
  4. Remove from state.json
  5. Return result

What happens to the certificate:
  → The certificate for this domain remains in Caddy's storage
  → Caddy will stop renewing it (no active route using it)
  → It expires naturally after 90 days
  → For MVP: do not actively delete certificates (risk of accidentally
    deleting a cert that is still needed)
  → Future: certificate cleanup for permanently removed domains
```

### Step 3D — Route Storage in State

```
Routes stored in state.json alongside container state:

{
  "routes": {
    "yourplatform-myshop": {
      "project": "myshop",
      "domains": ["myshop.yourdomain.com", "shop.clientdomain.com"],
      "upstream": "127.0.0.1:32847",
      "created_at": "2024-01-15T10:00:00Z",
      "updated_at": "2024-01-15T10:00:00Z"
    }
  }
}

On agent/Caddy restart:
  → Read all routes from state.json
  → Re-register each one with Caddy
  → Verify all are accepted
  → Report any failures
```

### Done Condition for Step 3
```
□ Adding a route makes the domain immediately accessible
□ Correct headers are added (X-Real-IP, X-Forwarded-Proto, etc.)
□ Route update (redeploy) changes upstream without error
□ Route removal makes the domain return 404 (not 502)
□ Routes survive agent restart (restored from state.json)
□ Routes survive Caddy restart (restored from state.json)
□ Adding the same route twice does not cause an error
□ Route ID is stable and derived from project name
```

---

## Step 4 — Subdomain Management

### The Platform Subdomain

```
Every deployed app gets a subdomain automatically.
No user configuration needed.

Format: {app-name}.{server-id}.yourplatform.app

Example:
  User creates project "myshop"
  Server ID is "srv-a1b2c3"
  App gets: myshop.srv-a1b2c3.yourplatform.app

Why include server ID:
  → Multiple users might have an app named "myshop"
  → Server ID makes the subdomain globally unique
  → No collision between different users' apps

DNS for this wildcard subdomain:
  → yourplatform.app has a wildcard DNS record: *.yourplatform.app → your control plane
  → Wait — the app is on the USER's server, not your control plane
  
  Correction:
  → The subdomain DNS must point to the USER's server IP
  → This means a wildcard does not work (wildcard points to one IP)
  → You need dynamic DNS records
```

### Subdomain DNS Strategy

```
Two approaches:

Approach A: Static wildcard per server
  → When a server is registered, create a wildcard DNS record:
    *.srv-a1b2c3.yourplatform.app → the server's public IP
  → Every app on that server uses a subdomain of that wildcard
  → myshop.srv-a1b2c3.yourplatform.app → server IP → Caddy → container

  Pros: Simple, one DNS record per server
  Cons: Requires DNS management API, one record per server

Approach B: Single wildcard, proxy layer
  → *.yourplatform.app → your control plane
  → Control plane proxies to the correct server based on subdomain
  → Adds a hop and adds latency
  → Requires your control plane to always be up for app traffic
  
  Not recommended — your control plane outage = all apps unreachable

Recommendation: Approach A
  → Use a DNS provider with an API (Cloudflare is ideal — free)
  → When a server registers: create *.srv-{id}.yourplatform.app → server IP
  → When an app deploys: no additional DNS changes needed
  → Wildcard covers all apps on that server automatically
```

### Subdomain Collision Avoidance

```
Within a single server, two projects cannot have the same name.
The agent enforces this:
  → When a project is created, check existing project names on this server
  → If name is taken: return error to control plane
  → Control plane shows user: "Project name 'myshop' is already in use
    on this server. Choose a different name."

Name sanitization for subdomains:
  → Lowercase only
  → Replace spaces with hyphens
  → Remove any character that is not alphanumeric or hyphen
  → Truncate to 63 characters (DNS label limit)
  → Must start with a letter or number (not a hyphen)
  
  Examples:
    "My Shop!" → "my-shop"
    "API Server v2" → "api-server-v2"
    "---test---" → "test"
```

### Done Condition for Step 4
```
□ Every deployed app gets a subdomain automatically
□ Subdomain follows the correct format
□ Wildcard DNS record is created when server registers
□ Two apps on same server cannot have the same name
□ Name sanitization handles special characters correctly
□ Subdomain is accessible over HTTPS immediately after deploy
```

---

## Step 5 — Custom Domain Routing

### What This Is

```
User has a domain they own: shop.clientbusiness.com
They want their deployed app to be accessible at that domain.
Not just at myshop.srv-a1b2c3.yourplatform.app

This requires:
  1. User points their domain's DNS to the server's IP
  2. User tells the dashboard "use this custom domain for myshop"
  3. Agent tells Caddy to handle requests for this domain
  4. Caddy gets a certificate for the custom domain automatically
```

### Step 5A — The User Flow

```
1. User goes to project settings in dashboard
2. Adds custom domain: shop.clientbusiness.com
3. Dashboard shows DNS instructions:
   "Point shop.clientbusiness.com to your server:
    Type: A Record
    Name: shop
    Value: 1.2.3.4 (your server IP)
    TTL: 3600"
4. User goes to their domain registrar and makes the DNS change
5. User comes back and clicks "Verify DNS"
6. Agent checks if the domain resolves to the server's IP
7. If yes: agent adds the domain to the app's Caddy route
8. Caddy automatically gets an HTTPS certificate for the domain
9. Domain is live with HTTPS
```

### Step 5B — DNS Verification

```
Before adding a custom domain to Caddy, verify DNS is correct.

Why verify:
  → If DNS does not point to this server: certificate issuance will fail
  → Let's Encrypt requires the domain to resolve to the server
    for HTTP challenge to work
  → Failed certificate attempt counts against rate limits
  → Better to check first than fail during cert issuance

Verification process:
  1. Look up the domain's A record
  2. Get the server's own public IP
     → Query an external service: https://api.ipify.org
     → Or: read from server registration data
  3. Compare: does the domain resolve to this server's IP?
  4. If yes: proceed
  5. If no: return error with current resolution
     "shop.clientbusiness.com currently points to 5.6.7.8.
      It needs to point to 1.2.3.4 (this server).
      DNS changes can take up to 48 hours to propagate."

DNS propagation timing:
  → After user makes DNS change, it may take minutes to hours to propagate
  → Agent should recheck every 5 minutes for up to 2 hours
  → Dashboard shows: "Waiting for DNS... (checking every 5 minutes)"
  → When DNS propagates: automatically add the domain, get certificate
  → Notify user: "shop.clientbusiness.com is now live with HTTPS"
```

### Step 5C — Certificate Issuance for Custom Domains

```
Once DNS verification passes and the route is added to Caddy:

Caddy handles certificate issuance automatically via on-demand TLS.
The first HTTPS request to the domain triggers:
  1. Caddy checks: do I have a certificate for this domain?
  2. No → trigger ACME HTTP-01 challenge
  3. Let's Encrypt makes a request to:
     http://shop.clientbusiness.com/.well-known/acme-challenge/{token}
  4. Caddy responds with the correct token (it handles this internally)
  5. Let's Encrypt verifies the response
  6. Let's Encrypt issues the certificate
  7. Caddy stores the certificate
  8. HTTPS works for the domain

Time from first request to working HTTPS: 5-15 seconds
During this time: the first request(s) may be slow or show a brief warning
After this: all requests use the cached certificate

Rate limits to be aware of:
  → Let's Encrypt: 5 certificates per domain per week (production)
  → If the user changes DNS incorrectly and tries multiple times:
    they might hit the rate limit
  → The DNS verification step (Step 5B) prevents most of this
  → Still: do not let users add the same custom domain repeatedly without DNS being correct
```

### Step 5D — Multiple Custom Domains

```
A single app can have multiple custom domains:
  → www.clientbusiness.com
  → clientbusiness.com (apex domain)
  → shop.clientbusiness.com

All of these are added to the same Caddy route:
  Match: ["www.clientbusiness.com", "clientbusiness.com", "shop.clientbusiness.com",
          "myshop.srv-a1b2c3.yourplatform.app"]

The platform subdomain is always included as a fallback.
Even if all custom domains are removed, the platform subdomain still works.

Apex domain consideration:
  → clientbusiness.com (without www) is the apex/root domain
  → Some DNS providers do not support ALIAS/ANAME records for apex domains
  → Cloudflare supports it (CNAME flattening)
  → Other providers require an A record pointing to the IP
  → The agent's DNS instructions should handle both cases
```

### Step 5E — Removing a Custom Domain

```
User removes a custom domain from the project settings.

Process:
  1. Remove the domain from the route's match list in Caddy
  2. Update the route via admin API (PUT with updated domain list)
  3. The certificate for the removed domain remains (let it expire)
  4. Update state.json

The domain is immediately non-functional as a route.
(Anyone visiting it gets a 404 from Caddy — no route matches)
```

### Done Condition for Step 5
```
□ DNS verification correctly detects when domain points to server
□ DNS verification fails clearly when domain points elsewhere
□ Agent waits and retries DNS verification for up to 2 hours
□ Custom domain added to route alongside platform subdomain
□ HTTPS certificate issued automatically on first request
□ Multiple custom domains on one app all work
□ Removing a custom domain updates the route immediately
□ DNS instructions shown to user are correct for their server IP
```

---

## Step 6 — HTTPS Certificate Lifecycle

### What the Agent Needs to Know About Certificates

```
Almost nothing — Caddy handles it all.

What Caddy does automatically:
  → Issues certificates on first HTTPS request to a domain
  → Renews certificates 30 days before expiry
  → Stores certificates across restarts
  → Handles ACME challenges without any config from us

What the agent needs to do:
  → Ensure the certificate storage directory exists and is writable
  → Monitor certificate expiry and alert if renewal is failing
  → Use Let's Encrypt staging during development
  → Switch to Let's Encrypt production for real deployments
```

### Certificate Monitoring

```
Even though Caddy auto-renews, the agent should monitor:

Every 24 hours:
  → Query Caddy admin API for certificate status
  → GET /pki/ca/local — lists managed certificates
  → For each certificate:
    → Check expiry date
    → If expiry < 14 days: alert (renewal should have happened by now)
    → If expiry < 7 days: critical alert

Alert message:
  "HTTPS certificate for shop.clientbusiness.com expires in 6 days
   and automatic renewal appears to have failed.

   This means your site will show certificate errors in 6 days.

   Common causes:
     - Domain no longer points to this server
     - Port 80 is blocked (needed for renewal verification)

   Please check your DNS settings and ensure port 80 is accessible."
```

### Certificate Storage Across Restarts

```
Caddy stores certificates at:
  /var/lib/yourplatform/caddy/certificates/

This directory is:
  → Persistent across agent restarts (it is on the server filesystem)
  → Persistent across Caddy restarts
  → Backed up as part of the server backup (Layer 3C)

On Caddy restart:
  → Caddy reads existing certificates from disk
  → No re-issuance needed for already-certified domains
  → HTTPS works immediately after restart, no waiting for new certs

On server migration (user moves to different VPS):
  → Certificate directory is included in the backup
  → Restored to new server
  → BUT: Let's Encrypt certificates are tied to the domain, not the server
  → New server needs the same domain to point to it for the cert to be valid
  → OR: restore the cert files and update DNS — both work
```

### Done Condition for Step 6
```
□ Certificates persist across Caddy restarts
□ Certificates persist across agent restarts
□ Certificate monitoring runs every 24 hours
□ Alert fires when certificate is within 14 days of expiry
□ Development mode uses Let's Encrypt staging (not production)
□ Production mode uses Let's Encrypt production
□ Certificate directory is included in backup manifest
```

---

## Step 7 — Error Handling and Edge Cases

### 502 Bad Gateway

```
When Caddy has a route for a domain but the upstream is not responding:
  → Caddy returns 502 Bad Gateway to the browser

This happens when:
  → App container is stopped
  → App container is starting (not ready yet)
  → App container crashed
  → App is listening on the wrong port

What the agent does:
  → Caddy access logs include 502 responses
  → Agent monitors Caddy logs for 502 patterns
  → If 502 rate exceeds threshold (10 per minute):
    → Check container health
    → If container is down: surface alert
    → If container is up: app is rejecting connections
  → Plain-English alert: "Your app myshop is returning errors.
    5 out of the last 10 requests failed.
    Check your deployment logs for crash information."
```

### Port Already in Use

```
What if something else is on port 32847 when Caddy tries to proxy to it?

This should not happen because:
  → Layer 3A assigns host ports from the ephemeral range (49152-65535)
  → The port is determined at container creation time by Docker
  → Docker ensures the port is available before binding

But if it does happen:
  → Caddy returns 502
  → Agent detects via log monitoring
  → Recheck container state
  → If container is running on a different port (port changed after restart):
    → Update the Caddy route with the new port
    → This is a reconciliation edge case
```

### Certificate Rate Limit Hit

```
Let's Encrypt limits: 5 certificates per registered domain per week

If a user:
  → Adds and removes the same custom domain multiple times
  → Hits the rate limit

Caddy will fail to issue the certificate and log an error.

Agent response:
  → Detect the ACME rate limit error in Caddy logs
  → Alert user: "HTTPS certificate for shop.clientbusiness.com
     could not be issued due to a Let's Encrypt rate limit.
     This is caused by too many certificate requests for this domain.
     The limit resets on [date - 7 days from first attempt].
     Your app is accessible at its platform subdomain in the meantime."

Prevention:
  → Only attempt certificate issuance when DNS verification passes
  → Cache failed attempts and prevent retry for 24 hours
```

### Caddy Admin API Unavailable

```
What if the agent needs to add a route but Caddy's admin API is down?

Causes:
  → Caddy is starting up (race condition)
  → Caddy crashed and is being restarted
  → Caddy admin API port 2019 is somehow in use

Agent response:
  → Retry the admin API call with exponential backoff
  → 500ms, 1s, 2s, 4s, 8s (5 attempts, ~15 seconds total)
  → If all retries fail:
    → Log the failure
    → Check if Caddy is alive (Step 1C)
    → If dead: restart Caddy, then retry
    → If alive but API unresponsive: this is a Caddy bug — log and alert
  → Queue the route addition
    → When API becomes available, apply the queued change
```

### Done Condition for Step 7
```
□ 502 errors are detected and surface a plain-English alert
□ Alert fires after threshold, not on every single 502
□ Certificate rate limit error is detected and explained clearly
□ Admin API unavailability causes retry, not immediate failure
□ Queued route additions are applied when API recovers
□ All error paths produce actionable messages
```

---

## Step 8 — Caddy Log Management

### What Caddy Logs

```
Caddy produces two types of logs:

1. Access logs — one line per HTTP request
   → Method, domain, path, status code, response time, client IP
   → Useful for: detecting errors, understanding traffic

2. Error logs — Caddy's internal errors
   → Certificate issuance failures
   → Upstream connection failures
   → Config errors
   → Useful for: diagnosing infrastructure problems
```

### Log Format and Capture

```
Configure Caddy to log in JSON format:
  → Machine-readable
  → Agent can parse and extract specific fields
  → Easier to filter for specific error types

Log destination:
  → Caddy writes to stdout (agent captures it as child process output)
  → Agent reads Caddy's output line by line
  → Parses JSON log lines
  → Forwards to control plane as part of server logs

Log retention:
  → Caddy access logs can be verbose (one line per request)
  → For MVP: do not store access logs long-term
  → Store: error logs and certificate events
  → Future: access log analysis for traffic insights
```

### What the Agent Extracts from Caddy Logs

```
Parse every Caddy log line and extract:

502/503 errors:
  → Which upstream, how many, rate
  → Trigger: Layer 4C health monitoring

Certificate events:
  → Successful issuance: record in server events
  → Failed issuance: create alert
  → Renewal success: record in server events
  → Renewal failure: create alert

ACME errors:
  → Rate limit: specific alert (Step 7)
  → DNS challenge failed: alert with DNS check instructions
  → Connection timeout to Let's Encrypt: alert about outbound connectivity
```

### Done Condition for Step 8
```
□ Caddy outputs JSON format logs
□ Agent captures and parses Caddy stdout
□ 502 errors are extracted and forwarded to Layer 4C
□ Certificate events are recorded as server events
□ ACME errors produce specific alerts with actionable messages
□ Log parsing does not block the agent's main loop
   (parsing runs in a separate goroutine)
```

---

## Layer 3B Overall Done Condition

```
The full integration test:

Test 1 — Fresh deploy gets HTTPS subdomain:
  □ App deployed
  □ Platform subdomain created: myshop.srv-abc.yourplatform.app
  □ HTTPS certificate issued automatically
  □ curl https://myshop.srv-abc.yourplatform.app returns 200
  □ HTTP redirects to HTTPS

Test 2 — Custom domain:
  □ User adds shop.clientdomain.com
  □ DNS instructions shown with correct server IP
  □ Agent waits for DNS propagation
  □ DNS propagates: domain added to route
  □ curl https://shop.clientdomain.com returns 200
  □ Certificate issued automatically

Test 3 — Redeploy:
  □ App redeployed with new image
  □ Domain still works during and after redeploy
  □ New container's port correctly updated in Caddy route

Test 4 — Caddy crash recovery:
  □ Kill Caddy process manually
  □ Agent detects within 5 seconds
  □ Caddy restarted
  □ All routes restored from state.json
  □ All apps accessible again within 10 seconds
  □ Certificates still valid (read from disk, not re-issued)

Test 5 — App deleted:
  □ Project deleted
  □ Route removed from Caddy
  □ Domain returns connection refused or 404
  □ Platform subdomain stops working

Test 6 — Agents restart with Caddy running:
  □ Agent restarts (Caddy keeps running separately)
  □ Agent reads state.json
  □ Agent verifies existing routes match state
  □ No route changes needed — all routes already in Caddy

When all 6 tests pass, Layer 3B is done.
Move to Layer 3C — Restic Backups.
```

---

## What Layer 3B Does NOT Do

```
Layer 3B does not:
├── Decide which app to route where (Layer 4B tells it)
├── Manage Docker containers or ports (Layer 3A)
├── Handle WebSocket connections to the control plane (Layer 4A)
├── Load balance across multiple containers (out of scope for MVP)
├── Cache static assets (future enhancement)
├── Rate limit requests (future enhancement)
└── Serve static files directly (apps serve their own static files)
```

---

**Ready for Layer 3C — Restic Backups?**