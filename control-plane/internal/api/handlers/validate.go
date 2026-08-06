package handlers

import (
	"net/http"

	chimw "github.com/go-chi/chi/v5/middleware"
)

// ValidationErrors collects field-level validation failures.
// Use Add() to record each error, then call RespondValidationError to send.
type ValidationErrors map[string]string

// Add records a validation error for the given field.
func (ve ValidationErrors) Add(field, message string) {
	ve[field] = message
}

// HasErrors returns true if any validation errors were recorded.
func (ve ValidationErrors) HasErrors() bool {
	return len(ve) > 0
}

// RespondValidationError writes a 400 response with all field errors.
func RespondValidationError(w http.ResponseWriter, r *http.Request, ve ValidationErrors) {
	body := map[string]interface{}{
		"error":   "validation_failed",
		"message": "Request validation failed",
		"fields":  ve,
	}
	if rid := chimw.GetReqID(r.Context()); rid != "" {
		body["request_id"] = rid
	}
	writeJSON(w, http.StatusBadRequest, body)
}
