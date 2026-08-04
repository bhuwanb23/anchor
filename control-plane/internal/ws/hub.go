package ws

import (
	"database/sql"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
)

type Hub struct {
	mu              sync.RWMutex
	agents          map[string]*AgentConn
	agentsByAgentID map[string]*AgentConn
	browsers        map[string][]*BrowserConn
	broadcast       chan []byte
	// streams tracks per-server log-stream desires requested by dashboards so
	// the control plane can re-establish live log views when an agent
	// reconnects (Layer 4C 3B). Keyed by a project|roles signature.
	streams map[string]map[string][]byte
}

type AgentConn struct {
	Conn     *websocket.Conn
	ServerID string
	AgentID  string
	Send     chan []byte
}

type BrowserConn struct {
	Conn     *websocket.Conn
	ServerID string
	Send     chan []byte
}

func NewHub() *Hub {
	return &Hub{
		agents:          make(map[string]*AgentConn),
		agentsByAgentID: make(map[string]*AgentConn),
		browsers:        make(map[string][]*BrowserConn),
		broadcast:       make(chan []byte),
		streams:         make(map[string]map[string][]byte),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case message := <-h.broadcast:
			h.mu.RLock()
			for _, browser := range h.browsers {
				for _, conn := range browser {
					select {
					case conn.Send <- message:
					default:
						close(conn.Send)
					}
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *Hub) RegisterAgent(serverID, agentID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	agent := &AgentConn{
		Conn:     conn,
		ServerID: serverID,
		AgentID:  agentID,
		Send:     make(chan []byte, 256),
	}
	h.agents[serverID] = agent
	h.agentsByAgentID[agentID] = agent
}

func (h *Hub) UnregisterAgent(serverID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if agent, ok := h.agents[serverID]; ok {
		delete(h.agentsByAgentID, agent.AgentID)
		close(agent.Send)
		agent.Conn.Close()
		delete(h.agents, serverID)
	}
}

func (h *Hub) GetAgentSend(serverID string) <-chan []byte {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if agent, ok := h.agents[serverID]; ok {
		return agent.Send
	}
	ch := make(chan []byte)
	close(ch)
	return ch
}

// SendToAgent sends a message to a specific agent by server ID.
// Returns false if the agent is not connected.
func (h *Hub) SendToAgent(serverID string, msg []byte) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
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

// ForwardToBrowsers sends a message to all browsers watching a specific server.
func (h *Hub) ForwardToBrowsers(serverID string, msg []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	browsers, ok := h.browsers[serverID]
	if !ok {
		return
	}
	for _, browser := range browsers {
		select {
		case browser.Send <- msg:
		default:
			close(browser.Send)
		}
	}
}

// RecordStreamCommand remembers a stream_logs desire for a server so it can be
// replayed when the agent (re)connects. key identifies the project+roles.
func (h *Hub) RecordStreamCommand(serverID, key string, cmd []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.streams == nil {
		h.streams = make(map[string]map[string][]byte)
	}
	if h.streams[serverID] == nil {
		h.streams[serverID] = make(map[string][]byte)
	}
	h.streams[serverID][key] = cmd
}

// ClearStreamCommand forgets a specific stream desire (selective stop).
func (h *Hub) ClearStreamCommand(serverID, key string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if m, ok := h.streams[serverID]; ok {
		delete(m, key)
		if len(m) == 0 {
			delete(h.streams, serverID)
		}
	}
}

// ClearServerStreams forgets every stream desire for a server (all:true stop
// or browser disconnect).
func (h *Hub) ClearServerStreams(serverID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.streams, serverID)
}

// ReplayStreamCommands re-sends every recorded stream_logs command to the
// agent. Called when an agent (re)connects so active log views resume without
// a dashboard refresh.
func (h *Hub) ReplayStreamCommands(serverID string) {
	h.mu.RLock()
	var cmds [][]byte
	if m, ok := h.streams[serverID]; ok {
		for _, cmd := range m {
			cmds = append(cmds, cmd)
		}
	}
	h.mu.RUnlock()
	for _, cmd := range cmds {
		h.SendToAgent(serverID, cmd)
	}
	if len(cmds) > 0 {
		slog.Info("replayed stream commands", "server_id", serverID, "count", len(cmds))
	}
}

func (h *Hub) RegisterBrowser(serverID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	browserConn := &BrowserConn{
		Conn:     conn,
		ServerID: serverID,
		Send:     make(chan []byte, 256),
	}
	h.browsers[serverID] = append(h.browsers[serverID], browserConn)
}

func (h *Hub) UnregisterBrowser(serverID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if conns, ok := h.browsers[serverID]; ok {
		for i, browser := range conns {
			if browser.Conn == conn {
				close(browser.Send)
				browser.Conn.Close()
				h.browsers[serverID] = append(conns[:i], conns[i+1:]...)
				break
			}
		}
	}
}

func (h *Hub) StartHeartbeat(db *sql.DB) {
	ticker := time.NewTicker(15 * time.Second)
	go func() {
		for range ticker.C {
			h.mu.RLock()
			for _, agent := range h.agents {
				select {
				case agent.Send <- []byte(`{"type":"heartbeat"}`):
				default:
				}
				if serverID := agent.ServerID; serverID != "" {
					_ = queries.UpdateServerConnection(db, serverID, "connected")
				}
			}
			h.mu.RUnlock()
		}
	}()
}
