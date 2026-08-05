package ws

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	_ "modernc.org/sqlite"
)

// setupAgentAuthDB creates an in-memory DB with the servers + pending_commands
// tables the agent WebSocket handler touches, and one registered agent.
func setupAgentAuthDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)

	_, err = db.Exec(`
		CREATE TABLE servers (
			id TEXT PRIMARY KEY,
			user_id TEXT,
			name TEXT,
			token TEXT,
			agent_id TEXT UNIQUE,
			agent_secret_hash TEXT,
			status TEXT DEFAULT 'pending',
			last_seen TEXT
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
		t.Fatalf("create tables: %v", err)
	}

	if _, err := db.Exec(
		"INSERT INTO servers (id, user_id, name, agent_id, agent_secret_hash, status) VALUES (?, ?, ?, ?, ?, ?)",
		"srv-1", "user-1", "test-server", "agt-1", hashSecret("secret1"), "pending",
	); err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	return db
}

func hashSecret(secret string) string {
	h := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(h[:])
}

func basicAuth(agentID, secret string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(agentID+":"+secret))
}

func wsURL(s *httptest.Server) string {
	return "ws" + strings.TrimPrefix(s.URL, "http")
}

// dialAgent performs the WebSocket handshake with the given auth header and
// returns the (possibly nil) connection and handshake response.
func dialAgent(srv *httptest.Server, authHeader string) (*websocket.Conn, *http.Response, error) {
	header := http.Header{}
	if authHeader != "" {
		header.Set("Authorization", authHeader)
	}
	return websocket.DefaultDialer.Dial(wsURL(srv), header)
}

func TestAgentWS_ValidCredentialsUpgrade(t *testing.T) {
	db := setupAgentAuthDB(t)
	defer db.Close()
	hub := NewHub()
	srv := httptest.NewServer(HandleAgentWS(hub, db, "example.com", nil))
	defer srv.Close()

	conn, resp, err := dialAgent(srv, basicAuth("agt-1", "secret1"))
	if err != nil {
		t.Fatalf("dial with valid credentials: %v (status %v)", err, resp)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	// The agent receives register_ack (with its server_id) then hello_ack. The
	// server marks the row connected AFTER hello_ack, so read both messages
	// before asserting the DB state.
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read register_ack: %v", err)
	}
	var msg map[string]interface{}
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("decode register_ack: %v", err)
	}
	if msg["type"] != "register_ack" {
		t.Errorf("first message type = %v, want register_ack", msg["type"])
	}
	if msg["server_id"] != "srv-1" {
		t.Errorf("server_id = %v, want srv-1", msg["server_id"])
	}

	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatalf("read hello_ack: %v", err)
	}

	// The server marks the row connected after the handshake messages; poll
	// briefly since that write races the client's reads.
	deadline := time.Now().Add(2 * time.Second)
	for {
		var status string
		if err := db.QueryRow("SELECT status FROM servers WHERE id = 'srv-1'").Scan(&status); err != nil {
			t.Fatalf("query status: %v", err)
		}
		if status == "connected" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server status = %q, want connected (never updated)", status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAgentWS_WrongSecretRejected(t *testing.T) {
	db := setupAgentAuthDB(t)
	defer db.Close()
	hub := NewHub()
	srv := httptest.NewServer(HandleAgentWS(hub, db, "example.com", nil))
	defer srv.Close()

	_, resp, err := dialAgent(srv, basicAuth("agt-1", "wrong-secret"))
	if err == nil {
		t.Fatal("expected handshake to fail with wrong secret")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %v, want 401", resp)
	}
}

func TestAgentWS_UnknownAgentIDRejected(t *testing.T) {
	db := setupAgentAuthDB(t)
	defer db.Close()
	hub := NewHub()
	srv := httptest.NewServer(HandleAgentWS(hub, db, "example.com", nil))
	defer srv.Close()

	_, resp, err := dialAgent(srv, basicAuth("agt-does-not-exist", "secret1"))
	if err == nil {
		t.Fatal("expected handshake to fail with unknown agent_id")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %v, want 401", resp)
	}
}

func TestAgentWS_MissingAuthRejected(t *testing.T) {
	db := setupAgentAuthDB(t)
	defer db.Close()
	hub := NewHub()
	srv := httptest.NewServer(HandleAgentWS(hub, db, "example.com", nil))
	defer srv.Close()

	_, resp, err := dialAgent(srv, "")
	if err == nil {
		t.Fatal("expected handshake to fail without auth header")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %v, want 401", resp)
	}
}

// TestAgentWS_RevokedAgentRejected simulates secret rotation/revocation
// (Layer 5A Step 6C): after the stored hash changes, the old secret no longer
// authenticates, so a reconnecting agent is rejected with 401.
func TestAgentWS_RevokedAgentRejected(t *testing.T) {
	db := setupAgentAuthDB(t)
	defer db.Close()
	hub := NewHub()
	srv := httptest.NewServer(HandleAgentWS(hub, db, "example.com", nil))
	defer srv.Close()

	// First connection with the current secret works.
	conn, _, err := dialAgent(srv, basicAuth("agt-1", "secret1"))
	if err != nil {
		t.Fatalf("initial dial: %v", err)
	}
	conn.Close()

	// Revoke: rotate the stored secret hash, then reconnect with the old one.
	if _, err := db.Exec("UPDATE servers SET agent_secret_hash = ? WHERE id = 'srv-1'", hashSecret("new-secret")); err != nil {
		t.Fatalf("rotate secret: %v", err)
	}

	_, resp, err := dialAgent(srv, basicAuth("agt-1", "secret1"))
	if err == nil {
		t.Fatal("expected handshake to fail with revoked secret")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %v, want 401", resp)
	}
}

// TestAgentWS_DeletedServerRejected verifies that a deleted server's agent
// cannot reconnect (Layer 5A Step 6A): the WebSocket upgrade is rejected
// with 403 before any connection is established.
func TestAgentWS_DeletedServerRejected(t *testing.T) {
	db := setupAgentAuthDB(t)
	defer db.Close()
	hub := NewHub()
	srv := httptest.NewServer(HandleAgentWS(hub, db, "example.com", nil))
	defer srv.Close()

	// Mark the server as deleted.
	if _, err := db.Exec("UPDATE servers SET status = 'deleted' WHERE id = 'srv-1'"); err != nil {
		t.Fatalf("mark deleted: %v", err)
	}

	_, resp, err := dialAgent(srv, basicAuth("agt-1", "secret1"))
	if err == nil {
		t.Fatal("expected handshake to fail with deleted server")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %v, want 403", resp)
	}
}
