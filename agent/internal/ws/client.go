package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

type Client struct {
	url         string
	token       string
	conn        *websocket.Conn
	reconnectSec int
	sendChan    chan []byte
	recvChan    chan []byte
}

type Message struct {
	Type    string          `json:"type"`
	ServerID string         `json:"server_id"`
	Payload json.RawMessage `json:"payload"`
}

func NewClient(url, token string, reconnectSec int) *Client {
	return &Client{
		url:          url,
		token:        token,
		reconnectSec: reconnectSec,
		sendChan:     make(chan []byte, 256),
		recvChan:     make(chan []byte, 256),
	}
}

func (c *Client) Connect(ctx context.Context) error {
	slog.Info("connecting to control plane", "url", c.url)

	header := make(http.Header)
	header.Set("Authorization", "Bearer "+c.token)

	conn, _, err := websocket.DefaultDialer.Dial(c.url, header)
	if err != nil {
		return err
	}

	c.conn = conn
	slog.Info("connected to control plane")

	go c.readLoop(ctx)
	go c.writeLoop(ctx)

	return nil
}

func (c *Client) readLoop(ctx context.Context) {
	defer func() {
		if c.conn != nil {
			c.conn.Close()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			slog.Warn("websocket read error", "error", err)
			return
		}

		c.recvChan <- data
	}
}

func (c *Client) writeLoop(ctx context.Context) {
	defer func() {
		if c.conn != nil {
			c.conn.Close()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-c.sendChan:
			err := c.conn.WriteMessage(websocket.TextMessage, msg)
			if err != nil {
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

func (c *Client) Recv() <-chan []byte {
	return c.recvChan
}

func (c *Client) Reconnect(ctx context.Context) error {
	slog.Info("reconnecting to control plane", "wait_sec", c.reconnectSec)
	time.Sleep(time.Duration(c.reconnectSec) * time.Second)
	return c.Connect(ctx)
}

func (c *Client) Close() {
	if c.conn != nil {
		c.conn.Close()
	}
}