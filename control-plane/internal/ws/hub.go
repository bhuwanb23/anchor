package ws

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
)

// Hub is the central real-time router of the control plane (Layer 5B).
//
// It is implemented as a SINGLE goroutine that owns all connection state:
//   - agents:          server_id -> connected agent
//   - browsers:        connection_id -> connected dashboard (browser)
//   - subscriptions:   server_id -> set of browser connection_ids watching it
//   - pendingCommands: command_id -> {browser connection_id, server_id} awaiting
//     the result (server_id lets the hub fail them when the agent disconnects)
//   - streamSubs:      stream_id -> browser connection_id (log routing)
//
// Every operation is delivered through the ops channel. The hub goroutine is
// the ONLY reader of the maps, so no locks are needed and all state changes
// are serialized naturally. Connections themselves run in their own goroutines
// (reading from their WebSocket, writing to the hub channel).
type Hub struct {
	ops     chan hubOp
	started sync.Once

	// The maps below are owned exclusively by the hub goroutine. Never read or
	// write them from any other goroutine — use the Hub methods (which enqueue
	// ops) instead.
	agents          map[string]*AgentConn
	agentsByAgentID map[string]*AgentConn
	browsers        map[string]*BrowserConn
	subscriptions   map[string]map[string]struct{}
	pendingCommands map[string]pendingCommandEntry
	// streamSubs routes agent log output (by stream_id) to the browser
	// connections that requested it (Layer 5B Step 5C). Multiple browsers
	// can watch the same stream simultaneously.
	streamSubs map[string]map[string]struct{}
	// streams records per-server log-stream desires requested by dashboards so
	// the control plane can re-establish live log views when an agent
	// reconnects (Layer 4C 3B). Keyed by a project|roles signature.
	streams map[string]map[string][]byte

	// Metrics counters (owned exclusively by hub goroutine).
	messagesRouted   int64
	broadcastCount   int64
	broadcastFanout  int64
	failedSends      int64
}

// AgentConn tracks a connected agent's WebSocket connection.
type AgentConn struct {
	Conn         *websocket.Conn
	ServerID     string
	AgentID      string
	UserID       string
	Send         chan []byte
	ConnectedAt  time.Time
	LastPingAt   time.Time
	AgentVersion string
}

// pendingCommandEntry records which browser connection is waiting for a
// command result and which server the command was sent to (the latter allows
// the hub to fail pending commands when the agent disconnects).
type pendingCommandEntry struct {
	connID    string
	serverID  string
	createdAt time.Time
}

// BrowserConn tracks a connected dashboard (browser) WebSocket connection.
type BrowserConn struct {
	ID               string
	UserID           string
	Conn             *websocket.Conn
	Send             chan []byte
	WatchingServerID string
	ActiveStreams    map[string]struct{}
	LastPongAt       time.Time
	LastPingSentAt   time.Time
}

// hubOpKind enumerates every operation the hub goroutine can process.
type hubOpKind int

const (
	opRegisterAgent hubOpKind = iota
	opUnregisterAgent
	opRegisterBrowser
	opUnregisterBrowser
	opSubscribe
	opUnsubscribe
	opSendToAgent
	opForwardToBrowsers
	opGetAgentSend
	opListAgents
	opTrackPendingCommand
	opResolvePendingCommand
	opLookupPendingCommand
	opFailTimedOutCommand
	opTimeoutCommand
	opFindBrowserByUser
	opHasInFlightCommand
	opAgentPong
	opBrowserPong
	opRecordBrowserPing
	opSendToBrowser
	opListBrowsers
	opCloseBrowser
	opCloseAgent
	opGetStats
	opRegisterLogStream
	opUnregisterLogStream
	opLookupLogStream
	opRecordStream
	opClearStream
	opClearServerStreams
	opReplayStreams
)

// hubOp is a single serialized operation for the hub goroutine. reply is set
// for synchronous operations; the hub goroutine never blocks on it (it is
// buffered) so routing is never stalled waiting on a caller.
type hubOp struct {
	kind      hubOpKind
	serverID  string
	agentID   string
	userID    string
	connID    string
	conn      *websocket.Conn
	msg       []byte
	key       string
	cmd       []byte
	commandID string
	reply     chan interface{}
}

// browserRegistration carries the result of registering a browser connection.
type browserRegistration struct {
	ID   string
	Send chan []byte
}

// agentSnapshot is a read-only copy of a connected agent handed to callers
// that need to act outside the hub goroutine (e.g. the heartbeat ticker).
type agentSnapshot struct {
	ServerID   string
	AgentID    string
	Send       chan []byte
	Conn       *websocket.Conn
	LastPingAt time.Time
}

// browserSnapshot is a read-only copy of a connected browser for heartbeat checks.
type browserSnapshot struct {
	ConnID         string
	Send           chan []byte
	Conn           *websocket.Conn
	LastPongAt     time.Time
	LastPingSentAt time.Time
}

