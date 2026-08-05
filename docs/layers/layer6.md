# Layer 6 — Control Plane API: Complete Plan

---

## What Layer 6 Actually Is

```
Layer 6 is the front door of the control plane.

Every request from the dashboard goes through Layer 6.
Every response the dashboard receives comes from Layer 6.

Layer 6 owns the contract between frontend and backend:
  → What URLs exist
  → What each URL accepts
  → What each URL returns
  → What errors look like
  → How auth is enforced
  → How requests are validated

Layer 6 does NOT own:
  → Auth logic (Layer 5A)
  → Database queries (Layer 5C)
  → WebSocket routing (Layer 5B)
  → Agent communication (Layer 5B)

Layer 6 is the coordinator.
It takes a request, validates it, calls the right layers,
assembles the response, and sends it back.
```

---

## The Mental Model

```
Dashboard (Next.js)
      │
      │  HTTP/HTTPS requests
      │  Authorization: Bearer {jwt}
      │
      ▼
┌─────────────────────────────────────────────────────┐
│                   Layer 6 — API                     │
│                                                     │
│  ┌─────────────┐  ┌──────────────┐  ┌───────────┐  │
│  │   Router    │  │  Middleware  │  │  Handlers │  │
│  │             │  │  Stack       │  │           │  │
│  │  Maps URLs  │  │              │  │  Business │  │
│  │  to handlers│  │  1. Logging  │  │  logic    │  │
│  │             │  │  2. CORS     │  │           │  │
│  │  /api/v1/   │  │  3. Auth     │  │  Calls:   │  │
│  │  /ws/       │  │  4. RateLimit│  │  5A, 5B,  │  │
│  │  /health    │  │  5. Validate │  │  5C       │  │
│  └─────────────┘  └──────────────┘  └───────────┘  │
└─────────────────────────────────────────────────────┘
      │
      ▼
  Layers 5A, 5B, 5C
  (auth, websocket hub, database)
```

---

## Step 1 — Router Setup

### URL Structure

```
All API routes live under /api/v1/

Why versioning from day one:
  → When you break a change: /api/v2/ exists alongside /api/v1/
  → Old clients keep working during transition
  → Dashboard can upgrade at its own pace
  → The cost is near zero (just a prefix)

Route groups:

Public (no auth required):
  POST   /api/v1/auth/register
  POST   /api/v1/auth/login
  POST   /api/v1/auth/refresh
  POST   /api/v1/auth/forgot-password
  POST   /api/v1/auth/reset-password
  GET    /health

Protected (JWT required):
  Auth management:
    POST   /api/v1/auth/logout
    POST   /api/v1/auth/logout-all
    GET    /api/v1/auth/sessions
    DELETE /api/v1/auth/sessions/{session_id}

  User:
    GET    /api/v1/user
    PUT    /api/v1/user
    DELETE /api/v1/user

  Teams:
    GET    /api/v1/teams
    GET    /api/v1/teams/{team_id}
    PUT    /api/v1/teams/{team_id}
    GET    /api/v1/teams/{team_id}/members
    DELETE /api/v1/teams/{team_id}/members/{user_id}
    POST   /api/v1/teams/{team_id}/invitations
    DELETE /api/v1/teams/{team_id}/invitations/{invitation_id}
    POST   /api/v1/invitations/accept

  Servers:
    GET    /api/v1/servers
    POST   /api/v1/servers
    GET    /api/v1/servers/{server_id}
    DELETE /api/v1/servers/{server_id}
    POST   /api/v1/servers/{server_id}/registration-token
    GET    /api/v1/servers/{server_id}/events

  Apps:
    GET    /api/v1/servers/{server_id}/apps
    POST   /api/v1/servers/{server_id}/apps
    GET    /api/v1/servers/{server_id}/apps/{app_id}
    DELETE /api/v1/servers/{server_id}/apps/{app_id}

  Deployments:
    POST   /api/v1/servers/{server_id}/apps/{app_id}/deploy
    POST   /api/v1/servers/{server_id}/apps/{app_id}/rollback
    GET    /api/v1/servers/{server_id}/apps/{app_id}/deployments

  App lifecycle:
    POST   /api/v1/servers/{server_id}/apps/{app_id}/start
    POST   /api/v1/servers/{server_id}/apps/{app_id}/stop
    POST   /api/v1/servers/{server_id}/apps/{app_id}/restart
    GET    /api/v1/servers/{server_id}/apps/{app_id}/logs

  Environment variables:
    GET    /api/v1/servers/{server_id}/apps/{app_id}/env
    PUT    /api/v1/servers/{server_id}/apps/{app_id}/env/{key}
    DELETE /api/v1/servers/{server_id}/apps/{app_id}/env/{key}

  Databases:
    GET    /api/v1/servers/{server_id}/apps/{app_id}/databases
    POST   /api/v1/servers/{server_id}/apps/{app_id}/databases
    DELETE /api/v1/servers/{server_id}/apps/{app_id}/databases/{db_id}

  Domains:
    GET    /api/v1/servers/{server_id}/apps/{app_id}/domains
    POST   /api/v1/servers/{server_id}/apps/{app_id}/domains
    DELETE /api/v1/servers/{server_id}/apps/{app_id}/domains/{domain}
    POST   /api/v1/servers/{server_id}/apps/{app_id}/domains/{domain}/verify

  Metrics:
    GET    /api/v1/servers/{server_id}/metrics
    GET    /api/v1/servers/{server_id}/metrics/history

  Alerts:
    GET    /api/v1/servers/{server_id}/alerts
    POST   /api/v1/servers/{server_id}/alerts/{alert_id}/acknowledge

  Backups:
    GET    /api/v1/servers/{server_id}/backups
    POST   /api/v1/servers/{server_id}/backups
    POST   /api/v1/servers/{server_id}/backups/{backup_id}/restore

  Commands:
    GET    /api/v1/servers/{server_id}/commands
    GET    /api/v1/servers/{server_id}/commands/{command_id}

WebSocket endpoints:
  GET    /ws/browser    ← dashboard real-time connection
  GET    /ws/agent      ← agent connection (Layer 5B)

Agent registration (no user JWT — uses registration token):
  POST   /api/v1/agent/register
```

