# Layer 5A — Auth and Sessions: Complete Plan

---

## What Layer 5A Actually Is

```
Layer 5A is the gatekeeper.

Every request to the control plane API goes through Layer 5A first.
Every WebSocket connection from a browser is validated by Layer 5A.
Every agent connection is authenticated by Layer 5A.

Three different types of identity to manage:

Type 1: Human users (browser)
  → Register with email + password
  → Login to get a JWT
  → JWT attached to every API request
  → Sessions expire, get refreshed

Type 2: Agents (server-side)
  → Authenticated via agent_id + agent_secret
  → Connected via WebSocket
  → No expiry (permanent until revoked)
  → Different auth path from humans

Type 3: Registration tokens (one-time)
  → Used only during server registration (Layer 1)
  → Single use, 1 hour expiry
  → Converts to agent credentials on use
  → Already designed in Layer 1 plan

Layer 5A owns all three.
```

---

## The Mental Model

```
                    ┌─────────────────────────────────┐
                    │         Control Plane           │
                    │                                 │
Browser Request     │  ┌──────────────────────────┐  │
─────────────────► │  │      Layer 5A            │  │
JWT in header       │  │      Auth Middleware      │  │
                    │  │                          │  │
                    │  │  1. Extract token        │  │
                    │  │  2. Validate JWT         │  │
                    │  │  3. Load user            │  │
                    │  │  4. Check permissions    │  │
                    │  │  5. Attach to request    │  │
                    │  └──────────┬───────────────┘  │
                    │             │                   │
                    │             ▼                   │
                    │  ┌──────────────────────────┐  │
                    │  │   Route Handler          │  │
                    │  │   Already knows who      │  │
                    │  │   the user is and what   │  │
                    │  │   they can do            │  │
                    │  └──────────────────────────┘  │
                    └─────────────────────────────────┘

Agent WebSocket     ┌─────────────────────────────────┐
─────────────────► │  Layer 5A validates              │
agent_id + secret   │  agent_secret hash               │
                    │  Links to server record          │
                    │  Attaches server context         │
                    └─────────────────────────────────┘
```

---

## Step 1 — User Registration

### What Registration Does

```
Takes: email, password, name
Returns: nothing (user must log in separately)

Why not return a token on registration:
  → Forces email verification before first login (future)
  → Cleaner separation: register is account creation, login is auth
  → Prevents accidental auto-login on re-registration attempts
  → Standard pattern, users expect it
```

### Step 1A — Input Validation

```
Email validation:
  → Must contain @ and a domain with a TLD
  → Lowercase before storing (treat as case-insensitive)
  → Maximum 254 characters (RFC 5321 limit)
  → Must not already exist in the database
  → Do NOT use a complex regex — simple checks are enough
    Most "invalid" emails that pass a simple check are caught
    by the email verification step (future feature)

Password validation:
  → Minimum 8 characters
  → No maximum length (some managers generate 100+ char passwords)
  → No complexity rules (uppercase + special char rules are theater)
    Why no complexity rules:
      → NIST 800-63B explicitly recommends AGAINST complexity rules
      → They lead to predictable patterns (Password1!)
      → Length is far more important than complexity
      → Users write complex passwords down — defeating the purpose
  → Check against a list of common passwords:
      "password", "12345678", "qwerty123", etc.
      Reject these with a clear message
  → Do NOT restrict special characters (breaks password managers)

Name validation:
  → Minimum 2 characters
  → Maximum 100 characters
  → Any Unicode character allowed (international names)
  → Trim leading/trailing whitespace
```

### Step 1B — Password Hashing

```
Algorithm: bcrypt

Why bcrypt:
  → Designed specifically for password hashing
  → Deliberately slow (cannot be parallelized efficiently)
  → Has a work factor that can be increased as hardware improves
  → Battle-tested since 1999
  → Available in Go's standard crypto library (golang.org/x/crypto)

Work factor for MVP: 12
  → At work factor 12: ~300ms to hash on a modern CPU
  → 300ms is imperceptible to a user logging in
  → But: attacker trying 1 billion passwords = 300 million seconds
  → Increase to 13 or 14 when servers get faster

What bcrypt does:
  → Takes: plaintext password + cost factor
  → Generates: random salt internally
  → Returns: hash that includes the salt and cost factor
  → Example output: $2a$12$R9h/cIPz0gi.URNNX3kh2OPST9/PgBkqquzi.Ss7KIUgO2t0jWMUW

  The hash includes everything needed to verify:
    → The algorithm version ($2a$)
    → The cost factor (12)
    → The salt (22 chars after the cost)
    → The hash (31 chars at the end)
  → Store just this string in the database

What NOT to do:
  → Do not hash before sending to server (hash on server only)
  → Do not add your own salt (bcrypt handles this)
  → Do not use MD5, SHA-1, SHA-256 for passwords (too fast)
  → Do not use PBKDF2 (acceptable but bcrypt is simpler in Go)
  → Never store plaintext passwords
  → Never log passwords even "for debugging"
```

