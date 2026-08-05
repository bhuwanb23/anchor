package ws

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
	_ "modernc.org/sqlite"
)

// setupCommandRoutingDB builds the tables the command pipeline touches:
// servers, commands (Step 4), and pending_commands (offline queue, 013).
func setupCommandRoutingDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	// One connection so the in-memory schema is visible to every query.
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`
		CREATE TABLE servers (
			id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'disconnected',
			ip_address TEXT NOT NULL DEFAULT '', token TEXT
		);
		CREATE TABLE commands (
			id TEXT PRIMARY KEY, server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
			command_type TEXT NOT NULL, project_key TEXT NOT NULL DEFAULT '',
			payload TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'queued',
			issued_by TEXT NOT NULL, created_at TEXT NOT NULL,
			started_at TEXT, completed_at TEXT, result TEXT
		);
		CREATE TABLE pending_commands (
			id TEXT PRIMARY KEY, server_id TEXT NOT NULL,
			command_type TEXT NOT NULL, payload TEXT NOT NULL,
			project_key TEXT, expires_at TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		INSERT INTO servers (id, user_id, name, status) VALUES ('srv-1', 'user-1', 'prod', 'connected');
	`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// pendingCommandCount returns the number of queued offline commands for a server.
func pendingCommandCount(t *testing.T, db *sql.DB, serverID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pending_commands WHERE server_id = ?`, serverID).Scan(&n); err != nil {
		t.Fatalf("count pending commands: %v", err)
	}
	return n
}

func deployMsg(commandID, project string) Message {
	raw, _ := json.Marshal(map[string]interface{}{
		"type":         "command",
		"command_id":   commandID,
		"command_type": "deploy",
		"server_id":    "srv-1",
		"payload":      map[string]interface{}{"project": project, "image": "nginx:latest"},
	})
	return Message{Type: "command", Payload: raw}
}

func TestCommandRouting_OnlineAcceptedAndFullJourney(t *testing.T) {
	hub := NewHub()
	db := setupCommandRoutingDB(t)

	// Agent is connected; browser is watching the server.
	agentConn := testConn(t)
	hub.RegisterAgent("srv-1", "agt-1", "user-1", agentConn)
	t.Cleanup(func() { hub.UnregisterAgent("srv-1", agentConn) })
	connID, sendA := hub.RegisterBrowser("user-1", testConn(t))
	hub.Subscribe("srv-1", connID)

	// Step 4: browser issues a command.
	resp := handleBrowserCommand(hub, db, connID, "user-1", "srv-1", deployMsg("cmd-1", "myshop"))
	if resp == nil {
		t.Fatal("expected a response")
	}
	var env struct {
		Type      string `json:"type"`
		CommandID string `json:"command_id"`
	}
	if err := json.Unmarshal(resp, &env); err != nil || env.Type != "command_accepted" || env.CommandID != "cmd-1" {
		t.Fatalf("response = %s, want command_accepted cmd-1", resp)
	}

	// The agent received the normalized envelope.
	agentCh := hub.GetAgentSend("srv-1")
	got := recvWithTimeout(t, agentCh, "agent command envelope")
	var agentMsg struct {
		Type    string `json:"type"`
		Payload struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(got, &agentMsg); err != nil || agentMsg.Type != "command" || agentMsg.Payload.ID != "cmd-1" || agentMsg.Payload.Type != "deploy" {
		t.Fatalf("agent envelope = %s, want command/deploy/cmd-1", got)
	}

	// DB row exists with queued status; pending entry tracked.
	rec, err := queries.GetCommandByID(db, "cmd-1")
	if err != nil || rec == nil || rec.Status != "queued" {
		t.Fatalf("command row: %+v err=%v", rec, err)
	}
	if got := hub.ResolvePendingCommand("cmd-1"); got != connID {
		t.Fatalf("pending owner = %q, want %q", got, connID)
	}

	// Step 7: agent acks -> in_progress + forwarded to the browser.
	ack := []byte(`{"type":"command_ack","payload":{"id":"cmd-1"}}`)
	routeCommandProgress(hub, db, "srv-1", []byte(`{"id":"cmd-1"}`), ack)
	if got := recvWithTimeout(t, sendA, "ack"); string(got) != string(ack) {
		t.Fatalf("browser got %q, want ack", got)
	}
	rec, _ = queries.GetCommandByID(db, "cmd-1")
	if rec.Status != "in_progress" {
		t.Fatalf("status = %q, want in_progress", rec.Status)
	}

	// Step 8: progress forwarded to the browser, entry survives.
	progress := []byte(`{"type":"command_progress","payload":{"id":"cmd-1","percent":42}}`)
	routeCommandProgress(hub, db, "srv-1", []byte(`{"id":"cmd-1"}`), progress)
	if got := recvWithTimeout(t, sendA, "progress"); string(got) != string(progress) {
		t.Fatalf("browser got %q, want progress", got)
	}

	// Step 9: result -> forwarded, DB success, pending removed.
	result := []byte(`{"type":"command_result","payload":{"id":"cmd-1","status":"success"}}`)
	routeCommandResult(hub, db, "srv-1", []byte(`{"id":"cmd-1","status":"success"}`), result)
	if got := recvWithTimeout(t, sendA, "result"); string(got) != string(result) {
		t.Fatalf("browser got %q, want result", got)
	}
	rec, _ = queries.GetCommandByID(db, "cmd-1")
	if rec.Status != "success" || rec.Result == "" {
		t.Fatalf("status = %q result=%q, want success with result", rec.Status, rec.Result)
	}
	if got := hub.ResolvePendingCommand("cmd-1"); got != "" {
		t.Fatal("pending entry should be resolved after the result")
	}
}

func TestCommandRouting_OfflineQueuesAndInforms(t *testing.T) {
	hub := NewHub()
	db := setupCommandRoutingDB(t)

	connID, _ := hub.RegisterBrowser("user-1", testConn(t))
	hub.Subscribe("srv-1", connID)

	// No agent: the command must be queued and the browser informed.
	resp := handleBrowserCommand(hub, db, connID, "user-1", "srv-1", deployMsg("cmd-1", "myshop"))
	var env struct {
		Type      string `json:"type"`
		CommandID string `json:"command_id"`
	}
	if err := json.Unmarshal(resp, &env); err != nil || env.Type != "command_queued" || env.CommandID != "cmd-1" {
		t.Fatalf("response = %s, want command_queued cmd-1", resp)
	}

	// Queued in the offline queue AND recorded in commands (Step 4A).
	if n := pendingCommandCount(t, db, "srv-1"); n != 1 {
		t.Fatalf("pending commands = %d, want 1", n)
	}
	rec, err := queries.GetCommandByID(db, "cmd-1")
	if err != nil || rec == nil || rec.Status != "queued" {
		t.Fatalf("command row: %+v err=%v", rec, err)
	}

	// NOT tracked as pending (spec: do not put in pending_commands).
	if got := hub.ResolvePendingCommand("cmd-1"); got != "" {
		t.Fatalf("queued command must not be pending-tracked, resolved to %q", got)
	}
}

func TestCommandRouting_DuplicateRejected(t *testing.T) {
	hub := NewHub()
	db := setupCommandRoutingDB(t)

	connID, _ := hub.RegisterBrowser("user-1", testConn(t))
	hub.Subscribe("srv-1", connID)

	// An in-progress deploy for myshop already exists.
	if err := queries.InsertCommand(db, "cmd-1", "srv-1", "deploy", "{}", "myshop", "in_progress", "user-1", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	resp := handleBrowserCommand(hub, db, connID, "user-1", "srv-1", deployMsg("cmd-2", "myshop"))
	var env struct {
		Type string `json:"type"`
	}
	json.Unmarshal(resp, &env)
	if env.Type != "error" {
		t.Fatalf("duplicate must be rejected with error, got %s", resp)
	}
	if !strings.Contains(string(resp), "already in progress") {
		t.Fatalf("rejection should mention in-progress: %s", resp)
	}
	// No second row was created.
	rec, _ := queries.GetCommandByID(db, "cmd-2")
	if rec != nil {
		t.Fatalf("duplicate command must not be recorded: %+v", rec)
	}
}

func TestCommandRouting_TimeoutFires(t *testing.T) {
	hub := NewHub()
	db := setupCommandRoutingDB(t)

	agentConn := testConn(t)
	hub.RegisterAgent("srv-1", "agt-1", "user-1", agentConn)
	t.Cleanup(func() { hub.UnregisterAgent("srv-1", agentConn) })
	connID, sendA := hub.RegisterBrowser("user-1", testConn(t))
	hub.Subscribe("srv-1", connID)

	// Short timeout so the test does not wait 10 minutes.
	cmd := parseBrowserCommand(deployMsg("cmd-1", "myshop"))
	resp := handleBrowserCommandWithTimeout(hub, db, connID, "user-1", "srv-1", deployMsg("cmd-1", "myshop"), cmd, 150*time.Millisecond)
	if resp == nil {
		t.Fatal("expected acceptance response")
	}
	// Mirror the handler: the acceptance response is delivered to the browser.
	hub.SendToBrowser(connID, resp)
	recvWithTimeout(t, sendA, "acceptance response")

	timeoutResult := recvWithTimeout(t, sendA, "timeout result")
	var env struct {
		Type   string `json:"type"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(timeoutResult, &env); err != nil || env.Type != "command_result" || env.Status != "timeout" {
		t.Fatalf("timeout message = %s, want command_result status timeout", timeoutResult)
	}
	// Pending entry removed and DB marked timeout.
	if got := hub.ResolvePendingCommand("cmd-1"); got != "" {
		t.Fatal("pending entry should be gone after timeout")
	}
	rec, err := queries.GetCommandByID(db, "cmd-1")
	if err != nil {
		t.Fatalf("GetCommandByID: %v", err)
	}
	if rec == nil || rec.Status != "timeout" {
		t.Fatalf("status = %+v, want timeout", rec)
	}
}