### Router Implementation

```
Use chi router (already in the base setup).

Router structure:

r := chi.NewRouter()

// Global middleware (runs on every request)
r.Use(RequestID)      // assign unique ID to every request
r.Use(RealIP)         // trust X-Forwarded-For from proxy
r.Use(Logger)         // log every request
r.Use(Recoverer)      // catch panics, return 500
r.Use(SecurityHeaders)// X-Content-Type-Options etc.
r.Use(CORS)           // CORS headers

// Public routes (no auth)
r.Group(func(r chi.Router) {
    r.Use(RateLimiter)
    r.Post("/api/v1/auth/register", handlers.Register)
    r.Post("/api/v1/auth/login", handlers.Login)
    r.Post("/api/v1/auth/refresh", handlers.Refresh)
    r.Post("/api/v1/auth/forgot-password", handlers.ForgotPassword)
    r.Post("/api/v1/auth/reset-password", handlers.ResetPassword)
    r.Get("/health", handlers.Health)
})

// Protected routes (auth required)
r.Group(func(r chi.Router) {
    r.Use(AuthMiddleware)    // validates JWT, attaches user to context

    r.Post("/api/v1/auth/logout", handlers.Logout)
    // ... all protected routes
})

// WebSocket endpoints
r.Get("/ws/browser", wsHandler.BrowserWebSocket)
r.Get("/ws/agent", wsHandler.AgentWebSocket)

// Agent registration (special auth - registration token)
r.Post("/api/v1/agent/register", handlers.AgentRegister)
```

### Done Condition for Step 1
```
□ All routes are registered
□ Public routes have no auth middleware
□ Protected routes all go through AuthMiddleware
□ WebSocket endpoints are registered separately
□ Route conflicts are detected at startup (chi panics on duplicate routes)
□ Unknown routes return 404 JSON (not HTML)
□ Unknown methods return 405 JSON
□ chi router starts without errors
```

---

## Step 2 — Middleware Stack

### Step 2A — Request ID Middleware

```
Assigns a unique ID to every request.

Format: req-{random 12 hex chars}
Example: req-a1b2c3d4e5f6

Added as:
  → Header on the response: X-Request-ID: req-a1b2c3d4e5f6
  → Added to the request context (handlers can read it)
  → Added to every log line for this request

Why it matters:
  → User reports a bug: "I got an error around 3pm"
  → Support asks: "What was the X-Request-ID in the response?"
  → Support finds the exact request in the logs
  → Correlates all log lines from that request
  → Invaluable for debugging production issues

Implementation:
  → chi has this built in: middleware.RequestID
  → Customise the format to use our req- prefix
```

### Step 2B — Logger Middleware

```
Logs every request with structured JSON.

What to log per request:
  → request_id
  → method (GET, POST, etc.)
  → path (/api/v1/servers)
  → status_code (200, 404, 500)
  → duration_ms (how long the handler took)
  → user_id (from auth, if authenticated)
  → ip_address (real IP after X-Forwarded-For)
  → user_agent (truncated to 100 chars)

What NOT to log:
  → Request bodies (may contain passwords, tokens, env vars)
  → Response bodies (may contain sensitive data)
  → Authorization header value (contains JWT)
  → Any query parameters named "token" or "password"

Log format:
{
  "level": "info",
  "ts": "2024-01-15T10:00:00.123Z",
  "msg": "request",
  "request_id": "req-a1b2c3d4e5f6",
  "method": "POST",
  "path": "/api/v1/servers/srv-abc/apps/app-xyz/deploy",
  "status": 200,
  "duration_ms": 47,
  "user_id": "usr-abc123",
  "ip": "1.2.3.4"
}
```

### Step 2C — CORS Middleware

```
Cross-Origin Resource Sharing configuration.

Allowed origins:
  → In development: http://localhost:3000
  → In production: https://app.yourplatform.com

NOT allowed:
  → Wildcard (*): too permissive, allows any site to call your API
  → Other origins: no reason to allow them

Configuration:
  AllowedOrigins: ["https://app.yourplatform.com"]
  AllowedMethods: ["GET", "POST", "PUT", "DELETE", "OPTIONS"]
  AllowedHeaders: ["Authorization", "Content-Type", "X-Request-ID"]
  ExposedHeaders: ["X-Request-ID", "X-Token-Expired"]
  AllowCredentials: false  (we use Authorization header, not cookies)
  MaxAge: 3600  (browser caches preflight for 1 hour)

Preflight handling:
  → OPTIONS requests handled by CORS middleware
  → Returns appropriate headers
  → Returns 204 No Content (empty body)

CORS errors:
  → Request from disallowed origin: 403 Forbidden
  → Missing origin header: allowed (curl, Postman, server-to-server)
```

### Step 2D — Auth Middleware

