package ws

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	defaultReconnectSec = 5
	maxBackoff          = 60 * time.Second
	authFailBackoff     = 5 * time.Minute
	sendBufferSize      = 256
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
	authFailed   bool
	onConnect    func()
	onDisconnect func()
	attempt      int
}

type Message struct {
	Type     string          `json:"type"`
	ServerID string          `json:"server_id"`
	Payload  json.RawMessage `json:"payload"`
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
	if reconnectSec <= 0 {
		reconnectSec = defaultReconnectSec
	}
	return &Client{
		url:          url,
		agentID:      agentID,
		agentSecret:  agentSecret,
		reconnectSec: reconnectSec,
		sendChan:     make(chan []byte, sendBufferSize),
		recvChan:     make(chan Message, sendBufferSize),
		disconnected: make(chan struct{}, 1),
	}
}

// SetConnectHooks registers callbacks for connect/disconnect events.
func (c *Client) SetConnectHooks(onConnect, onDisconnect func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onConnect = onConnect
	c.onDisconnect = onDisconnect
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

	conn, resp, err := dialer.DialContext(ctx, c.url, header)
	if err != nil {
		if resp != nil && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
			c.mu.Lock()
			c.authFailed = true
			c.mu.Unlock()
			return &AuthError{StatusCode: resp.StatusCode, Err: err}
		}
		return fmt.Errorf("dial websocket: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.authFailed = false
	c.attempt = 0
	onConnect := c.onConnect
	c.disconnected = make(chan struct{}, 1)
	c.mu.Unlock()

	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	slog.Info("connected to control plane")

	if onConnect != nil {
		onConnect()
	}

	go c.readLoop(ctx)
	go c.writeLoop(ctx)

	return nil
}

func (c *Client) readLoop(ctx context.Context) {
	defer func() {
		c.mu.Lock()
		if c.conn != nil {
			c.conn.Close()
			c.conn = nil
		}
		onDisconnect := c.onDisconnect
		select {
		case c.disconnected <- struct{}{}:
		default:
		}
		c.mu.Unlock()
		if onDisconnect != nil {
			onDisconnect()
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

		select {
		case c.recvChan <- msg:
		default:
			slog.Warn("recv channel full, dropping message", "type", msg.Type)
		}
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
				// Offline: message already buffered in sendChan (drop-oldest on Send)
				continue
			}
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				slog.Warn("websocket write error", "error", err)
				return
			}
		}
	}
}

// Send queues a message. If the buffer is full, drops the oldest message.
func (c *Client) Send(msg []byte) {
	select {
	case c.sendChan <- msg:
	default:
		// Drop oldest, then enqueue
		select {
		case <-c.sendChan:
		default:
		}
		select {
		case c.sendChan <- msg:
		default:
			slog.Warn("send channel full, dropping message")
		}
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
			wait := c.nextBackoff(err)
			slog.Error("failed to connect", "error", err, "retry_in", wait)
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
				continue
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-c.disconnected:
		}

		wait := c.nextBackoff(nil)
		slog.Info("connection lost, reconnecting...", "wait", wait)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func (c *Client) nextBackoff(err error) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err != nil {
		if _, ok := err.(*AuthError); ok || c.authFailed {
			return authFailBackoff
		}
	}

	c.attempt++
	base := time.Duration(c.reconnectSec) * time.Second
	if base < time.Second {
		base = time.Second
	}
	// Exponential: base * 2^(attempt-1), capped at 60s
	wait := base << (c.attempt - 1)
	if wait > maxBackoff || wait <= 0 {
		wait = maxBackoff
	}
	return ApplyJitter(wait)
}

// ApplyJitter adds ±20% jitter to a duration.
func ApplyJitter(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	// ±20%
	jitter := float64(d) * 0.2
	offset := (rand.Float64()*2 - 1) * jitter
	out := time.Duration(float64(d) + offset)
	if out < time.Second {
		return time.Second
	}
	return out
}

// BackoffDuration returns the backoff for attempt n (1-based) before jitter.
func BackoffDuration(reconnectSec, attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	base := time.Duration(reconnectSec) * time.Second
	if base < time.Second {
		base = time.Second
	}
	wait := base << (attempt - 1)
	if wait > maxBackoff || wait <= 0 {
		return maxBackoff
	}
	return wait
}

func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		c.conn.Close()
	}
}

// AuthError indicates the control plane rejected agent credentials.
type AuthError struct {
	StatusCode int
	Err        error
}

func (e *AuthError) Error() string {
	return fmt.Sprintf("authentication failed (HTTP %d): %v", e.StatusCode, e.Err)
}

func (e *AuthError) Unwrap() error { return e.Err }