### Step 1C — Registration Flow

```
POST /api/v1/auth/register

1. Parse request body
     → email, password, name
     → Return 400 if any field missing

2. Validate email format
     → Return 400 with specific field error if invalid

3. Validate password rules
     → Return 400 with specific field error if invalid

4. Validate name
     → Return 400 with specific field error if invalid

5. Check email uniqueness
     → SELECT id FROM users WHERE email = ?
     → If found: return 409 Conflict
     → Response: { "error": "An account with this email already exists" }
     → Do NOT say "email already registered" without offering
       password reset — users forget they have accounts

6. Hash the password
     → bcrypt.GenerateFromPassword([]byte(password), 12)
     → This takes ~300ms — normal

7. Generate user ID
     → UUID v4 (random)
     → Example: 550e8400-e29b-41d4-a716-446655440000

8. Insert user record
     → INSERT INTO users (id, email, name, password, created_at, updated_at)
     → Use a database transaction in case of concurrent writes

9. Return 201 Created
     → Response body: { "message": "Account created successfully" }
     → Do NOT return user data or tokens here
     → Do NOT log the password hash (log that registration occurred)
```

### Step 1D — Database Schema for Users

```sql
CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

CREATE INDEX idx_users_email ON users(email);
```

### Done Condition for Step 1
```
□ Registration with valid data creates user record
□ Registration with duplicate email returns 409 with clear message
□ Password is bcrypt hashed before storage
□ Hash is never logged or returned in responses
□ Email is lowercased before storage and lookup
□ Common passwords ("password123") are rejected
□ Name allows Unicode characters
□ Registration returns 201 with no token or user data
□ Concurrent registrations with same email: only one succeeds
```

---

## Step 2 — Login and Token Issuance

### JWT Architecture Decision

```
Two common approaches:

Approach A: Short-lived access token + long-lived refresh token
  → Access token: 15-60 minutes
  → Refresh token: 7-30 days
  → Dashboard silently gets new access token using refresh token
  → If refresh token expires: user must log in again
  → Refresh tokens stored in database (can be revoked)

Approach B: Single long-lived token
  → Token lives 24-72 hours
  → User logs in again after expiry
  → Simpler but: cannot revoke individual sessions

Choice: Approach A (access + refresh tokens)

Why:
  → If an access token is stolen: it expires in 24 hours maximum
  → Logout actually works (invalidate the refresh token in database)
  → Can log out "all devices" (delete all refresh tokens for user)
  → Access token is stateless (no database lookup per request)
  → Only refresh token requests hit the database
```

### Step 2A — The Login Flow

```
POST /api/v1/auth/login

1. Parse request body
     → email, password

2. Look up user by email (case-insensitive)
     → SELECT * FROM users WHERE email = LOWER(?)
     → If not found: do NOT say "email not found"
       Return: "Invalid email or password" (same message for both cases)
       Why: prevents email enumeration (attacker learns which emails exist)

3. Verify password
     → bcrypt.CompareHashAndPassword(hash, password)
     → If wrong: same "Invalid email or password" message
     → If correct: proceed
     → Timing note: bcrypt.Compare takes ~300ms whether it succeeds or fails
       This is intentional — prevents timing attacks

4. Check if account is suspended (future)
     → For MVP: no suspension — all accounts are active

5. Generate access token (Step 2B)

6. Generate refresh token (Step 2C)

7. Store refresh token in database

8. Return 200 OK:
   {
     "access_token": "eyJhbGc...",
     "refresh_token": "eyJhbGc...",
     "token_type": "Bearer",
     "expires_in": 86400,
     "user": {
       "id": "550e8400...",
       "email": "user@example.com",
       "name": "Alice Smith"
     }
   }
```

### Step 2B — Access Token Generation

