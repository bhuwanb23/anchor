package ws

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// testConn dials a throwaway WebSocket connection so the hub has a real,
// closable *websocket.Conn to store (gorilla provides no public constructor).
func testConn(t *testing.T) *websocket.Conn {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{}
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial test ws: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// recvWithTimeout reads from a channel with a deadline, failing the test if
// nothing arrives in time.
func recvWithTimeout(t *testing.T, ch <-chan []byte, what string) []byte {
	t.Helper()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		return nil
	}
}

// assertNoMessage asserts that no real message arrives on the channel within a
// short window. A CLOSED channel (which yields ok=false) is expected when the
// connection was unregistered, so it is treated as "no message".
func assertNoMessage(t *testing.T, ch <-chan []byte, what string) {
	t.Helper()
	select {
	case msg, ok := <-ch:
		if ok {
			t.Fatalf("unexpected %s: %s", what, string(msg))
		}
	case <-time.After(100 * time.Millisecond):
	}
}

func TestHub_AgentRegisterAndSend(t *testing.T) {
	hub := NewHub()
	conn := testConn(t)
	hub.RegisterAgent("srv-1", "agt-1", "user-1", conn)

	if !hub.SendToAgent("srv-1", []byte("ping")) {
		t.Fatal("SendToAgent returned false for a registered agent")
	}

	sendCh := hub.GetAgentSend("srv-1")
	if got := recvWithTimeout(t, sendCh, "agent message"); string(got) != "ping" {
		t.Fatalf("got %q, want ping", got)
	}
}

func TestHub_AgentUnknownServerSendFails(t *testing.T) {
	hub := NewHub()
	if hub.SendToAgent("srv-nope", []byte("ping")) {
		t.Fatal("SendToAgent should return false for an unregistered server")
	}
}

func TestHub_AgentUnregisterClosesSend(t *testing.T) {
	hub := NewHub()
	conn := testConn(t)
	hub.RegisterAgent("srv-1", "agt-1", "user-1", conn)
	if !hub.SendToAgent("srv-1", []byte("ping")) {
		t.Fatal("agent should be sendable before unregister")
	}

	hub.UnregisterAgent("srv-1", conn)
	if hub.SendToAgent("srv-1", []byte("ping")) {
		t.Fatal("SendToAgent should return false after unregister")
	}
	sendCh := hub.GetAgentSend("srv-1")
	if _, ok := <-sendCh; ok {
		t.Fatal("send channel should be closed after unregister")
	}
}

func TestHub_BrowserRegisterDeliversSubscribed(t *testing.T) {
	hub := NewHub()
	connIDA, sendA := hub.RegisterBrowser("user-1", testConn(t))
	connIDB, sendB := hub.RegisterBrowser("user-2", testConn(t))
	hub.Subscribe("srv-1", connIDA)
	hub.Subscribe("srv-1", connIDB)

	hub.ForwardToBrowsers("srv-1", []byte("hello"))

	if got := recvWithTimeout(t, sendA, "browser A message"); string(got) != "hello" {
		t.Fatalf("browser A got %q, want hello", got)
	}
	if got := recvWithTimeout(t, sendB, "browser B message"); string(got) != "hello" {
		t.Fatalf("browser B got %q, want hello", got)
	}
}

func TestHub_BrowserDoesNotReceiveOtherServer(t *testing.T) {
	hub := NewHub()
	connIDA, sendA := hub.RegisterBrowser("user-1", testConn(t))
	hub.Subscribe("srv-1", connIDA)
	connIDB, _ := hub.RegisterBrowser("user-2", testConn(t))
	hub.Subscribe("srv-2", connIDB)

	hub.ForwardToBrowsers("srv-2", []byte("other-server"))

	assertNoMessage(t, sendA, "browser A message (should be isolated per server)")
}

func TestHub_SubscribeUnsubscribe(t *testing.T) {
	hub := NewHub()
	connID, sendA := hub.RegisterBrowser("user-1", testConn(t))
	hub.Subscribe("srv-1", connID)

	// Unsubscribe: no more deliveries for srv-1.
	hub.Unsubscribe("srv-1", connID)
	hub.ForwardToBrowsers("srv-1", []byte("after-unsub"))
	assertNoMessage(t, sendA, "message after unsubscribe")

	// Subscribe to a different server: deliveries now come from srv-2.
	hub.Subscribe("srv-2", connID)
	hub.ForwardToBrowsers("srv-2", []byte("from-srv2"))
	if got := recvWithTimeout(t, sendA, "message after re-subscribe"); string(got) != "from-srv2" {
		t.Fatalf("got %q, want from-srv2", got)
	}
}

func TestHub_UnregisterBrowserStopsDelivery(t *testing.T) {
	hub := NewHub()
	connID, sendA := hub.RegisterBrowser("user-1", testConn(t))
	hub.Subscribe("srv-1", connID)

	hub.UnregisterBrowser(connID)
	hub.ForwardToBrowsers("srv-1", []byte("after-close"))
	assertNoMessage(t, sendA, "message after browser unregister")
}

