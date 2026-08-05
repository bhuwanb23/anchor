package middleware

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/golang-jwt/jwt/v5"
	"github.com/yourname/yourplatform/control-plane/internal/auth"
	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
)

// contextKey is an unexported type so that only this package can mint the
// context key that carries the authenticated user.
//
// IMPORTANT: userContextKey and claimsContextKey MUST have distinct types.
// An empty struct's zero value is comparable to any other empty struct, so two
// vars of the same type would be the SAME key and storing one would overwrite
// the other in request context.
type contextKey struct{}
type claimsContextKeyType struct{}

// userContextKey is the key under which Auth stores the authenticated user.
var userContextKey = contextKey{}

// claimsContextKey is the key under which Auth stores the validated JWT
// claims (Layer 5A Step 4B needs the "sid" claim to mark the current session).
var claimsContextKey = claimsContextKeyType{}

// authError is the structured error body returned for every auth failure
// (Layer 5A Step 3C). Codes: authentication_required | invalid_token |
// token_expired | user_not_found. The request_id (Layer 5A Step 8C) lets the
// user quote it to support, who can look up the full server-side log line.
type authError struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

func writeAuthError(w http.ResponseWriter, r *http.Request, code, message string, extraHeaders map[string]string) {
	for k, v := range extraHeaders {
		w.Header().Set(k, v)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(authError{Error: code, Message: message, RequestID: chimw.GetReqID(r.Context())})
}

// Auth guards every protected API endpoint (Layer 5A Step 3).
//
// Flow: extract the Bearer token from the Authorization header → validate it
// strictly (signature, HS256-only algorithm, expiry with 30s clock skew,
// access-type claim) → load the user from the database by sub claim → attach
// the user to the request context for the handler.
//
// The DB lookup on every request is intentional for the MVP: it is the only
// way to detect deleted accounts, and SQLite is fast for a single-row PK
// read. An in-memory cache can be added later if this ever shows up in
// profiling.
func Auth(db *sql.DB, jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 3A — token extraction. Authorization header only: query-string
			// tokens are never accepted on HTTP endpoints because URLs get
			// logged by web servers and proxies. (WebSocket upgrades accept
			// ?token= separately in the WS handler.)
			header := r.Header.Get("Authorization")
			if header == "" {
				writeAuthError(w, r, "authentication_required", "Please log in to access this resource", nil)
				return
			}
			parts := strings.SplitN(header, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
				writeAuthError(w, r, "invalid_token", "Your session is invalid. Please log in again.", nil)
				return
			}

			// 3B — validate the token (structure, signature, algorithm,
			// expiry with leeway, type claim).
			claims, err := auth.ValidateAccessToken(parts[1], jwtSecret)
			if err != nil {
				if errors.Is(err, jwt.ErrTokenExpired) {
					// Frontend uses this header to trigger a silent refresh
					// instead of logging the user out.
					writeAuthError(w, r, "token_expired", "Your session has expired. Please log in again.", map[string]string{"X-Token-Expired": "true"})
				} else {
					writeAuthError(w, r, "invalid_token", "Your session is invalid. Please log in again.", nil)
				}
				return
			}

			// 3B.6 — load the user. Handles the case where the account was
			// deleted while the token was still valid.
			user, err := queries.GetUserByID(db, claims.UserID())
			if errors.Is(err, sql.ErrNoRows) {
				writeAuthError(w, r, "user_not_found", "Account not found. Please register or log in.", nil)
				return
			}
			if err != nil {
				// A DB failure is not an auth failure: 500 (not 401) so the
				// frontend does not loop refresh/login attempts.
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(authError{Error: "internal_error", Message: "Something went wrong. Please try again.", RequestID: chimw.GetReqID(r.Context())})
				return
			}

			// 3B.7 — attach the user and the validated claims so handlers never
			// parse tokens or query the DB themselves.
			ctx := context.WithValue(r.Context(), userContextKey, user)
			ctx = context.WithValue(ctx, claimsContextKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ClaimsFromContext returns the validated JWT claims attached by Auth, or nil
// when the request did not go through Auth. Used by the sessions view (Layer
// 5A Step 4B) to read the current session ID.
func ClaimsFromContext(ctx context.Context) *auth.Claims {
	c, _ := ctx.Value(claimsContextKey).(*auth.Claims)
	return c
}

// UserFromContext returns the authenticated user attached by Auth.
func UserFromContext(ctx context.Context) (queries.User, bool) {
	u, ok := ctx.Value(userContextKey).(queries.User)
	return u, ok
}

// UserIDFromContext returns the authenticated user's ID, or "" when the
// request did not go through Auth.
func UserIDFromContext(ctx context.Context) string {
	u, ok := UserFromContext(ctx)
	if !ok {
		return ""
	}
	return u.ID
}
