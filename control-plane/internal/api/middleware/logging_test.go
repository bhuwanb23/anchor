package middleware

import (
	"bytes"
	"database/sql"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/yourname/yourplatform/control-plane/internal/auth"
)

// captureLogs swaps the default slog logger for one that writes JSON into buf,
// restoring the previous logger afterwards. Tests in this package run
// sequentially (no t.Parallel), so the global default swap is safe.
func captureLogs(t *testing.T, buf *bytes.Buffer) {
	t.Helper()
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
}

// The request log line carries every field the plan specifies: request_id,
// method, path, status, duration_ms, user_id, ip, user_agent.
func TestLogging_LogsStructuredFields(t *testing.T) {
	var buf bytes.Buffer
	captureLogs(t, &buf)

	handler := RequestID(Logging(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setLogUserID(r, "usr-abc123")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"ok":true}`))
	})))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/servers", nil)
	req.RemoteAddr = "203.0.113.7:54321"
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36 extra-long-user-agent-string-for-truncation-testing")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	logLine := buf.String()
	for _, want := range []string{
		`"request_id":"req-`,
		`"method":"POST"`,
		`"path":"/api/v1/servers"`,
		`"status":201`,
		`"duration_ms":`,
		`"user_id":"usr-abc123"`,
		`"ip":"203.0.113.7"`,
	} {
		if !strings.Contains(logLine, want) {
			t.Errorf("log line missing %s\nlog: %s", want, logLine)
		}
	}
	// user_agent truncated to 100 chars (the long string above would exceed it).
	idx := strings.Index(logLine, `"user_agent":"`)
	if idx < 0 {
		t.Fatalf("log line missing user_agent\nlog: %s", logLine)
	}
	ua := logLine[idx+len(`"user_agent":"`):]
	ua = ua[:strings.Index(ua, `"`)]
	if len(ua) > 100 {
		t.Errorf("user_agent not truncated: %d chars", len(ua))
	}
}

// The handler never writes a status explicitly — the log records 200 (what
// net/http would have sent), not 0.
func TestLogging_DefaultsStatusTo200(t *testing.T) {
	var buf bytes.Buffer
	captureLogs(t, &buf)

	handler := Logging(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok")) // implicit 200
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if !strings.Contains(buf.String(), `"status":200`) {
		t.Errorf("expected status 200 in log, got: %s", buf.String())
	}
}

// Passwords, tokens, and query parameters never reach the log: the logger
// only emits method/path/status/duration/user_id/ip/user_agent.
func TestLogging_NeverLogsSecrets(t *testing.T) {
	var buf bytes.Buffer
	captureLogs(t, &buf)

	handler := Logging(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// A login request carrying a password in the body, a JWT in the
	// Authorization header, and a token in the query string.
	body := strings.NewReader(`{"email":"a@b.com","password":"hunter2-secret"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login?token=qry-secret", body)
	req.Header.Set("Authorization", "Bearer jwt.secret.payload")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	logLine := buf.String()
	// The query string must not appear at all (only r.URL.Path is logged).
	if strings.Contains(logLine, "token=qry-secret") || strings.Contains(logLine, "qry-secret") {
		t.Errorf("query token leaked into log: %s", logLine)
	}
	for _, secret := range []string{"hunter2-secret", "jwt.secret.payload", "Bearer"} {
		if strings.Contains(logLine, secret) {
			t.Errorf("secret %q leaked into log: %s", secret, logLine)
		}
	}
	// And the path itself must be logged.
	if !strings.Contains(logLine, `"path":"/api/v1/auth/login"`) {
		t.Errorf("path missing from log: %s", logLine)
	}
}

// A request that never goes through Auth logs user_id as "" (public route).
func TestLogging_EmptyUserIDForPublicRoute(t *testing.T) {
	var buf bytes.Buffer
	captureLogs(t, &buf)

	handler := Logging(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))

	if !strings.Contains(buf.String(), `"user_id":""`) {
		t.Errorf(`expected user_id "" in log, got: %s`, buf.String())
	}
}

// setLogUserID is a no-op outside a Logging-wrapped request (direct handler
// tests) — it must not panic.
func TestSetLogUserID_NoOpWithoutLogging(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	setLogUserID(req, "usr-1") // must not panic
}

// The wrapped writer preserves headers the handler sets and the status code.
func TestLogging_PreservesHandlerBehavior(t *testing.T) {
	handler := Logging(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom", "yes")
		w.WriteHeader(http.StatusTeapot)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTeapot {
		t.Errorf("status = %d, want 418", w.Code)
	}
	if w.Header().Get("X-Custom") != "yes" {
		t.Errorf("X-Custom = %q, want yes", w.Header().Get("X-Custom"))
	}
}

// End-to-end through the real chain: RequestID → Logging → Auth → handler. A
// valid JWT must surface as user_id in the request log line (Layer 6 Step 2B
// "user_id from auth, if authenticated"), and a missing/invalid token as
// user_id="unauthenticated" (Step 2D).
func TestLogging_AuthUserIDPropagatesToLog(t *testing.T) {
	const jwtSecret = "a-test-secret-that-is-long-enough"

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`
		CREATE TABLE users (
			id            TEXT PRIMARY KEY,
			email         TEXT NOT NULL UNIQUE,
			name          TEXT NOT NULL,
			password_hash TEXT NOT NULL,
			created_at    TEXT NOT NULL,
			updated_at    TEXT NOT NULL
		);
	`); err != nil {
		t.Fatalf("create users table: %v", err)
	}
	if _, err := db.Exec(
		"INSERT INTO users (id, email, name, password_hash, created_at, updated_at) VALUES (?, ?, ?, ?, datetime('now'), datetime('now'))",
		"usr-1", "alice@example.com", "Alice", "hash",
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	handler := RequestID(Logging(Auth(db, jwtSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))))

	// Valid token → user_id = usr-1.
	t.Run("authenticated", func(t *testing.T) {
		var buf bytes.Buffer
		captureLogs(t, &buf)

		token, err := auth.GenerateAccessToken("usr-1", "sess-1", "alice@example.com", "Alice", jwtSecret, time.Hour)
		if err != nil {
			t.Fatalf("mint token: %v", err)
		}
		req := httptest.NewRequest(http.MethodGet, "/api/v1/user", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", w.Code)
		}
		if !strings.Contains(buf.String(), `"user_id":"usr-1"`) {
			t.Errorf("log missing user_id=usr-1: %s", buf.String())
		}
	})

	// No token → Auth rejects with 401 and logs user_id=unauthenticated.
	t.Run("unauthenticated", func(t *testing.T) {
		var buf bytes.Buffer
		captureLogs(t, &buf)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/user", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", w.Code)
		}
		if !strings.Contains(buf.String(), `"user_id":"unauthenticated"`) {
			t.Errorf("log missing user_id=unauthenticated: %s", buf.String())
		}
	})
}