```
Runs on all protected routes.
Already designed in Layer 5A Step 3.

What it does:
  1. Extract JWT from Authorization: Bearer {token}
  2. Validate signature, expiry, type
  3. Load user from database
  4. Attach user to request context
  5. Call next handler

On failure:
  → Return 401 with specific error code
  → Do NOT call next handler
  → Log: user_id = "unauthenticated" in request log

Context key:
  type contextKey string
  const userContextKey contextKey = "user"

  // Setting:
  ctx := context.WithValue(r.Context(), userContextKey, user)

  // Reading in handler:
  user := r.Context().Value(userContextKey).(*db.User)

Helper function:
  func UserFromContext(ctx context.Context) *db.User
  → Reads from context
  → Panics if not set (indicates middleware was skipped — programming error)
  → Panic is caught by Recoverer middleware, returns 500
```

### Step 2E — Rate Limiter Middleware

```
Applied only to auth endpoints (register, login, forgot-password).

Implementation:
  In-memory sliding window counter.
  Key: "ratelimit:{endpoint}:{ip}"

  type rateLimitEntry struct {
    count     int
    windowEnd time.Time
  }

  Map: sync.Map (concurrent safe)
    key → rateLimitEntry

  On each request:
    1. Build key: "ratelimit:login:1.2.3.4"
    2. Read entry from map
    3. If entry exists and windowEnd is in the future:
         increment count
         if count > limit: return 429
    4. If entry expired or not exists:
         create new entry: count=1, windowEnd=now+15min

Limits:
  POST /api/v1/auth/login:          10 per IP per 15 minutes
  POST /api/v1/auth/register:       5 per IP per hour
  POST /api/v1/auth/forgot-password: 3 per IP per hour

429 Response:
{
  "error": "rate_limit_exceeded",
  "message": "Too many attempts. Try again in 12 minutes.",
  "retry_after_seconds": 720
}

Header: Retry-After: 720

Cleanup:
  → Every hour: iterate map, delete expired entries
  → Prevents unbounded memory growth
```

### Step 2F — Security Headers Middleware

```
Added to every response:

X-Content-Type-Options: nosniff
  → Browser must not MIME-sniff the response
  → Prevents content type confusion attacks

X-Frame-Options: DENY
  → Page cannot be embedded in an iframe
  → Prevents clickjacking

Referrer-Policy: strict-origin-when-cross-origin
  → Full URL sent to same-origin requests
  → Only origin sent to cross-origin HTTPS requests
  → Nothing sent to cross-origin HTTP requests

Permissions-Policy: camera=(), microphone=(), geolocation=()
  → Explicitly disable features the dashboard does not use

Strict-Transport-Security: max-age=31536000; includeSubDomains
  → Browser must use HTTPS for this domain for 1 year
  → Only sent if the request came in over HTTPS

Cache-Control: no-store
  → API responses are not cached by browsers
  → Each request goes to the server fresh
  → Exception: static assets (not handled by the API)
```

### Done Condition for Step 2
```
□ Every response has X-Request-ID header
□ Every request logged with correct fields
□ Passwords and tokens never appear in logs
□ CORS blocks requests from unexpected origins
□ CORS allows requests from dashboard origin
□ Auth middleware attaches user to context on success
□ Auth middleware returns 401 on failure without calling handler
□ Rate limit fires after configured number of attempts
□ Rate limit response includes Retry-After header
□ Security headers present on every response
□ All middleware runs in correct order (RequestID → Logger → Auth → Handler)
```

---

## Step 3 — Request Validation

### Validation Strategy

```
Every handler that accepts a request body:
  1. Reads the body (with size limit)
  2. Decodes JSON
  3. Validates the decoded struct
  4. Returns 400 with specific field errors if invalid
  5. Proceeds to business logic if valid

Validation happens before any database calls.
Never call the database with unvalidated input.

Body size limit:
  → Apply to all requests: MaxBytesReader(r.Body, 1MB)
  → Prevents DoS via huge request bodies
  → 1MB is generous for any API request in this system
  → Deployment configs, env vars, etc. never approach 1MB
```

### Step 3A — Request Decoder Helper

```
Helper function used in every handler:

func DecodeJSON(r *http.Request, dst interface{}) error
  1. Limit body to 1MB
  2. Decode JSON into dst
  3. Handle specific JSON errors:
     → json.SyntaxError → "Invalid JSON in request body"
     → json.UnmarshalTypeError → "Field {field} must be {type}"
     → io.EOF → "Request body is empty"
     → io.ErrUnexpectedEOF → "Request body is incomplete"
  4. Disallow unknown fields:
     → decoder.DisallowUnknownFields()
     → Returns error if request contains fields not in the struct
     → Prevents confusion when client sends wrong field names
  5. Return nil on success, error on failure

Usage in handler:
  var req DeployRequest
  if err := DecodeJSON(r, &req); err != nil {
    RespondError(w, http.StatusBadRequest, "invalid_request", err.Error())
    return
  }
```

### Step 3B — Validation Rules Per Endpoint