// HubStats holds current connection metrics for the stats endpoint.
type HubStats struct {
	AgentConnections  int     `json:"agent_connections"`
	BrowserConnections int    `json:"browser_connections"`
	Subscriptions     int     `json:"subscriptions"`
	PendingCommands   int     `json:"pending_commands"`
	ActiveLogStreams  int     `json:"active_log_streams"`
	MessagesRouted    int64   `json:"messages_routed"`
	BroadcastCount    int64   `json:"broadcast_count"`
	AverageFanout     float64 `json:"average_broadcast_fanout"`
	FailedSends       int64   `json:"failed_sends"`
}

// NewHub creates the hub and starts its single goroutine immediately. The
// goroutine begins on control plane startup (main calls NewHub during boot).
func NewHub() *Hub {
	h := &Hub{
		ops:             make(chan hubOp, 256),
		agents:          make(map[string]*AgentConn),
		agentsByAgentID: make(map[string]*AgentConn),
		browsers:        make(map[string]*BrowserConn),
		subscriptions:   make(map[string]map[string]struct{}),
		pendingCommands: make(map[string]pendingCommandEntry),
		streamSubs:      make(map[string]map[string]struct{}),
		streams:         make(map[string]map[string][]byte),
	}
	h.started.Do(func() { go h.run() })
	return h
}

// Run starts the hub goroutine if it is not already running. NewHub starts it
// automatically; this is exposed so startup can be made explicit.
func (h *Hub) Run() {
	h.started.Do(func() { go h.run() })
}

func (h *Hub) run() {
	slog.Info("ws hub started", "subsystems", "agents,browsers,subscriptions,pending_commands")
	for op := range h.ops {
		h.handleOp(op)
	}
}

