package ws

import (
	"database/sql"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
)

type Hub struct {
	mu             sync.RWMutex
	agents         map[string]*AgentConn
	agentsByAgentID map[string]*AgentConn
	browsers       map[string][]*BrowserConn
	broadcast     chan []byte
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
		browsers:       make(map[string][]*BrowserConn),
		broadcast:      make(chan []byte),
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
