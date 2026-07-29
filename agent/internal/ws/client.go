package ws

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Client struct {
	url          string
	agentID      string
	agentSecret  string
	token        string
	conn         *websocket.Conn
	reconnectSec int
	sendChan     chan []byte
	recvChan     chan Message
	disconnected chan struct{}
	mu           sync.Mutex
}

type Message struct {
	Type    string          `json:"type"`
	ServerID string         `json:"server_id"`
	Payload json.RawMessage `json:"payload"`
}

type CommandPayload struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type ResultPayload struct {
	CommandID string `json:"command_id"`
	Status    string `json:"status"`
	Output    string `json:"output"`
	Error     string `json:"error,omitempty"`
}

func NewClient(url, agentID, agentSecret string, reconnectSec int) *Client {
	return &Client{
		url:          url,
		agentID:      agentID,
		agentSecret:  agentSecret,
		reconnectSec: reconnectSec,
		sendChan:     make(chan []byte, 256),
		recvChan:     make(chan Message, 256),
		disconnected: make(chan struct{}, 1),
	}
}

func (c *Client) Connect(ctx context.Context) error {
	slog.Info("connecting to control plane", "url", c.url)

	dialer := &websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		NetDialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}

	header := make(http.Header)
	credentials := base64.StdEncoding.EncodeToString([]byte(c.agentID + ":" + c.agentSecret))
	header.Set("Authorization", "Basic "+credentials)

	conn, _, err := dialer.DialContext(ctx, c.url, header)
	if err != nil {
		return fmt.Errorf("dial websocket: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	slog.Info("connected to control plane")

	go c.readLoop(ctx)
	go c.writeLoop(ctx)

	return nil
}

func (c *Client) readLoop(ctx context.Context) {
	defer func() {
		c.mu.Lock()
		if c.conn != nil {
			c.conn.Close()
		}
		c.mu.Unlock()
		select {
		case c.disconnected <- struct{}{}:
		default:
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()
		if conn == nil {
			return
		}

		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		_, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				slog.Info("websocket closed", "error", err)
			} else {
				slog.Warn("websocket read error", "error", err)
			}
			return
		}

		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			slog.Warn("failed to parse message", "error", err)
			continue
		}

		c.recvChan <- msg
	}
}

func (c *Client) writeLoop(ctx context.Context) {
	defer func() {
		c.mu.Lock()
		if c.conn != nil {
			c.conn.Close()
		}
		c.mu.Unlock()
	}()

	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-pingTicker.C:
			c.mu.Lock()
			conn := c.conn
			c.mu.Unlock()
			if conn != nil {
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					slog.Warn("websocket ping error", "error", err)
					return
				}
			}
		case msg, ok := <-c.sendChan:
			if !ok {
				return
			}
			c.mu.Lock()
			conn := c.conn
			c.mu.Unlock()
			if conn == nil {
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				slog.Warn("websocket write error", "error", err)
				return
			}
		}
	}
}

func (c *Client) Send(msg []byte) {
	select {
	case c.sendChan <- msg:
	default:
		slog.Warn("send channel full, dropping message")
	}
}

func (c *Client) SendJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.Send(data)
	return nil
}

func (c *Client) Recv() <-chan Message {
	return c.recvChan
}

func (c *Client) Disconnected() <-chan struct{} {
	return c.disconnected
}

func (c *Client) Run(ctx context.Context) {
	for {
		if err := c.Connect(ctx); err != nil {
			slog.Error("failed to connect", "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(c.reconnectSec) * time.Second):
				continue
			}
		}

		<-c.disconnected

		slog.Info("connection lost, reconnecting...", "wait_sec", c.reconnectSec)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(c.reconnectSec) * time.Second):
		}
	}
}

func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		c.conn.Close()
	}
	close(c.sendChan)
	close(c.recvChan)
}