// handleOp processes one operation. It must only be called from the hub
// goroutine. Internal helpers (sendToAgent, addSubscription, ...) are used
// here instead of the public methods to avoid re-entering the ops channel.
func (h *Hub) handleOp(op hubOp) {
	switch op.kind {
	case opRegisterAgent:
		// Duplicate connection (e.g. the agent restarted before its old socket
		// timed out): close the old connection and replace it. The old reader
		// goroutine's eventual UnregisterAgent call becomes a no-op because its
		// conn no longer matches the registered one.
		if old, ok := h.agents[op.serverID]; ok {
			delete(h.agentsByAgentID, old.AgentID)
			close(old.Send)
			go old.Conn.Close() // never block routing on a possibly-slow socket
			slog.Info("replaced duplicate agent connection", "server_id", op.serverID)
		}
		agent := &AgentConn{
			Conn:        op.conn,
			ServerID:    op.serverID,
			AgentID:     op.agentID,
			UserID:      op.userID,
			Send:        make(chan []byte, 256),
			ConnectedAt: time.Now().UTC(),
			LastPingAt:  time.Now().UTC(),
		}
		h.agents[op.serverID] = agent
		h.agentsByAgentID[op.agentID] = agent
		if op.reply != nil {
			op.reply <- agent.Send
		}
		// Notify dashboards watching this server that the agent is live.
		h.notifyAgentConnection(op.serverID, true)

	case opUnregisterAgent:
		// Only unregister if the conn matches the registered agent. This makes
		// a duplicate-connection replacement safe: the stale reader's
		// unregister (with the old conn) is a no-op and cannot kill the new
		// agent.
		if agent, ok := h.agents[op.serverID]; ok && agent.Conn == op.conn {
			delete(h.agentsByAgentID, agent.AgentID)
			close(agent.Send)
			go agent.Conn.Close() // never block routing on a possibly-slow socket
			delete(h.agents, op.serverID)
			// Anything the waiting dashboards asked for is now impossible:
			// fail those commands and tell the waiters.
			h.failPendingCommandsForServer(op.serverID)
			h.notifyAgentConnection(op.serverID, false)
			if op.reply != nil {
				op.reply <- true
			}
		} else if op.reply != nil {
			op.reply <- false
		}

	case opRegisterBrowser:
		// A browser connects WITHOUT a server subscription (Layer 5B Step 3A:
		// "connected but not viewing any server"); it subscribes explicitly via
		// a subscribe message or an immediate URL-driven subscribe.
		connID := uuid.New().String()
		browser := &BrowserConn{
			ID:            connID,
			UserID:        op.userID,
			Conn:          op.conn,
			Send:          make(chan []byte, 256),
			ActiveStreams: make(map[string]struct{}),
		}
		h.browsers[connID] = browser
		if op.reply != nil {
			op.reply <- browserRegistration{ID: connID, Send: browser.Send}
		}

	case opUnregisterBrowser:
		if browser, ok := h.browsers[op.connID]; ok {
			h.removeSubscription(browser.WatchingServerID, op.connID)
			// Drop any command results this browser is still waiting on.
			for cmdID, entry := range h.pendingCommands {
				if entry.connID == op.connID {
					delete(h.pendingCommands, cmdID)
				}
			}
		// Release every log stream this connection started (Step 3C Way 2).
		for streamID, conns := range h.streamSubs {
			delete(conns, op.connID)
			if len(conns) == 0 {
				delete(h.streamSubs, streamID)
			}
		}
			close(browser.Send)
			browser.Conn.Close()
			delete(h.browsers, op.connID)
		}

	case opSubscribe:
		if browser, ok := h.browsers[op.connID]; ok {
			// Navigating to a different server auto-unsubscribes from the
			// previous one (Layer 5B Step 3C Way 3).
			if browser.WatchingServerID != "" && browser.WatchingServerID != op.serverID {
				h.removeSubscription(browser.WatchingServerID, op.connID)
			}
			h.addSubscription(op.serverID, op.connID)
			browser.WatchingServerID = op.serverID
		}

	case opUnsubscribe:
		h.removeSubscription(op.serverID, op.connID)

	case opSendToAgent:
		if op.reply != nil {
			op.reply <- h.sendToAgent(op.serverID, op.msg)
		}

	case opForwardToBrowsers:
		h.forwardToBrowsers(op.serverID, op.msg)

	case opGetAgentSend:
		if agent, ok := h.agents[op.serverID]; ok {
			if op.reply != nil {
				op.reply <- agent.Send
			}
		} else if op.reply != nil {
			ch := make(chan []byte)
			close(ch)
			op.reply <- ch
		}

	case opListAgents:
		if op.reply == nil {
			return
		}
		snap := make([]agentSnapshot, 0, len(h.agents))
		for serverID, agent := range h.agents {
			snap = append(snap, agentSnapshot{
				ServerID:   serverID,
				AgentID:    agent.AgentID,
				Send:       agent.Send,
				Conn:       agent.Conn,
				LastPingAt: agent.LastPingAt,
			})
		}
		op.reply <- snap

	case opListBrowsers:
		if op.reply == nil {
			return
		}
		snap := make([]browserSnapshot, 0, len(h.browsers))
		for connID, b := range h.browsers {
			snap = append(snap, browserSnapshot{
				ConnID:         connID,
				Send:           b.Send,
				Conn:           b.Conn,
				LastPongAt:     b.LastPongAt,
				LastPingSentAt: b.LastPingSentAt,
			})
		}
		op.reply <- snap

	case opCloseBrowser:
		// Close and unregister a browser connection (used by heartbeat timeout).
		browser, ok := h.browsers[op.connID]
		if !ok {
			return
		}
		// Clean up subscriptions.
		if browser.WatchingServerID != "" {
			if subs, ok := h.subscriptions[browser.WatchingServerID]; ok {
				delete(subs, op.connID)
				if len(subs) == 0 {
					delete(h.subscriptions, browser.WatchingServerID)
				}
			}
		}
		// Clean up stream subscriptions.
		for streamID, conns := range h.streamSubs {
			delete(conns, op.connID)
			if len(conns) == 0 {
				delete(h.streamSubs, streamID)
			}
		}
		delete(h.browsers, op.connID)
		close(browser.Send)
		browser.Conn.Close()

	case opCloseAgent:
		// Close and unregister an agent connection (used by heartbeat staleness).
		agent, ok := h.agents[op.serverID]
		if !ok {
			return
		}
		// Only close if this is still the registered connection.
		if agent.Conn != op.conn {
			return
		}
		delete(h.agents, op.serverID)
		delete(h.agentsByAgentID, agent.AgentID)
		close(agent.Send)
		agent.Conn.Close()
		// Fail any pending commands for this server.
		for cmdID, entry := range h.pendingCommands {
			if entry.serverID == op.serverID {
				delete(h.pendingCommands, cmdID)
				if browser, ok := h.browsers[entry.connID]; ok {
					errMsg := fmt.Sprintf(`{"type":"error","payload":{"command_id":"%s","error":"agent disconnected"}}`, cmdID)
					select {
					case browser.Send <- []byte(errMsg):
					default:
					}
				}
			}
		}
		// Notify browsers watching this server.
		if subs, ok := h.subscriptions[op.serverID]; ok {
			notif := []byte(`{"type":"agent_status","payload":{"status":"disconnected"}}`)
			for connID := range subs {
				if b, ok := h.browsers[connID]; ok {
					select {
					case b.Send <- notif:
					default:
					}
				}
			}
		}

	case opGetStats:
		if op.reply != nil {
			// Count active log streams.
			streamCount := 0
			for _, conns := range h.streamSubs {
				if len(conns) > 0 {
					streamCount++
				}
			}
			// Count subscriptions.
			subCount := 0
			for _, subs := range h.subscriptions {
				subCount += len(subs)
			}
			var avgFanout float64
			if h.broadcastCount > 0 {
				avgFanout = float64(h.broadcastFanout) / float64(h.broadcastCount)
			}
			op.reply <- HubStats{
				AgentConnections:   len(h.agents),
				BrowserConnections: len(h.browsers),
				Subscriptions:      subCount,
				PendingCommands:    len(h.pendingCommands),
				ActiveLogStreams:   streamCount,
				MessagesRouted:     h.messagesRouted,
				BroadcastCount:     h.broadcastCount,
				AverageFanout:      avgFanout,
				FailedSends:        h.failedSends,
			}
		}

	case opTrackPendingCommand:
		h.pendingCommands[op.commandID] = pendingCommandEntry{connID: op.connID, serverID: op.serverID, createdAt: time.Now()}

	case opResolvePendingCommand:
		entry := h.pendingCommands[op.commandID]
		delete(h.pendingCommands, op.commandID)
		if op.reply != nil {
			op.reply <- entry.connID
		}

	case opFailTimedOutCommand:
		// If the command is still pending, fail it and notify the browser.
		if entry, ok := h.pendingCommands[op.commandID]; ok {
			delete(h.pendingCommands, op.commandID)
			if browser, ok := h.browsers[entry.connID]; ok {
				errMsg := fmt.Sprintf(`{"type":"error","payload":{"command_id":"%s","error":"command timed out"}}`, op.commandID)
				select {
				case browser.Send <- []byte(errMsg):
				default:
				}
			}
		}

	case opTimeoutCommand:
		// A command's deadline passed (Layer 5B Step 4B): remove the pending
		// entry and tell the waiting dashboard. The caller updates the DB.
		entry, ok := h.pendingCommands[op.commandID]
		delete(h.pendingCommands, op.commandID)
		if op.reply == nil {
			break
		}
		if !ok {
			op.reply <- ""
			break
		}
		timeoutMsg, _ := json.Marshal(map[string]interface{}{
			"type":       "command_result",
			"command_id": op.commandID,
			"status":     "timeout",
			"error":      "Command did not complete within the allotted time. Your server may be experiencing issues. Check the server status and try again.",
		})
		if browser, ok := h.browsers[entry.connID]; ok {
			select {
			case browser.Send <- timeoutMsg:
				h.messagesRouted++
			default:
				h.failedSends++
				slog.Debug("dropped ws message for slow browser", "connection_id", entry.connID)
			}
		}
		op.reply <- entry.connID

	case opFindBrowserByUser:
		if op.reply == nil {
			break
		}
		found := ""
		for _, b := range h.browsers {
			if b.UserID == op.userID {
				found = b.ID
				break
			}
		}
		op.reply <- found

	case opLookupPendingCommand:
		// Non-destructive lookup for progress messages: the entry stays until
		// the final result arrives.
		if op.reply != nil {
			entry, ok := h.pendingCommands[op.commandID]
			if !ok {
				op.reply <- ""
			} else {
				op.reply <- entry.connID
			}
		}

	case opHasInFlightCommand:
		// Check if any pending command targets the given server.
		if op.reply != nil {
			found := false
			for _, entry := range h.pendingCommands {
				if entry.serverID == op.serverID {
					found = true
					break
				}
			}
			op.reply <- found
		}

	case opAgentPong:
		if agent, ok := h.agents[op.serverID]; ok {
			agent.LastPingAt = time.Now().UTC()
		}

	case opBrowserPong:
		if browser, ok := h.browsers[op.connID]; ok {
			browser.LastPongAt = time.Now()
		}

	case opRecordBrowserPing:
		if browser, ok := h.browsers[op.connID]; ok {
			browser.LastPingSentAt = time.Now()
		}

	case opSendToBrowser:
		if browser, ok := h.browsers[op.connID]; ok {
			select {
			case browser.Send <- op.msg:
				h.messagesRouted++
			default:
				h.failedSends++
				slog.Debug("dropped ws message for slow browser", "connection_id", op.connID)
			}
		}

	case opRegisterLogStream:
		if _, ok := h.browsers[op.connID]; ok {
			if h.streamSubs[op.key] == nil {
				h.streamSubs[op.key] = make(map[string]struct{})
			}
			h.streamSubs[op.key][op.connID] = struct{}{}
		}

	case opUnregisterLogStream:
		if conns, ok := h.streamSubs[op.key]; ok {
			delete(conns, op.connID)
			if len(conns) == 0 {
				delete(h.streamSubs, op.key)
			}
		}

	case opLookupLogStream:
		if op.reply != nil {
			conns, ok := h.streamSubs[op.key]
			if !ok || len(conns) == 0 {
				op.reply <- []string{}
			} else {
				ids := make([]string, 0, len(conns))
				for id := range conns {
					ids = append(ids, id)
				}
				op.reply <- ids
			}
		}

	case opRecordStream:
		if h.streams[op.serverID] == nil {
			h.streams[op.serverID] = make(map[string][]byte)
		}
		h.streams[op.serverID][op.key] = op.cmd

	case opClearStream:
		if m, ok := h.streams[op.serverID]; ok {
			delete(m, op.key)
			if len(m) == 0 {
				delete(h.streams, op.serverID)
			}
		}

	case opClearServerStreams:
		delete(h.streams, op.serverID)

	case opReplayStreams:
		m, ok := h.streams[op.serverID]
		if !ok {
			return
		}
		count := 0
		for _, cmd := range m {
			if h.sendToAgent(op.serverID, cmd) {
				count++
			}
		}
		if count > 0 {
			slog.Info("replayed stream commands", "server_id", op.serverID, "count", count)
		}
	}
}

