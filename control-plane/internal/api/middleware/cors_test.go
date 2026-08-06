package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func corsHandler(allowed ...string) http.Handler {
	return CORS(allowed...)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
}

// A request from the dashboard origin passes through with the exact origin
// echoed back (never a wildcard).
func TestCORS_AllowsDashboardOrigin(t *testing.T) {
	handler := corsHandler("http://localhost:3000", "https://app.yourplatform.com")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/servers", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Errorf("Allow-Origin = %q, want exact origin echo", got)
	}
	if got := w.Header().Get("Access-Control-Expose-Headers"); got != "X-Request-ID, X-Token-Expired" {
		t.Errorf("Expose-Headers = %q, want X-Request-ID, X-Token-Expired", got)
	}
}

// A request from an unexpected origin is rejected with 403 before the handler.
func TestCORS_BlocksUnexpectedOrigin(t *testing.T) {
	handler := corsHandler("http://localhost:3000")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/servers", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for disallowed origin", w.Code)
	}
	if w.Body.String() == `{"ok":true}` {
		t.Error("handler ran for a disallowed origin; must be short-circuited")
	}
}

// A request with no Origin header (curl, Postman, server-to-server) is
// allowed through without CORS headers.
func TestCORS_MissingOriginAllowed(t *testing.T) {
	handler := corsHandler("http://localhost:3000")

	req := httptest.NewRequest(http.MethodGet, "/health", nil) // no Origin header
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for origin-less request", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q, want none for origin-less request", got)
	}
}

// Browser preflight (OPTIONS) from the dashboard origin returns 204 with the
// full CORS header set including Max-Age. Allow-Credentials is never set
// (auth uses the Authorization header, not cookies).
func TestCORS_Preflight(t *testing.T) {
	handler := corsHandler("http://localhost:3000")

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/servers", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "POST")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Errorf("Allow-Origin = %q, want echo", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Methods"); got != "GET, POST, PUT, DELETE, OPTIONS" {
		t.Errorf("Allow-Methods = %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Headers"); got != "Authorization, Content-Type, X-Request-ID" {
		t.Errorf("Allow-Headers = %q", got)
	}
	if got := w.Header().Get("Access-Control-Max-Age"); got != "3600" {
		t.Errorf("Max-Age = %q, want 3600", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Errorf("Allow-Credentials = %q, want unset", got)
	}
}

// Origin matching is case-insensitive (scheme/host are case-insensitive per
// RFC 6454) but the echoed header preserves the caller's exact casing.
func TestCORS_OriginMatchIsCaseInsensitive(t *testing.T) {
	handler := corsHandler("http://localhost:3000")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "HTTP://LOCALHOST:3000")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for case-variant origin", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "HTTP://LOCALHOST:3000" {
		t.Errorf("Allow-Origin = %q, want original casing echoed", got)
	}
}
