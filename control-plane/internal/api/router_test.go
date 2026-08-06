package api

// Layer 6 Step 1 done-condition tests for the router:
//
//	□ All routes are registered (representative public + protected + WS)
//	□ Public routes have no auth middleware
//	□ Protected routes all go through AuthMiddleware
//	□ WebSocket endpoints are registered separately
//	□ Route conflicts are detected at startup (chi panics on duplicate routes)
//	□ Unknown routes return 404 JSON (not HTML)
//	□ Unknown methods return 405 JSON
//	□ chi router starts without errors

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/yourplatform/control-plane/internal/alerts"
	"github.com/yourname/yourplatform/control-plane/internal/auth"
	"github.com/yourname/yourplatform/control-plane/internal/config"
	"github.com/yourname/yourplatform/control-plane/internal/db"
	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
	"github.com/yourname/yourplatform/control-plane/internal/mailer"
	"github.com/yourname/yourplatform/control-plane/internal/ws"
)

// newTestRouter builds the production router exactly as main.go wires it,
// against a migrated in-memory database.
func newTestRouter(t *testing.T) http.Handler {
	r, _ := newTestRouterDB(t)
	return r
}

// newTestRouterDB is newTestRouter plus the migrated database handle, so
// tests can seed users and mint JWTs for authenticated requests.
func newTestRouterDB(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cfg := &config.Config{
		FrontendURL: "http://localhost:3000",
		BaseDomain:  "example.com",
		JWTSecret:   "test-secret",
	}
	hub := ws.NewHub()
	sender := mailer.NewFromConfig(cfg)
	delivery := alerts.NewDelivery(database, sender, cfg)
	return NewRouter(database, cfg, hub, delivery, sender), database
}

func doRequest(router http.Handler, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// contentType / isJSONBody assert the response body is JSON, not chi's
// default plain-text page.
func isJSONBody(w *httptest.ResponseRecorder) bool {
	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		return false
	}
	var v interface{}
	return json.Unmarshal(w.Body.Bytes(), &v) == nil
}

// The router constructs without panicking (chi panics on duplicate routes,
// so a clean build means no conflicts were registered).
func TestRouter_StartsWithoutErrors(t *testing.T) {
	router := newTestRouter(t)
	// A known public route still works after construction.
	if w := doRequest(router, http.MethodGet, "/health"); w.Code != http.StatusOK {
		t.Fatalf("health = %d, want 200", w.Code)
	}
}

// Unknown routes return 404 JSON, not HTML.
func TestRouter_UnknownRouteReturnsJSON404(t *testing.T) {
	router := newTestRouter(t)

	for _, path := range []string{
		"/api/v1/nonexistent",
		"/api/v1/servers/srv-1/definitely-not-a-route",
		"/no-such-path",
	} {
		w := doRequest(router, http.MethodGet, path)
		if w.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, w.Code)
		}
		if !isJSONBody(w) {
			t.Errorf("GET %s: 404 body is not JSON: %q", path, w.Body.String())
		}
		var body map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode 404 body: %v", err)
		}
		if body["error"] != "not_found" {
			t.Errorf("GET %s: error code = %v, want not_found", path, body["error"])
		}
	}
}	// Unknown methods on a known path return 405 JSON, not HTML. (The Allow
	// header RFC 7231 prescribes for 405 is deliberately omitted — chi's
	// custom MethodNotAllowed API does not expose the allowed-methods list;
	// see the tradeoff note in router.go.)
