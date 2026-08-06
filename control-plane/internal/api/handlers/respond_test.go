package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	chimw "github.com/go-chi/chi/v5/middleware"
)

// withReqID wraps a request with a chi request ID in context.
func withReqID(r *http.Request) *http.Request {
	ctx := context.WithValue(r.Context(), chimw.RequestIDKey, "req-test123")
	return r.WithContext(ctx)
}

func TestRespondJSON(t *testing.T) {
	w := httptest.NewRecorder()
	data := map[string]string{"key": "value"}

	RespondJSON(w, http.StatusOK, data)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if got["key"] != "value" {
		t.Errorf("key = %q, want %q", got["key"], "value")
	}
}

func TestRespondJSON_NilData(t *testing.T) {
	w := httptest.NewRecorder()
	RespondJSON(w, http.StatusOK, nil)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if w.Body.Len() != 0 {
		t.Errorf("body length = %d, want 0", w.Body.Len())
	}
}

func TestRespondNoContent(t *testing.T) {
	w := httptest.NewRecorder()
	RespondNoContent(w)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	if w.Body.Len() != 0 {
		t.Errorf("body length = %d, want 0", w.Body.Len())
	}
}

func TestRespond201(t *testing.T) {
	w := httptest.NewRecorder()
	data := map[string]string{"id": "123"}

	Respond201(w, data)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", w.Code, http.StatusCreated)
	}
	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if got["id"] != "123" {
		t.Errorf("id = %q, want %q", got["id"], "123")
	}
}

func TestRespondError(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", nil)
	r = withReqID(r)

	RespondError(w, r, http.StatusBadRequest, "invalid_input", "Bad data")

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var body errorBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if body.Error != "invalid_input" {
		t.Errorf("error = %q, want %q", body.Error, "invalid_input")
	}
	if body.Message != "Bad data" {
		t.Errorf("message = %q, want %q", body.Message, "Bad data")
	}
}

func TestRespond400(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", nil)

	Respond400(w, r, "Email is required")

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var body errorBody
	json.Unmarshal(w.Body.Bytes(), &body)
	if body.Error != "bad_request" {
		t.Errorf("error = %q, want %q", body.Error, "bad_request")
	}
	if body.Message != "Email is required" {
		t.Errorf("message = %q", body.Message)
	}
}

func TestRespond401(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)

	Respond401(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	var body errorBody
	json.Unmarshal(w.Body.Bytes(), &body)
	if body.Error != "unauthorized" {
		t.Errorf("error = %q, want %q", body.Error, "unauthorized")
	}
}

func TestRespond403(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)

	Respond403(w, r, "You do not have access")

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
	var body errorBody
	json.Unmarshal(w.Body.Bytes(), &body)
	if body.Error != "forbidden" {
		t.Errorf("error = %q, want %q", body.Error, "forbidden")
	}
}

func TestRespond404(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)

	Respond404(w, r, "Server")

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
	var body errorBody
	json.Unmarshal(w.Body.Bytes(), &body)
	if body.Error != "not_found" {
		t.Errorf("error = %q, want %q", body.Error, "not_found")
	}
	if body.Message != "Server not found" {
		t.Errorf("message = %q, want %q", body.Message, "Server not found")
	}
}

func TestRespond409(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", nil)

	Respond409(w, r, "Email already exists")

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", w.Code, http.StatusConflict)
	}
	var body errorBody
	json.Unmarshal(w.Body.Bytes(), &body)
	if body.Error != "conflict" {
		t.Errorf("error = %q, want %q", body.Error, "conflict")
	}
}

func TestRespond500(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withReqID(r)

	Respond500(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	var body errorBody
	json.Unmarshal(w.Body.Bytes(), &body)
	if body.Error != "internal_error" {
		t.Errorf("error = %q, want %q", body.Error, "internal_error")
	}
	if body.Message != "An internal error occurred. Please try again later." {
		t.Errorf("message should be generic, got %q", body.Message)
	}
}

func TestRespondList(t *testing.T) {
	w := httptest.NewRecorder()
	items := []map[string]string{{"id": "1"}, {"id": "2"}}

	RespondList(w, items, 42, 1, 20)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp ListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if resp.Total != 42 {
		t.Errorf("total = %d, want 42", resp.Total)
	}
	if resp.Page != 1 {
		t.Errorf("page = %d, want 1", resp.Page)
	}
	if resp.PerPage != 20 {
		t.Errorf("per_page = %d, want 20", resp.PerPage)
	}
	data, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatal("data is not an array")
	}
	if len(data) != 2 {
		t.Errorf("data length = %d, want 2", len(data))
	}
}

func TestRespondList_NilData(t *testing.T) {
	w := httptest.NewRecorder()
	RespondList(w, nil, 0, 1, 20)

	var resp ListResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	data, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatal("data is not an array")
	}
	if len(data) != 0 {
		t.Errorf("data length = %d, want 0", len(data))
	}
}