// sendToAgent performs a non-blocking send to an agent's send channel.
// Hub-goroutine-only helper. If the buffer is full (agent too slow), the
// OLDEST queued message is dropped to make room for the newest (Layer 5B
// Step 2C: drop-oldest on overflow, never block routing).
func (h *Hub) sendToAgent(serverID string, msg []byte) bool {
	agent, ok := h.agents[serverID]
	if !ok {
		return false
	}
	h.messagesRouted++
	select {
	case agent.Send <- msg:
		return true
	default:
		slog.Warn("agent send buffer full, dropping oldest", "server_id", serverID)
		// Free one slot (the hub is the only writer and the writer goroutine
		// is the only reader, so draining one slot guarantees the push below
		// succeeds).
		select {
		case <-agent.Send:
		default:
		}
		select {
		case agent.Send <- msg:
			return true
		default:
			return false
		}
	}
}

// notifyAgentConnection pushes an agent_connected / agent_disconnected event
// to every dashboard watching the server (Layer 5B Steps 2A/2D).
// Hub-goroutine-only helper.
func (h *Hub) notifyAgentConnection(serverID string, connected bool) {
	msgType := "agent_disconnected"
	if connected {
		msgType = "agent_connected"
	}
	msg, _ := json.Marshal(map[string]interface{}{
		"type": msgType,
		"payload": map[string]interface{}{
			"server_id": serverID,
		},
	})
	h.forwardToBrowsers(serverID, msg)
}