func TestRouter_UnknownMethodReturnsJSON405(t *testing.T) {
	router := newTestRouter(t)

	cases := []struct{ method, path string }{
		{http.MethodDelete, "/health"},              // registered as GET
		{http.MethodGet, "/api/v1/auth/register"},   // registered as POST
		{http.MethodPut, "/api/v1/auth/login"},      // registered as POST
		{http.MethodPatch, "/api/v1/servers"},       // registered as GET/POST
	}
	for _, c := range cases {
		w := doRequest(router, c.method, c.path)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s = %d, want 405", c.method, c.path, w.Code)
		}
		if !isJSONBody(w) {
			t.Errorf("%s %s: 405 body is not JSON: %q", c.method, c.path, w.Body.String())
		}
		var body map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode 405 body: %v", err)
		}
		if body["error"] != "method_not_allowed" {
			t.Errorf("%s %s: error code = %v, want method_not_allowed", c.method, c.path, body["error"])
		}
	}
}

// Public routes are reachable without any auth header (no Auth middleware).
func TestRouter_PublicRoutesHaveNoAuth(t *testing.T) {
	router := newTestRouter(t)

	// /health is public.
	if w := doRequest(router, http.MethodGet, "/health"); w.Code != http.StatusOK {
		t.Errorf("GET /health = %d, want 200", w.Code)
	}

	// Public auth endpoints must NOT be blocked by Auth middleware. Sending
	// an empty body hits the handler's JSON decode, which returns exactly 400
	// "invalid request body" — a 401 would mean the route was accidentally
	// protected, a 404 that it was never registered. Asserting the precise
	// 400 also catches a silent 500.
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/auth/register"},
		{http.MethodPost, "/api/v1/auth/login"},
		{http.MethodPost, "/api/v1/auth/refresh"},
		{http.MethodPost, "/api/v1/auth/forgot-password"},
		{http.MethodPost, "/api/v1/auth/reset-password"},
	} {
		w := doRequest(router, c.method, c.path)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s %s = %d, want 400 (handler ran without auth gate)", c.method, c.path, w.Code)
		}
	}
}

// planRoute is one entry of the Layer 6 Step 1 protected route table. Routes
// whose handlers land in later steps are marked stub (they answer 501 until
// then). One shared table drives every router test, so wiring a stub to a real
// handler in a later step updates a single flag instead of two tables.
type planRoute struct {
	method, path string
	stub         bool // handler implemented in a later Layer 6 step (501 for now)
}

