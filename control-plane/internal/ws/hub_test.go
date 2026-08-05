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
	hub.RegisterAgent("srv-1", "agt-1", "user-1", testConn(t))
	if !hub.SendToAgent("srv-1", []byte("ping")) {
		t.Fatal("agent should be sendable before unregister")
	}

	hub.UnregisterAgent("srv-1")
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
	_, sendA := hub.RegisterBrowser("srv-1", "user-1", testConn(t))
	_, sendB := hub.RegisterBrowser("srv-1", "user-2", testConn(t))

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
	_, sendA := hub.RegisterBrowser("srv-1", "user-1", testConn(t))
	hub.RegisterBrowser("srv-2", "user-2", testConn(t))

	hub.ForwardToBrowsers("srv-2", []byte("other-server"))

	assertNoMessage(t, sendA, "browser A message (should be isolated per server)")
}

func TestHub_SubscribeUnsubscribe(t *testing.T) {
	hub := NewHub()
	connID, sendA := hub.RegisterBrowser("srv-1", "user-1", testConn(t))

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
	connID, sendA := hub.RegisterBrowser("srv-1", "user-1", testConn(t))

	hub.UnregisterBrowser(connID)
	hub.ForwardToBrowsers("srv-1", []byte("after-close"))
	assertNoMessage(t, sendA, "message after browser unregister")
}

func TestHub_PendingCommandsRouteToBrowser(t *testing.T) {
	hub := NewHub()
	connID, sendA := hub.RegisterBrowser("srv-1", "user-1", testConn(t))

	hub.TrackPendingCommand("cmd-1", connID)
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
	hub.TrackPendingCommand("cmd-2", connID)
	hub.ForwardToBrowsers("srv-1", []byte(`{"type":"result","command_id":"cmd-2"}`))
	if got := recvWithTimeout(t, sendA, "result message"); got == nil {
		t.Fatal("result not delivered to waiting browser")
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
	t.Cleanup(func() { hub.UnregisterAgent("srv-1") })
	hub.RegisterAgent("srv-1", "agt-1", "user-1", testConn(t))
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
			connID, sendCh := hub.RegisterBrowser(serverID, "user-1", conns[i+20])
			hub.Subscribe(serverID, connID)
			hub.Unsubscribe(serverID, connID)
			hub.TrackPendingCommand("cmd-"+serverID, connID)
			_ = hub.ResolvePendingCommand("cmd-" + serverID)
			hub.ForwardToBrowsers(serverID, []byte("hi"))
			_ = sendCh
		}(i)
	}
	wg.Wait()
}
