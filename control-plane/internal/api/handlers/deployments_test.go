package handlers_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yourname/yourplatform/control-plane/internal/api/handlers"
	"github.com/yourname/yourplatform/control-plane/internal/config"
	"github.com/yourname/yourplatform/control-plane/internal/ws"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}

	// Create tables matching the schema
	_, err = db.Exec(`
		CREATE TABLE servers (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			name TEXT NOT NULL,
			agent_id TEXT UNIQUE,
			agent_secret_hash TEXT,
			status TEXT DEFAULT 'pending',
			ip_address TEXT,
			os_info TEXT,
			arch TEXT,
			ram_mb INTEGER,
			disk_gb INTEGER,
			os_version TEXT,
			os_pretty TEXT,
			docker_version TEXT,
			ram_available_mb INTEGER,
			disk_total_gb INTEGER,
			disk_available_gb INTEGER,
			disk_used_percent REAL,
			token TEXT,
			connected_at DATETIME,
			last_seen DATETIME
		);
		CREATE TABLE deployments (
			id TEXT PRIMARY KEY,
			server_id TEXT NOT NULL,
			app_name TEXT NOT NULL,
			image TEXT NOT NULL,
			port INTEGER NOT NULL,
			domain TEXT,
			status TEXT DEFAULT 'pending',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		t.Fatalf("create tables: %v", err)
	}

	return db
}

func insertTestServer(t *testing.T, db *sql.DB, serverID, ipAddress string) {
	t.Helper()
	_, err := db.Exec(
		"INSERT INTO servers (id, user_id, name, agent_id, agent_secret_hash, status, ip_address) VALUES (?, ?, ?, ?, ?, ?, ?)",
		serverID, "user-1", "test-server", "agent-1", "hash1", "connected", ipAddress,
	)
	if err != nil {
		t.Fatalf("insert test server: %v", err)
	}
}

func TestDeployApp_Success(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	serverID := "00000000-0000-0000-0000-000000000001"
	insertTestServer(t, db, serverID, "1.2.3.4")

	cfg := &config.Config{BaseDomain: "yourplatform.app"}
	hub := ws.NewHub()
	handler := handlers.MakeDeployApp(db, cfg, hub)

	body := map[string]interface{}{
		"server_id": serverID,
		"app_name":  "my-shop",
		"image":     "nginx:latest",
		"port":      8080,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/deploy", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)

	if resp["status"] != "queued" {
		t.Errorf("status = %q, want queued", resp["status"])
	}
	if resp["domain"] != "my-shop.srv-00000000.yourplatform.app" {
		t.Errorf("domain = %q, want my-shop.srv-00000000.yourplatform.app", resp["domain"])
	}
	if resp["deployment_id"] == "" {
		t.Error("deployment_id should not be empty")
	}
}

func TestDeployApp_ExplicitDomain(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	serverID := "00000000-0000-0000-0000-000000000001"
	insertTestServer(t, db, serverID, "1.2.3.4")

	cfg := &config.Config{BaseDomain: "yourplatform.app"}
	hub := ws.NewHub()
	handler := handlers.MakeDeployApp(db, cfg, hub)

	body := map[string]interface{}{
		"server_id": serverID,
		"app_name":  "my-shop",
		"image":     "nginx:latest",
		"port":      8080,
		"domain":    "custom.example.com",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/deploy", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)

	if resp["domain"] != "custom.example.com" {
		t.Errorf("domain = %q, want custom.example.com", resp["domain"])
	}
}

func TestDeployApp_Collision(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	serverID := "00000000-0000-0000-0000-000000000001"
	insertTestServer(t, db, serverID, "1.2.3.4")

	// Insert existing deployment
	_, err := db.Exec(
		"INSERT INTO deployments (id, server_id, app_name, image, port, domain, status) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"existing-deploy", serverID, "my-shop", "nginx:1.0", 80, "my-shop.srv-00000000.yourplatform.app", "running",
	)
	if err != nil {
		t.Fatalf("insert existing deployment: %v", err)
	}

	cfg := &config.Config{BaseDomain: "yourplatform.app"}
	hub := ws.NewHub()
	handler := handlers.MakeDeployApp(db, cfg, hub)

	body := map[string]interface{}{
		"server_id": serverID,
		"app_name":  "my-shop",
		"image":     "nginx:latest",
		"port":      8080,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/deploy", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 for collision, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeployApp_MissingFields(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	cfg := &config.Config{BaseDomain: "yourplatform.app"}
	hub := ws.NewHub()
	handler := handlers.MakeDeployApp(db, cfg, hub)

	body := map[string]interface{}{
		"app_name": "my-shop",
		"image":    "nginx:latest",
		"port":     8080,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/deploy", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing server_id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeployApp_ServerNotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	cfg := &config.Config{BaseDomain: "yourplatform.app"}
	hub := ws.NewHub()
	handler := handlers.MakeDeployApp(db, cfg, hub)

	body := map[string]interface{}{
		"server_id": "nonexistent-server",
		"app_name":  "my-shop",
		"image":     "nginx:latest",
		"port":      8080,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/deploy", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for nonexistent server, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeployApp_NoBaseDomain(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	serverID := "00000000-0000-0000-0000-000000000001"
	insertTestServer(t, db, serverID, "1.2.3.4")

	cfg := &config.Config{BaseDomain: ""}
	hub := ws.NewHub()
	handler := handlers.MakeDeployApp(db, cfg, hub)

	body := map[string]interface{}{
		"server_id": serverID,
		"app_name":  "my-shop",
		"image":     "nginx:latest",
		"port":      8080,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/deploy", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)

	// Domain should be empty when no BaseDomain is configured
	if resp["domain"] != "" {
		t.Errorf("domain = %q, want empty when BaseDomain not set", resp["domain"])
	}
}

func TestDeployApp_CollisionAllowsRedeployAfterStop(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	serverID := "00000000-0000-0000-0000-000000000001"
	insertTestServer(t, db, serverID, "1.2.3.4")

	// Insert stopped deployment — should allow redeploy
	_, err := db.Exec(
		"INSERT INTO deployments (id, server_id, app_name, image, port, domain, status) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"old-deploy", serverID, "my-shop", "nginx:1.0", 80, "my-shop.srv-00000000.yourplatform.app", "stopped",
	)
	if err != nil {
		t.Fatalf("insert stopped deployment: %v", err)
	}

	cfg := &config.Config{BaseDomain: "yourplatform.app"}
	hub := ws.NewHub()
	handler := handlers.MakeDeployApp(db, cfg, hub)

	body := map[string]interface{}{
		"server_id": serverID,
		"app_name":  "my-shop",
		"image":     "nginx:latest",
		"port":      8080,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/deploy", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("expected 202 for redeploy after stop, got %d: %s", w.Code, w.Body.String())
	}
}