```
Each endpoint has explicit validation rules.
These are checked after JSON decoding.

POST /api/v1/auth/register:
  email:
    → Required
    → Valid email format (contains @, has domain)
    → Maximum 254 chars
    → Lowercased before use
  password:
    → Required
    → Minimum 8 characters
    → Not in common passwords list
  name:
    → Required
    → Minimum 2 characters
    → Maximum 100 characters

POST /api/v1/servers/{server_id}/apps/{app_id}/deploy:
  image:
    → Required
    → Valid Docker image reference format
    → Maximum 500 chars
    → Must contain only valid characters [a-z0-9._/-:]
  port:
    → Required
    → Integer between 1 and 65535
    → Not a privileged port (warning if < 1024, not a block)
  memory_limit_mb:
    → Optional (default 512)
    → Integer between 64 and 8192
  cpu_quota_percent:
    → Optional (default 50)
    → Integer between 10 and 100
  project_name:
    → Required
    → Matches [a-z0-9-], starts with letter
    → Maximum 63 characters

POST /api/v1/servers/{server_id}/apps/{app_id}/domains:
  domain:
    → Required
    → Valid hostname format
    → Not already used by another app (database check)
    → Not a yourplatform.app subdomain (reserved)
    → Maximum 253 characters

PUT /api/v1/servers/{server_id}/apps/{app_id}/env/{key}:
  value:
    → Required (can be empty string — that is valid)
    → Maximum 32KB (some env vars are large)
  URL param key:
    → Valid env var name: [A-Z][A-Z0-9_]*
    → Maximum 256 characters
    → Not a reserved name (PORT, YOURPLATFORM)
```

### Step 3C — Validation Error Format

```
When validation fails: always return 400 with this format:

{
  "error": "validation_failed",
  "message": "Request validation failed",
  "fields": {
    "email": "Must be a valid email address",
    "password": "Must be at least 8 characters",
    "name": "Required"
  }
}

Rules for validation errors:
  → Always report ALL field errors (not just the first one)
  → Messages are written for the end user, not a developer
  → Never say "validation failed" without saying what failed
  → Never include the invalid value in the error message
    (the value might be a password or other sensitive data)

Helper:
  type ValidationErrors map[string]string

  func (ve ValidationErrors) Add(field, message string)
  func (ve ValidationErrors) HasErrors() bool
  func (ve ValidationErrors) Response() (int, interface{})
    → Returns 400 + the error structure above
```

### Done Condition for Step 3
```
□ All handlers validate input before any database calls
□ Body size limited to 1MB on all endpoints
□ Invalid JSON returns specific error message
□ Unknown JSON fields return an error
□ All field validation rules enforced
□ All field errors returned at once (not one at a time)
□ Error messages are user-readable
□ Sensitive values never appear in error messages
□ Validation runs before any database queries
```

---

## Step 4 — Response Shaping

### Step 4A — Standard Response Format

```
All API responses follow a consistent structure.

Success responses:
  → HTTP status 200, 201, or 204
  → Body: the data (no wrapper object for simple responses)
  → Example GET /api/v1/user:
    {
      "id": "usr-abc123",
      "email": "user@example.com",
      "name": "Alice Smith",
      "created_at": "2024-01-15T10:00:00Z"
    }

List responses:
  → Always an object with a data array (not a bare array)
  → Includes pagination info even if not paginated yet
  {
    "data": [...],
    "total": 42,
    "page": 1,
    "per_page": 20
  }
  
  Why not a bare array:
  → Cannot add metadata later without breaking clients
  → Consistent structure across all list endpoints
  → Easy to add pagination when needed

Error responses:
  → HTTP 4xx or 5xx
  → Always have this structure:
  {
    "error": "error_code",       ← machine-readable
    "message": "Human message",  ← user-readable
    "request_id": "req-abc123",  ← for support
    "fields": { ... }            ← only for validation errors
  }

No-content responses:
  → HTTP 204 for DELETE operations
  → Empty body
  → No Content-Type header needed
```

### Step 4B — Response Helper Functions

```
Functions used in every handler to send responses:

RespondJSON(w, status, data interface{})
  → Sets Content-Type: application/json
  → Sets status code
  → Encodes data as JSON
  → Writes to response

RespondError(w, status, code, message string)
  → Builds error response struct
  → Adds X-Request-ID from response header
  → Calls RespondJSON

RespondValidationError(w, errors ValidationErrors)
  → Calls RespondError with 400 and fields

RespondNoContent(w)
  → Sets 204, empty body

Respond201(w, data interface{})
  → Sets 201 Created
  → Calls RespondJSON

Helper for common errors:
  Respond400(w, message string)    → bad request
  Respond401(w)                    → unauthorized (fixed message)
  Respond403(w, message string)    → forbidden
  Respond404(w, resource string)   → "{resource} not found"
  Respond409(w, message string)    → conflict
  Respond500(w, r *http.Request)   → internal error (logs full error)
```

### Step 4C — Field Filtering

```
Never return fields the client should not see.

User object as stored in database:
  id, email, name, password_hash, created_at, updated_at

User object returned to the client:
  id, email, name, created_at

password_hash is NEVER returned.
EVER.
Not even to the user themselves.

How to ensure this:
  → Define separate structs for database models and API responses
  → Database model: matches the database exactly
  → API response: only the fields the client should see

Example:
  // Database model
  type User struct {
    ID           string
    Email        string
    Name         string
    PasswordHash string  // never in API response
    CreatedAt    string
    UpdatedAt    string
  }

  // API response struct
  type UserResponse struct {
    ID        string `json:"id"`
    Email     string `json:"email"`
    Name      string `json:"name"`
    CreatedAt string `json:"created_at"`
  }

  // Converter
  func UserToResponse(u *db.User) UserResponse {
    return UserResponse{
      ID:        u.ID,
      Email:     u.Email,
      Name:      u.Name,
      CreatedAt: u.CreatedAt,
    }
  }

Same pattern for:
  → Server: never return agent_secret_hash
  → Refresh token: never return token_hash
  → Registration token: never return after initial creation
  → Commands: never return raw payload (may contain env var values)
```

