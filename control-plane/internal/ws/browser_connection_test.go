package ws

import (
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/yourname/yourplatform/control-plane/internal/auth"
	_ "modernc.org/sqlite"
)

func TestBrowserConnection_RegisterWithoutSubscription(t *testing.T) {
	hub := NewHub()
	connID, sendA := hub.RegisterBrowser("user-1", testConn(t))

	// No server subscription yet: broadcasts must NOT reach the browser.
	hub.ForwardToBrowsers("srv-1", []byte("before-subscribe"))
	assertNoMessage(t, sendA, "message before subscribe")

	hub.Subscribe("srv-1", connID)
	hub.ForwardToBrowsers("srv-1", []byte("after-subscribe"))
	if got := recvWithTimeout(t, sendA, "message after subscribe"); string(got) != "after-subscribe" {
		t.Fatalf("got %q, want after-subscribe", got)
	}
}

func TestBrowserConnection_SubscribeSwitchesServer(t *testing.T) {
	hub := NewHub()
	connID, sendA := hub.RegisterBrowser("user-1", testConn(t))

	hub.Subscribe("srv-1", connID)
	hub.ForwardToBrowsers("srv-1", []byte("from-1"))
	recvWithTimeout(t, sendA, "first server message")

	// Navigating to srv-2 must auto-unsubscribe from srv-1 (Step 3C Way 3).
	hub.Subscribe("srv-2", connID)
	hub.ForwardToBrowsers("srv-1", []byte("stale-server"))
	assertNoMessage(t, sendA, "message from previous server after switch")
	hub.ForwardToBrowsers("srv-2", []byte("from-2"))
	if got := recvWithTimeout(t, sendA, "new server message"); string(got) != "from-2" {
		t.Fatalf("got %q, want from-2", got)
	}
}

func TestBrowserConnection_UnsubscribeRemoves(t *testing.T) {
	hub := NewHub()
	connID, sendA := hub.RegisterBrowser("user-1", testConn(t))
	hub.Subscribe("srv-1", connID)

	hub.Unsubscribe("srv-1", connID)
	hub.ForwardToBrowsers("srv-1", []byte("after-unsubscribe"))
	assertNoMessage(t, sendA, "message after unsubscribe")
}

func TestBrowserConnection_UnregisterCleansStreamSubs(t *testing.T) {
	hub := NewHub()
	connID, _ := hub.RegisterBrowser("user-1", testConn(t))
	hub.RegisterLogStream("st-1", connID)
	got := hub.LookupLogStream("st-1")
	if len(got) != 1 || got[0] != connID {
		t.Fatalf("LookupLogStream(st-1) = %v, want [%q]", got, connID)
	}

	// Tab close: the browser's unregister must release its log streams.
	hub.UnregisterBrowser(connID)
	if got := hub.LookupLogStream("st-1"); len(got) != 0 {
		t.Fatalf("stream routing should be cleaned on unregister, still %v", got)
	}
}

func TestBrowserConnection_LogStreamTable(t *testing.T) {
	hub := NewHub()
	connID, _ := hub.RegisterBrowser("user-1", testConn(t))

	hub.RegisterLogStream("stream-abc", connID)
	got := hub.LookupLogStream("stream-abc")
	if len(got) != 1 || got[0] != connID {
		t.Fatalf("LookupLogStream = %v, want [%q]", got, connID)
	}
	hub.UnregisterLogStream("stream-abc", connID)
	if got := hub.LookupLogStream("stream-abc"); len(got) != 0 {
		t.Fatalf("stream should be unregistered, still %v", got)
	}
}