// failPendingCommandsForServer fails every command the disconnected server was
// still executing: it removes the pending entry and sends a command_error to
// the waiting browser (Layer 5B Step 2D).
// Hub-goroutine-only helper.
func (h *Hub) failPendingCommandsForServer(serverID string) {
	for cmdID, entry := range h.pendingCommands {
		if entry.serverID != serverID {
			continue
		}
		delete(h.pendingCommands, cmdID)
		browser, ok := h.browsers[entry.connID]
		if !ok {
			continue
		}
		failMsg, _ := json.Marshal(map[string]interface{}{
			"type": "command_error",
			"payload": map[string]interface{}{
				"command_id": cmdID,
				"message":    "Server disconnected before command completed",
			},
		})
		select {
		case browser.Send <- failMsg:
		default:
		}
	}
}

// addSubscription records that a browser connection is watching a server.
// Hub-goroutine-only helper.
func (h *Hub) addSubscription(serverID, connID string) {
	if h.subscriptions[serverID] == nil {
		h.subscriptions[serverID] = make(map[string]struct{})
	}
	h.subscriptions[serverID][connID] = struct{}{}
}

// removeSubscription forgets a browser connection's watch on a server.
// Hub-goroutine-only helper.
func (h *Hub) removeSubscription(serverID, connID string) {
	if conns, ok := h.subscriptions[serverID]; ok {
		delete(conns, connID)
		if len(conns) == 0 {
			delete(h.subscriptions, serverID)
		}
	}
}

// forwardToBrowsers sends a message to every browser watching a server.
// Hub-goroutine-only helper.
func (h *Hub) forwardToBrowsers(serverID string, msg []byte) {
	h.broadcastCount++
	fanout := 0
	for connID := range h.subscriptions[serverID] {
		browser, ok := h.browsers[connID]
		if !ok {
			delete(h.subscriptions[serverID], connID)
			continue
		}
		fanout++
		select {
		case browser.Send <- msg:
			h.messagesRouted++
		default:
			// Slow consumer: drop the message rather than block routing. Kept
			// connected so a momentarily busy dashboard is not disconnected.
			h.failedSends++
			slog.Debug("dropped ws message for slow browser", "server_id", serverID, "connection_id", connID)
		}
	}
	h.broadcastFanout += int64(fanout)
}

// --- Public API: all operations enqueue ops and never touch the maps ---