### Done Condition for Step 4
```
□ All success responses use consistent format
□ All list responses use data wrapper with metadata
□ All error responses include error code and request_id
□ RespondJSON used in all handlers (no manual JSON encoding)
□ Separate response structs for all database models
□ password_hash never appears in any response
□ agent_secret never appears in any response
□ 204 responses have no body
□ Content-Type is always set on responses with bodies
```

---

## Step 5 — Handler Implementation

### Handler Structure Pattern

```
Every handler follows this exact pattern:

func (h *Handler) HandleActionName(w http.ResponseWriter, r *http.Request) {
    // 1. Extract path parameters
    serverID := chi.URLParam(r, "server_id")
    appID := chi.URLParam(r, "app_id")

    // 2. Get authenticated user from context
    user := auth.UserFromContext(r.Context())

    // 3. Check permissions
    role, err := h.db.GetUserRoleForServer(r.Context(), user.ID, serverID)
    if err != nil || role == "" {
        Respond403(w, "You do not have access to this server")
        return
    }

    // 4. Decode and validate request body (for POST/PUT)
    var req SomeRequest
    if err := DecodeJSON(r, &req); err != nil {
        Respond400(w, err.Error())
        return
    }
    if errs := req.Validate(); errs.HasErrors() {
        RespondValidationError(w, errs)
        return
    }

    // 5. Business logic (database calls, hub calls, etc.)
    result, err := h.db.DoSomething(r.Context(), serverID, req)
    if err != nil {
        // check for specific errors
        if errors.Is(err, db.ErrNotFound) {
            Respond404(w, "App")
            return
        }
        Respond500(w, r)
        return
    }

    // 6. Send result
    RespondJSON(w, http.StatusOK, AppToResponse(result))
}
```

### Step 5A — Auth Handlers

```
POST /api/v1/auth/register
  Validation: email, password, name
  Logic:
    → Check email uniqueness (db)
    → Hash password (5A)
    → Create user + personal team + team_member (db transaction)
  Response 201: { "message": "Account created" }

POST /api/v1/auth/login
  Validation: email, password
  Logic:
    → Find user by email (db)
    → Verify password (5A)
    → Generate access token (5A)
    → Generate refresh token (5A)
    → Store refresh token (db)
  Response 200: { access_token, refresh_token, expires_in, user }

POST /api/v1/auth/refresh
  Validation: refresh_token field
  Logic:
    → Hash the provided token
    → Find in db, check not revoked/expired
    → Load user
    → Generate new access token
    → Update last_used_at
  Response 200: { access_token, expires_in }

POST /api/v1/auth/logout
  Auth required
  Validation: refresh_token field
  Logic:
    → Hash the provided token
    → Verify it belongs to the authenticated user
    → Set revoked_at = now
  Response 204

POST /api/v1/auth/forgot-password
  Validation: email field
  Logic:
    → Look up user (do not reveal if found or not)
    → If found: generate reset token, store hash, queue email
  Response 200: { "message": "If account exists, email sent" }

POST /api/v1/auth/reset-password
  Validation: token, new_password
  Logic:
    → Hash the token
    → Find in db, check not used/expired
    → Validate new password
    → Update password hash
    → Mark token used
    → Revoke all refresh tokens for this user
  Response 200: { "message": "Password updated" }
```

### Step 5B — Server Handlers

```
GET /api/v1/servers
  Auth required
  Logic:
    → Find teams user belongs to
    → Find servers for those teams
  Response 200: { data: [server list], total }
  
  Server response shape:
  {
    "id": "srv-abc123",
    "name": "My Production Server",
    "status": "connected",
    "public_ip": "1.2.3.4",
    "agent_version": "1.0.0",
    "last_seen": "2024-01-15T10:00:00Z",
    "os": "ubuntu",
    "os_version": "22.04",
    "cpu_cores": 2,
    "ram_total_mb": 2048,
    "disk_total_gb": 40,
    "created_at": "2024-01-10T08:00:00Z",
    "metrics": {
      "cpu_percent": 45.2,
      "ram_percent": 59.9,
      "disk_percent": 46.0
    }
  }

POST /api/v1/servers (creates the server record before install)
  Auth required
  Validation: name (optional)
  Logic:
    → Create server record with status=pending
    → Link to user's team
  Response 201: { server_id }

POST /api/v1/servers/{server_id}/registration-token
  Auth required
  Permission: owner role on this server's team
  Logic:
    → Verify user owns this server's team
    → Generate registration token
    → Store hash with expiry
    → Build install command string
  Response 200:
  {
    "token": "reg_abc123...",
    "install_command": "curl -fsSL ... | sudo sh -s -- --token=reg_abc123...",
    "expires_at": "2024-01-15T11:00:00Z"
  }

GET /api/v1/servers/{server_id}
  Auth required
  Permission: any team member
  Logic:
    → Get server record
    → Get latest metrics (server_metrics_latest)
    → Get container states
    → Get active alerts
  Response 200: full server detail object

DELETE /api/v1/servers/{server_id}
  Auth required
  Permission: owner role only
  Logic:
    → Check: is agent currently connected?
      → If yes: send disconnect command, wait for ack, or force disconnect
    → Delete server record (cascades to apps, deployments, metrics, etc.)
    → Hub disconnects agent (Layer 5B)
  Response 204

GET /api/v1/servers/{server_id}/events
  Auth required
  Permission: any team member
  Query params: page, per_page, type (filter by event type)
  Logic:
    → Query server_events with filters
  Response 200: { data: [events], total, page, per_page }
```

### Step 5C — App and Deployment Handlers

