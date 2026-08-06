package middleware

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := w.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY", got)
	}
	if got := w.Header().Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Errorf("Referrer-Policy = %q, want strict-origin-when-cross-origin", got)
	}
	if got := w.Header().Get("Permissions-Policy"); got != "camera=(), microphone=(), geolocation=()" {
		t.Errorf("Permissions-Policy = %q, want camera=(), microphone=(), geolocation=()", got)
	}
	// Plain HTTP (no TLS): HSTS must NOT be sent (Layer 6 Step 2F).
	if got := w.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("Strict-Transport-Security = %q on plain HTTP, want unset", got)
	}
	// API responses are not cached (Layer 6 Step 2F).
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

// HSTS is only sent when the request arrived over HTTPS (r.TLS set, or the
// X-Forwarded-Proto header from the TLS-terminating proxy).
func TestSecurityHeaders_HSTSOnlyOverHTTPS(t *testing.T) {
	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	// Direct TLS request.
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.TLS = &tls.ConnectionState{}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if got := w.Header().Get("Strict-Transport-Security"); got != "max-age=31536000; includeSubDomains" {
		t.Errorf("TLS request: HSTS = %q, want max-age=31536000; includeSubDomains", got)
	}

	// Proxied HTTPS (Caddy terminates TLS, sets X-Forwarded-Proto: https).
	req2 := httptest.NewRequest(http.MethodGet, "/health", nil)
	req2.Header.Set("X-Forwarded-Proto", "https")
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if got := w2.Header().Get("Strict-Transport-Security"); got != "max-age=31536000; includeSubDomains" {
		t.Errorf("proxied HTTPS: HSTS = %q, want max-age=31536000; includeSubDomains", got)
	}
}

// Static assets served by the router (install script, release binaries) are
// exempt from Cache-Control: no-store so clients can cache the large
// downloads (Layer 6 Step 2F exception).
func TestSecurityHeaders_StaticAssetsCacheable(t *testing.T) {
	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, path := range []string{"/install.sh", "/releases/latest.json", "/releases/v1.0.0/agent-linux-amd64"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if got := w.Header().Get("Cache-Control"); got != "" {
			t.Errorf("GET %s: Cache-Control = %q, want unset (static asset)", path, got)
		}
	}
}

func TestSecurityHeaders_LeavesHandlerWritesIntact(t *testing.T) {
	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom", "yes")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := w.Header().Get("X-Custom"); got != "yes" {
		t.Errorf("X-Custom = %q, want yes (headers must merge, not overwrite)", got)
	}
}
