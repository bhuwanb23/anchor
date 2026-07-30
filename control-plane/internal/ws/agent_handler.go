package ws

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
)

type Message struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func HandleAgentWS(hub *Hub, db *sql.DB) http.HandlerFunc {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Basic ") {
			http.Error(w, "missing basic auth", http.StatusUnauthorized)
			return
		}

		encoded := strings.TrimPrefix(authHeader, "Basic ")
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			http.Error(w, "invalid base64 auth", http.StatusUnauthorized)
			return
		}

		parts := strings.SplitN(string(decoded), ":", 2)
		if len(parts) != 2 {
			http.Error(w, "invalid auth format (expected agent_id:agent_secret)", http.StatusUnauthorized)
			return
		}
		agentID, agentSecret := parts[0], parts[1]

		serverID, _, _, secretHash, status, err := queries.GetServerByAgentID(db, agentID)
		if err != nil {
			slog.Warn("agent lookup failed", "agent_id", agentID, "error", err)
			http.Error(w, "agent not found", http.StatusUnauthorized)
			return
		}

		providedHash := sha256.Sum256([]byte(agentSecret))
		providedHex := hex.EncodeToString(providedHash[:])
		if providedHex != secretHash {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Error("websocket upgrade", "agent_id", agentID, "error", err)
			return
		}

		hub.RegisterAgent(serverID, agentID, conn)
		slog.Info("agent connected", "agent_id", agentID, "server_id", serverID, "status", status)

		_ = conn.WriteJSON(map[string]string{
			"type":      "register_ack",
			"server_id": serverID,
		})

		if status != "connected" {
			_ = queries.UpdateServerConnection(db, serverID, "connected")
		}

		go func() {
			defer func() {
				hub.UnregisterAgent(serverID)
				_ = queries.UpdateServerConnection(db, serverID, "disconnected")
				slog.Info("agent disconnected", "agent_id", agentID, "server_id", serverID)
			}()

			conn.SetReadDeadline(time.Time{})
			for {
				_, data, err := conn.ReadMessage()
				if err != nil {
					if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
						slog.Info("agent ws closed normally", "server_id", serverID)
					} else {
						slog.Warn("agent ws read error", "server_id", serverID, "error", err)
					}
					return
				}

				var msg Message
				if err := json.Unmarshal(data, &msg); err != nil {
					slog.Warn("bad message from agent", "server_id", serverID, "error", err)
					continue
				}

				switch msg.Type {			case "result":
					slog.Info("command result", "server_id", serverID, "payload", string(msg.Payload))
			case "preflight_result":
					handlePreflightResult(db, serverID, msg.Payload)
			default:
					slog.Debug("agent message", "type", msg.Type, "server_id", serverID)
				}
			}
		}()

		go func() {
			sendCh := hub.GetAgentSend(serverID)
			if sendCh == nil {
				return
			}
			for msg := range sendCh {
				if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					slog.Warn("ws write to agent", "server_id", serverID, "error", err)
					return
				}
			}
		}()
	}
}
