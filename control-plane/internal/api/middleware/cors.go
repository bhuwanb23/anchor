package middleware

import (
	"encoding/json"
	"net/http"
	"strings"

	chimw "github.com/go-chi/chi/v5/middleware"
)

// CORS implements the Layer 6 Step 2C policy. It accepts the dashboard
// origin(s) explicitly — never a wildcard — and applies per the plan:
//
//   - Request with an allowed Origin: Access-Control-Allow-Origin echoes the
//     exact origin (with Vary: Origin for correct caching), plus
//     Allow-Methods / Allow-Headers / Expose-Headers / Max-Age.
//   - Request from a disallowed Origin: 403 before the handler runs.
//   - Request with no Origin header (curl, Postman, server-to-server,
//     WebSocket clients that omit it): allowed, no CORS headers needed.
//   - OPTIONS (browser preflight): 204 No Content with the CORS headers.
//
// Allow-Credentials is deliberately NOT set: auth uses the Authorization
// header, not cookies, so a credentialed CORS grant is unnecessary and would
// only widen the attack surface.
func CORS(allowedOrigins ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool, len(allowedOrigins))
	for _, o := range allowedOrigins {
		allowed[strings.ToLower(o)] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				// Non-browser client (curl, Postman, agent, same-origin tooling).
				next.ServeHTTP(w, r)
				return
			}

			if !allowed[strings.ToLower(origin)] {
				// JSON body matching the API error contract (Layer 6 Step 4A) so
				// clients parse one shape everywhere. RequestID runs before CORS
				// in the router, so the request_id is available for correlation.
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				body := map[string]interface{}{
					"error":   "origin_not_allowed",
					"message": "This origin is not allowed to access the API",
				}
				if rid := chimw.GetReqID(r.Context()); rid != "" {
					body["request_id"] = rid
				}
				json.NewEncoder(w).Encode(body)
				return
			}

			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID")
			w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID, X-Token-Expired")
			w.Header().Set("Access-Control-Max-Age", "3600")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