func TestBrowserConnection_LogLinesRouteToStreamOwner(t *testing.T) {
	hub := NewHub()
	connIDA, sendA := hub.RegisterBrowser("user-1", testConn(t))
	hub.Subscribe("srv-1", connIDA)
	connIDB, sendB := hub.RegisterBrowser("user-2", testConn(t))
	hub.Subscribe("srv-1", connIDB)

	// Only the stream owner receives stream-scoped log lines.
	hub.RegisterLogStream("stream-1", connIDA)
	line := []byte(`{"type":"log_line","payload":{"stream_id":"stream-1","text":"hello"}}`)
	routeLogLines(hub, "srv-1", []byte(`{"stream_id":"stream-1"}`), line)

	if got := recvWithTimeout(t, sendA, "log line for stream owner"); string(got) != string(line) {
		t.Fatalf("stream owner got %q, want the log line", got)
	}
	assertNoMessage(t, sendB, "other browser (should not receive another browser's stream)")

	// Without a routable stream id, log lines broadcast to all watchers.
	line2 := []byte(`{"type":"log_line","payload":{"text":"global"}}`)
	routeLogLines(hub, "srv-1", []byte(`{"text":"global"}`), line2)
	if got := recvWithTimeout(t, sendA, "broadcast log line"); string(got) != string(line2) {
		t.Fatalf("browser A got %q, want broadcast line", got)
	}
	if got := recvWithTimeout(t, sendB, "broadcast log line"); string(got) != string(line2) {
		t.Fatalf("browser B got %q, want broadcast line", got)
	}
}

// --- Multi-browser log streaming (Step 5C) ---

func TestBrowserConnection_MultipleBrowsersSameStream(t *testing.T) {
	hub := NewHub()
	connIDA, sendA := hub.RegisterBrowser("user-1", testConn(t))
	connIDB, sendB := hub.RegisterBrowser("user-2", testConn(t))

	hub.RegisterLogStream("ms-1", connIDA)
	hub.RegisterLogStream("ms-1", connIDB)

	got := hub.LookupLogStream("ms-1")
	if len(got) != 2 {
		t.Fatalf("LookupLogStream(ms-1) returned %d connIDs, want 2", len(got))
	}

	// Both browsers receive the same log line.
	line := []byte(`{"type":"log_line","payload":{"stream_id":"ms-1","text":"broadcast"}}`)
	routeLogLines(hub, "srv-1", []byte(`{"stream_id":"ms-1"}`), line)
	if got := recvWithTimeout(t, sendA, "browser A log"); string(got) != string(line) {
		t.Fatalf("browser A got %q, want log line", got)
	}
	if got := recvWithTimeout(t, sendB, "browser B log"); string(got) != string(line) {
		t.Fatalf("browser B got %q, want log line", got)
	}
}

func TestBrowserConnection_UnregisterOneBrowserLeavesOthers(t *testing.T) {
	hub := NewHub()
	connIDA, sendA := hub.RegisterBrowser("user-1", testConn(t))
	connIDB, sendB := hub.RegisterBrowser("user-2", testConn(t))

	hub.RegisterLogStream("ms-2", connIDA)
	hub.RegisterLogStream("ms-2", connIDB)

	hub.UnregisterBrowser(connIDA)
	got := hub.LookupLogStream("ms-2")
	if len(got) != 1 || got[0] != connIDB {
		t.Fatalf("after unregister A: LookupLogStream(ms-2) = %v, want [%q]", got, connIDB)
	}

	// Only browser B gets the line now.
	line := []byte(`{"type":"log_line","payload":{"stream_id":"ms-2","text":"for B"}}`)
	routeLogLines(hub, "srv-1", []byte(`{"stream_id":"ms-2"}`), line)
	assertNoMessage(t, sendA, "unregistered browser A")
	if got := recvWithTimeout(t, sendB, "browser B still subscribed"); string(got) != string(line) {
		t.Fatalf("browser B got %q, want log line", got)
	}
}

func TestBrowserConnection_UnregisterSecondBrowserCleansUp(t *testing.T) {
	hub := NewHub()
	connIDA, _ := hub.RegisterBrowser("user-1", testConn(t))
	connIDB, _ := hub.RegisterBrowser("user-2", testConn(t))

	hub.RegisterLogStream("ms-3", connIDA)
	hub.RegisterLogStream("ms-3", connIDB)

	hub.UnregisterBrowser(connIDA)
	hub.UnregisterBrowser(connIDB)
	got := hub.LookupLogStream("ms-3")
	if len(got) != 0 {
		t.Fatalf("after unregistering both: LookupLogStream(ms-3) = %v, want empty", got)
	}
}

// --- Handler-level integration tests (Step 3A/3B/3D end to end) ---

