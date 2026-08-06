package middleware

import (
	"net/http"
	"strings"
)

// SecurityHeaders applies hardening headers to every control-plane response
// (Layer 5A Step 8B + Layer 6 Step 2F):
//
//   - X-Content-Type-Options: nosniff stops browsers from MIME-sniffing
//     responses into a different type than declared.
//   - X-Frame-Options: DENY stops the dashboard being framed (clickjacking).
//   - Referrer-Policy: strict-origin-when-cross-origin limits what referrer
//     information leaks to third parties (the browser default behavior).
//   - Permissions-Policy: explicitly disables camera/microphone/geolocation,
//     which the dashboard does not use (Layer 6 Step 2F).
//   - Strict-Transport-Security: only sent over HTTPS (Layer 6 Step 2F).
//   - Cache-Control: no-store on API responses so browsers always fetch fresh
//     (Layer 6 Step 2F). Static assets served by this router (/install.sh,
//     /releases/*) are exempt per the plan — they are large downloads that
//     should be cacheable.
//
// CORS is applied separately (see CORS) so only the dashboard origin is
// allowed, never a wildcard. CSP is a dashboard (browser) concern and is set
// by the Next.js app, not the API.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")

		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		if !isStaticAsset(r.URL.Path) {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// isStaticAsset reports whether the path is a large static download served by
// the router (install script, release binaries) that should remain cacheable.
func isStaticAsset(path string) bool {
	return path == "/install.sh" || strings.HasPrefix(path, "/releases/")
}