```
POST /api/v1/servers/{server_id}/apps/{app_id}/deploy
  Auth required
  Permission: any team member (deploy is not restricted to owner)
  Validation: image, port, memory_limit_mb, etc.
  Logic:
    → Create deployment record (status: pending)
    → Build command payload
    → Send command to agent via hub (Layer 5B)
    → Return immediately (do not wait for deploy to complete)
  Response 202 Accepted:
  {
    "command_id": "cmd-abc123",
    "deployment_id": "dep-xyz789",
    "message": "Deploy started. Watch progress in the dashboard."
  }

  Why 202 not 200:
    → The work has not completed yet
    → It is accepted and in progress
    → Client tracks progress via WebSocket (Layer 5B)
    → 200 would imply the work is done

POST /api/v1/servers/{server_id}/apps/{app_id}/rollback
  Auth required
  Permission: any team member
  Validation:
    → target: "previous" or "specific"
    → deployment_id: required if target is "specific"
  Logic:
    → If target is "previous":
        Find the last successful deployment for this app
        Get its image
    → If target is "specific":
        Find the specified deployment
        Verify it belongs to this app
        Get its image
    → Build and send rollback command
  Response 202: { command_id, message }

GET /api/v1/servers/{server_id}/apps/{app_id}/deployments
  Auth required
  Permission: any team member
  Query params: page, per_page (default 20)
  Logic:
    → Query deployments ordered by triggered_at DESC
  Response 200: { data: [deployments], total, page, per_page }
  
  Deployment shape:
  {
    "id": "dep-abc123",
    "image": "nginx:latest",
    "status": "success",
    "started_at": "2024-01-15T10:00:00Z",
    "completed_at": "2024-01-15T10:00:47Z",
    "duration_ms": 47000,
    "triggered_by": {
      "id": "usr-abc123",
      "name": "Alice Smith"
    },
    "domain": "myshop.srv-abc.yourplatform.app"
  }

POST /api/v1/servers/{server_id}/apps/{app_id}/start
POST /api/v1/servers/{server_id}/apps/{app_id}/stop
POST /api/v1/servers/{server_id}/apps/{app_id}/restart
  Auth required
  Permission: any team member
  Logic:
    → Build and send lifecycle command
  Response 202: { command_id }

GET /api/v1/servers/{server_id}/apps/{app_id}/logs
  Auth required
  Permission: any team member
  Query params: lines (default 200), container (default: app)
  Logic:
    → Send get_logs command to agent
    → Wait for result (short timeout: 15 seconds)
    → Return the log lines
  Response 200:
  {
    "lines": [
      { "timestamp": "...", "stream": "stdout", "text": "..." }
    ],
    "container": "yourplatform_myshop_app",
    "total_lines": 200
  }

  Note: this is a synchronous endpoint (waits for agent response)
  because log history fetch should be fast.
  Live streaming uses WebSocket (not this endpoint).
```

### Step 5D — Environment Variable Handlers

```
GET /api/v1/servers/{server_id}/apps/{app_id}/env
  Auth required
  Permission: any team member
  Logic:
    → Query env_var_keys for this app
    → Return key names only, never values
  Response 200:
  {
    "keys": [
      { "key": "DATABASE_URL", "is_auto": true, "created_at": "..." },
      { "key": "STRIPE_KEY", "is_auto": false, "created_at": "..." }
    ]
  }

PUT /api/v1/servers/{server_id}/apps/{app_id}/env/{key}
  Auth required
  Permission: any team member
  Validation:
    → key: valid env var name (from URL param)
    → value: required (can be empty string)
    → restart_after: boolean (optional, default false)
  Logic:
    → Validate key is not reserved
    → Send set_env command to agent via hub
    → Agent writes to env file, acks
    → Upsert env_var_keys record (key name only in DB)
    → If restart_after: also send restart command
  Response 202: { command_id, message: "Variable updated" }

DELETE /api/v1/servers/{server_id}/apps/{app_id}/env/{key}
  Auth required
  Permission: any team member
  Logic:
    → Validate key is not auto-generated (cannot delete DATABASE_URL)
    → Send delete_env command to agent
    → Delete from env_var_keys
  Response 202: { command_id }
```

### Step 5E — Metrics Handlers

```
GET /api/v1/servers/{server_id}/metrics
  Auth required
  Permission: any team member
  Logic:
    → Read from server_metrics_latest (instant, always fresh)
    → Read container states for this server
  Response 200:
  {
    "server": {
      "cpu_percent": 45.2,
      "ram_used_mb": 1228,
      "ram_total_mb": 2048,
      "ram_percent": 59.9,
      "disk_used_gb": 18.4,
      "disk_total_gb": 40.0,
      "disk_percent": 46.0,
      "load_1min": 0.82,
      "recorded_at": "2024-01-15T10:00:30Z"
    },
    "containers": [
      {
        "project": "myshop",
        "role": "app",
        "status": "running",
        "cpu_percent": 12.3,
        "ram_used_mb": 187,
        "ram_limit_mb": 512,
        "restart_count": 0
      }
    ]
  }

GET /api/v1/servers/{server_id}/metrics/history
  Auth required
  Permission: any team member
  Query params:
    → period: "1h", "24h", "7d", "30d" (default "24h")
    → metric: "cpu", "ram", "disk" (default "cpu")
  Logic:
    → "1h": return raw data from last hour
    → "24h": return hourly data from last 24 hours
    → "7d": return hourly data from last 7 days
    → "30d": return daily data from last 30 days
    → Select correct granularity from server_metrics
  Response 200:
  {
    "metric": "cpu",
    "period": "24h",
    "granularity": "hourly",
    "data": [
      { "timestamp": "2024-01-14T10:00:00Z", "value": 34.5 },
      { "timestamp": "2024-01-14T11:00:00Z", "value": 45.2 },
      ...24 points...
    ]
  }
```

