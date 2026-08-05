package queries

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// setupServersDB mirrors the real servers schema plus the team tables needed
// by the access checks. Columns are limited to what the queries select.
func setupServersDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		PRAGMA foreign_keys=ON;
		CREATE TABLE users (id TEXT PRIMARY KEY, email TEXT UNIQUE NOT NULL);
		CREATE TABLE servers (
			id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT NOT NULL,
			token TEXT UNIQUE NOT NULL,
			connected_at TEXT NOT NULL DEFAULT (datetime('now')),
			last_seen TEXT NOT NULL DEFAULT (datetime('now')),
			status TEXT NOT NULL DEFAULT 'connected',
			agent_id TEXT, agent_secret_hash TEXT, os_info TEXT, arch TEXT,
			ram_mb INTEGER, disk_gb INTEGER, ip_address TEXT,
			os_version TEXT, os_pretty TEXT, ram_available_mb INTEGER,
			disk_total_gb INTEGER, disk_available_gb INTEGER,
			disk_used_percent REAL, docker_version TEXT,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		);
		CREATE TABLE teams (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, owner_id TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE TABLE team_members (
			id TEXT PRIMARY KEY, team_id TEXT NOT NULL, user_id TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'member', invited_by TEXT, joined_at TEXT NOT NULL DEFAULT (datetime('now')),
			UNIQUE(team_id, user_id),
			FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		);
		CREATE TABLE server_team (
			server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
			team_id TEXT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
			PRIMARY KEY (server_id, team_id)
		);
		INSERT INTO users (id, email) VALUES ('user-1', 'owner@example.com');
		INSERT INTO users (id, email) VALUES ('user-2', 'member@example.com');
		INSERT INTO servers (id, user_id, name, token, status) VALUES ('srv-1', 'user-1', 'prod', 'tok-1', 'connected');
		INSERT INTO servers (id, user_id, name, token, status) VALUES ('srv-2', 'user-1', 'staging', 'tok-2', 'disconnected');
		INSERT INTO servers (id, user_id, name, token, status) VALUES ('srv-3', 'user-2', 'shared', 'tok-3', 'connected');
		INSERT INTO teams (id, name, owner_id) VALUES ('team-1', 'Agency', 'user-2');
		INSERT INTO team_members (id, team_id, user_id, role) VALUES ('tm-1', 'team-1', 'user-2', 'owner');
		INSERT INTO team_members (id, team_id, user_id, role) VALUES ('tm-2', 'team-1', 'user-1', 'member');
		INSERT INTO server_team (server_id, team_id) VALUES ('srv-3', 'team-1');
	`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

// Pattern 1: Get by ID, not found returns nil, nil (not an error).
func TestGetServer_Found(t *testing.T) {
	db := setupServersDB(t)
	defer db.Close()

	s, err := GetServer(db, "srv-1")
	if err != nil {
		t.Fatal(err)
	}
	if s == nil {
		t.Fatal("expected server, got nil")
	}
	if s.ID != "srv-1" || s.Name != "prod" || s.UserID != "user-1" || s.Status != "connected" {
		t.Fatalf("unexpected server: %+v", s)
	}
}

func TestGetServer_NotFoundReturnsNil(t *testing.T) {
	db := setupServersDB(t)
	defer db.Close()

	s, err := GetServer(db, "does-not-exist")
	if err != nil {
		t.Fatalf("expected nil error for missing row, got %v", err)
	}
	if s != nil {
		t.Fatalf("expected nil server, got %+v", s)
	}
}

// Pattern 3: List with filters; empty list returns an empty slice, never nil.
func TestListServersByUser(t *testing.T) {
	db := setupServersDB(t)
	defer db.Close()

	servers, err := ListServersByUser(db, "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 2 {
		t.Fatalf("len=%d want 2", len(servers))
	}

	// User with no servers: empty slice, not nil.
	empty, err := ListServersByUser(db, "user-none")
	if err != nil {
		t.Fatal(err)
	}
	if empty == nil {
		t.Fatal("expected empty slice, got nil")
	}
	if len(empty) != 0 {
		t.Fatalf("len=%d want 0", len(empty))
	}
}

func TestListServersByTeam(t *testing.T) {
	db := setupServersDB(t)
	defer db.Close()

	servers, err := ListServersByTeam(db, "team-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 || servers[0].ID != "srv-3" {
		t.Fatalf("got %+v, want only srv-3", servers)
	}

	empty, err := ListServersByTeam(db, "team-none")
	if err != nil {
		t.Fatal(err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("expected empty slice, got %v", empty)
	}
}

// Step 3C #7: user + server access check.
func TestCanUserAccessServer(t *testing.T) {
	db := setupServersDB(t)
	defer db.Close()

	// Owner access.
	ok, err := CanUserAccessServer(db, "user-1", "srv-1")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("owner should have access to own server")
	}

	// Team member access (user-1 is a member of team-1 which owns srv-3).
	ok, err = CanUserAccessServer(db, "user-1", "srv-3")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("team member should have access to linked server")
	}

	// No access at all.
	ok, err = CanUserAccessServer(db, "user-2", "srv-1")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("user-2 must not access srv-1 (neither owner nor team member)")
	}
}

func TestGetUserServerRole(t *testing.T) {
	db := setupServersDB(t)
	defer db.Close()

	// Direct ownership wins.
	role, err := GetUserServerRole(db, "user-1", "srv-1")
	if err != nil {
		t.Fatal(err)
	}
	if role != "owner" {
		t.Fatalf("role=%q want owner", role)
	}

	// Team role when access comes through a team.
	role, err = GetUserServerRole(db, "user-1", "srv-3")
	if err != nil {
		t.Fatal(err)
	}
	if role != "member" {
		t.Fatalf("role=%q want member", role)
	}

	// No access → empty role.
	role, err = GetUserServerRole(db, "user-2", "srv-1")
	if err != nil {
		t.Fatal(err)
	}
	if role != "" {
		t.Fatalf("role=%q want empty", role)
	}
}