```
JWT structure:

Header:
{
  "alg": "HS256",    ← HMAC-SHA256
  "typ": "JWT"
}

Payload (claims):
{
  "sub": "550e8400-e29b-41d4-a716-446655440000",  ← user ID
  "email": "user@example.com",
  "name": "Alice Smith",
  "iat": 1705312800,    ← issued at (Unix timestamp)
  "exp": 1705399200,    ← expires at (issued + 24 hours)
  "type": "access"      ← distinguishes from refresh tokens
}

Signing:
  → Algorithm: HS256 (HMAC with SHA-256)
  → Secret: JWT_SECRET from environment variable
  → Secret must be: minimum 32 bytes, random, never committed to git

Why HS256 for MVP:
  → Simpler than RS256 (asymmetric)
  → Only the control plane verifies tokens (not distributed)
  → RS256 needed if third parties verify tokens — not our case
  → Can upgrade to RS256 later without breaking anything

Access token expiry: 24 hours
  → Users stay logged in through a full day
  → Not so long that stolen tokens cause extended damage
  → Dashboard silently refreshes before expiry

What NOT to put in the JWT payload:
  → Password hash (never)
  → Payment information (never)
  → Large amounts of data (JWT is sent with every request)
  → Permissions lists (fetch from DB on access — can change at runtime)
```

### Step 2C — Refresh Token Generation

```
Refresh token structure:
  → NOT a JWT
  → A random 32-byte hex string: 
    "rt_a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4"
  → Prefixed with "rt_" for identification

Why not a JWT for refresh tokens:
  → Refresh tokens must be stored in the database (to enable revocation)
  → If it is stored in DB: no benefit to JWT structure
  → Random string is simpler and equally secure
  → Harder to accidentally use as an access token

Storage:
  → Store the HASH of the refresh token in the database
  → Same reason as registration tokens: breach protection

Database schema for refresh tokens:
```

```sql
CREATE TABLE refresh_tokens (
    id            TEXT PRIMARY KEY,
    token_hash    TEXT NOT NULL UNIQUE,
    user_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at    TEXT NOT NULL,
    expires_at    TEXT NOT NULL,
    last_used_at  TEXT,
    user_agent    TEXT,     ← browser/client info for session display
    ip_address    TEXT,     ← IP for security display
    revoked_at    TEXT      ← NULL until revoked
);

CREATE INDEX idx_refresh_tokens_user ON refresh_tokens(user_id);
CREATE INDEX idx_refresh_tokens_hash ON refresh_tokens(token_hash);
```

```
Refresh token expiry: 30 days
  → Long enough that active users never need to re-login
  → Short enough that inactive accounts auto-expire
  → Sliding expiry: each use extends it another 30 days

What "30 days" means for the user:
  → If they use the app at least once per month: stay logged in forever
  → If they abandon the app for 30 days: must log in again
  → This is the standard behavior of most web apps
```

### Step 2D — Token Refresh Flow

```
POST /api/v1/auth/refresh

Called silently by the dashboard when access token is about to expire.
Dashboard checks token expiry before each request.
If expiry < 5 minutes away: silently call refresh first.

1. Parse request body
     → refresh_token: "rt_a1b2c3..."

2. Hash the provided refresh token

3. Look up hash in refresh_tokens table
     → If not found: 401 Unauthorized
     → If revoked_at is not null: 401 Unauthorized
     → If expires_at is past: 401 Unauthorized

4. Load the associated user
     → If user not found (deleted account): 401 Unauthorized

5. Generate new access token (Step 2B)

6. Optionally rotate refresh token:
     → Generate a new refresh token
     → Revoke the old one
     → Return new refresh token
     → Why: refresh token rotation means a stolen refresh token
       is invalidated after the attacker uses it
       (legitimate user refreshes first, invalidating attacker's copy)
     → For MVP: rotation is optional, implement if time allows

7. Update last_used_at on the refresh token

8. Return new access token (and optionally new refresh token)
```

### Done Condition for Step 2
```
□ Login with correct credentials returns access + refresh tokens
□ Login with wrong password returns same error as wrong email
□ Access token contains correct claims
□ Access token expires in 24 hours
□ Refresh token is stored as a hash (not plaintext)
□ Refresh token expires in 30 days
□ Refresh endpoint issues new access token
□ Expired refresh token returns 401
□ Revoked refresh token returns 401
□ Concurrent logins from different devices all work independently
```

---

## Step 3 — JWT Validation Middleware

### What the Middleware Does

```
Every protected API endpoint runs this middleware before the handler.

The middleware:
  1. Extracts the JWT from the Authorization header
  2. Validates the JWT signature and expiry
  3. Loads the user from the token claims
  4. Attaches the user to the request context
  5. Passes control to the handler

Handlers never deal with auth logic directly.
They just read the user from the context.
```

### Step 3A — Token Extraction

