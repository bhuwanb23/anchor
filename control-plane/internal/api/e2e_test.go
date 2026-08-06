package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yourname/yourplatform/control-plane/internal/alerts"
	"github.com/yourname/yourplatform/control-plane/internal/auth"
	"github.com/yourname/yourplatform/control-plane/internal/config"
	"github.com/yourname/yourplatform/control-plane/internal/db"
	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
	"github.com/yourname/yourplatform/control-plane/internal/mailer"
	"github.com/yourname/yourplatform/control-plane/internal/ws"
)

// ---------------------------------------------------------------------------
// Test infrastructure
// ---------------------------------------------------------------------------

type e2eEnv struct {
	router http.Handler
	db     *sql.DB
	hub    *ws.Hub
	cfg    *config.Config
}

func setupE2E(t *testing.T) *e2eEnv {
	t.Helper()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })

	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cfg := &config.Config{
		Port:            "8080",
		Env:             "test",
		JWTSecret:       "e2e-test-secret",
		JWTExpiryHrs:    1,
		RefreshTokenDays: 30,
		DatabasePath:    ":memory:",
		FrontendURL:     "http://localhost:3000",
		WSPath:          "/ws/agent",
		BaseDomain:      "e2e.test",
	}
	hub := ws.NewHub()
	sender := mailer.NewFromConfig(cfg)
	delivery := alerts.NewDelivery(database, sender, cfg)
	router := NewRouter(database, cfg, hub, delivery, sender)

	return &e2eEnv{router: router, db: database, hub: hub, cfg: cfg}
}

func (e *e2eEnv) seedUser(t *testing.T, id, email, name, passwordHash string) {
	t.Helper()
	if err := queries.InsertUser(e.db, id, email, name, passwordHash); err != nil {
		t.Fatalf("seed user: %v", err)
	}
}

func (e *e2eEnv) mintToken(t *testing.T, userID, sessionID, email, name string, ttl time.Duration) string {
	t.Helper()
	token, err := auth.GenerateAccessToken(userID, sessionID, email, name, e.cfg.JWTSecret, ttl)
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	return token
}

func (e *e2eEnv) do(method, path string, body interface{}) *httptest.ResponseRecorder {
	var reqBody *bytes.Buffer
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewBuffer(b)
	} else {
		reqBody = &bytes.Buffer{}
	}
	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	return w
}

func (e *e2eEnv) doWithJWT(method, path, token string, body interface{}) *httptest.ResponseRecorder {
	var reqBody *bytes.Buffer
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewBuffer(b)
	} else {
		reqBody = &bytes.Buffer{}
	}
	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	return w
}