// RegisterAgent registers a connected agent under its server and agent IDs and
// returns the buffered send channel its writer goroutine should drain. If the
// server already had an agent, the old connection is closed and replaced.
func (h *Hub) RegisterAgent(serverID, agentID, userID string, conn *websocket.Conn) <-chan []byte {
	reply := make(chan interface{}, 1)
	h.ops <- hubOp{kind: opRegisterAgent, serverID: serverID, agentID: agentID, userID: userID, conn: conn, reply: reply}
	ch, _ := (<-reply).(chan []byte)
	if ch == nil {
		ch = make(chan []byte)
		close(ch)
	}
	return ch
}

// UnregisterAgent removes an agent connection and closes its channels. conn
// must be the connection that was registered; passing a stale connection (e.g.
// a reader goroutine of a replaced duplicate) makes this a safe no-op. Returns
// true if the connection was actually removed, false if it was already gone or
// the conn did not match (duplicate replacement).
func (h *Hub) UnregisterAgent(serverID string, conn *websocket.Conn) bool {
	reply := make(chan interface{}, 1)
	h.ops <- hubOp{kind: opUnregisterAgent, serverID: serverID, conn: conn, reply: reply}
	removed, _ := (<-reply).(bool)
	return removed
}

// RegisterBrowser registers a connected dashboard under a fresh connection ID
// WITHOUT subscribing it to any server (Step 3A: connected but not viewing a
// server; the dashboard subscribes explicitly). It returns the connection ID
// and the buffered channel the browser's writer goroutine should drain.
func (h *Hub) RegisterBrowser(userID string, conn *websocket.Conn) (string, <-chan []byte) {
	reply := make(chan interface{}, 1)
	h.ops <- hubOp{kind: opRegisterBrowser, userID: userID, conn: conn, reply: reply}
	reg := (<-reply).(browserRegistration)
	return reg.ID, reg.Send
}

// UnregisterBrowser removes a browser connection by its connection ID.
func (h *Hub) UnregisterBrowser(connID string) {
	h.ops <- hubOp{kind: opUnregisterBrowser, connID: connID}
}

// Subscribe makes a browser connection start watching a server.
func (h *Hub) Subscribe(serverID, connID string) {
	h.ops <- hubOp{kind: opSubscribe, serverID: serverID, connID: connID}
}

// Unsubscribe makes a browser connection stop watching a server.
func (h *Hub) Unsubscribe(serverID, connID string) {
	h.ops <- hubOp{kind: opUnsubscribe, serverID: serverID, connID: connID}
}

// SendToAgent sends a message to a specific agent by server ID.
// Returns false if the agent is not connected.
func (h *Hub) SendToAgent(serverID string, msg []byte) bool {
	reply := make(chan interface{}, 1)
	h.ops <- hubOp{kind: opSendToAgent, serverID: serverID, msg: msg, reply: reply}
	ok, _ := (<-reply).(bool)
	return ok
}

// ForwardToBrowsers sends a message to all browsers watching a specific server.
func (h *Hub) ForwardToBrowsers(serverID string, msg []byte) {
	h.ops <- hubOp{kind: opForwardToBrowsers, serverID: serverID, msg: msg}
}

// GetAgentSend returns the send channel for an agent, or a closed channel if
// the agent is not connected.
func (h *Hub) GetAgentSend(serverID string) <-chan []byte {
	reply := make(chan interface{}, 1)
	h.ops <- hubOp{kind: opGetAgentSend, serverID: serverID, reply: reply}
	ch, _ := (<-reply).(chan []byte)
	if ch == nil {
		ch = make(chan []byte)
		close(ch)
	}
	return ch
}

// TrackPendingCommand records which browser connection is waiting for a
// command result sent to the given server, so the result (or a disconnect
// failure) can be routed to exactly that dashboard.
func (h *Hub) TrackPendingCommand(commandID, connID, serverID string) {
	h.ops <- hubOp{kind: opTrackPendingCommand, commandID: commandID, connID: connID, serverID: serverID}
	h.startCommandTimeout(commandID)
}

// startCommandTimeout starts a background goroutine that fails a pending
// command after 10 minutes if no result has arrived.
func (h *Hub) startCommandTimeout(commandID string) {
	go func() {
		time.Sleep(10 * time.Minute)
		h.ops <- hubOp{kind: opFailTimedOutCommand, commandID: commandID}
	}()
}

// ResolvePendingCommand returns (and removes) the browser connection waiting
// for a command result. Returns "" if there is no pending entry.
func (h *Hub) ResolvePendingCommand(commandID string) string {
	reply := make(chan interface{}, 1)
	h.ops <- hubOp{kind: opResolvePendingCommand, commandID: commandID, reply: reply}
	connID, _ := (<-reply).(string)
	return connID
}