```
Where tokens come from:

Primary: Authorization header
  → Format: "Authorization: Bearer eyJhbGc..."
  → Extract: split on space, take second part
  → Reject if: header missing, not "Bearer" scheme, token part missing

Alternative: Query parameter (for WebSocket connections)
  → WebSockets cannot set custom headers in browsers
  → Token passed as: ?token=eyJhbGc...
  → Only accepted on WebSocket upgrade endpoints
  → Not accepted on regular HTTP endpoints (security: tokens in URLs
    are logged by web servers and proxies)

Never from cookies (for MVP):
  → Cookies require CSRF protection (complex)
  → JWT in Authorization header is standard for APIs
  → Single-page apps (Next.js) handle this naturally
```

### Step 3B — Token Validation Steps

```
Given a token string, validate in this exact order:

1. Parse the JWT structure
     → Must have three parts (header.payload.signature)
     → Base64 decode each part
     → If malformed: 401 "Invalid token format"

2. Verify the signature
     → Use JWT_SECRET from environment
     → HMAC-SHA256 of header + payload must match signature
     → If mismatch: 401 "Invalid token"
     → This catches: tampered tokens, tokens from other services

3. Check the algorithm
     → Must be HS256
     → Reject "none" algorithm (classic JWT attack)
     → Reject RS256 (we only issue HS256)

4. Check expiry (exp claim)
     → exp must be in the future
     → Allow 30 seconds of clock skew (servers can drift slightly)
     → If expired: 401 "Token expired"
     → Include: "X-Token-Expired: true" header in response
       Frontend uses this to trigger a refresh instead of logout

5. Check token type
     → type claim must be "access"
     → Reject refresh tokens used as access tokens

6. Load user from sub claim
     → SELECT id, email, name FROM users WHERE id = ?
     → For MVP: do this on every request (simple, correct)
     → If user not found: 401 "User not found"
       (Handles case where account was deleted but token still valid)

7. Attach user to request context
     → ctx := context.WithValue(ctx, userKey, user)
     → Handler reads: user := ctx.Value(userKey).(*User)

Optimization note:
  For MVP: database lookup on every request is fine
  SQLite is fast for this (single file, in-process)
  Future: add a short-lived in-memory cache if needed
```

### Step 3C — Error Responses

```
All auth errors return 401 Unauthorized with a clear body:

Missing token:
  {
    "error": "authentication_required",
    "message": "Please log in to access this resource"
  }

Invalid token (malformed, wrong signature):
  {
    "error": "invalid_token",
    "message": "Your session is invalid. Please log in again."
  }

Expired token:
  {
    "error": "token_expired",
    "message": "Your session has expired. Please log in again."
  }
  Header: X-Token-Expired: true  ← frontend can auto-refresh

User not found (account deleted):
  {
    "error": "user_not_found",
    "message": "Account not found. Please register or log in."
  }

Why clear messages:
  → This is a dashboard for end users, not a public API
  → Users are small business owners, not developers
  → They need to understand what to do next
  → Security through obscurity is not a concern here
```

### Done Condition for Step 3
```
□ Valid JWT passes middleware and handler receives user
□ Missing Authorization header returns 401
□ Malformed JWT returns 401
□ Expired JWT returns 401 with X-Token-Expired header
□ Tampered JWT (wrong signature) returns 401
□ Refresh token used as access token is rejected
□ "none" algorithm is rejected
□ User not in database returns 401
□ User is correctly attached to request context
□ Handler can read user from context without any auth logic
```

---

## Step 4 — Logout and Session Management

### Step 4A — Logout

```
POST /api/v1/auth/logout
Requires: valid access token

Three levels of logout:

Level 1: Logout this device
  → Revoke the refresh token used by this session
  → Access token remains valid until it expires (24 hours)
  → This is the standard logout behavior

  Payload: { "refresh_token": "rt_abc123..." }

  Why access token stays valid:
    → JWTs cannot be invalidated without a database lookup
    → We could add a blocklist, but that defeats the purpose of JWTs
    → 24 hours is acceptable for MVP
    → For truly sensitive apps: shorter access token expiry (15 min)

  Sequence:
    1. Hash the provided refresh token
    2. Find in database
    3. Check it belongs to the authenticated user (prevent cross-user revocation)
    4. Set revoked_at = now
    5. Return 200

Level 2: Logout all devices
  → Revoke ALL refresh tokens for this user
  → User is logged out everywhere

  Endpoint: POST /api/v1/auth/logout-all
  Sequence:
    1. UPDATE refresh_tokens SET revoked_at = now()
       WHERE user_id = ? AND revoked_at IS NULL
    2. Return 200: { "message": "Logged out from all devices" }

Level 3: Automatic expiry
  → No action needed
  → Tokens with expires_at in the past are considered revoked
  → Cleanup job: delete expired tokens weekly
```

### Step 4B — Active Sessions View

