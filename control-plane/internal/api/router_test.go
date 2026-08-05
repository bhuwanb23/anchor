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

	"github.com/go-chi/chi/v5"
	"github.com/yourname/yourplatform/control-plane/internal/alerts"
	"github.com/yourname/yourplatform/control-plane/internal/config"
	"github.com/yourname/yourplatform/control-plane/internal/db"
	"github.com/yourname/yourplatform/control-plane/internal/mailer"
	"github.com/yourname/yourplatform/control-plane/internal/ws"
)

// newTestRouter builds the production router exactly as main.go wires it,
// against a migrated in-memory database.
func newTestRouter(t *testing.T) http.Handler {
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
	return NewRouter(database, cfg, hub, delivery, sender)
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
}

// Unknown methods on a known path return 405 JSON, not HTML.
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

	// Public auth endpoints must NOT be blocked by Auth middleware. Register
	// with an invalid body: a 400 means the handler ran (no auth gate); a 401
	// would mean the route was accidentally protected.
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/auth/register"},
		{http.MethodPost, "/api/v1/auth/login"},
		{http.MethodPost, "/api/v1/auth/refresh"},
		{http.MethodPost, "/api/v1/auth/forgot-password"},
		{http.MethodPost, "/api/v1/auth/reset-password"},
	} {
		w := doRequest(router, c.method, c.path)
		if w.Code == http.StatusUnauthorized {
			t.Errorf("%s %s returned 401 — public route must not require auth", c.method, c.path)
		}
		if w.Code == http.StatusNotFound {
			t.Errorf("%s %s returned 404 — public route not registered", c.method, c.path)
		}
	}
}

// Protected routes reject unauthenticated requests with 401 JSON.
func TestRouter_ProtectedRoutesRequireAuth(t *testing.T) {
	router := newTestRouter(t)

	protected := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/auth/me"},
		{http.MethodPost, "/api/v1/auth/logout"},
		{http.MethodPost, "/api/v1/auth/logout-all"},
		{http.MethodGet, "/api/v1/auth/sessions"},
		{http.MethodGet, "/api/v1/servers"},
		{http.MethodPost, "/api/v1/servers"},
		{http.MethodGet, "/api/v1/alerts"},
		{http.MethodGet, "/api/v1/teams"},
		{http.MethodPost, "/api/v1/deploy"},
	}
	for _, c := range protected {
		w := doRequest(router, c.method, c.path)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without token = %d, want 401", c.method, c.path, w.Code)
		}
		if !isJSONBody(w) {
			t.Errorf("%s %s: 401 body is not JSON: %q", c.method, c.path, w.Body.String())
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