func TestCommandRouting_LateResultAfterTimeoutIsAudited(t *testing.T) {
	hub := NewHub()
	db := setupCommandRoutingDB(t)

	agentConn := testConn(t)
	hub.RegisterAgent("srv-1", "agt-1", "user-1", agentConn)
	t.Cleanup(func() { hub.UnregisterAgent("srv-1", agentConn) })
	connID, sendA := hub.RegisterBrowser("user-1", testConn(t))
	hub.Subscribe("srv-1", connID)

	// The command exists in the DB and was tracked, then timed out.
	_ = queries.InsertCommand(db, "cmd-1", "srv-1", "deploy", "{}", "myshop", "queued", "user-1", time.Now().UTC().Format(time.RFC3339))
	hub.TrackPendingCommand("cmd-1", connID, "srv-1")
	if got := hub.TimeoutPendingCommand("cmd-1"); got != connID {
		t.Fatalf("TimeoutPendingCommand = %q, want %q", got, connID)
	}
	_ = queries.UpdateCommandStatus(db, "cmd-1", "timeout", "Command did not complete within the allotted time.")
	recvWithTimeout(t, sendA, "timeout result")

	// The agent eventually reports a result: audit only, no re-delivery.
	late := []byte(`{"type":"command_result","payload":{"id":"cmd-1","status":"success"}}`)
	routeCommandResult(hub, db, "srv-1", []byte(`{"id":"cmd-1","status":"success"}`), late)
	assertNoMessage(t, sendA, "late result after timeout (must not re-deliver)")

	rec, _ := queries.GetCommandByID(db, "cmd-1")
	if rec.Status != "timeout" {
		t.Fatalf("status should stay timeout, got %q", rec.Status)
	}
	if rec.Result == "" {
		t.Fatal("late result should be recorded for audit")
	}
}