```
Users should be able to see where they are logged in.

GET /api/v1/auth/sessions

Returns:
[
  {
    "id": "token-record-id",
    "created_at": "2024-01-15T10:00:00Z",
    "last_used_at": "2024-01-15T14:30:00Z",
    "expires_at": "2024-02-14T10:00:00Z",
    "user_agent": "Mozilla/5.0 (Mac; Chrome/120.0)",
    "ip_address": "1.2.3.4",
    "current": true    ← is this the token making this request?
  },
  {
    "id": "token-record-id-2",
    "created_at": "2024-01-10T08:00:00Z",
    "last_used_at": "2024-01-12T16:00:00Z",
    "expires_at": "2024-02-09T08:00:00Z",
    "user_agent": "Mozilla/5.0 (iPhone; Safari/17.0)",
    "ip_address": "5.6.7.8",
    "current": false
  }
]

User can revoke any session from this view.
DELETE /api/v1/auth/sessions/{session_id}
```

### Done Condition for Step 4
```
□ Logout revokes the refresh token
□ Revoked refresh token cannot generate new access tokens
□ Logout-all revokes all refresh tokens for the user
□ Sessions list shows all active sessions with device info
□ User can revoke any specific session
□ Current session is marked in the sessions list
□ Expired tokens are cleaned up periodically
```

---

## Step 5 — RBAC (Role-Based Access Control)

### The Role Model for MVP

```
For MVP: two roles only.

Role 1: Owner
  → Created when registering a server
  → Full access to everything on that server
  → Can invite members
  → Can remove members
  → Can delete the server
  → Can manage billing

Role 2: Member
  → Invited by the owner
  → Can deploy, restart, view logs
  → Can NOT delete the server
  → Can NOT manage billing
  → Can NOT remove other members
  → Can NOT change server settings

Why only two roles for MVP:
  → More roles = more complexity = more bugs
  → Real users mostly need: "can do everything" or "can deploy but not delete"
  → Add more granular roles when users ask for them
  → Do not over-engineer permissions upfront
```

### Step 5A — Team and Membership Schema

```sql
CREATE TABLE teams (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    owner_id    TEXT NOT NULL REFERENCES users(id),
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE TABLE team_members (
    id          TEXT PRIMARY KEY,
    team_id     TEXT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role        TEXT NOT NULL DEFAULT 'member',  -- 'owner' or 'member'
    invited_by  TEXT REFERENCES users(id),
    joined_at   TEXT NOT NULL,

    UNIQUE(team_id, user_id)
);

CREATE TABLE server_team (
    server_id   TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    team_id     TEXT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,

    PRIMARY KEY (server_id, team_id)
);

CREATE INDEX idx_team_members_user ON team_members(user_id);
CREATE INDEX idx_team_members_team ON team_members(team_id);
```

```
How it connects:

User registers → a personal team is created automatically
                  (team name = user's name + "'s Team")
User registers a server → server is linked to their personal team
User invites someone → creates a team_member record

A server belongs to one team.
A team has one owner and any number of members.
```

### Step 5B — Permission Checking

```
Where permission checks happen:
  → In the HTTP handler, after auth middleware runs
  → Auth middleware identifies WHO the user is
  → Permission check identifies WHAT they can do

Pattern in every handler:

func HandleDeleteServer(w http.ResponseWriter, r *http.Request) {
    user := auth.UserFromContext(r.Context())
    serverID := chi.URLParam(r, "serverID")

    // Check permission
    if err := permissions.RequireRole(r.Context(), user, serverID, "owner"); err != nil {
        http.Error(w, "Only the server owner can delete servers", 403)
        return
    }

    // Proceed with deletion...
}

The RequireRole function:
  1. Load the server record → get team_id
  2. Look up team_member record for user + team
  3. Check role matches required role
  4. Return error if not authorized

This is a database lookup.
For MVP: acceptable (one lookup per protected operation)
```

### Step 5C — Permission Matrix

```
Operation                     Owner   Member
─────────────────────────────────────────────
View server dashboard         ✓       ✓
Deploy an app                 ✓       ✓
Rollback a deployment         ✓       ✓
Restart / stop an app         ✓       ✓
View logs                     ✓       ✓
Add environment variable      ✓       ✓
Create a database             ✓       ✓
Run a backup                  ✓       ✓
Restore from backup           ✓       ✓ *
Add custom domain             ✓       ✓
View server metrics           ✓       ✓
View alerts                   ✓       ✓
─────────────────────────────────────────────
Delete a database             ✓       ✗
Delete an app                 ✓       ✗
Delete the server             ✓       ✗
Change server settings        ✓       ✗
Invite team members           ✓       ✗
Remove team members           ✓       ✗
View billing                  ✓       ✗
─────────────────────────────────────────────

* Restore requires explicit confirmation regardless of role
```

