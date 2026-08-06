package handlers_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yourname/yourplatform/control-plane/internal/api/handlers"
	"github.com/yourname/yourplatform/control-plane/internal/auth"
	"github.com/yourname/yourplatform/control-plane/internal/config"

	_ "modernc.org/sqlite"
)

func setupAgentTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	db.SetMaxOpenConns(1)

	_, err = db.Exec(`
		CREATE TABLE users (
			id TEXT PRIMARY KEY,
			email TEXT UNIQUE NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			password_hash TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT
		);
		CREATE TABLE servers (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			name TEXT NOT NULL,
			token TEXT UNIQUE NOT NULL,
			connected_at TEXT NOT NULL DEFAULT (datetime('now')),
			last_seen TEXT NOT NULL DEFAULT (datetime('now')),
			status TEXT NOT NULL DEFAULT 'connected',
			agent_id TEXT,
			agent_secret_hash TEXT,
			os_info TEXT,
			arch TEXT,
			ram_mb INTEGER,
			disk_gb INTEGER,
			ip_address TEXT,
			os_version TEXT,
			os_pretty TEXT,
			ram_available_mb INTEGER,
			disk_total_gb INTEGER,
			disk_available_gb INTEGER,
			disk_used_percent REAL,
			docker_version TEXT,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		);
		CREATE INDEX idx_servers_user_id ON servers(user_id);
		CREATE INDEX idx_servers_agent_id ON servers(agent_id);
		CREATE TABLE registration_tokens (
			id TEXT PRIMARY KEY,
			token_hash TEXT UNIQUE NOT NULL,
			user_id TEXT NOT NULL,
			server_name TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			expires_at TEXT NOT NULL,
			used_at TEXT,
			used_by_ip TEXT,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		);
		CREATE INDEX idx_registration_tokens_hash ON registration_tokens(token_hash);
		CREATE TABLE server_events (
			id TEXT PRIMARY KEY,
			server_id TEXT NOT NULL,
			event_type TEXT NOT NULL,
			check_name TEXT,
			message TEXT,
			details TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE CASCADE
		);
		CREATE TABLE teams (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			owner_id TEXT NOT NULL REFERENCES users(id),
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE TABLE team_members (
			id TEXT PRIMARY KEY,
			team_id TEXT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			role TEXT NOT NULL DEFAULT 'member',
			invited_by TEXT REFERENCES users(id),
			joined_at TEXT NOT NULL DEFAULT (datetime('now')),
			UNIQUE(team_id, user_id)
		);
		CREATE TABLE server_team (
			server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
			team_id TEXT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
			PRIMARY KEY (server_id, team_id)
		);
	`)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}

	if _, err := db.Exec("INSERT INTO users (id, email, name, password_hash) VALUES (?, ?, ?, ?)",
		"usr-1", "alice@example.com", "Alice", "hash"); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	return db
}

func seedRegistrationToken(t *testing.T, db *sql.DB, userID, serverName string, expiresAt time.Time) (rawToken string) {
	t.Helper()
	rawToken, hashedToken, err := auth.GenerateRegistrationToken()
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	if _, err := db.Exec(
		"INSERT INTO registration_tokens (id, token_hash, user_id, server_name, expires_at) VALUES (?, ?, ?, ?, ?)",
		"tok-1", hashedToken, userID, serverName, expiresAt.Format(time.RFC3339),
	); err != nil {
		t.Fatalf("seed registration token: %v", err)
	}
	return rawToken
}