### Step 5F — Backup Handlers

```
GET /api/v1/servers/{server_id}/backups
  Auth required
  Permission: any team member
  Query params: page, per_page
  Logic:
    → Query backups ordered by started_at DESC
  Response 200:
  {
    "data": [
      {
        "id": "bkp-abc123",
        "status": "success",
        "size_new_bytes": 47185920,
        "size_total_bytes": 892416000,
        "verified": true,
        "started_at": "2024-01-15T02:00:00Z",
        "completed_at": "2024-01-15T02:04:23Z",
        "duration_seconds": 263,
        "project_results": [
          { "project": "myshop", "status": "success" },
          { "project": "myblog", "status": "success" }
        ]
      }
    ],
    "total": 14,
    "schedule": {
      "enabled": true,
      "hour_utc": 2,
      "next_backup_at": "2024-01-16T02:00:00Z"
    }
  }

POST /api/v1/servers/{server_id}/backups
  Auth required
  Permission: any team member
  Logic:
    → Send run_backup command to agent
  Response 202: { command_id, message: "Backup started" }

POST /api/v1/servers/{server_id}/backups/{backup_id}/restore
  Auth required
  Permission: owner only (restore is destructive)
  Validation:
    → project_name: which project to restore
    → confirmed: must be exactly true
  Logic:
    → Verify backup belongs to this server
    → Verify confirmed is true
    → Send restore command to agent
  Response 202: { command_id, message: "Restore started" }
```

### Step 5G — Alert Handlers

```
GET /api/v1/servers/{server_id}/alerts
  Auth required
  Permission: any team member
  Query params:
    → status: "active", "resolved", "all" (default "active")
    → severity: "warning", "critical", "all" (default "all")
  Logic:
    → Query alerts with filters
    → Order by fired_at DESC
  Response 200: { data: [alerts], total }

POST /api/v1/servers/{server_id}/alerts/{alert_id}/acknowledge
  Auth required
  Permission: any team member
  Logic:
    → Verify alert belongs to this server
    → Set status = acknowledged
    → Set acknowledged_by = user.ID
    → Set acknowledged_at = now
  Response 200: { "message": "Alert acknowledged" }
```

### Done Condition for Step 5
```
□ Register creates user + team + team_member in one transaction
□ Login rate limited per IP
□ Deploy returns 202 (not 200) with command_id
□ Rollback to previous finds the correct previous deployment
□ Rollback to specific verifies deployment belongs to this app
□ Env GET returns only key names never values
□ Env PUT sends to agent before updating database
□ Metrics history uses correct granularity per time period
□ Backup restore requires confirmed: true
□ Backup restore permission is owner-only
□ Alert acknowledge updates all three fields atomically
□ All handlers return correct HTTP status codes
```

---

## Step 6 — Agent Registration Endpoint

```
POST /api/v1/agent/register

This endpoint is special:
  → No JWT auth (agent does not have a user JWT)
  → Auth via registration token in the request body
  → Called by the agent during Layer 1 registration

Validation:
  → token: required, must start with "reg_"
  → agent_version: required, semantic version string
  → server_info: required object with os, arch, ram_total_mb, etc.

Logic:
  1. Hash the provided token
  2. Find in server_registration_tokens:
     → Not found: 401 "Invalid registration token"
     → Already used: 401 "Registration token already used"
     → Expired: 401 "Registration token expired. Generate a new one."
  3. All checks pass:
     → Generate agent_id: "agt-{random hex}"
     → Generate agent_secret: 32 random bytes hex encoded
     → Hash the agent_secret
     → Find or create server record linked to the token's user
     → Update server record:
         agent_id = agent_id
         agent_secret_hash = hash
         public_ip = request IP
         os, os_version, arch, cpu_cores, ram_total_mb, disk_total_gb
         status = pending (becomes connected when WebSocket connects)
     → Mark token as used
     → Create server event: "Server registered"
  4. Return 200:
     {
       "agent_id": "agt-abc123",
       "agent_secret": "raw secret here",
       "server_id": "srv-xyz789",
       "websocket_url": "wss://ws.yourplatform.com/agent",
       "control_plane_version": "1.0.0"
     }
     
  Note: agent_secret is returned ONCE here, never again.
  Agent stores it in config.yaml.
  Control plane stores only the hash.
```

### Done Condition for Step 6
```
□ Valid token registers the server and returns credentials
□ Invalid token returns 401 with specific reason
□ Already-used token returns 401
□ Expired token returns 401 with instruction to generate new one
□ agent_secret returned only in this one response
□ Server record updated with hardware info from registration
□ Token marked as used atomically with server record update
□ agent_secret is not logged
□ Server event recorded for audit trail
```

---

## Step 7 — WebSocket Upgrade Endpoints

```
GET /ws/browser
  Auth: JWT in Authorization header OR ?token= query param
  Logic:
    1. Layer 5A validates JWT
    2. Upgrade to WebSocket
    3. Register with hub (Layer 5B)
    4. Return (handler returns, goroutines handle the connection)
  
  Why the handler returns immediately:
    → The WebSocket goroutines (reader/writer) run independently
    → The HTTP handler just sets up the connection and hands it off
    → chi router stays available for other requests

GET /ws/agent
  Auth: agent_id + agent_secret
  Logic:
    1. Extract agent_id from query: ?agent_id=agt-abc123
    2. Extract secret from Authorization: Bearer {secret}
    3. Look up server by agent_id
    4. Hash and compare secret
    5. If valid: upgrade to WebSocket, register with hub
    6. If invalid: return 401 before upgrade
```