### Step 5D — Team Invitation Flow

```
Owner invites a user:

POST /api/v1/teams/{team_id}/invitations
{
  "email": "newmember@example.com",
  "role": "member"
}

1. Verify requester is owner of this team

2. Check: is email already a member?
     → If yes: return "already a member"

3. Check: does a user with this email exist?
   Case A: user exists
     → Create team_member record directly
     → Send notification email: "You have been added to X team"
   Case B: user does not exist
     → Create a pending invitation record
     → Send invitation email with signup link
     → On signup: invitation is automatically applied

Invitation record schema:
CREATE TABLE invitations (
    id          TEXT PRIMARY KEY,
    team_id     TEXT NOT NULL REFERENCES teams(id),
    email       TEXT NOT NULL,
    role        TEXT NOT NULL,
    token       TEXT NOT NULL UNIQUE,  ← random token in email link
    invited_by  TEXT NOT NULL REFERENCES users(id),
    created_at  TEXT NOT NULL,
    expires_at  TEXT NOT NULL,         ← 7 days
    accepted_at TEXT
);

Invitation acceptance:
  → User clicks link in email: /invitations/accept?token=abc123
  → If not logged in: redirect to login/register, then back to accept
  → Verify token is valid and not expired
  → Create team_member record
  → Mark invitation as accepted
  → Redirect to team's server dashboard
```

### Done Condition for Step 5
```
□ Owner can perform all operations
□ Member cannot delete server, database, or manage team
□ Permission check happens in every sensitive handler
□ RequireRole lookup is correct and returns proper errors
□ Team is created automatically on user registration
□ Server is linked to owner's team on registration
□ Invitation email is sent for new users
□ Existing users are added directly to team
□ Invitation token is single-use and expires in 7 days
□ Member accepts invitation and gains correct access
```

---

## Step 6 — Agent Authentication

### How Agents Auth Differently from Users

```
Agents are not humans.
They do not log in with a password.
They have long-lived credentials that were issued during registration.

Agent credentials:
  → agent_id: "agt-abc123" (public identifier)
  → agent_secret: "sec-xyz789abc..." (secret, 32 random bytes)

These were issued during Layer 1 registration.
The agent stores them in /etc/yourplatform/config.yaml (chmod 600).
The control plane stores the hashed secret in the servers table.
```

### Step 6A — Agent WebSocket Authentication

```
When agent connects to the WebSocket endpoint:

URL: wss://ws.yourplatform.com/agent

Authentication is done during the HTTP upgrade request:
  Header: Authorization: Bearer {agent_secret}
  Query:  ?agent_id=agt-abc123

The WebSocket upgrade handler (before upgrading):

1. Extract agent_id from query parameter

2. Extract agent_secret from Authorization header

3. Look up server record by agent_id:
     SELECT * FROM servers WHERE agent_id = ?
     → If not found: reject with 401, do not upgrade

4. Verify the secret:
     → Hash the provided secret
     → Compare with stored hash
     → If mismatch: reject with 401

5. Check server is not suspended/deleted:
     → If server.status = 'deleted': reject with 403
     → Message: "This server has been removed from your account"

6. Upgrade the WebSocket connection

7. Register the connection in the WebSocket Hub (Layer 5B):
     → hub.RegisterAgent(serverID, connection)

8. Mark server as connected:
     UPDATE servers SET status = 'connected', last_seen = now()

This entire auth flow happens before the WebSocket upgrade completes.
If auth fails: the upgrade is rejected, no WebSocket connection is made.
The agent sees a 401/403 HTTP response and handles it (Step 2B of Layer 4A).
```

### Step 6B — Authenticating Agent Commands

```
Every message the agent sends over the WebSocket
is already implicitly authenticated by the connection itself.

Once a WebSocket connection is established and authenticated:
  → Every message on that connection is from that agent
  → No per-message auth needed
  → The connection IS the auth

This is correct because:
  → WebSocket connections are long-lived but authenticated once
  → If the connection is broken: reconnection requires re-auth
  → The connection cannot be hijacked (TLS protects the channel)
  → An attacker cannot inject messages into an established connection

What the control plane tracks per connection:
  → server_id: which server this connection represents
  → connected_at: when the connection was established
  → last_message_at: last activity (for timeout detection)
```

### Step 6C — Agent Secret Rotation

