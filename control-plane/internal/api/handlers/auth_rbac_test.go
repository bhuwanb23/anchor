package handlers_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/yourname/yourplatform/control-plane/internal/api/handlers"
	appmiddleware "github.com/yourname/yourplatform/control-plane/internal/api/middleware"
	"github.com/yourname/yourplatform/control-plane/internal/config"
	"github.com/yourname/yourplatform/control-plane/internal/ws"

	_ "modernc.org/sqlite"
)

// setupRBACTestDB creates the schema needed for the team/RBAC flow: users,
// refresh_tokens, teams, team_members, server_team, invitations, servers,
// deployments and pending_commands.
func setupRBACTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
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
		CREATE INDEX idx_users_email ON users(email);
		CREATE TABLE refresh_tokens (
			id TEXT PRIMARY KEY,
			token_hash TEXT NOT NULL UNIQUE,
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			expires_at TEXT NOT NULL,
			last_used_at TEXT,
			user_agent TEXT,
			ip_address TEXT,
			revoked_at TEXT
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
			server_id TEXT NOT NULL,
			team_id TEXT NOT NULL,
			PRIMARY KEY (server_id, team_id)
		);
		CREATE TABLE invitations (
			id TEXT PRIMARY KEY,
			team_id TEXT NOT NULL,
			email TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'member',
			token TEXT NOT NULL UNIQUE,
			invited_by TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			expires_at TEXT NOT NULL,
			accepted_at TEXT
		);
		CREATE TABLE servers (
			id TEXT PRIMARY KEY,
			user_id TEXT,
			name TEXT,
			token TEXT,
			ip_address TEXT DEFAULT '',
			status TEXT DEFAULT 'pending',
			agent_id TEXT,
			agent_secret_hash TEXT,
			last_seen TEXT
		);
		CREATE TABLE deployments (
			id TEXT PRIMARY KEY,
			server_id TEXT,
			app_name TEXT,
			image TEXT,
			port INTEGER,
			domain TEXT,
			status TEXT DEFAULT 'running',
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE TABLE pending_commands (
			id TEXT PRIMARY KEY,
			server_id TEXT NOT NULL,
			command_type TEXT NOT NULL,
			payload TEXT NOT NULL,
			project_key TEXT,
			expires_at TEXT,
			status TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
	`)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}

// rbacRouter builds a chi router with the real Auth middleware and the
// endpoints exercised by the Layer 5A Test 3 flow.
func rbacRouter(db *sql.DB, cfg *config.Config, hub *ws.Hub) http.Handler {
	r := chi.NewRouter()
	r.Use(appmiddleware.Auth(db, "test-secret"))
	th := &handlers.Teams{DB: db, Logger: slog.Default()}
	sh := &handlers.Server{DB: db}
	r.Post("/servers", sh.CreateServer)
	r.Delete("/servers/{serverID}", sh.DeleteServer)
	r.Post("/deploy", handlers.MakeDeployApp(db, cfg, hub))
	r.Post("/teams/{teamID}/invite", th.SendInvitation)
	r.Post("/invitations/{token}/accept", th.AcceptInvitation)
	return r
}

// doJSON runs a JSON request against the router with a Bearer token attached.
// body may be any JSON-marshalable value (maps with ints like port work).
func doJSON(router http.Handler, method, path, accessToken string, body interface{}) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
} // registerAndLogin registers a fresh user and returns (email, accessToken).
// The email is lowercased because registration normalizes before storing.
func registerAndLogin(t *testing.T, h *handlers.Auth, name string) (string, string) {
	t.Helper()
	email := strings.ToLower(name) + uniqueSuffix() + "@example.com"
	if w := registerRequest(t, h, map[string]string{
		"name":     name,
		"email":    email,
		"password": "correct horse battery staple",
	}); w.Code != http.StatusCreated {
		t.Fatalf("register %s: expected 201, got %d", name, w.Code)
	}
	login := loginWith(h, email, "correct horse battery staple")
	if login.Code != http.StatusOK {
		t.Fatalf("login %s: expected 200, got %d", name, login.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(login.Body).Decode(&resp)
	return email, resp["access_token"].(string)
}

func TestRBAC_InvitationFlowAndPermissions(t *testing.T) {
	db := setupRBACTestDB(t)
	defer db.Close()
	hub := ws.NewHub()
	router := rbacRouter(db, &config.Config{}, hub)
	h := newAuthHandler(db)

	ownerEmail, ownerToken := registerAndLogin(t, h, "Owner")
	memberEmail, memberToken := registerAndLogin(t, h, "Member")

	// Owner creates a server (linked to their personal team).
	create := doJSON(router, http.MethodPost, "/servers", ownerToken, map[string]string{"name": "myserver"})
	if create.Code != http.StatusCreated {
		t.Fatalf("create server: expected 201, got %d: %s", create.Code, create.Body.String())
	}
	var created map[string]interface{}
	json.NewDecoder(create.Body).Decode(&created)
	serverID := created["id"].(string)
	if serverID == "" {
		t.Fatal("create server returned no id")
	}

	// Member is not in the owner's team yet → cannot delete the server (403).
	if w := doJSON(router, http.MethodDelete, "/servers/"+serverID, memberToken, nil); w.Code != http.StatusForbidden {
		t.Errorf("member delete before joining: expected 403, got %d", w.Code)
	}

	// Owner invites the member.
	var teamID string
	if err := db.QueryRow("SELECT id FROM teams WHERE owner_id = (SELECT id FROM users WHERE email = ?)", ownerEmail).Scan(&teamID); err != nil {
		t.Fatalf("lookup owner team: %v", err)
	}
	invite := doJSON(router, http.MethodPost, "/teams/"+teamID+"/invite", ownerToken, map[string]string{"email": memberEmail, "role": "member"})
	if invite.Code != http.StatusCreated {
		t.Fatalf("invite: expected 201, got %d: %s", invite.Code, invite.Body.String())
	}

	// Member accepts the invitation.
	var inviteToken string
	if err := db.QueryRow("SELECT token FROM invitations WHERE team_id = ?", teamID).Scan(&inviteToken); err != nil {
		t.Fatalf("lookup invitation token: %v", err)
	}
	accept := doJSON(router, http.MethodPost, "/invitations/"+inviteToken+"/accept", memberToken, nil)
	if accept.Code != http.StatusOK {
		t.Fatalf("accept: expected 200, got %d: %s", accept.Code, accept.Body.String())
	}

	// Member still cannot delete the server (member role → 403).
	if w := doJSON(router, http.MethodDelete, "/servers/"+serverID, memberToken, nil); w.Code != http.StatusForbidden {
		t.Errorf("member delete after joining: expected 403, got %d", w.Code)
	}

	// Member CAN deploy — the deploy command is accepted and queued. (port is
	// an integer in the API contract.)
	deploy := doJSON(router, http.MethodPost, "/deploy", memberToken, map[string]interface{}{
		"server_id": serverID,
		"app_name":  "myapp",
		"image":     "nginx:latest",
		"port":      8080,
	})
	if deploy.Code != http.StatusAccepted {
		t.Fatalf("member deploy: expected 202, got %d: %s", deploy.Code, deploy.Body.String())
	}
	var deployCount, pendingCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM deployments WHERE server_id = ?", serverID).Scan(&deployCount); err != nil {
		t.Fatalf("count deployments: %v", err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM pending_commands WHERE server_id = ?", serverID).Scan(&pendingCount); err != nil {
		t.Fatalf("count pending commands: %v", err)
	}
	if deployCount != 1 || pendingCount != 1 {
		t.Errorf("deploy recorded: deployments=%d pending_commands=%d, want 1 and 1", deployCount, pendingCount)
	}

	// Owner CAN delete the server (204).
	if w := doJSON(router, http.MethodDelete, "/servers/"+serverID, ownerToken, nil); w.Code != http.StatusNoContent {
		t.Errorf("owner delete: expected 204, got %d", w.Code)
	}
}