func TestAgentRegister_ValidToken(t *testing.T) {
	db := setupAgentTestDB(t)
	defer db.Close()

	rawToken := seedRegistrationToken(t, db, "usr-1", "my-server", time.Now().Add(1*time.Hour))

	handler := &handlers.Agent{DB: db, Config: &config.Config{BaseDomain: "yourplatform.app"}}

	body := `{"token":"` + rawToken + `","agent_version":"1.0.0","system_info":{"os":"linux","arch":"amd64","ram_mb":8192,"disk_total_gb":100},"ip_address":"1.2.3.4"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Register(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp["agent_id"] == "" {
		t.Error("agent_id is empty")
	}
	if resp["agent_id"][:4] != "agt-" {
		t.Errorf("agent_id = %q, want prefix 'agt-'", resp["agent_id"])
	}
	if resp["agent_secret"] == "" {
		t.Error("agent_secret is empty")
	}
	if resp["agent_secret"][:3] != "as_" {
		t.Errorf("agent_secret = %q, want prefix 'as_'", resp["agent_secret"])
	}
	if resp["server_id"] == "" {
		t.Error("server_id is empty")
	}
	if resp["websocket_url"] == "" {
		t.Error("websocket_url is empty")
	}
	if resp["control_plane_version"] == "" {
		t.Error("control_plane_version is empty")
	}

	// Verify server record exists
	var agentID, status string
	err := db.QueryRow("SELECT agent_id, status FROM servers WHERE id = ?", resp["server_id"]).Scan(&agentID, &status)
	if err != nil {
		t.Fatalf("query server: %v", err)
	}
	if agentID != resp["agent_id"] {
		t.Errorf("server.agent_id = %q, want %q", agentID, resp["agent_id"])
	}
	if status != "pending" {
		t.Errorf("server.status = %q, want 'pending'", status)
	}

	// Verify token is marked used
	var usedAt sql.NullString
	err = db.QueryRow("SELECT used_at FROM registration_tokens WHERE id = ?", "tok-1").Scan(&usedAt)
	if err != nil {
		t.Fatalf("query token: %v", err)
	}
	if !usedAt.Valid {
		t.Error("registration token not marked as used")
	}

	// Verify server event recorded
	var eventType string
	err = db.QueryRow("SELECT event_type FROM server_events WHERE server_id = ? LIMIT 1", resp["server_id"]).Scan(&eventType)
	if err != nil {
		t.Fatalf("query events: %v", err)
	}
	if eventType != "server_registered" {
		t.Errorf("event_type = %q, want 'server_registered'", eventType)
	}
}

func TestAgentRegister_InvalidToken(t *testing.T) {
	db := setupAgentTestDB(t)
	defer db.Close()

	handler := &handlers.Agent{DB: db, Config: &config.Config{}}

	body := `{"token":"reg_nonexistent","agent_version":"1.0.0","system_info":{"os":"linux","arch":"amd64","ram_mb":1024,"disk_total_gb":50},"ip_address":"10.0.0.1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Register(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "invalid_token" {
		t.Errorf("error = %v, want 'invalid_token'", resp["error"])
	}
}

func TestAgentRegister_InvalidPrefix(t *testing.T) {
	db := setupAgentTestDB(t)
	defer db.Close()

	handler := &handlers.Agent{DB: db, Config: &config.Config{}}

	body := `{"token":"bad_token","agent_version":"1.0.0","system_info":{"os":"linux","arch":"amd64","ram_mb":1024,"disk_total_gb":50},"ip_address":"10.0.0.1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Register(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "invalid_token" {
		t.Errorf("error = %v, want 'invalid_token'", resp["error"])
	}
}

func TestAgentRegister_AlreadyUsedToken(t *testing.T) {
	db := setupAgentTestDB(t)
	defer db.Close()

	rawToken := seedRegistrationToken(t, db, "usr-1", "my-server", time.Now().Add(1*time.Hour))

	// Mark token as used
	_, _ = db.Exec("UPDATE registration_tokens SET used_at = datetime('now') WHERE id = ?", "tok-1")

	handler := &handlers.Agent{DB: db, Config: &config.Config{}}

	body := `{"token":"` + rawToken + `","agent_version":"1.0.0","system_info":{"os":"linux","arch":"amd64","ram_mb":1024,"disk_total_gb":50},"ip_address":"10.0.0.1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Register(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "token_already_used" {
		t.Errorf("error = %v, want 'token_already_used'", resp["error"])
	}
}