```
Scenario: user suspects their server is compromised.
They want to revoke the agent's access and re-register.

Flow:
  1. User clicks "Revoke Agent" in dashboard
  2. Control plane marks the server's agent_secret as revoked
  3. Control plane closes the agent's WebSocket connection
     (Layer 5B: hub.DisconnectAgent(serverID))
  4. Agent detects disconnection, tries to reconnect
  5. Agent presents old secret → rejected (401)
  6. Agent enters auth-failed state (Layer 4A Step 2B)
  7. User generates a new registration token from dashboard
  8. User runs the install command again (Layer 1)
     → Install script detects agent already installed
     → With --force flag: reinstalls with new token
  9. Agent re-registers with new token → gets new credentials
  10. Agent reconnects and is fully functional

For MVP: this is a manual process.
Future: automated secret rotation on a schedule.
```

### Done Condition for Step 6
```
□ Agent WebSocket connection authenticated before upgrade
□ Invalid agent_id returns 401 before upgrade
□ Wrong agent_secret returns 401 before upgrade
□ Deleted/suspended server returns 403 before upgrade
□ Authenticated connection is registered in WebSocket Hub
□ Server status updated to connected on successful auth
□ Per-message auth not needed (connection is the auth)
□ Agent revocation closes the WebSocket connection
□ Revoked agent cannot reconnect until re-registered
```

---

## Step 7 — Password Reset

### Why Include This in MVP

```
Users forget passwords.
If they cannot reset: they create a new account.
Now you have duplicate accounts and confused users.
Password reset is table stakes for any auth system.
```

### Step 7A — Reset Request Flow

```
POST /api/v1/auth/forgot-password
{ "email": "user@example.com" }

1. Find user by email
     → Whether email exists or not: return the SAME response
     → Response: { "message": "If an account exists with this email,
                    a reset link has been sent." }
     → Why same response: prevents email enumeration
       Attacker cannot learn which emails have accounts

2. If user found (internally, do not reveal this):
     a. Generate reset token: 32 random bytes, hex encoded
        Prefix: "pw_"
     b. Hash the token
     c. Store in database:

CREATE TABLE password_resets (
    id          TEXT PRIMARY KEY,
    token_hash  TEXT NOT NULL UNIQUE,
    user_id     TEXT NOT NULL REFERENCES users(id),
    created_at  TEXT NOT NULL,
    expires_at  TEXT NOT NULL,    ← 1 hour from creation
    used_at     TEXT
);

     d. Send email (via control plane email service)
        Subject: "Reset your YourPlatform password"
        Body: "Click to reset: https://app.yourplatform.com/reset?token=pw_abc..."
        Expires in 1 hour.
     e. Old unused reset tokens for this user: leave them
        They expire naturally. No need to revoke old ones.
        (One hour window is short enough this is fine)

3. Return 200 with the same message regardless
```

### Step 7B — Reset Completion Flow

```
POST /api/v1/auth/reset-password
{
  "token": "pw_abc123...",
  "new_password": "newpassword123"
}

1. Hash the provided token

2. Look up in password_resets:
     → If not found: 400 "Invalid or expired reset link"
     → If used_at is not null: 400 "This reset link has already been used"
     → If expires_at is past: 400 "This reset link has expired.
        Please request a new one."

3. Validate new password (same rules as registration)

4. Load the user

5. Hash the new password with bcrypt

6. Update the user's password_hash

7. Mark reset token as used (used_at = now)

8. Revoke all existing refresh tokens for this user
     → Security: someone else might have been using the account
     → Force re-login everywhere

9. Return 200: { "message": "Password updated. Please log in." }
```

### Done Condition for Step 7
```
□ Forgot password returns same response whether email exists or not
□ Reset token is stored as a hash
□ Reset link in email contains raw token (not hash)
□ Token expires after 1 hour
□ Used token cannot be used again
□ New password is validated before updating
□ All existing sessions revoked after password reset
□ Reset password returns generic success message (not "account found")
```

---

## Step 8 — Security Hardening

### Step 8A — Rate Limiting Auth Endpoints

```
Auth endpoints are primary targets for brute force attacks.

Endpoints that need rate limiting:
  POST /api/v1/auth/login
  POST /api/v1/auth/forgot-password
  POST /api/v1/auth/reset-password

Rate limit strategy for login:
  → Per IP address: 10 attempts per 15 minutes
  → Per email: 5 attempts per 15 minutes
  → On limit hit: 429 Too Many Requests
  → Response includes: "Too many login attempts. Try again in X minutes."
  → Do NOT lock the account (legitimate user locked out = bad UX)

Implementation for MVP:
  → In-memory rate limit store (lost on restart, acceptable)
  → Use a simple sliding window counter
  → Key: "login:{ip}" or "login:{email}"
  → Increment on each attempt
  → Expire after 15 minutes

Why in-memory for MVP:
  → No Redis needed (zero infrastructure requirement)
  → Works for single-instance deployment (MVP is single control plane)
  → If control plane restarts: counters reset (acceptable)
  → Switch to persistent store when scaling to multiple instances
```