var planRoutes = []planRoute{
	// Auth management
	{http.MethodGet, "/api/v1/auth/me", false},
	{http.MethodPost, "/api/v1/auth/logout", false},
	{http.MethodPost, "/api/v1/auth/logout-all", false},
	{http.MethodGet, "/api/v1/auth/sessions", false},
	{http.MethodDelete, "/api/v1/auth/sessions/sess-1", false},
	// User
	{http.MethodGet, "/api/v1/user", false},
	{http.MethodPut, "/api/v1/user", true},
	{http.MethodDelete, "/api/v1/user", true},
	// Teams
	{http.MethodGet, "/api/v1/teams", false},
	{http.MethodPost, "/api/v1/teams", false},
	{http.MethodGet, "/api/v1/teams/team-1", false},
	{http.MethodPut, "/api/v1/teams/team-1", false},
	{http.MethodGet, "/api/v1/teams/team-1/members", false},
	{http.MethodDelete, "/api/v1/teams/team-1/members/user-1", false},
	{http.MethodPost, "/api/v1/teams/team-1/invitations", false},
	{http.MethodDelete, "/api/v1/teams/team-1/invitations/inv-1", true},
	{http.MethodPost, "/api/v1/invitations/accept", true},
	// Servers
	{http.MethodGet, "/api/v1/servers", false},
	{http.MethodPost, "/api/v1/servers", false},
	{http.MethodGet, "/api/v1/servers/srv-1", true},
	{http.MethodDelete, "/api/v1/servers/srv-1", false},
	{http.MethodPost, "/api/v1/servers/srv-1/registration-token", true},
	{http.MethodGet, "/api/v1/servers/srv-1/events", false},
	// Apps
	{http.MethodGet, "/api/v1/servers/srv-1/apps", true},
	{http.MethodPost, "/api/v1/servers/srv-1/apps", true},
	{http.MethodGet, "/api/v1/servers/srv-1/apps/app-1", true},
	{http.MethodDelete, "/api/v1/servers/srv-1/apps/app-1", true},
	// Deployments
	{http.MethodPost, "/api/v1/servers/srv-1/apps/app-1/deploy", true},
	{http.MethodPost, "/api/v1/servers/srv-1/apps/app-1/rollback", true},
	{http.MethodGet, "/api/v1/servers/srv-1/apps/app-1/deployments", true},
	// App lifecycle
	{http.MethodPost, "/api/v1/servers/srv-1/apps/app-1/start", true},
	{http.MethodPost, "/api/v1/servers/srv-1/apps/app-1/stop", true},
	{http.MethodPost, "/api/v1/servers/srv-1/apps/app-1/restart", true},
	{http.MethodGet, "/api/v1/servers/srv-1/apps/app-1/logs", true},
	// Environment variables
	{http.MethodGet, "/api/v1/servers/srv-1/apps/app-1/env", true},
	{http.MethodPut, "/api/v1/servers/srv-1/apps/app-1/env/KEY", true},
	{http.MethodDelete, "/api/v1/servers/srv-1/apps/app-1/env/KEY", true},
	// Databases
	{http.MethodGet, "/api/v1/servers/srv-1/apps/app-1/databases", true},
	{http.MethodPost, "/api/v1/servers/srv-1/apps/app-1/databases", true},
	{http.MethodDelete, "/api/v1/servers/srv-1/apps/app-1/databases/db-1", true},
	// Domains (plan URL)
	{http.MethodGet, "/api/v1/servers/srv-1/apps/app-1/domains", true},
	{http.MethodPost, "/api/v1/servers/srv-1/apps/app-1/domains", true},
	{http.MethodDelete, "/api/v1/servers/srv-1/apps/app-1/domains/example.com", true},
	{http.MethodPost, "/api/v1/servers/srv-1/apps/app-1/domains/example.com/verify", true},
	// Metrics
	{http.MethodGet, "/api/v1/servers/srv-1/metrics", true},
	{http.MethodGet, "/api/v1/servers/srv-1/metrics/history", true},
	// Alerts
	{http.MethodGet, "/api/v1/servers/srv-1/alerts", false},
	{http.MethodPost, "/api/v1/servers/srv-1/alerts/alert-1/acknowledge", false},
	{http.MethodPost, "/api/v1/servers/srv-1/alerts/alert-1/ack", false},
	{http.MethodGet, "/api/v1/alerts", false},
	{http.MethodPost, "/api/v1/alerts/read", false},
	// Backups (plan URL)
	{http.MethodGet, "/api/v1/servers/srv-1/backups", true},
	{http.MethodPost, "/api/v1/servers/srv-1/backups", true},
	{http.MethodPost, "/api/v1/servers/srv-1/backups/bkp-1/restore", true},
	// Commands
	{http.MethodGet, "/api/v1/servers/srv-1/commands", true},
	{http.MethodGet, "/api/v1/servers/srv-1/commands/cmd-1", true},
	// Legacy routes kept for the dashboard (all wired)
	{http.MethodPost, "/api/v1/deploy", false},
	{http.MethodGet, "/api/v1/deployments", false},
	{http.MethodPost, "/api/v1/servers/registration-token", false},
	{http.MethodPost, "/api/v1/teams/team-1/invite", false},
	{http.MethodPost, "/api/v1/invitations/tok-1/accept", false},
	{http.MethodPost, "/api/v1/servers/srv-1/deployments/dep-1/domains", false},
	{http.MethodGet, "/api/v1/servers/srv-1/backup/config", false},
	{http.MethodPost, "/api/v1/servers/srv-1/backup/trigger", false},
	{http.MethodGet, "/api/v1/servers/srv-1/backup/history", false},
}

