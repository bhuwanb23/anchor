package ws

import (
	"sync"

	"github.com/gorilla/websocket"
)

type Hub struct {
	mu          sync.RWMutex
	agents      map[string]*AgentConn
	browsers   map[string][]*BrowserConn
	broadcast chan []byte
}

type AgentConn struct {
	Conn   *websocket.Conn
	ServerID string
	Send    chan []byte
}

type BrowserConn struct {
	Conn     *websocket.Conn
	ServerID string
	Send     chan []byte
}

func NewHub() *Hub {
	return &Hub{
		agents:    make(map[string]*AgentConn),
		browsers: make(map[string][]*BrowserConn),
		broadcast: make(chan []byte),
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

func (h *Hub) RegisterAgent(serverID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.agents[serverID] = &AgentConn{
		Conn:     conn,
		ServerID: serverID,
		Send:     make(chan []byte, 256),
	}
}

func (h *Hub) UnregisterAgent(serverID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if agent, ok := h.agents[serverID]; ok {
		close(agent.Send)
		agent.Conn.Close()
		delete(h.agents, serverID)
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