### Step 8B — Secure Headers

```
Every response from the control plane API should include:

Security headers:

X-Content-Type-Options: nosniff
  → Prevents browser from MIME-sniffing the response
  → Should be on every response

X-Frame-Options: DENY
  → Prevents the dashboard from being loaded in an iframe
  → Protects against clickjacking attacks

Referrer-Policy: strict-origin-when-cross-origin
  → Limits what referrer information is sent to third parties

Content-Security-Policy: (for the dashboard, not the API)
  → Restricts what resources the dashboard can load
  → Prevents XSS by restricting script sources

CORS headers:
  → Access-Control-Allow-Origin: https://app.yourplatform.com
  → Only allow requests from the dashboard's origin
  → NOT wildcard (*) — too permissive
  → Access-Control-Allow-Methods: GET, POST, PUT, DELETE, OPTIONS
  → Access-Control-Allow-Headers: Authorization, Content-Type
```

### Step 8C — Sensitive Data Handling

```
Rules enforced everywhere in the control plane:

Never log:
  → Passwords (hashed or not)
  → JWT tokens
  → Refresh tokens
  → Agent secrets
  → Environment variable values
  → Registration tokens

Always log (for debugging):
  → User IDs (not emails)
  → Server IDs
  → Request IDs
  → Action types (not payloads)
  → Timestamps

Response bodies never include:
  → Password hashes
  → Agent secrets (after initial issuance)
  → Other users' data
  → Internal system details (stack traces, file paths)

Error responses:
  → In development: include error details
  → In production: generic message + error code
  → Log full error server-side with request ID
  → Response to client: { "error": "internal_error", "request_id": "req-abc" }
  → User can give request_id to support for investigation
```

### Done Condition for Step 8
```
□ Login endpoint rate limited: 10 attempts per IP per 15 minutes
□ Forgot password endpoint rate limited
□ Rate limit response includes time until reset
□ Security headers present on all responses
□ CORS only allows dashboard origin
□ Passwords never appear in logs
□ JWT tokens never appear in logs
□ Production error responses are generic
□ Error request_id is logged server-side for support
```

---

## Layer 5A Overall Done Condition

```
The full test sequence:

Test 1 — Registration and login:
  □ Register with valid data: 201 response
  □ Login immediately after: access + refresh tokens returned
  □ Duplicate email registration: 409 with clear message
  □ Login with wrong password: same error as wrong email
  □ Access token validates on protected endpoints

Test 2 — Token lifecycle:
  □ Access token works for 24 hours
  □ Access token expired after 24 hours: 401 with X-Token-Expired
  □ Refresh endpoint issues new access token
  □ Expired refresh token: 401
  □ Logout: refresh token revoked, cannot refresh anymore

Test 3 — Role-based access:
  □ Owner can delete server
  □ Member cannot delete server (403)
  □ Member can deploy successfully
  □ Invitation flow: member accepts, can access server

Test 4 — Agent authentication:
  □ Agent with valid credentials: WebSocket upgrade succeeds
  □ Agent with wrong secret: 401, no upgrade
  □ Agent with unknown agent_id: 401, no upgrade
  □ Revoked agent: 401 on reconnect

Test 5 — Password reset:
  □ Forgot password for existing email: email sent
  □ Forgot password for non-existing email: same response
  □ Reset with valid token: password updated, all sessions revoked
  □ Reset with expired token: 400 with clear message
  □ Reset with used token: 400 with clear message

Test 6 — Rate limiting:
  □ 10 failed logins in 15 minutes: 429 response
  □ Wait 15 minutes: login works again
  □ Rate limit per IP is independent of per email

Test 7 — Security:
  □ Security headers present on all responses
  □ CORS blocks requests from other origins
  □ No sensitive data in any log line
  □ Tampered JWT is rejected

When all 7 tests pass, Layer 5A is done.
Move to Layer 5B — WebSocket Hub.
```

---

## What Layer 5A Does NOT Do

```
Layer 5A does not:
├── Handle WebSocket connections (Layer 5B)
├── Store metrics or health data (Layer 5C)
├── Send emails directly (uses an email service)
├── Make authorization decisions for agent commands
│   (agents are trusted once authenticated via WebSocket)
├── Handle billing or subscription limits
│   (those are enforced in Layer 6 handlers)
└── Manage what the user can deploy
    (any authenticated owner can deploy anything on their server)

Layer 5A answers one question:
  "Who is making this request, and are they allowed to?"
Everything else is someone else's job.
```

---

**Ready for Layer 5B — WebSocket Hub?**