func decodeJSON(t *testing.T, w *httptest.ResponseRecorder, dst interface{}) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), dst); err != nil {
		t.Fatalf("decode response (status %d): %v\nbody: %s", w.Code, err, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Test 1 — Auth flow
// ---------------------------------------------------------------------------

func TestE2E_AuthFlow(t *testing.T) {
	e := setupE2E(t)

	// 1. Register
	w := e.do(http.MethodPost, "/api/v1/auth/register", map[string]string{
		"email":    "alice@test.com",
		"password": "securePass123!",
		"name":     "Alice",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("register = %d, want 201: %s", w.Code, w.Body.String())
	}

	// 2. Login
	w = e.do(http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email":    "alice@test.com",
		"password": "securePass123!",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("login = %d, want 200: %s", w.Code, w.Body.String())
	}
	var loginResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		User         struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"user"`
	}
	decodeJSON(t, w, &loginResp)
	if loginResp.AccessToken == "" {
		t.Fatal("access_token is empty")
	}
	if loginResp.RefreshToken == "" {
		t.Fatal("refresh_token is empty")
	}
	if loginResp.User.Email != "alice@test.com" {
		t.Errorf("user.email = %q, want alice@test.com", loginResp.User.Email)
	}

	// 3. Access protected endpoint with valid JWT
	w = e.doWithJWT(http.MethodGet, "/api/v1/auth/me", loginResp.AccessToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /auth/me = %d, want 200: %s", w.Code, w.Body.String())
	}
	var meResp struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	}
	decodeJSON(t, w, &meResp)
	if meResp.Email != "alice@test.com" {
		t.Errorf("me.email = %q, want alice@test.com", meResp.Email)
	}

	// 4. Expired JWT → 401 with X-Token-Expired header
	expiredToken := e.mintToken(t, meResp.ID, "sess-expired", "alice@test.com", "Alice", -1*time.Hour)
	w = e.doWithJWT(http.MethodGet, "/api/v1/auth/me", expiredToken, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expired JWT = %d, want 401: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("X-Token-Expired") != "true" {
		t.Errorf("X-Token-Expired header missing, got %q", w.Header().Get("X-Token-Expired"))
	}

	// 5. Refresh token → new access token
	w = e.do(http.MethodPost, "/api/v1/auth/refresh", map[string]string{
		"refresh_token": loginResp.RefreshToken,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("refresh = %d, want 200: %s", w.Code, w.Body.String())
	}
	var refreshResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	decodeJSON(t, w, &refreshResp)
	if refreshResp.AccessToken == "" {
		t.Fatal("new access_token is empty")
	}

	// 6. New JWT works
	w = e.doWithJWT(http.MethodGet, "/api/v1/auth/me", refreshResp.AccessToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("new JWT /auth/me = %d, want 200", w.Code)
	}

	// 7. Logout (revoke refresh token)
	w = e.doWithJWT(http.MethodPost, "/api/v1/auth/logout", refreshResp.AccessToken, map[string]string{
		"refresh_token": refreshResp.RefreshToken,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("logout = %d, want 200: %s", w.Code, w.Body.String())
	}

	// 8. Revoked refresh token → 401
	w = e.do(http.MethodPost, "/api/v1/auth/refresh", map[string]string{
		"refresh_token": refreshResp.RefreshToken,
	})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("revoked refresh = %d, want 401: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Test 2 — Server registration
// ---------------------------------------------------------------------------

func TestE2E_ServerRegistration(t *testing.T) {
	e := setupE2E(t)

	// Setup: register + login
	e.seedUser(t, "usr-reg", "reg@test.com", "RegUser", func() string {
		h, _ := auth.HashPassword("pass1234!")
		return h
	}())
	token := e.mintToken(t, "usr-reg", "sess-reg", "reg@test.com", "RegUser", time.Hour)

	// 1. Create server
	w := e.doWithJWT(http.MethodPost, "/api/v1/servers", token, map[string]string{"name": "test-server"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create server = %d, want 201: %s", w.Code, w.Body.String())
	}
	var serverResp struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	decodeJSON(t, w, &serverResp)
	if serverResp.ID == "" {
		t.Fatal("server ID is empty")
	}

	// 2. Generate registration token
	w = e.doWithJWT(http.MethodPost, "/api/v1/servers/"+serverResp.ID+"/registration-token", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("create reg token = %d, want 200: %s", w.Code, w.Body.String())
	}
	var tokenResp struct {
		Token string `json:"token"`
	}
	decodeJSON(t, w, &tokenResp)
	if tokenResp.Token == "" || !strings.HasPrefix(tokenResp.Token, "reg_") {
		t.Fatalf("registration token = %q, want reg_ prefix", tokenResp.Token)
	}

	// 3. Agent registers with valid token
	w = e.do(http.MethodPost, "/api/v1/agent/register", map[string]interface{}{
		"token":          tokenResp.Token,
		"agent_version":  "1.0.0",
		"system_info":    map[string]interface{}{"os": "linux", "arch": "amd64", "ram_mb": 4096, "disk_total_gb": 100},
		"ip_address":     "10.0.0.1",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("agent register = %d, want 201: %s", w.Code, w.Body.String())
	}
	var agentResp struct {
		AgentID     string `json:"agent_id"`
		AgentSecret string `json:"agent_secret"`
		ServerID    string `json:"server_id"`
	}
	decodeJSON(t, w, &agentResp)
	if !strings.HasPrefix(agentResp.AgentID, "agt-") {
		t.Errorf("agent_id = %q, want agt- prefix", agentResp.AgentID)
	}
	if agentResp.AgentSecret == "" {
		t.Error("agent_secret is empty")
	}
	if agentResp.ServerID == "" {
		t.Error("server_id is empty")
	}

	// 4. Same token again → 401
	w = e.do(http.MethodPost, "/api/v1/agent/register", map[string]interface{}{
		"token":          tokenResp.Token,
		"agent_version":  "1.0.0",
		"system_info":    map[string]interface{}{"os": "linux", "arch": "amd64", "ram_mb": 4096, "disk_total_gb": 100},
		"ip_address":     "10.0.0.1",
	})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("reuse token = %d, want 401: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Test 3 — Deploy command round trip
// ---------------------------------------------------------------------------

func TestE2E_DeployCommandRoundTrip(t *testing.T) {
	e := setupE2E(t)

	// Setup user + server + app
	e.seedUser(t, "usr-deploy", "deploy@test.com", "Deployer", func() string {
		h, _ := auth.HashPassword("pass1234!")
		return h
	}())
	token := e.mintToken(t, "usr-deploy", "sess-deploy", "deploy@test.com", "Deployer", time.Hour)

	// Create server
	w := e.doWithJWT(http.MethodPost, "/api/v1/servers", token, map[string]string{"name": "deploy-server"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create server = %d: %s", w.Code, w.Body.String())
	}
	var srv struct{ ID string }
	decodeJSON(t, w, &srv)

	// Create app
	w = e.doWithJWT(http.MethodPost, "/api/v1/servers/"+srv.ID+"/apps", token, map[string]string{"project_name": "myapp"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create app = %d: %s", w.Code, w.Body.String())
	}
	var app struct{ ID string }
	decodeJSON(t, w, &app)

	// Deploy → 202
	w = e.doWithJWT(http.MethodPost, "/api/v1/servers/"+srv.ID+"/apps/"+app.ID+"/deploy", token, map[string]interface{}{
		"image": "nginx:latest",
		"port":  8080,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("deploy = %d, want 202: %s", w.Code, w.Body.String())
	}
	var deployResp struct {
		CommandID    string `json:"command_id"`
		DeploymentID string `json:"deployment_id"`
	}
	decodeJSON(t, w, &deployResp)
	if deployResp.CommandID == "" {
		t.Fatal("command_id is empty")
	}
	if deployResp.DeploymentID == "" {
		t.Fatal("deployment_id is empty")
	}

	// Verify deployment record exists
	var depCount int
	err := e.db.QueryRow("SELECT COUNT(*) FROM deployments WHERE id = ?", deployResp.DeploymentID).Scan(&depCount)
	if err != nil {
		t.Fatalf("query deployment: %v", err)
	}
	if depCount != 1 {
		t.Errorf("deployments count = %d, want 1", depCount)
	}

	// Verify command was enqueued
	var cmdCount int
	err = e.db.QueryRow("SELECT COUNT(*) FROM pending_commands WHERE id = ?", deployResp.CommandID).Scan(&cmdCount)
	if err != nil {
		t.Fatalf("query command: %v", err)
	}
	if cmdCount != 1 {
		t.Errorf("pending_commands count = %d, want 1", cmdCount)
	}

	// List deployments → 200
	w = e.doWithJWT(http.MethodGet, "/api/v1/servers/"+srv.ID+"/apps/"+app.ID+"/deployments", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list deployments = %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Test 4 — Permission enforcement
// ---------------------------------------------------------------------------

func TestE2E_PermissionEnforcement(t *testing.T) {
	e := setupE2E(t)

	// Alice (owner)
	e.seedUser(t, "usr-owner", "owner@test.com", "Owner", func() string {
		h, _ := auth.HashPassword("pass1234!")
		return h
	}())
	aliceToken := e.mintToken(t, "usr-owner", "sess-owner", "owner@test.com", "Owner", time.Hour)

	// Create team + server
	w := e.doWithJWT(http.MethodPost, "/api/v1/teams", aliceToken, map[string]string{"name": "TestTeam"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create team = %d: %s", w.Code, w.Body.String())
	}
	var team struct{ ID string }
	decodeJSON(t, w, &team)

	w = e.doWithJWT(http.MethodPost, "/api/v1/servers", aliceToken, map[string]string{"name": "shared-server"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create server = %d: %s", w.Code, w.Body.String())
	}
	var srv struct{ ID string }
	decodeJSON(t, w, &srv)

	// Link server to team
	if err := queries.LinkServerToTeam(e.db, srv.ID, team.ID); err != nil {
		t.Fatalf("link server to team: %v", err)
	}

	// Create app for deploy test
	w = e.doWithJWT(http.MethodPost, "/api/v1/servers/"+srv.ID+"/apps", aliceToken, map[string]string{"project_name": "permapp"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create app = %d: %s", w.Code, w.Body.String())
	}
	var app struct{ ID string }
	decodeJSON(t, w, &app)

	// Invite Bob
	w = e.doWithJWT(http.MethodPost, "/api/v1/teams/"+team.ID+"/invitations", aliceToken, map[string]string{
		"email": "bob@test.com",
		"role":  "member",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("invite bob = %d: %s", w.Code, w.Body.String())
	}

	// Register Bob
	e.seedUser(t, "usr-member", "bob@test.com", "Bob", func() string {
		h, _ := auth.HashPassword("pass1234!")
		return h
	}())
	bobToken := e.mintToken(t, "usr-member", "sess-bob", "bob@test.com", "Bob", time.Hour)

	// Find invitation token
	var invToken string
	err := e.db.QueryRow("SELECT token FROM invitations WHERE team_id = ? AND email = ?", team.ID, "bob@test.com").Scan(&invToken)
	if err != nil {
		t.Fatalf("find invitation: %v", err)
	}

	// Bob accepts invitation
	w = e.doWithJWT(http.MethodPost, "/api/v1/invitations/"+invToken+"/accept", bobToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("accept invitation = %d: %s", w.Code, w.Body.String())
	}

	// Bob can deploy (member access)
	w = e.doWithJWT(http.MethodPost, "/api/v1/servers/"+srv.ID+"/apps/"+app.ID+"/deploy", bobToken, map[string]interface{}{
		"image": "node:18",
		"port":  3000,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("bob deploy = %d, want 202: %s", w.Code, w.Body.String())
	}

	// Bob cannot delete server (not owner/admin)
	w = e.doWithJWT(http.MethodDelete, "/api/v1/servers/"+srv.ID, bobToken, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("bob delete server = %d, want 403: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Test 5 — Validation
// ---------------------------------------------------------------------------

func TestE2E_Validation(t *testing.T) {
	e := setupE2E(t)

	e.seedUser(t, "usr-val", "val@test.com", "Validator", func() string {
		h, _ := auth.HashPassword("pass1234!")
		return h
	}())
	token := e.mintToken(t, "usr-val", "sess-val", "val@test.com", "Validator", time.Hour)

	// Create server + app
	w := e.doWithJWT(http.MethodPost, "/api/v1/servers", token, map[string]string{"name": "val-server"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create server = %d: %s", w.Code, w.Body.String())
	}
	var srv struct{ ID string }
	decodeJSON(t, w, &srv)

	w = e.doWithJWT(http.MethodPost, "/api/v1/servers/"+srv.ID+"/apps", token, map[string]string{"project_name": "valapp"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create app = %d: %s", w.Code, w.Body.String())
	}
	var app struct{ ID string }
	decodeJSON(t, w, &app)

	// 1. Deploy with empty image → 400
	w = e.doWithJWT(http.MethodPost, "/api/v1/servers/"+srv.ID+"/apps/"+app.ID+"/deploy", token, map[string]interface{}{
		"image": "",
		"port":  8080,
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty image = %d, want 400: %s", w.Code, w.Body.String())
	}

	// 2. Deploy with invalid port → 400
	w = e.doWithJWT(http.MethodPost, "/api/v1/servers/"+srv.ID+"/apps/"+app.ID+"/deploy", token, map[string]interface{}{
		"image": "nginx:latest",
		"port":  99999,
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid port = %d, want 400: %s", w.Code, w.Body.String())
	}

	// 3. Deploy with unknown field → 400
	w = e.doWithJWT(http.MethodPost, "/api/v1/servers/"+srv.ID+"/apps/"+app.ID+"/deploy", token, map[string]interface{}{
		"image":       "nginx:latest",
		"port":        8080,
		"unknown_key": "value",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown field = %d, want 400: %s", w.Code, w.Body.String())
	}

	// 4. Register with common password → 400
	w = e.do(http.MethodPost, "/api/v1/auth/register", map[string]string{
		"email":    "new@test.com",
		"password": "password",
		"name":     "Test",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("common password = %d, want 400: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Test 6 — Error handling
// ---------------------------------------------------------------------------

func TestE2E_ErrorHandling(t *testing.T) {
	e := setupE2E(t)

	e.seedUser(t, "usr-err", "err@test.com", "ErrorUser", func() string {
		h, _ := auth.HashPassword("pass1234!")
		return h
	}())
	token := e.mintToken(t, "usr-err", "sess-err", "err@test.com", "ErrorUser", time.Hour)

	// 1. GET nonexistent server → 403 (access check fails first)
	w := e.doWithJWT(http.MethodGet, "/api/v1/servers/nonexistent", token, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("nonexistent server = %d, want 403: %s", w.Code, w.Body.String())
	}

	// Create a server to test 404 on sub-resources
	w = e.doWithJWT(http.MethodPost, "/api/v1/servers", token, map[string]string{"name": "err-server"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create server = %d: %s", w.Code, w.Body.String())
	}
	var errSrv struct{ ID string }
	decodeJSON(t, w, &errSrv)

	// GET nonexistent app → 404
	w = e.doWithJWT(http.MethodGet, "/api/v1/servers/"+errSrv.ID+"/apps/nonexistent", token, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("nonexistent app = %d, want 404: %s", w.Code, w.Body.String())
	}
	var errResp struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	decodeJSON(t, w, &errResp)
	if errResp.Error != "not_found" {
		t.Errorf("error code = %q, want not_found", errResp.Error)
	}

	// 2. DELETE server without owner role → 403
	// Create server as another user
	e.seedUser(t, "usr-other", "other@test.com", "Other", func() string {
		h, _ := auth.HashPassword("pass1234!")
		return h
	}())
	otherToken := e.mintToken(t, "usr-other", "sess-other", "other@test.com", "Other", time.Hour)
	w = e.doWithJWT(http.MethodPost, "/api/v1/servers", otherToken, map[string]string{"name": "other-server"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create other server = %d: %s", w.Code, w.Body.String())
	}
	var otherSrv struct{ ID string }
	decodeJSON(t, w, &otherSrv)

	w = e.doWithJWT(http.MethodDelete, "/api/v1/servers/"+otherSrv.ID, token, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("delete other server = %d, want 403: %s", w.Code, w.Body.String())
	}

	// 3. POST without required field → 400
	w = e.doWithJWT(http.MethodPost, "/api/v1/servers/"+errSrv.ID+"/apps", token, map[string]string{})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing field = %d, want 400: %s", w.Code, w.Body.String())
	}

	// 4. POST to unknown route → 404 JSON
	w = e.do(http.MethodPost, "/api/v1/totally-fake-endpoint", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown route = %d, want 404: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
		t.Errorf("404 Content-Type = %q, want JSON", w.Header().Get("Content-Type"))
	}

	// 5. Verify request_id present on error responses
	if w.Header().Get("X-Request-ID") == "" {
		t.Error("X-Request-ID header missing on error response")
	}
}

// ---------------------------------------------------------------------------
// Test 7 — Health
// ---------------------------------------------------------------------------

func TestE2E_Health(t *testing.T) {
	e := setupE2E(t)

	// 1. Healthy → 200
	w := e.do(http.MethodGet, "/health", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("health = %d, want 200: %s", w.Code, w.Body.String())
	}
	var healthResp struct {
		Status           string `json:"status"`
		Version          string `json:"version"`
		Database         string `json:"database"`
		UptimeSeconds    int64  `json:"uptime_seconds"`
		ConnectedAgents  int    `json:"connected_agents"`
		ConnectedBrowsers int   `json:"connected_browsers"`
	}
	decodeJSON(t, w, &healthResp)
	if healthResp.Status != "ok" {
		t.Errorf("status = %q, want ok", healthResp.Status)
	}
	if healthResp.Database != "ok" {
		t.Errorf("database = %q, want ok", healthResp.Database)
	}
	if healthResp.Version == "" {
		t.Error("version is empty")
	}

	// 2. Degraded → 503
	e.db.Close()
	w = e.do(http.MethodGet, "/health", nil)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("degraded health = %d, want 503: %s", w.Code, w.Body.String())
	}
	decodeJSON(t, w, &healthResp)
	if healthResp.Status != "degraded" {
		t.Errorf("status = %q, want degraded", healthResp.Status)
	}
}

// ---------------------------------------------------------------------------
// Test 8 — Concurrent requests
// ---------------------------------------------------------------------------

func TestE2E_ConcurrentRequests(t *testing.T) {
	e := setupE2E(t)

	e.seedUser(t, "usr-conc", "conc@test.com", "ConcUser", func() string {
		h, _ := auth.HashPassword("pass1234!")
		return h
	}())
	token := e.mintToken(t, "usr-conc", "sess-conc", "conc@test.com", "ConcUser", time.Hour)

	// Seed 50 servers
	for i := 0; i < 50; i++ {
		w := e.doWithJWT(http.MethodPost, "/api/v1/servers", token, map[string]string{
			"name": fmt.Sprintf("server-%d", i),
		})
		if w.Code != http.StatusCreated {
			t.Fatalf("seed server %d = %d: %s", i, w.Code, w.Body.String())
		}
	}

	// 50 concurrent GET /api/v1/servers
	var wg sync.WaitGroup
	errors := make(chan string, 50)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := e.doWithJWT(http.MethodGet, "/api/v1/servers", token, nil)
			if w.Code != http.StatusOK {
				errors <- fmt.Sprintf("GET /servers = %d, want 200", w.Code)
			}
			var servers []map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &servers); err != nil {
				errors <- fmt.Sprintf("decode servers: %v", err)
			}
		}()
	}
	wg.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}
}