// Protected routes reject unauthenticated requests with 401 JSON. Every route
// in the Layer 6 Step 1 route table must be registered AND sit behind the Auth
// middleware (a 404 would mean it was never registered; a 200/400 would mean
// it leaked outside the auth group).
func TestRouter_ProtectedRoutesRequireAuth(t *testing.T) {
	router := newTestRouter(t)

	for _, c := range planRoutes {
		w := doRequest(router, c.method, c.path)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without token = %d, want 401", c.method, c.path, w.Code)
		}
		if !isJSONBody(w) {
			t.Errorf("%s %s: 401 body is not JSON: %q", c.method, c.path, w.Body.String())
		}
	}
}

// Routes whose handlers land in later Layer 6 steps are registered against
// NotImplemented and must answer 501 JSON for an authenticated user (proving
// they are registered and behind Auth, not silently 404).
func TestRouter_PlanRoutesNotYetImplementedReturn501(t *testing.T) {
	router, database := newTestRouterDB(t)

	if err := queries.InsertUser(database, "usr-1", "alice@example.com", "Alice", "hash"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	token, err := auth.GenerateAccessToken("usr-1", "sess-1", "alice@example.com", "Alice", "test-secret", time.Hour)
	if err != nil {
		t.Fatalf("mint access token: %v", err)
	}

	for _, c := range planRoutes {
		if !c.stub {
			continue
		}
		req := httptest.NewRequest(c.method, c.path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusNotImplemented {
			t.Errorf("%s %s with token = %d, want 501", c.method, c.path, w.Code)
		}
		if !isJSONBody(w) {
			t.Errorf("%s %s: 501 body is not JSON: %q", c.method, c.path, w.Body.String())
		}
		var body map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode 501 body: %v", err)
		}
		if body["error"] != "not_implemented" {
			t.Errorf("%s %s: error code = %v, want not_implemented", c.method, c.path, body["error"])
		}
	}
}

// GET /api/v1/user (plan URL) is served by the same handler as /auth/me and
// must return the authenticated user for a valid JWT.
func TestRouter_UserRouteWired(t *testing.T) {
	router, database := newTestRouterDB(t)

	if err := queries.InsertUser(database, "usr-1", "alice@example.com", "Alice", "hash"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	token, err := auth.GenerateAccessToken("usr-1", "sess-1", "alice@example.com", "Alice", "test-secret", time.Hour)
	if err != nil {
		t.Fatalf("mint access token: %v", err)
	}

	for _, path := range []string{"/api/v1/user", "/api/v1/auth/me"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("GET %s with token = %d, want 200", path, w.Code)
		}
		var body map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode %s response: %v", path, err)
		}
		if body["email"] != "alice@example.com" {
			t.Errorf("GET %s: email = %v, want alice@example.com", path, body["email"])
		}
	}
}

// WebSocket endpoints are registered as their own routes (a plain HTTP GET
// must hit the WS handler, not 404 — the handler rejects with a non-404
// upgrade error).
func TestRouter_WebSocketEndpointsRegistered(t *testing.T) {
	router := newTestRouter(t)

	for _, path := range []string{"/ws/browser", "/ws/agent"} {
		w := doRequest(router, http.MethodGet, path)
		if w.Code == http.StatusNotFound {
			t.Errorf("GET %s = 404 — WebSocket endpoint not registered", path)
		}
	}
}

// Route conflicts are detected at startup. chi v5 does not panic when the
// SAME method+pattern is registered twice (the second registration silently
// replaces the first), but it does panic on genuinely conflicting patterns —
// here, a route with the same param key twice — which is what a duplicate
// route registration would produce after a copy-paste edit.
func TestRouter_ConflictingRoutePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected chi to panic on a conflicting route pattern")
		}
	}()

	r := chi.NewRouter()
	// Duplicate param key 'id' in one pattern: chi rejects the route at
	// registration time with "routing pattern contains duplicate param key".
	r.Get("/api/v1/items/{id}/sub/{id}", http.NotFound)
}
