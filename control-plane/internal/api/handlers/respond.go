package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	chimw "github.com/go-chi/chi/v5/middleware"
)

// RespondJSON writes a JSON response with the given status code.
func RespondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		json.NewEncoder(w).Encode(data)
	}
}

// RespondNoContent writes a 204 No Content response with an empty body.
func RespondNoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// Respond201 writes a 201 Created response with the given data.
func Respond201(w http.ResponseWriter, data interface{}) {
	RespondJSON(w, http.StatusCreated, data)
}

// errorBody is the standard error response shape.
type errorBody struct {
	Error     string            `json:"error"`
	Message   string            `json:"message"`
	RequestID string            `json:"request_id,omitempty"`
	Fields    ValidationErrors  `json:"fields,omitempty"`
}

// RespondError writes a structured JSON error response.
func RespondError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	body := errorBody{
		Error:   code,
		Message: message,
	}
	if rid := chimw.GetReqID(r.Context()); rid != "" {
		body.RequestID = rid
	}
	RespondJSON(w, status, body)
}

// Respond400 writes a 400 Bad Request error.
func Respond400(w http.ResponseWriter, r *http.Request, message string) {
	RespondError(w, r, http.StatusBadRequest, "bad_request", message)
}

// Respond401 writes a 401 Unauthorized error with a fixed message.
func Respond401(w http.ResponseWriter, r *http.Request) {
	RespondError(w, r, http.StatusUnauthorized, "unauthorized", "Authentication required")
}

// Respond403 writes a 403 Forbidden error.
func Respond403(w http.ResponseWriter, r *http.Request, message string) {
	RespondError(w, r, http.StatusForbidden, "forbidden", message)
}

// Respond404 writes a 404 Not Found error for the given resource.
func Respond404(w http.ResponseWriter, r *http.Request, resource string) {
	RespondError(w, r, http.StatusNotFound, "not_found", resource+" not found")
}

// Respond409 writes a 409 Conflict error.
func Respond409(w http.ResponseWriter, r *http.Request, message string) {
	RespondError(w, r, http.StatusConflict, "conflict", message)
}

// Respond500 writes a 500 Internal Server Error. It logs the full error
// server-side but returns a generic message to the client.
func Respond500(w http.ResponseWriter, r *http.Request) {
	slog.Error("internal server error",
		"request_id", chimw.GetReqID(r.Context()),
		"path", r.URL.Path,
	)
	RespondError(w, r, http.StatusInternalServerError, "internal_error",
		"An internal error occurred. Please try again later.")
}
