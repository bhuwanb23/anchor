package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidationErrors_Add(t *testing.T) {
	ve := ValidationErrors{}
	ve.Add("email", "Required")
	ve.Add("password", "Must be at least 8 characters")

	if len(ve) != 2 {
		t.Fatalf("len = %d, want 2", len(ve))
	}
	if ve["email"] != "Required" {
		t.Errorf("email = %q, want %q", ve["email"], "Required")
	}
}

func TestValidationErrors_HasErrors_Empty(t *testing.T) {
	ve := ValidationErrors{}
	if ve.HasErrors() {
		t.Error("expected HasErrors() = false for empty map")
	}
}

func TestValidationErrors_HasErrors_WithErrors(t *testing.T) {
	ve := ValidationErrors{}
	ve.Add("name", "Required")
	if !ve.HasErrors() {
		t.Error("expected HasErrors() = true")
	}
}

func TestRespondValidationError(t *testing.T) {
	ve := ValidationErrors{}
	ve.Add("email", "Must be a valid email address")
	ve.Add("password", "Must be at least 8 characters")

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	RespondValidationError(w, r, ve)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["error"] != "validation_failed" {
		t.Errorf("error = %q, want %q", body["error"], "validation_failed")
	}
	if body["message"] != "Request validation failed" {
		t.Errorf("message = %q, want %q", body["message"], "Request validation failed")
	}

	fields, ok := body["fields"].(map[string]interface{})
	if !ok {
		t.Fatal("fields is not a map")
	}
	if fields["email"] != "Must be a valid email address" {
		t.Errorf("fields.email = %q", fields["email"])
	}
	if fields["password"] != "Must be at least 8 characters" {
		t.Errorf("fields.password = %q", fields["password"])
	}
}
