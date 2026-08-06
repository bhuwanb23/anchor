package middleware

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	chimw "github.com/go-chi/chi/v5/middleware"
)

var requestIDPattern = regexp.MustCompile(`^req-[0-9a-f]{12}$`)

// Every response carries an X-Request-ID in the plan's req-{12 hex} format.
func TestRequestID_SetsResponseHeader(t *testing.T) {
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	got := w.Header().Get(RequestIDHeader)
	if !requestIDPattern.MatchString(got) {
		t.Errorf("X-Request-ID = %q, want req-{12 hex}", got)
	}
}

// The ID is placed in the request context under chi's RequestIDKey, so every
// existing chimw.GetReqID caller (error writers, auth middleware) reads it.
func TestRequestID_AvailableInContext(t *testing.T) {
	var got string
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = chimw.GetReqID(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !requestIDPattern.MatchString(got) {
		t.Errorf("context request id = %q, want req-{12 hex}", got)
	}
	if w.Header().Get(RequestIDHeader) != got {
		t.Errorf("header %q != context %q", w.Header().Get(RequestIDHeader), got)
	}
}

// Two requests get distinct IDs (the atomic counter chi uses is replaced by
// crypto/rand, which never repeats for all practical purposes).
func TestRequestID_DistinctAcrossRequests(t *testing.T) {
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		id := w.Header().Get(RequestIDHeader)
		if seen[id] {
			t.Fatalf("duplicate request id %q after %d requests", id, i)
		}
		seen[id] = true
	}
}

// A client-supplied X-Request-ID is ignored (always regenerated) so callers
// cannot spoof or collide IDs used for log correlation and support lookup.
func TestRequestID_IgnoresClientSuppliedHeader(t *testing.T) {
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(RequestIDHeader, "client-chosen-id")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	got := w.Header().Get(RequestIDHeader)
	if !requestIDPattern.MatchString(got) {
		t.Errorf("X-Request-ID = %q, want regenerated req-{12 hex} (client header ignored)", got)
	}
}