func TestAgentRegister_ExpiredToken(t *testing.T) {
	db := setupAgentTestDB(t)
	defer db.Close()

	rawToken := seedRegistrationToken(t, db, "usr-1", "my-server", time.Now().Add(-1*time.Hour))

	handler := &handlers.Agent{DB: db, Config: &config.Config{}}

	body := `{"token":"` + rawToken + `","agent_version":"1.0.0","system_info":{"os":"linux","arch":"amd64","ram_mb":1024,"disk_total_gb":50},"ip_address":"10.0.0.1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Register(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "token_expired" {
		t.Errorf("error = %v, want 'token_expired'", resp["error"])
	}
	if resp["message"] != "Registration token expired. Generate a new one." {
		t.Errorf("message = %v, want 'Registration token expired. Generate a new one.'", resp["message"])
	}
}

func TestAgentRegister_EmptyToken(t *testing.T) {
	db := setupAgentTestDB(t)
	defer db.Close()

	handler := &handlers.Agent{DB: db, Config: &config.Config{}}

	body := `{"token":"","agent_version":"1.0.0","system_info":{"os":"linux","arch":"amd64","ram_mb":1024,"disk_total_gb":50},"ip_address":"10.0.0.1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Register(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

func TestAgentRegister_ResponseShape(t *testing.T) {
	db := setupAgentTestDB(t)
	defer db.Close()

	rawToken := seedRegistrationToken(t, db, "usr-1", "test-server", time.Now().Add(1*time.Hour))

	handler := &handlers.Agent{DB: db, Config: &config.Config{BaseDomain: "yourplatform.app"}}

	body := `{"token":"` + rawToken + `","agent_version":"2.0.0","system_info":{"os":"ubuntu","os_version":"22.04","os_pretty":"Ubuntu 22.04","arch":"x86_64","ram_mb":16384,"ram_available_mb":12000,"disk_total_gb":500,"disk_available_gb":400,"disk_used_percent":20,"docker_version":"24.0.7"},"ip_address":"5.6.7.8"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Register(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// All required fields must be present
	required := []string{"agent_id", "agent_secret", "server_id", "websocket_url", "control_plane_version"}
	for _, key := range required {
		if resp[key] == "" {
			t.Errorf("response missing required field %q", key)
		}
	}

	// Verify websocket_url format
	wsURL := resp["websocket_url"]
	if !strings.HasPrefix(wsURL, "ws://") && !strings.HasPrefix(wsURL, "wss://") {
		t.Errorf("websocket_url = %q, want ws:// or wss:// prefix", wsURL)
	}

	// Verify control_plane_version is set
	if resp["control_plane_version"] != "1.0.0" {
		t.Errorf("control_plane_version = %q, want '1.0.0'", resp["control_plane_version"])
	}

	// Verify hardware info stored in DB
	var ramMB, diskGB int
	var osInfo, arch string
	err := db.QueryRow("SELECT os_info, arch, ram_mb, disk_gb FROM servers WHERE id = ?", resp["server_id"]).
		Scan(&osInfo, &arch, &ramMB, &diskGB)
	if err != nil {
		t.Fatalf("query server hardware: %v", err)
	}
	if osInfo != "ubuntu" {
		t.Errorf("os_info = %q, want 'ubuntu'", osInfo)
	}
	if arch != "x86_64" {
		t.Errorf("arch = %q, want 'x86_64'", arch)
	}
	if ramMB != 16384 {
		t.Errorf("ram_mb = %d, want 16384", ramMB)
	}
	if diskGB != 500 {
		t.Errorf("disk_gb = %d, want 500", diskGB)
	}
}

func TestAgentRegister_WebSocketURL_BaseDomain(t *testing.T) {
	db := setupAgentTestDB(t)
	defer db.Close()

	rawToken := seedRegistrationToken(t, db, "usr-1", "srv", time.Now().Add(1*time.Hour))

	handler := &handlers.Agent{DB: db, Config: &config.Config{BaseDomain: "example.com"}}

	body := `{"token":"` + rawToken + `","agent_version":"1.0.0","system_info":{"os":"linux","arch":"amd64","ram_mb":512,"disk_total_gb":20},"ip_address":"1.1.1.1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Register(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)

	wsURL := resp["websocket_url"]
	want := "ws://ws.example.com/ws/agent"
	if wsURL != want {
		t.Errorf("websocket_url = %q, want %q", wsURL, want)
	}
}

func TestAgentRegister_MissingTokenField(t *testing.T) {
	db := setupAgentTestDB(t)
	defer db.Close()

	handler := &handlers.Agent{DB: db, Config: &config.Config{}}

	body := `{"agent_version":"1.0.0","system_info":{"os":"linux","arch":"amd64","ram_mb":1024,"disk_total_gb":50},"ip_address":"10.0.0.1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Register(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

func TestAgentRegister_AgentSecretNotStoredPlaintext(t *testing.T) {
	db := setupAgentTestDB(t)
	defer db.Close()

	rawToken := seedRegistrationToken(t, db, "usr-1", "srv", time.Now().Add(1*time.Hour))

	handler := &handlers.Agent{DB: db, Config: &config.Config{}}

	body := `{"token":"` + rawToken + `","agent_version":"1.0.0","system_info":{"os":"linux","arch":"amd64","ram_mb":1024,"disk_total_gb":50},"ip_address":"10.0.0.1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Register(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)

	// The raw secret should NOT be stored in the database
	var secretHash string
	err := db.QueryRow("SELECT agent_secret_hash FROM servers WHERE id = ?", resp["server_id"]).Scan(&secretHash)
	if err != nil {
		t.Fatalf("query agent_secret_hash: %v", err)
	}
	if secretHash == resp["agent_secret"] {
		t.Error("agent_secret stored as plaintext in DB — must be hashed")
	}
}
