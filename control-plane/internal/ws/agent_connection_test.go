package ws

import (
	"encoding/json"
	"testing"
	"time"
)

// drain receives one message, failing if nothing arrives. Used to skip over
// expected messages (e.g. agent_connected) to reach the one under test.
func drain(t *testing.T, ch <-chan []byte, what string) []byte {
	t.Helper()
	return recvWithTimeout(t, ch, what)
}

func TestAgentConnection_BrowserNotifiedOnConnect(t *testing.T) {
	hub := NewHub()
	_, sendA := hub.RegisterBrowser("srv-1", "user-1", testConn(t))

	conn := testConn(t)
	hub.RegisterAgent("srv-1", "agt-1", "user-1", conn)
	t.Cleanup(func() { hub.UnregisterAgent("srv-1", conn) })

	msg := drain(t, sendA, "agent_connected event")
	var env struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(msg, &env); err != nil || env.Type != "agent_connected" {
		t.Fatalf("got %q, want agent_connected event", msg)
	}
}

func TestAgentConnection_BrowserNotifiedOnDisconnect(t *testing.T) {
	hub := NewHub()
	_, sendA := hub.RegisterBrowser("srv-1", "user-1", testConn(t))

	conn := testConn(t)
	hub.RegisterAgent("srv-1", "agt-1", "user-1", conn)
	drain(t, sendA, "agent_connected event")

	hub.UnregisterAgent("srv-1", conn)

	msg := drain(t, sendA, "agent_disconnected event")
	var env struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(msg, &env); err != nil || env.Type != "agent_disconnected" {
		t.Fatalf("got %q, want agent_disconnected event", msg)
	}
}