// LookupPendingCommand returns the browser connection waiting for a command
// result WITHOUT removing the entry (used for progress updates that keep the
// command pending until its final result). Returns "" if not pending.
func (h *Hub) LookupPendingCommand(commandID string) string {
	reply := make(chan interface{}, 1)
	h.ops <- hubOp{kind: opLookupPendingCommand, commandID: commandID, reply: reply}
	connID, _ := (<-reply).(string)
	return connID
}

// TimeoutPendingCommand fires when a command's deadline passes (Layer 5B
// Step 4B): it removes the pending entry, notifies the waiting dashboard with
// a timeout result, and returns the dashboard's connection id ("" when the
// command already completed and the entry is gone). The caller updates the DB.
func (h *Hub) TimeoutPendingCommand(commandID string) string {
	reply := make(chan interface{}, 1)
	h.ops <- hubOp{kind: opTimeoutCommand, commandID: commandID, reply: reply}
	connID, _ := (<-reply).(string)
	return connID
}

// FindBrowserByUser returns an active browser connection id for the user, or
// "" if the user has no live dashboard. Used to route late command results to
// a reconnected dashboard (Layer 5B Step 4A).
func (h *Hub) FindBrowserByUser(userID string) string {
	reply := make(chan interface{}, 1)
	h.ops <- hubOp{kind: opFindBrowserByUser, userID: userID, reply: reply}
	connID, _ := (<-reply).(string)
	return connID
}

// HasInFlightCommand reports whether there is any pending command for the given
// server (used to deduplicate concurrent browser commands).
func (h *Hub) HasInFlightCommand(serverID string) bool {
	reply := make(chan interface{}, 1)
	h.ops <- hubOp{kind: opHasInFlightCommand, serverID: serverID, reply: reply}
	found, _ := (<-reply).(bool)
	return found
}

// AgentPong records that the agent answered a heartbeat ping.
func (h *Hub) AgentPong(serverID string) {
	h.ops <- hubOp{kind: opAgentPong, serverID: serverID}
}

// BrowserPong records that a browser answered a heartbeat ping.
func (h *Hub) BrowserPong(connID string) {
	h.ops <- hubOp{kind: opBrowserPong, connID: connID}
}

// RecordBrowserPing records when a ping was sent to a browser for timeout tracking.
func (h *Hub) RecordBrowserPing(connID string) {
	h.ops <- hubOp{kind: opRecordBrowserPing, connID: connID}
}

// SendToBrowser sends a message directly to one browser connection, ignoring
// the per-server subscription. Used to route command results to the exact
// dashboard that issued the command.
func (h *Hub) SendToBrowser(connID string, msg []byte) {
	h.ops <- hubOp{kind: opSendToBrowser, connID: connID, msg: msg}
}

// RegisterLogStream routes agent log output for a stream to the given browser
// connection (Layer 5B Step 3D start_log_stream).
func (h *Hub) RegisterLogStream(streamID, connID string) {
	h.ops <- hubOp{kind: opRegisterLogStream, key: streamID, connID: connID}
}

// UnregisterLogStream stops routing log output for a stream for a specific
// browser connection (stop_log_stream).
func (h *Hub) UnregisterLogStream(streamID, connID string) {
	h.ops <- hubOp{kind: opUnregisterLogStream, key: streamID, connID: connID}
}

// LookupLogStream returns the browser connections currently receiving log
// output for a stream, or an empty slice if nobody is subscribed.
func (h *Hub) LookupLogStream(streamID string) []string {
	reply := make(chan interface{}, 1)
	h.ops <- hubOp{kind: opLookupLogStream, key: streamID, reply: reply}
	r := <-reply
	if r == nil {
		return nil
	}
	ids, _ := r.([]string)
	return ids
}

// RecordStreamCommand remembers a stream_logs desire for a server so it can be
// replayed when the agent (re)connects. key identifies the project+roles.
func (h *Hub) RecordStreamCommand(serverID, key string, cmd []byte) {
	h.ops <- hubOp{kind: opRecordStream, serverID: serverID, key: key, cmd: cmd}
}

// ClearStreamCommand forgets a specific stream desire (selective stop).
func (h *Hub) ClearStreamCommand(serverID, key string) {
	h.ops <- hubOp{kind: opClearStream, serverID: serverID, key: key}
}

// ClearServerStreams forgets every stream desire for a server (all:true stop
// or browser disconnect).
func (h *Hub) ClearServerStreams(serverID string) {
	h.ops <- hubOp{kind: opClearServerStreams, serverID: serverID}
}

// ReplayStreamCommands re-sends every recorded stream_logs command to the
// agent. Called when an agent (re)connects so active log views resume without
// a dashboard refresh.
func (h *Hub) ReplayStreamCommands(serverID string) {
	h.ops <- hubOp{kind: opReplayStreams, serverID: serverID}
}

