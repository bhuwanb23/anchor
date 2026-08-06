package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync/atomic"

	chimw "github.com/go-chi/chi/v5/middleware"
)

// RequestIDHeader is the name of the header that carries the request ID on
// both the request and the response (Layer 6 Step 2A).
const RequestIDHeader = "X-Request-ID"

// RequestID assigns a unique ID to every request (Layer 6 Step 2A).
//
// Format: req-{12 random hex chars} (e.g. req-a1b2c3d4e5f6), deliberately
// different from chi's built-in (hostname/counter) so IDs never leak server
// hostnames. The ID is:
//
//   - Set on the response: X-Request-ID
//   - Stored in the request context under chi's RequestIDKey, so every
//     existing chimw.GetReqID caller (error writers, auth middleware) keeps
//     working unchanged.
//
// The incoming X-Request-ID header is intentionally ignored (always
// regenerated): trusting a client-supplied ID would let callers collide or
// spoof IDs used for log correlation and support lookup.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := "req-" + randomHex(12)
		ctx := context.WithValue(r.Context(), chimw.RequestIDKey, requestID)
		w.Header().Set(RequestIDHeader, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// randomHex returns n lowercase hex characters from crypto/rand. crypto/rand
// is cryptographically strong, so IDs are unguessable (an attacker cannot
// iterate a peer's request IDs to correlate traffic).
func randomHex(n int) string {
	b := make([]byte, (n+1)/2)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing is essentially impossible on supported platforms,
		// but if it does, fall back to a monotonic counter-based ID rather
		// than panicking: a panic here would produce a bare 500 with no
		// request_id (the ID is not yet in context), making the response
		// impossible to correlate with a log line.
		fallback := atomic.AddUint64(&fallbackSeq, 1)
		return fmt.Sprintf("%0*x", n, fallback)
	}
	return hex.EncodeToString(b)[:n]
}

// fallbackSeq backs the (unreachable-in-practice) crypto/rand failure path.
var fallbackSeq uint64
