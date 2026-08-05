package ws

import (
	"database/sql"
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
//   - pendingCommands: command_id -> browser connection_id awaiting the result
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
	pendingCommands map[string]string
	// streams records per-server log-stream desires requested by dashboards so
	// the control plane can re-establish live log views when an agent
	// reconnects (Layer 4C 3B). Keyed by a project|roles signature.
	streams map[string]map[string][]byte
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

// BrowserConn tracks a connected dashboard (browser) WebSocket connection.
type BrowserConn struct {
	ID               string
	UserID           string
	Conn             *websocket.Conn
	Send             chan []byte
	WatchingServerID string
	ActiveStreams    map[string]struct{}
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
	ServerID string
	Send     chan []byte
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
		pendingCommands: make(map[string]string),
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

	case opUnregisterAgent:
		if agent, ok := h.agents[op.serverID]; ok {
			delete(h.agentsByAgentID, agent.AgentID)
			close(agent.Send)
			agent.Conn.Close()
			delete(h.agents, op.serverID)
		}

	case opRegisterBrowser:
		connID := uuid.New().String()
		browser := &BrowserConn{
			ID:               connID,
			UserID:           op.userID,
			Conn:             op.conn,
			Send:             make(chan []byte, 256),
			WatchingServerID: op.serverID,
			ActiveStreams:    make(map[string]struct{}),
		}
		h.browsers[connID] = browser
		h.addSubscription(op.serverID, connID)
		if op.reply != nil {
			op.reply <- browserRegistration{ID: connID, Send: browser.Send}
		}

	case opUnregisterBrowser:
		if browser, ok := h.browsers[op.connID]; ok {
			h.removeSubscription(browser.WatchingServerID, op.connID)
			// Drop any command results this browser is still waiting on.
			for cmdID, owner := range h.pendingCommands {
				if owner == op.connID {
					delete(h.pendingCommands, cmdID)
				}
			}
			close(browser.Send)
			browser.Conn.Close()
			delete(h.browsers, op.connID)
		}

	case opSubscribe:
		if browser, ok := h.browsers[op.connID]; ok {
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
			snap = append(snap, agentSnapshot{ServerID: serverID, Send: agent.Send})
		}
		op.reply <- snap

	case opTrackPendingCommand:
		h.pendingCommands[op.commandID] = op.connID

	case opResolvePendingCommand:
		connID := h.pendingCommands[op.commandID]
		delete(h.pendingCommands, op.commandID)
		if op.reply != nil {
			op.reply <- connID
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
// Hub-goroutine-only helper.
func (h *Hub) sendToAgent(serverID string, msg []byte) bool {
	agent, ok := h.agents[serverID]
	if !ok {
		return false
	}
	select {
	case agent.Send <- msg:
		return true
	default:
		return false
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
	for connID := range h.subscriptions[serverID] {
		browser, ok := h.browsers[connID]
		if !ok {
			continue
		}
		select {
		case browser.Send <- msg:
		default:
			// Slow consumer: drop the message rather than block routing. Kept
			// connected so a momentarily busy dashboard is not disconnected.
			slog.Debug("dropped ws message for slow browser", "server_id", serverID, "connection_id", connID)
		}
	}
}

// --- Public API: all operations enqueue ops and never touch the maps ---

// RegisterAgent registers a connected agent under its server and agent IDs.
func (h *Hub) RegisterAgent(serverID, agentID, userID string, conn *websocket.Conn) {
	h.ops <- hubOp{kind: opRegisterAgent, serverID: serverID, agentID: agentID, userID: userID, conn: conn}
}

// UnregisterAgent removes an agent connection and closes its channels.
func (h *Hub) UnregisterAgent(serverID string) {
	h.ops <- hubOp{kind: opUnregisterAgent, serverID: serverID}
}

// RegisterBrowser registers a connected dashboard under a fresh connection ID
// and subscribes it to the given server. It returns the connection ID and the
// buffered channel the browser's writer goroutine should drain.
func (h *Hub) RegisterBrowser(serverID, userID string, conn *websocket.Conn) (string, <-chan []byte) {
	reply := make(chan interface{}, 1)
	h.ops <- hubOp{kind: opRegisterBrowser, serverID: serverID, userID: userID, conn: conn, reply: reply}
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
// command result so the result can be routed to exactly that dashboard.
func (h *Hub) TrackPendingCommand(commandID, connID string) {
	h.ops <- hubOp{kind: opTrackPendingCommand, commandID: commandID, connID: connID}
}

// ResolvePendingCommand returns (and removes) the browser connection waiting
// for a command result. Returns "" if there is no pending entry.
func (h *Hub) ResolvePendingCommand(commandID string) string {
	reply := make(chan interface{}, 1)
	h.ops <- hubOp{kind: opResolvePendingCommand, commandID: commandID, reply: reply}
	connID, _ := (<-reply).(string)
	return connID
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

// StartHeartbeat periodically heartbeats connected agents and keeps their
// server rows marked connected. It snapshots the agent list through the hub
// channel so all DB work happens outside the hub goroutine.
func (h *Hub) StartHeartbeat(db *sql.DB) {
	ticker := time.NewTicker(15 * time.Second)
	go func() {
		for range ticker.C {
			snap := h.listAgents()
			for _, a := range snap {
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
