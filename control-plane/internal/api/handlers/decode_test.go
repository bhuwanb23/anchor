package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type testRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Age   int    `json:"age"`
}

func TestDecodeJSON_Success(t *testing.T) {
	body := `{"name":"Alice","email":"alice@example.com","age":30}`
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	var req testRequest
	if err := DecodeJSON(r, &req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Name != "Alice" {
		t.Errorf("Name = %q, want %q", req.Name, "Alice")
	}
	if req.Email != "alice@example.com" {
		t.Errorf("Email = %q, want %q", req.Email, "alice@example.com")
	}
	if req.Age != 30 {
		t.Errorf("Age = %d, want %d", req.Age, 30)
	}
}

func TestDecodeJSON_EmptyBody(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(""))

	var req testRequest
	err := DecodeJSON(r, &req)
	if err == nil {
		t.Fatal("expected error for empty body")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error = %q, want it to contain 'empty'", err.Error())
	}
}

func TestDecodeJSON_InvalidJSON(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{bad json}"))

	var req testRequest
	err := DecodeJSON(r, &req)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Errorf("error = %q, want it to contain 'invalid JSON'", err.Error())
	}
}

func TestDecodeJSON_UnknownField(t *testing.T) {
	body := `{"name":"Alice","unknown_field":"value"}`
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))

	var req testRequest
	err := DecodeJSON(r, &req)
	if err == nil {
		t.Fatal("expected error for unknown field")
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Errorf("error = %q, want it to contain 'unknown field'", err.Error())
	}
}

func TestDecodeJSON_WrongType(t *testing.T) {
	body := `{"name":123}`
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))

	var req testRequest
	err := DecodeJSON(r, &req)
	if err == nil {
		t.Fatal("expected error for wrong type")
	}
	if !strings.Contains(err.Error(), "must be a") {
		t.Errorf("error = %q, want it to contain type error", err.Error())
	}
}

func TestDecodeJSON_TooLarge(t *testing.T) {
	// Build a body larger than 1 MB.
	big := strings.Repeat("x", maxBodySize+100)
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(big))

	var req testRequest
	err := DecodeJSON(r, &req)
	if err == nil {
		t.Fatal("expected error for oversized body")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("error = %q, want it to contain 'too large'", err.Error())
	}
}

func TestDecodeJSON_TrailingData(t *testing.T) {
	body := `{"name":"Alice"} {"name":"Bob"}`
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))

	var req testRequest
	err := DecodeJSON(r, &req)
	if err == nil {
		t.Fatal("expected error for trailing JSON")
	}
	if !strings.Contains(err.Error(), "single JSON value") {
		t.Errorf("error = %q, want it to contain 'single JSON value'", err.Error())
	}
}

func TestDecodeJSON_PartialJSON(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":`))

	var req testRequest
	err := DecodeJSON(r, &req)
	if err == nil {
		t.Fatal("expected error for incomplete JSON")
	}
	if !strings.Contains(err.Error(), "incomplete") && !strings.Contains(err.Error(), "Invalid JSON") {
		t.Errorf("error = %q, want incomplete or invalid JSON error", err.Error())
	}
}