func TestAgentConnection_DuplicateReplacesOldConnection(t *testing.T) {
	hub := NewHub()

	connA := testConn(t)
	chA := hub.RegisterAgent("srv-1", "agt-1", "user-1", connA)

	// A second agent for the same server replaces the first (agent restarted
	// before the old socket timed out).
	connB := testConn(t)
	chB := hub.RegisterAgent("srv-1", "agt-2", "user-1", connB)

	// The old send channel must be closed by the hub.
	select {
	case _, ok := <-chA:
		if ok {
			t.Fatal("old agent send channel should be closed after replacement")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for old send channel to close")
	}

	// The new agent is the live one.
	if !hub.SendToAgent("srv-1", []byte("to-new")) {
		t.Fatal("new agent should be sendable after replacement")
	}
	if got := recvWithTimeout(t, chB, "message on new channel"); string(got) != "to-new" {
		t.Fatalf("new agent got %q, want to-new", got)
	}

	// The stale reader's unregister (old conn) must NOT kill the new agent:
	// it reports false (nothing removed), so the handler must not stamp the
	// DB "disconnected" while the replacement is live.
	if removed := hub.UnregisterAgent("srv-1", connA); removed {
		t.Fatal("stale unregister must report false (nothing removed)")
	}
	if !hub.SendToAgent("srv-1", []byte("still-alive")) {
		t.Fatal("stale unregister must not remove the replacement agent")
	}

	// The current agent's unregister does remove it and reports true.
	if removed := hub.UnregisterAgent("srv-1", connB); !removed {
		t.Fatal("current agent unregister must report true")
	}
	if hub.SendToAgent("srv-1", []byte("after-close")) {
		t.Fatal("SendToAgent should fail after the current agent unregisters")
	}
}

func TestAgentConnection_PendingCommandFailedOnAgentDisconnect(t *testing.T) {
	hub := NewHub()
	connID, sendA := hub.RegisterBrowser("srv-1", "user-1", testConn(t))

	conn := testConn(t)
	hub.RegisterAgent("srv-1", "agt-1", "user-1", conn)
	drain(t, sendA, "agent_connected event")

	// Browser issued a command the agent was executing.
	hub.TrackPendingCommand("cmd-1", connID, "srv-1")

	// Agent drops: the pending command must be failed and reported.
	hub.UnregisterAgent("srv-1", conn)

	var gotErr, gotDisc bool
	for i := 0; i < 2; i++ {
		msg := drain(t, sendA, "failure/event message")
		var env struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(msg, &env); err != nil {
			t.Fatalf("bad message %q: %v", msg, err)
		}
		switch env.Type {
		case "command_error":
			gotErr = true
		case "agent_disconnected":
			gotDisc = true
		}
	}
	if !gotErr {
		t.Fatal("waiting browser did not receive command_error on agent disconnect")
	}
	if !gotDisc {
		t.Fatal("waiting browser did not receive agent_disconnected event")
	}
	// The failed entry must be gone.
	if got := hub.ResolvePendingCommand("cmd-1"); got != "" {
		t.Fatalf("pending command should be removed after failure, still resolves to %q", got)
	}
}

func TestAgentConnection_CommandResultRoutesToWaitingBrowserOnly(t *testing.T) {
	hub := NewHub()
	connIDA, sendA := hub.RegisterBrowser("srv-1", "user-1", testConn(t))
	_, sendB := hub.RegisterBrowser("srv-1", "user-2", testConn(t))
	conn := testConn(t)
	hub.RegisterAgent("srv-1", "agt-1", "user-1", conn)
	t.Cleanup(func() { hub.UnregisterAgent("srv-1", conn) })
	drain(t, sendA, "agent_connected event")
	drain(t, sendB, "agent_connected event")

	hub.TrackPendingCommand("cmd-1", connIDA, "srv-1")

	// Agent completes the command.
	result := []byte(`{"type":"command_result","payload":{"id":"cmd-1","ok":true}}`)
	routeCommandResult(hub, "srv-1", []byte(`{"id":"cmd-1"}`), result)

	if got := recvWithTimeout(t, sendA, "result for waiting browser"); string(got) != string(result) {
		t.Fatalf("waiting browser got %q, want the command result", got)
	}
	assertNoMessage(t, sendB, "other browser (should not receive another browser's result)")

	// The entry is resolved/removed.
	if got := hub.ResolvePendingCommand("cmd-1"); got != "" {
		t.Fatal("command result should have removed the pending entry")
	}
}

func TestAgentConnection_CommandProgressKeepsPending(t *testing.T) {
	hub := NewHub()
	connID, sendA := hub.RegisterBrowser("srv-1", "user-1", testConn(t))
	conn := testConn(t)
	hub.RegisterAgent("srv-1", "agt-1", "user-1", conn)
	t.Cleanup(func() { hub.UnregisterAgent("srv-1", conn) })
	drain(t, sendA, "agent_connected event")

	hub.TrackPendingCommand("cmd-1", connID, "srv-1")

	progress := []byte(`{"type":"command_progress","payload":{"id":"cmd-1","percent":42}}`)
	routeCommandProgress(hub, "srv-1", []byte(`{"id":"cmd-1"}`), progress)

	if got := recvWithTimeout(t, sendA, "progress for waiting browser"); string(got) != string(progress) {
		t.Fatalf("waiting browser got %q, want progress", got)
	}
	// Progress must not consume the pending entry.
	if got := hub.ResolvePendingCommand("cmd-1"); got != connID {
		t.Fatalf("pending entry should survive progress, resolved to %q, want %q", got, connID)
	}
}

func TestAgentConnection_CommandResultFallbackBroadcasts(t *testing.T) {
	hub := NewHub()
	_, sendA := hub.RegisterBrowser("srv-1", "user-1", testConn(t))
	_, sendB := hub.RegisterBrowser("srv-1", "user-2", testConn(t))
	conn := testConn(t)
	hub.RegisterAgent("srv-1", "agt-1", "user-1", conn)
	t.Cleanup(func() { hub.UnregisterAgent("srv-1", conn) })
	drain(t, sendA, "agent_connected event")
	drain(t, sendB, "agent_connected event")

	// Untracked command (e.g. server-initiated): result is broadcast.
	result := []byte(`{"type":"command_result","payload":{"id":"untracked"}}`)
	routeCommandResult(hub, "srv-1", []byte(`{"id":"untracked"}`), result)

	if got := recvWithTimeout(t, sendA, "broadcast result"); string(got) != string(result) {
		t.Fatalf("browser A got %q, want broadcast result", got)
	}
	if got := recvWithTimeout(t, sendB, "broadcast result"); string(got) != string(result) {
		t.Fatalf("browser B got %q, want broadcast result", got)
	}
}

func TestAgentConnection_CommandIDExtraction(t *testing.T) {
	cases := []struct {
		payload string
		want    string
	}{
		{`{"id":"cmd-a"}`, "cmd-a"},
		{`{"command_id":"cmd-b"}`, "cmd-b"},
		{`{"cmd_id":"cmd-c"}`, "cmd-c"},
		{`{"id":"","command_id":"cmd-d"}`, "cmd-d"},
		{`{"type":"deploy"}`, ""},
		{`not json`, ""},
		{``, ""},
	}
	for _, c := range cases {
		if got := commandIDFromPayload(json.RawMessage(c.payload)); got != c.want {
			t.Errorf("commandIDFromPayload(%q) = %q, want %q", c.payload, got, c.want)
		}
	}
}
