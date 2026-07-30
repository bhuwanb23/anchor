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

func handlePreflightResult(db *sql.DB, serverID string, payload json.RawMessage) {
	var result struct {
		SystemInfo struct {
			OS              string  `json:"os"`
			OSVersion       string  `json:"os_version"`
			OSPretty        string  `json:"os_pretty,omitempty"`
			Arch            string  `json:"arch"`
			RAMMB           int     `json:"ram_mb"`
			RAMAvailableMB  int     `json:"ram_available_mb"`
			DiskTotalGB     int     `json:"disk_total_gb"`
			DiskAvailableGB int     `json:"disk_available_gb"`
			DiskUsedPercent float64 `json:"disk_used_percent"`
			DockerVersion   string  `json:"docker_version,omitempty"`
		} `json:"system_info"`
		Passed    bool              `json:"passed"`
		Warnings  []json.RawMessage `json:"warnings"`
		AutoFixed []struct {
			Check     string `json:"check"`
			Action    string `json:"action"`
			Timestamp string `json:"timestamp"`
		} `json:"auto_fixed"`
	}

	if err := json.Unmarshal(payload, &result); err != nil {
		slog.Warn("failed to parse preflight_result", "server_id", serverID, "error", err)
		return
	}

	si := result.SystemInfo
	_ = queries.UpdateServerSystemInfo(db, serverID,
		si.OSVersion, si.OSPretty, si.DockerVersion,
		si.RAMAvailableMB, si.DiskTotalGB, si.DiskAvailableGB,
		si.DiskUsedPercent,
	)

	for _, fix := range result.AutoFixed {
		_ = queries.InsertServerEvent(db, uuid.New().String(), serverID, "auto_fixed", fix.Check, fix.Action, fix.Timestamp)
	}

	for _, warn := range result.Warnings {
		_ = queries.InsertServerEvent(db, uuid.New().String(), serverID, "warning", "", string(warn), "")
	}

	slog.Info("preflight result processed", "server_id", serverID, "passed", result.Passed, "warnings", len(result.Warnings))
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

				switch msg.Type {
				case "result":
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
