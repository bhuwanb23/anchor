package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yourname/yourplatform/control-plane/internal/ws"

	_ "modernc.org/sqlite"
)

func setupHealthTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("ping test db: %v", err)
	}
	return db
}

func TestHealth_OK(t *testing.T) {
	db := setupHealthTestDB(t)
	defer db.Close()

	hub := ws.NewHub()
	handler := &Health{DB: db, Hub: hub}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp healthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Status != "ok" {
		t.Errorf("status = %q, want 'ok'", resp.Status)
	}
	if resp.Version == "" {
		t.Error("version is empty")
	}
	if resp.Database != "ok" {
		t.Errorf("database = %q, want 'ok'", resp.Database)
	}
	if resp.UptimeSeconds < 0 {
		t.Errorf("uptime_seconds = %d, want >= 0", resp.UptimeSeconds)
	}
	if resp.ConnectedAgents != 0 {
		t.Errorf("connected_agents = %d, want 0", resp.ConnectedAgents)
	}
	if resp.ConnectedBrowsers != 0 {
		t.Errorf("connected_browsers = %d, want 0", resp.ConnectedBrowsers)
	}
	if resp.Error != "" {
		t.Errorf("error = %q, want empty", resp.Error)
	}
}

func TestHealth_DatabaseDown(t *testing.T) {
	db := setupHealthTestDB(t)
	// Close DB immediately to simulate failure.
	db.Close()

	hub := ws.NewHub()
	handler := &Health{DB: db, Hub: hub}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body: %s", w.Code, w.Body.String())
	}

	var resp healthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Status != "degraded" {
		t.Errorf("status = %q, want 'degraded'", resp.Status)
	}
	if resp.Database != "error" {
		t.Errorf("database = %q, want 'error'", resp.Database)
	}
	if resp.Error != "database unavailable" {
		t.Errorf("error = %q, want 'database unavailable'", resp.Error)
	}
}

func TestHealth_ResponseShape(t *testing.T) {
	db := setupHealthTestDB(t)
	defer db.Close()

	handler := &Health{DB: db, Hub: nil}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	required := []string{"status", "version", "database", "uptime_seconds", "connected_agents", "connected_browsers"}
	for _, key := range required {
		if _, ok := resp[key]; !ok {
			t.Errorf("response missing required field %q", key)
		}
	}
}

func TestHealth_NoAuthRequired(t *testing.T) {
	db := setupHealthTestDB(t)
	defer db.Close()

	handler := &Health{DB: db, Hub: ws.NewHub()}

	// No Authorization header — should still return 200.
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (health is public)", w.Code)
	}
}

func TestHealth_UptimeIncreases(t *testing.T) {
	db := setupHealthTestDB(t)
	defer db.Close()

	handler := &Health{DB: db, Hub: nil}

	// First request.
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req)
	var resp1 healthResponse
	json.Unmarshal(w1.Body.Bytes(), &resp1)

	time.Sleep(100 * time.Millisecond)

	// Second request.
	req = httptest.NewRequest(http.MethodGet, "/health", nil)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req)
	var resp2 healthResponse
	json.Unmarshal(w2.Body.Bytes(), &resp2)

	if resp2.UptimeSeconds < resp1.UptimeSeconds {
		t.Errorf("uptime decreased from %d to %d", resp1.UptimeSeconds, resp2.UptimeSeconds)
	}
}

func TestHealth_NilHub(t *testing.T) {
	db := setupHealthTestDB(t)
	defer db.Close()

	handler := &Health{DB: db, Hub: nil}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	// Should not panic with nil Hub.
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp healthResponse
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.ConnectedAgents != 0 || resp.ConnectedBrowsers != 0 {
		t.Errorf("agents=%d browsers=%d, want 0/0 with nil hub", resp.ConnectedAgents, resp.ConnectedBrowsers)
	}
}