### Done Condition for Step 7
```
□ Browser WebSocket upgrade validates JWT first
□ Browser WebSocket upgrade fails with 401 if JWT invalid
□ Agent WebSocket upgrade validates agent credentials first
□ Agent WebSocket upgrade fails with 401 if credentials invalid
□ After upgrade: handler returns and goroutines take over
□ WebSocket endpoint does not block the HTTP server
□ Multiple simultaneous WebSocket connections work correctly
```

---

## Step 8 — Health Endpoint

```
GET /health
  No auth required
  Always returns quickly
  Used by: load balancers, uptime monitors, deployment checks

Response 200:
{
  "status": "ok",
  "version": "1.0.0",
  "database": "ok",
  "uptime_seconds": 86400,
  "connected_agents": 12,
  "connected_browsers": 8
}

If database is unavailable:
Response 503:
{
  "status": "degraded",
  "version": "1.0.0",
  "database": "error",
  "error": "database unavailable"
}

Why include database check:
  → Health endpoint should reflect actual health
  → A control plane that cannot reach its database is not healthy
  → Load balancers use this to route traffic away from unhealthy instances

Database check:
  → SELECT 1 (simplest possible query)
  → Timeout: 1 second
  → If it fails: report degraded

Note: connected_agents and connected_browsers come from hub stats
  → Hub exposes a method: hub.Stats() → {agents, browsers}
  → Health handler calls this
```

### Done Condition for Step 8
```
□ GET /health returns 200 when everything is fine
□ GET /health returns 503 when database is unreachable
□ Health check includes database ping
□ Database ping has 1 second timeout
□ connected_agents and connected_browsers are accurate
□ Health endpoint requires no auth
□ Health endpoint responds in under 100ms always
```

---

## Step 9 — API Versioning Strategy

```
Current: all routes under /api/v1/

When to create /api/v2/:
  → When a breaking change is needed
  → Breaking change = existing client code would break

What is a breaking change:
  → Removing a field from a response
  → Renaming a field
  → Changing a field's type
  → Changing the meaning of a field
  → Removing an endpoint
  → Changing authentication requirements

What is NOT a breaking change:
  → Adding a new field to a response
  → Adding a new optional field to a request
  → Adding a new endpoint
  → Changing error message text (not the error code)

For MVP:
  → Only /api/v1/ exists
  → Document: "v1 will remain stable"
  → When v2 is needed: both run simultaneously
  → Old dashboard continues using v1
  → New dashboard uses v2
  → v1 deprecated with 6 months notice
  → v1 removed after all clients migrate
```

---

## Layer 6 Overall Done Condition

```
The full test sequence:

Test 1 — Auth flow:
  □ Register → Login → get JWT → access protected endpoint
  □ Use JWT on protected endpoint: 200
  □ Use expired JWT: 401 with X-Token-Expired header
  □ Use refresh token: get new JWT
  □ Logout: refresh token revoked
  □ Use revoked refresh token: 401

Test 2 — Server registration:
  □ Create server record via POST /api/v1/servers
  □ Generate registration token
  □ POST /api/v1/agent/register with token: get credentials
  □ Same token again: 401

Test 3 — Deploy command round trip:
  □ POST /api/v1/servers/{id}/apps/{id}/deploy
  □ Returns 202 with command_id
  □ Dashboard receives progress via WebSocket
  □ Dashboard receives result via WebSocket
  □ GET /api/v1/servers/{id}/apps/{id}/deployments shows the deploy

Test 4 — Permission enforcement:
  □ Invite member to team
  □ Member can deploy: 202
  □ Member cannot delete server: 403
  □ Member cannot restore backup: 403

Test 5 — Validation:
  □ POST deploy with invalid image format: 400 with field error
  □ POST deploy with memory_limit_mb as string: 400
  □ POST deploy with unknown field: 400
  □ POST register with common password: 400

Test 6 — Error handling:
  □ GET nonexistent server: 404
  □ DELETE server without owner role: 403
  □ POST without required field: 400 with specific field error
  □ POST to unknown route: 404 JSON (not HTML)
  □ Trigger a panic in a handler: 500 with request_id

Test 7 — Health:
  □ GET /health returns 200 with all fields
  □ Database disconnected: GET /health returns 503

Test 8 — Concurrent requests:
  □ 50 simultaneous GET /api/v1/servers: all return 200
  □ No race conditions (run with -race flag)
  □ SQLite does not return SQLITE_BUSY errors

When all 8 tests pass, Layer 6 is done.
Move to Layer 7 — Frontend Dashboard.
```

---

## What Layer 6 Does NOT Do

```
Layer 6 does not:
├── Implement auth logic (Layer 5A does)
├── Route WebSocket messages (Layer 5B does)
├── Query the database directly (Layer 5C query layer does)
├── Talk to agents (Layer 5B hub does via WebSocket)
├── Send emails (notification service does)
├── Execute deployments (agent Layer 4B does)
├── Store metrics (Layer 5C does, triggered by Layer 5B)
└── Make product decisions about what users can do
    (permissions are enforced but the rules come from Layer 5A)

Layer 6 is the HTTP surface area.
It translates HTTP into function calls on other layers.
It translates results back into HTTP responses.
Clean input, clean output, no business logic of its own.
```

---

**Ready for Layer 7 — Frontend Dashboard?**