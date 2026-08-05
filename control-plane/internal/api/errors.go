package api

// Shared JSON error responses for the router layer (Layer 6 Step 1).
//
// Every non-2xx response the API produces follows the same shape:
//
//	{ "error": "machine_readable_code", "message": "human message", "request_id": "..." }
//
// The request_id (set by the RequestID middleware) lets a user quote it to
// support, who can look up the full server-side log line.

import (
	"encoding/json"
	"net/http"

	chimw "github.com/go-chi/chi/v5/middleware"
)

// writeError writes a JSON error body with the given status code. It is used
// by the router's NotFound / MethodNotAllowed handlers so that unknown routes
// and methods return JSON (not chi's default plain-text page) — the done
// conditions for Layer 6 Step 1.
//
// NOTE: this deliberately mirrors handlers.writeAPIError (same {error,
// message, request_id} shape). The two live in different packages because api
// imports handlers; if the error shape ever changes, update both.
func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := map[string]interface{}{"error": code, "message": message}
	if rid := chimw.GetReqID(r.Context()); rid != "" {
		body["request_id"] = rid
	}
	json.NewEncoder(w).Encode(body)
}