// setupBrowserWSTestDB builds a minimal schema the snapshot queries touch and
// seeds one owned server with a container, a metric sample, and an active alert.
func setupBrowserWSTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE users (id TEXT PRIMARY KEY, email TEXT UNIQUE NOT NULL);
		CREATE TABLE servers (
			id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'disconnected',
			ip_address TEXT NOT NULL DEFAULT '', token TEXT
		);
		CREATE TABLE server_team (server_id TEXT PRIMARY KEY, team_id TEXT NOT NULL);
		CREATE TABLE team_members (team_id TEXT NOT NULL, user_id TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'member');
		CREATE TABLE alerts (
			id TEXT PRIMARY KEY, server_id TEXT NOT NULL, project TEXT, container TEXT,
			severity TEXT NOT NULL, type TEXT NOT NULL, status TEXT NOT NULL,
			title TEXT, message TEXT, detail TEXT, action TEXT, metrics TEXT,
			fired_at TEXT, resolved_at TEXT, read_at TEXT, acknowledged_at TEXT, acknowledged_by TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE TABLE container_status (
			id TEXT PRIMARY KEY, server_id TEXT NOT NULL,
			project TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'app', container_id TEXT NOT NULL,
			status TEXT NOT NULL, health TEXT, cpu_percent REAL NOT NULL DEFAULT 0,
			ram_used_mb INTEGER NOT NULL DEFAULT 0, ram_limit_mb INTEGER NOT NULL DEFAULT 0,
			ram_percent REAL NOT NULL DEFAULT 0, restart_count INTEGER NOT NULL DEFAULT 0,
			uptime_secs INTEGER NOT NULL DEFAULT 0, exit_code INTEGER,
			net_rx_bytes INTEGER NOT NULL DEFAULT 0, net_tx_bytes INTEGER NOT NULL DEFAULT 0,
			last_seen TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE TABLE metrics_history (
			id TEXT PRIMARY KEY, server_id TEXT NOT NULL, recorded_at TEXT NOT NULL, collected_in_ms INTEGER,
			cpu_percent REAL, ram_used_mb INTEGER, ram_total_mb INTEGER, ram_percent REAL,
			disk_used_gb REAL, disk_total_gb REAL, disk_percent REAL, load_1min REAL, load_per_core REAL,
			caddy_running INTEGER NOT NULL DEFAULT 0, caddy_routes_count INTEGER NOT NULL DEFAULT 0,
			last_backup_age_sec INTEGER, container_count INTEGER NOT NULL DEFAULT 0
		);
		INSERT INTO users (id, email) VALUES ('user-1', 'owner@example.com'), ('user-2', 'intruder@example.com');
		INSERT INTO servers (id, user_id, name, status) VALUES ('srv-1', 'user-1', 'prod', 'connected');
		INSERT INTO container_status (id, server_id, project, role, container_id, status, health, cpu_percent, ram_used_mb, ram_limit_mb, ram_percent, restart_count, uptime_secs)
			VALUES ('c-1', 'srv-1', 'blog', 'web', 'abc123', 'running', 'healthy', 5.5, 128, 512, 25, 0, 3600);
		INSERT INTO metrics_history (id, server_id, recorded_at, cpu_percent, ram_percent, disk_percent, ram_used_mb, ram_total_mb, caddy_running, caddy_routes_count, container_count)
			VALUES ('m-1', 'srv-1', '2026-08-05T00:00:00Z', 12.5, 33.0, 40.0, 2048, 8192, 1, 3, 1);
		INSERT INTO alerts (id, server_id, severity, type, status, title, fired_at)
			VALUES ('a-1', 'srv-1', 'warning', 'cpu_high', 'active', 'High CPU', '2026-08-05T00:00:00Z');
	`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// dialBrowserWS connects a test dashboard to HandleBrowserWS with a JWT.
func dialBrowserWS(t *testing.T, hub *Hub, db *sql.DB, jwtSecret, token string) (*websocket.Conn, string) {
	t.Helper()
	srv := httptest.NewServer(HandleBrowserWS(hub, db, jwtSecret))
	t.Cleanup(srv.Close)
	url := "ws" + srv.URL[4:] + "/ws/browser?token=" + token
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial browser ws: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn, srv.URL
}

// readWSMsg reads one WebSocket message with a deadline and decodes it.
func readWSMsg(t *testing.T, conn *websocket.Conn) map[string]interface{} {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read ws message: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("decode ws message %q: %v", data, err)
	}
	return m
}

func TestBrowserWS_WelcomePingAndSubscribeSnapshot(t *testing.T) {
	hub := NewHub()
	db := setupBrowserWSTestDB(t)
	const jwtSecret = "test-secret"
	token, err := auth.GenerateAccessToken("user-1", "sess-1", "owner@example.com", "Owner", jwtSecret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	conn, _ := dialBrowserWS(t, hub, db, jwtSecret, token)

	// Step 3A: welcome message with the connection id.
	welcome := readWSMsg(t, conn)
	if typ, _ := welcome["type"].(string); typ != "connected" {
		t.Fatalf("first message type = %v, want connected", welcome["type"])
	}
	if _, ok := welcome["connection_id"].(string); !ok {
		t.Fatalf("welcome missing connection_id: %v", welcome)
	}

	// Step 3D: browser ping receives a pong.
	if err := conn.WriteJSON(map[string]interface{}{"type": "ping"}); err != nil {
		t.Fatal(err)
	}
	if pong := readWSMsg(t, conn); pong["type"] != "pong" {
		t.Fatalf("got %v, want pong", pong["type"])
	}

	// Step 3B: subscribe sends the immediate server_state snapshot.
	if err := conn.WriteJSON(map[string]interface{}{"type": "subscribe", "server_id": "srv-1"}); err != nil {
		t.Fatal(err)
	}
	snap := readWSMsg(t, conn)
	if snap["type"] != "server_state" {
		t.Fatalf("got type %v, want server_state", snap["type"])
	}
	payload, _ := snap["payload"].(map[string]interface{})
	server, _ := payload["server"].(map[string]interface{})
	if server["status"] != "connected" {
		t.Fatalf("snapshot status = %v, want connected", server["status"])
	}
	metrics, _ := server["metrics"].(map[string]interface{})
	if cpu, _ := metrics["cpu_percent"].(float64); cpu != 12.5 {
		t.Fatalf("snapshot cpu_percent = %v, want 12.5", metrics["cpu_percent"])
	}
	alerts, _ := server["alerts"].([]interface{})
	if len(alerts) != 1 {
		t.Fatalf("snapshot alerts = %d, want 1 active alert", len(alerts))
	}
	containers, _ := server["containers"].([]interface{})
	if len(containers) != 1 {
		t.Fatalf("snapshot containers = %d, want 1", len(containers))
	}

	// The subscribe also makes the browser a watcher: agent_connected flows.
	agentConn := testConn(t)
	hub.RegisterAgent("srv-1", "agt-1", "user-1", agentConn)
	event := readWSMsg(t, conn)
	if event["type"] != "agent_connected" {
		t.Fatalf("got %v, want agent_connected event for subscribed browser", event["type"])
	}
	hub.UnregisterAgent("srv-1", agentConn)
	event = readWSMsg(t, conn)
	if event["type"] != "agent_disconnected" {
		t.Fatalf("got %v, want agent_disconnected event for subscribed browser", event["type"])
	}
}

func TestBrowserWS_SubscribeDeniedWithoutAccess(t *testing.T) {
	hub := NewHub()
	db := setupBrowserWSTestDB(t)
	const jwtSecret = "test-secret"
	token, err := auth.GenerateAccessToken("user-2", "sess-2", "intruder@example.com", "Intruder", jwtSecret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	conn, _ := dialBrowserWS(t, hub, db, jwtSecret, token)
	welcome := readWSMsg(t, conn)
	if typ, _ := welcome["type"].(string); typ != "connected" {
		t.Fatalf("first message type = %v, want connected", welcome["type"])
	}

	// user-2 owns no server and shares no team: subscribe must be rejected.
	if err := conn.WriteJSON(map[string]interface{}{"type": "subscribe", "server_id": "srv-1"}); err != nil {
		t.Fatal(err)
	}
	resp := readWSMsg(t, conn)
	if resp["type"] != "error" {
		t.Fatalf("got type %v, want error for unauthorized subscribe", resp["type"])
	}
	payload, _ := resp["payload"].(map[string]interface{})
	if msg, _ := payload["message"].(string); msg == "" {
		t.Fatalf("error response missing message: %v", resp)
	}

	// And the unauthorized subscribe must not have created a subscription:
	// a broadcast to srv-1 must NOT reach this browser (nothing else in the
	// pipeline writes to it, so a timed-out read is the assertion).
	hub.ForwardToBrowsers("srv-1", []byte(`{"type":"state_update"}`))
	conn.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("unauthorized browser received a broadcast it should not get")
	}
}