// StartHeartbeat periodically heartbeats connected agents, keeps their server
// rows marked connected, and disconnects agents that have not responded to
// pings within 45 seconds (30s interval + 15s grace period).
func (h *Hub) StartHeartbeat(db *sql.DB) {
	ticker := time.NewTicker(15 * time.Second)
	go func() {
		for range ticker.C {
			snap := h.listAgents()
			now := time.Now()
			for _, a := range snap {
				// Check for stale agents: if LastPingAt is more than 45s ago
				// and the agent was sent a ping at least 30s ago.
				if !a.LastPingAt.IsZero() && now.Sub(a.LastPingAt) > 45*time.Second {
					slog.Warn("agent heartbeat timeout, disconnecting",
						"server_id", a.ServerID, "agent_id", a.AgentID,
						"last_pong", a.LastPingAt)
					h.closeAgent(a.ServerID, a.Conn)
					_ = queries.UpdateServerConnection(db, a.ServerID, "disconnected")
					continue
				}
				// Send heartbeat ping.
				select {
				case a.Send <- []byte(`{"type":"heartbeat"}`):
				default:
				}
				if a.ServerID != "" {
					_ = queries.UpdateServerConnection(db, a.ServerID, "connected")
				}
			}
		}
	}()
}

// listAgents snapshots the currently connected agents (server ID + send
// channel) via a synchronous hub operation.
func (h *Hub) listAgents() []agentSnapshot {
	reply := make(chan interface{}, 1)
	h.ops <- hubOp{kind: opListAgents, reply: reply}
	snap, _ := (<-reply).([]agentSnapshot)
	return snap
}

// listBrowsers snapshots all connected browsers via a synchronous hub operation.
func (h *Hub) listBrowsers() []browserSnapshot {
	reply := make(chan interface{}, 1)
	h.ops <- hubOp{kind: opListBrowsers, reply: reply}
	snap, _ := (<-reply).([]browserSnapshot)
	return snap
}

// closeBrowser closes and unregisters a browser connection. Used by the
// heartbeat goroutine to evict stale connections.
func (h *Hub) closeBrowser(connID string) {
	h.ops <- hubOp{kind: opCloseBrowser, connID: connID}
}

// closeAgent closes and unregisters an agent connection. Used by the heartbeat
// goroutine to evict stale agents that have not responded to pings.
func (h *Hub) closeAgent(serverID string, conn *websocket.Conn) {
	h.ops <- hubOp{kind: opCloseAgent, serverID: serverID, conn: conn}
}

// StartBrowserHeartbeat sends pings to all connected browsers every 30 seconds
// and closes connections that have not responded within 10 seconds.
func (h *Hub) StartBrowserHeartbeat() {
	pingTicker := time.NewTicker(30 * time.Second)
	checkTicker := time.NewTicker(5 * time.Second)
	go func() {
		for {
			select {
			case <-pingTicker.C:
				snap := h.listBrowsers()
				for _, b := range snap {
					ts := time.Now().UTC().Format(time.RFC3339)
					pingMsg := []byte(`{"type":"ping","timestamp":"` + ts + `"}`)
					h.RecordBrowserPing(b.ConnID)
					select {
					case b.Send <- pingMsg:
					default:
					}
				}
			case <-checkTicker.C:
				snap := h.listBrowsers()
				now := time.Now()
				for _, b := range snap {
					// Close if ping was sent more than 10s ago and no pong received.
					if !b.LastPingSentAt.IsZero() && now.Sub(b.LastPingSentAt) > 10*time.Second {
						if b.LastPongAt.Before(b.LastPingSentAt) {
							slog.Warn("browser heartbeat timeout, closing connection",
								"connection_id", b.ConnID)
						h.closeBrowser(b.ConnID)
					}
				}
				}
			}
		}
	}()
}

// Stats returns current hub connection metrics.
func (h *Hub) Stats() HubStats {
	reply := make(chan interface{}, 1)
	h.ops <- hubOp{kind: opGetStats, reply: reply}
	stats, _ := (<-reply).(HubStats)
	return stats
}

// StartMetricsLogger logs hub metrics every 5 minutes for debugging and
// capacity planning.
func (h *Hub) StartMetricsLogger() {
	ticker := time.NewTicker(5 * time.Minute)
	go func() {
		for range ticker.C {
			stats := h.Stats()
			slog.Info("hub metrics",
				"agent_connections", stats.AgentConnections,
				"browser_connections", stats.BrowserConnections,
				"subscriptions", stats.Subscriptions,
				"pending_commands", stats.PendingCommands,
				"active_log_streams", stats.ActiveLogStreams,
				"messages_routed", stats.MessagesRouted,
				"broadcast_count", stats.BroadcastCount,
				"average_fanout", fmt.Sprintf("%.1f", stats.AverageFanout),
				"failed_sends", stats.FailedSends,
			)
		}
	}()
}
