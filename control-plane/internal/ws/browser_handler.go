package ws

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
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

		claims, err := auth.ValidateJWT(token, jwtSecret)
		if err != nil {
			http.Error(w, "invalid or expired token", http.StatusUnauthorized)
			return
		}

		userID := claims.UserID

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

		hub.RegisterBrowser(serverID, conn)
		slog.Info("browser connected", "user_id", userID, "server_id", serverID)

		// Write goroutine: forwards messages from hub to browser
		go func() {
			defer func() {
				hub.UnregisterBrowser(serverID, conn)
				slog.Info("browser disconnected", "user_id", userID, "server_id", serverID)
			}()

			// Get the send channel for this browser
			hub.mu.RLock()
			var sendCh chan []byte
			for _, b := range hub.browsers[serverID] {
				if b.Conn == conn {
					sendCh = b.Send
					break
				}
			}
			hub.mu.RUnlock()

			if sendCh == nil {
				return
			}

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
				// On disconnect, stop any active log streams for this browser
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
				hub.UnregisterBrowser(serverID, conn)
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

// getUserIDFromContext extracts the user_id from request context.
func getUserIDFromContext(ctx context.Context) string {
	if uid, ok := ctx.Value("user_id").(string); ok {
		return uid
	}
	return ""
}
