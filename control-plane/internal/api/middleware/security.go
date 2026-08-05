package middleware

import "net/http"

// SecurityHeaders applies hardening headers to every control-plane response
// (Layer 5A Step 8B):
//
//   - X-Content-Type-Options: nosniff stops browsers from MIME-sniffing
//     responses into a different type than declared.
//   - X-Frame-Options: DENY stops the dashboard being framed (clickjacking).
//   - Referrer-Policy: strict-origin-when-cross-origin limits what referrer
//     information leaks to third parties (the browser default behavior).
//
// CORS is applied separately (see CORS) so only the dashboard origin is
// allowed, never a wildcard. CSP is a dashboard (browser) concern and is set
// by the Next.js app, not the API.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}