func TestCommandRouting_ResultDeliveredToIssuerAfterReconnect(t *testing.T) {
	hub := NewHub()
	db := setupCommandRoutingDB(t)

	// The issuing dashboard reconnects (same user, new connection).
	connID, sendA := hub.RegisterBrowser("user-1", testConn(t))
	hub.Subscribe("srv-1", connID)

	// A command row exists (issued earlier, agent offline then), no pending entry.
	_ = queries.InsertCommand(db, "cmd-1", "srv-1", "deploy", "{}", "myshop", "queued", "user-1", time.Now().UTC().Format(time.RFC3339))

	result := []byte(`{"type":"command_result","payload":{"id":"cmd-1","status":"success"}}`)
	routeCommandResult(hub, db, "srv-1", []byte(`{"id":"cmd-1","status":"success"}`), result)

	if got := recvWithTimeout(t, sendA, "result to issuing user"); string(got) != string(result) {
		t.Fatalf("browser got %q, want result", got)
	}
	rec, _ := queries.GetCommandByID(db, "cmd-1")
	if rec.Status != "success" {
		t.Fatalf("status = %q, want success", rec.Status)
	}

	// No active connection for the issuer: result stored in DB only.
	_ = queries.InsertCommand(db, "cmd-2", "srv-1", "backup", "{}", "data", "queued", "user-ghost", time.Now().UTC().Format(time.RFC3339))
	routeCommandResult(hub, db, "srv-1", []byte(`{"id":"cmd-2","status":"success"}`), []byte(`{"type":"command_result","payload":{"id":"cmd-2"}}`))
	assertNoMessage(t, sendA, "result with no active issuer connection")
}