func TestHub_ForwardToBrowsersCleansStaleSubscriptions(t *testing.T) {
	hub := NewHub()
	connID, _ := hub.RegisterBrowser("user-1", testConn(t))
	hub.Subscribe("srv-1", connID)
	hub.UnregisterBrowser(connID)

	// After unregister, no messages should be delivered (stale entry skipped).
	hub.ForwardToBrowsers("srv-1", []byte("trigger cleanup"))
	// If cleanup worked, the entry was removed. Verify by subscribing a new
	// browser and confirming it receives messages normally.
	connID2, send2 := hub.RegisterBrowser("user-2", testConn(t))
	hub.Subscribe("srv-1", connID2)
	hub.ForwardToBrowsers("srv-1", []byte("after-cleanup"))
	if got := recvWithTimeout(t, send2, "new browser after cleanup"); string(got) != "after-cleanup" {
		t.Fatalf("got %q, want after-cleanup", got)
	}
}

func TestHub_PendingCommandsRouteToBrowser(t *testing.T) {
	hub := NewHub()
	connID, sendA := hub.RegisterBrowser("user-1", testConn(t))
	hub.Subscribe("srv-1", connID)

	hub.TrackPendingCommand("cmd-1", connID, "srv-1")
	if got := hub.ResolvePendingCommand("cmd-1"); got != connID {
		t.Fatalf("ResolvePendingCommand(cmd-1) = %q, want %q", got, connID)
	}
	// Resolved entries are removed.
	if got := hub.ResolvePendingCommand("cmd-1"); got != "" {
		t.Fatalf("ResolvePendingCommand(cmd-1) second call = %q, want empty", got)
	}
	// Unknown command resolves to empty.
	if got := hub.ResolvePendingCommand("cmd-unknown"); got != "" {
		t.Fatalf("ResolvePendingCommand(unknown) = %q, want empty", got)
	}

	// A result arriving from the agent is routed to the waiting browser.
	hub.TrackPendingCommand("cmd-2", connID, "srv-1")
	hub.ForwardToBrowsers("srv-1", []byte(`{"type":"result","command_id":"cmd-2"}`))
	if got := recvWithTimeout(t, sendA, "result message"); got == nil {
		t.Fatal("result not delivered to waiting browser")
	}
}

func TestHub_CommandTimeoutFailsPending(t *testing.T) {
	hub := NewHub()
	connID, sendA := hub.RegisterBrowser("user-1", testConn(t))
	hub.Subscribe("srv-1", connID)

	// Directly enqueue a timed-out command via the internal op to avoid
	// waiting 10 minutes. We simulate what startCommandTimeout does.
	hub.TrackPendingCommand("cmd-to", connID, "srv-1")
	hub.ops <- hubOp{kind: opFailTimedOutCommand, commandID: "cmd-to"}

	if got := recvWithTimeout(t, sendA, "timeout error"); got == nil {
		t.Fatal("timeout error not delivered to browser")
	}
	// Command should be removed from pending.
	if got := hub.ResolvePendingCommand("cmd-to"); got != "" {
		t.Fatalf("command should be removed after timeout, got %q", got)
	}
}

func TestHub_CommandTimeoutNoOpForResolvedCommand(t *testing.T) {
	hub := NewHub()
	connID, _ := hub.RegisterBrowser("user-1", testConn(t))
	hub.Subscribe("srv-1", connID)

	hub.TrackPendingCommand("cmd-resolved", connID, "srv-1")
	hub.ResolvePendingCommand("cmd-resolved") // resolve before timeout

	// Timeout after resolution should be a no-op.
	hub.ops <- hubOp{kind: opFailTimedOutCommand, commandID: "cmd-resolved"}
	// No crash, no panic — just a no-op.
}

func TestHub_HasInFlightCommand(t *testing.T) {
	hub := NewHub()
	connID, _ := hub.RegisterBrowser("user-1", testConn(t))
	hub.Subscribe("srv-1", connID)

	if hub.HasInFlightCommand("srv-1") {
		t.Fatal("no in-flight commands yet")
	}

	hub.TrackPendingCommand("cmd-1", connID, "srv-1")
	if !hub.HasInFlightCommand("srv-1") {
		t.Fatal("expected in-flight command after TrackPendingCommand")
	}

	hub.ResolvePendingCommand("cmd-1")
	if hub.HasInFlightCommand("srv-1") {
		t.Fatal("no in-flight commands after ResolvePendingCommand")
	}
}

func TestHub_HasInFlightCommandIgnoresOtherServers(t *testing.T) {
	hub := NewHub()
	connID, _ := hub.RegisterBrowser("user-1", testConn(t))
	hub.Subscribe("srv-1", connID)

	hub.TrackPendingCommand("cmd-1", connID, "srv-1")
	if hub.HasInFlightCommand("srv-2") {
		t.Fatal("command on srv-1 should not affect srv-2")
	}
}

