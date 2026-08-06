package middleware

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
)

// logStateKey is the context key for the mutable per-request log state. It is
// created by Logging (which runs early in the chain) and mutated by Auth
// (which runs later, inside the protected group), so the request log line can
// include the authenticated user even though the logger sees the request
// before auth does. A pointer is used because context values are immutable.
type logStateKey struct{}

type logState struct {
	userID string
}

// setLogUserID records the authenticated user (or "unauthenticated" on auth
// failure) for the request's log line. No-op when the request did not flow
// through Logging (e.g. direct handler tests).
func setLogUserID(r *http.Request, userID string) {
	if s, ok := r.Context().Value(logStateKey{}).(*logState); ok {
		s.userID = userID
	}
}

// Logging logs every request as a single structured slog line (Layer 6
// Step 2B): request_id, method, path, status, duration_ms, user_id, ip,
// user_agent.
//
// Deliberately NOT logged (the plan's "what NOT to log"): request bodies
// (may contain passwords/tokens/env values), response bodies, the
// Authorization header, and any query parameters (r.URL.Path is used, never
// r.URL.RawQuery, so token/password query params cannot leak).
//
// The response writer is wrapped with chi's WrapResponseWriter, which
// captures the status code while preserving Flusher/Hijacker/ReaderFrom
// (WebSocket upgrades keep working).
func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
		state := &logState{}
		ctx := context.WithValue(r.Context(), logStateKey{}, state)
		next.ServeHTTP(ww, r.WithContext(ctx))

		status := ww.Status()
		if status == 0 {
			status = http.StatusOK // handler wrote nothing; http would default 200
		}

		slog.Info("request",
			"request_id", chimw.GetReqID(r.Context()),
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"duration_ms", time.Since(start).Milliseconds(),
			"user_id", state.userID,
			"ip", clientIP(r),
			"user_agent", truncateUserAgent(r.UserAgent()),
		)
	})
}

// clientIP returns the remote address without the port. chi's RealIP
// middleware runs before Logging in the router and has already rewritten
// RemoteAddr from X-Forwarded-For, so this is the caller's real IP.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// truncateUserAgent caps the user-agent at 100 chars for log hygiene.
func truncateUserAgent(ua string) string {
	if len(ua) > 100 {
		return ua[:100]
	}
	return strings.TrimSpace(ua)
}