func TestCommandRouting_ParseLegacyShape(t *testing.T) {
	// Legacy envelope: type/id live on the inner payload envelope, not on the
	// message's own "type" (which is "command").
	legacy := Message{Type: "command", Payload: json.RawMessage(`{"type":"command","payload":{"type":"deploy","id":"cmd-9","payload":{"project":"myshop"}}}`)}
	cmd := parseBrowserCommand(legacy)
	if cmd.ID != "cmd-9" {
		t.Fatalf("legacy id = %q, want cmd-9", cmd.ID)
	}
	if cmd.Type != "deploy" {
		t.Fatalf("legacy type = %q, want deploy", cmd.Type)
	}
	if cmd.Project != "myshop" {
		t.Fatalf("legacy project = %q, want myshop", cmd.Project)
	}
	if string(cmd.InnerPayload) != `{"project":"myshop"}` {
		t.Fatalf("legacy inner payload = %s", cmd.InnerPayload)
	}

	// New Step 4 shape still parses.
	newShape := parseBrowserCommand(deployMsg("cmd-1", "myshop"))
	if newShape.ID != "cmd-1" || newShape.Type != "deploy" || newShape.Project != "myshop" {
		t.Fatalf("new shape parsed wrong: %+v", newShape)
	}
}

func TestCommandRouting_CommandResultStatusDerivation(t *testing.T) {
	cases := []struct {
		payload string
		want    string
	}{
		{`{"status":"success"}`, "success"},
		{`{"status":"completed"}`, "success"},
		{`{"status":"failed"}`, "failed"},
		{`{"status":"error"}`, "failed"},
		{`{"success":true}`, "success"},
		{`{"error":"boom"}`, "failed"},
		{`{"ok":true}`, "success"},
		{`not json`, "success"},
	}
	for _, c := range cases {
		if got := commandResultStatus(json.RawMessage(c.payload)); got != c.want {
			t.Errorf("commandResultStatus(%q) = %q, want %q", c.payload, got, c.want)
		}
	}
}
