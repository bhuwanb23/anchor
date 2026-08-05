package ws

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/yourname/yourplatform/control-plane/internal/auth"
	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
)

// HandleBrowserWS creates an HTTP handler for browser WebSocket connections.
// Browsers connect here to receive real-time updates (log streaming, etc.)
// for a specific server they own.
func HandleBrowserWS(hub *Hub, db *sql.DB, jwtSecret string) http.HandlerFunc {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	return func(w http.ResponseWriter, r *http.Request) {
		// Authenticate via JWT query parameter or Authorization header
		token := r.URL.Query().Get("token")
		if token == "" {
			authHeader := r.Header.Get("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				token = strings.TrimPrefix(authHeader, "Bearer ")
			}
		}

		if token == "" {
			http.Error(w, "missing authentication token", http.StatusUnauthorized)
			return
		}

		claims, err := auth.ValidateAccessToken(token, jwtSecret)
		if err != nil {
			http.Error(w, "invalid or expired token", http.StatusUnauthorized)
			return
		}

		userID := claims.UserID()

		// Get server ID from query parameter
		serverID := r.URL.Query().Get("server_id")
		if serverID == "" {
			http.Error(w, "missing server_id parameter", http.StatusBadRequest)
			return
		}

		// Verify ownership by checking server exists for this user
		rows, err := queries.QueryServersByUser(db, userID)
		if err != nil {
			slog.Warn("server query failed", "user_id", userID, "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		found := false
		for rows.Next() {
			var id, name, status string
			if err := rows.Scan(&id, &name, &status); err != nil {
				continue
			}
			if id == serverID {
				found = true
				break
			}
		}

		if !found {
			http.Error(w, "server not found or access denied", http.StatusNotFound)
			return
		}

		// Upgrade to WebSocket
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Error("browser websocket upgrade", "user_id", userID, "error", err)
			return
		}

		connID, sendCh := hub.RegisterBrowser(serverID, userID, conn)
		slog.Info("browser connected", "user_id", userID, "server_id", serverID, "connection_id", connID)

		// Write goroutine: forwards messages from hub to browser
		go func() {
			defer func() {
				hub.UnregisterBrowser(connID)
				slog.Info("browser disconnected", "user_id", userID, "server_id", serverID)
			}()

			for msg := range sendCh {
				if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					slog.Warn("browser ws write", "user_id", userID, "error", err)
					return
				}
			}
		}()

		// Read goroutine: receives commands from browser, forwards to agent
		go func() {
			defer func() {
				// On disconnect, stop any active log streams and forget the
				// recorded desires; the dashboard re-sends stream_logs on
				// reconnect.
				stopMsg, _ := json.Marshal(map[string]interface{}{
					"type": "command",
					"payload": map[string]interface{}{
						"id":   "browser_disconnect",
						"type": "stop_stream_logs",
						"payload": map[string]interface{}{
							"all": true,
						},
					},
				})
				hub.SendToAgent(serverID, stopMsg)
				hub.ClearServerStreams(serverID)
				hub.UnregisterBrowser(connID)
				conn.Close()
			}()

			conn.SetReadDeadline(time.Time{})
			for {
				_, data, err := conn.ReadMessage()
				if err != nil {
					if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
						slog.Info("browser ws closed", "user_id", userID)
					} else {
						slog.Warn("browser ws read error", "user_id", userID, "error", err)
					}
					return
				}

				var msg Message
				if err := json.Unmarshal(data, &msg); err != nil {
					slog.Warn("bad message from browser", "user_id", userID, "error", err)
					continue
				}

				// Forward commands from browser to agent
				if msg.Type == "command" {
					agentMsg, _ := json.Marshal(msg)
					// Track log-stream desires so active views are re-established
					// when the agent reconnects (Layer 4C 3B).
					trackStreamCommand(hub, serverID, msg, agentMsg)
					if !hub.SendToAgent(serverID, agentMsg) {
						slog.Warn("no agent connected for command", "user_id", userID, "server_id", serverID)
						// Send error back to browser
						errResp, _ := json.Marshal(map[string]interface{}{
							"type": "error",
							"payload": map[string]interface{}{
								"message": "agent not connected",
							},
						})
						_ = conn.WriteMessage(websocket.TextMessage, errResp)
					}
				}
			}
		}()
	}
}

// trackStreamCommand records or clears the hub's per-server log-stream
// desires based on stream_logs / stop_stream_logs commands from a browser.
func trackStreamCommand(hub *Hub, serverID string, msg Message, agentMsg []byte) {
	var inner struct {
		Type    string                 `json:"type"`
		Payload map[string]interface{} `json:"payload"`
	}
	if err := json.Unmarshal(msg.Payload, &inner); err != nil || inner.Type == "" {
		return
	}
	switch inner.Type {
	case "stream_logs":
		if key := streamCommandKey(inner.Payload); key != "" {
			hub.RecordStreamCommand(serverID, key, agentMsg)
		}
	case "stop_stream_logs":
		if all, _ := inner.Payload["all"].(bool); all {
			hub.ClearServerStreams(serverID)
		} else if key := streamCommandKey(inner.Payload); key != "" {
			hub.ClearStreamCommand(serverID, key)
		}
	}
}

// streamCommandKey derives a stable identity (project|roles) for a stream
// request so duplicates and matching stops can be tracked.
func streamCommandKey(p map[string]interface{}) string {
	if p == nil {
		return ""
	}
	project, _ := p["project_name"].(string)
	if project == "" {
		return ""
	}
	var roles []string
	if raw, ok := p["containers"].([]interface{}); ok {
		for _, r := range raw {
			if s, ok := r.(string); ok {
				roles = append(roles, s)
			}
		}
	}
	sort.Strings(roles)
	return project + "|" + strings.Join(roles, ",")
}

// getUserIDFromContext extracts the user_id from request context.
func getUserIDFromContext(ctx context.Context) string {
	if uid, ok := ctx.Value("user_id").(string); ok {
		return uid
	}
	return ""
}