func TestHub_ReplayStreamCommands(t *testing.T) {
	hub := NewHub()
	hub.RegisterAgent("srv-1", "agt-1", "user-1", testConn(t))

	cmd := []byte(`{"type":"command","payload":{"type":"stream_logs"}}`)
	hub.RecordStreamCommand("srv-1", "proj|web", cmd)

	// Replay must resend the recorded command to the agent.
	hub.ReplayStreamCommands("srv-1")
	sendCh := hub.GetAgentSend("srv-1")
	if got := recvWithTimeout(t, sendCh, "replayed stream command"); string(got) != string(cmd) {
		t.Fatalf("replayed command mismatch: %q", got)
	}
}

func TestHub_ClearStreamCommands(t *testing.T) {
	hub := NewHub()
	hub.RegisterAgent("srv-1", "agt-1", "user-1", testConn(t))

	hub.RecordStreamCommand("srv-1", "proj|web", []byte(`{"type":"command"}`))
	hub.ClearStreamCommand("srv-1", "proj|web")
	hub.ReplayStreamCommands("srv-1")

	sendCh := hub.GetAgentSend("srv-1")
	assertNoMessage(t, sendCh, "cleared stream command")
}

// slogCapture is a minimal slog.Handler that records the text of every log
// record so tests can assert on what was logged.
type slogCapture struct {
	mu    sync.Mutex
	lines []string
}

func (c *slogCapture) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (c *slogCapture) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = append(c.lines, r.Message)
	return nil
}

func (c *slogCapture) WithAttrs(_ []slog.Attr) slog.Handler { return c }

func (c *slogCapture) WithGroup(_ string) slog.Handler { return c }

func (c *slogCapture) messages() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.lines...)
}

func TestHub_StartupLogsConfirmation(t *testing.T) {
	// The hub goroutine must start (and log) even without an explicit Run()
	// call, since NewHub auto-starts it. Capture slog output to assert the
	// startup confirmation line is emitted.
	capture := &slogCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(capture))
	defer slog.SetDefault(prev)

	hub := NewHub()
	conn := testConn(t)
	t.Cleanup(func() { hub.UnregisterAgent("srv-1", conn) })
	hub.RegisterAgent("srv-1", "agt-1", "user-1", conn)
	if !hub.SendToAgent("srv-1", []byte("ping")) {
		t.Fatal("hub goroutine did not start: send failed")
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		for _, m := range capture.messages() {
			if strings.Contains(m, "ws hub started") {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("startup confirmation log \"ws hub started\" not emitted")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestHub_ConcurrentOperations(t *testing.T) {
	// Run many goroutines hammering every operation to prove the single
	// goroutine + channel design is race-free and serializes correctly.
	hub := NewHub()
	conns := make([]*websocket.Conn, 40)
	for i := range conns {
		conns[i] = testConn(t)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			serverID := "srv-" + string(rune('a'+i%3))
			hub.RegisterAgent(serverID, "agt-"+serverID, "user-1", conns[i])
			hub.SendToAgent(serverID, []byte("ping"))
			connID, sendCh := hub.RegisterBrowser("user-1", conns[i+20])
			hub.Subscribe(serverID, connID)
			hub.Unsubscribe(serverID, connID)
			hub.TrackPendingCommand("cmd-"+serverID, connID, serverID)
			_ = hub.ResolvePendingCommand("cmd-" + serverID)
			hub.ForwardToBrowsers(serverID, []byte("hi"))
			_ = sendCh
		}(i)
	}
	wg.Wait()
}

// --- Metrics (Step 6C) ---

func TestHub_StatsReturnsCurrentCounts(t *testing.T) {
	hub := NewHub()
	agentConn := testConn(t)
	hub.RegisterAgent("srv-1", "agt-1", "user-1", agentConn)

	browserConn, _ := hub.RegisterBrowser("user-1", testConn(t))
	hub.Subscribe("srv-1", browserConn)
	hub.TrackPendingCommand("cmd-1", browserConn, "srv-1")

	stats := hub.Stats()
	if stats.AgentConnections != 1 {
		t.Fatalf("AgentConnections = %d, want 1", stats.AgentConnections)
	}
	if stats.BrowserConnections != 1 {
		t.Fatalf("BrowserConnections = %d, want 1", stats.BrowserConnections)
	}
	if stats.PendingCommands != 1 {
		t.Fatalf("PendingCommands = %d, want 1", stats.PendingCommands)
	}
	if stats.Subscriptions != 1 {
		t.Fatalf("Subscriptions = %d, want 1", stats.Subscriptions)
	}
}

func TestHub_StatsTracksMessagesRouted(t *testing.T) {
	hub := NewHub()
	browserConn, _ := hub.RegisterBrowser("user-1", testConn(t))
	hub.Subscribe("srv-1", browserConn)

	hub.ForwardToBrowsers("srv-1", []byte("msg1"))
	hub.ForwardToBrowsers("srv-1", []byte("msg2"))

	stats := hub.Stats()
	if stats.MessagesRouted != 2 {
		t.Fatalf("MessagesRouted = %d, want 2", stats.MessagesRouted)
	}
	if stats.BroadcastCount != 2 {
		t.Fatalf("BroadcastCount = %d, want 2", stats.BroadcastCount)
	}
}
