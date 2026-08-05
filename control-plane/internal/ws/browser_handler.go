package ws

import (
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
// Browsers connect here to receive real-time updates (server state, log
// streaming, command progress) for servers they own or share via a team.
//
// Flow (Layer 5B Step 3A): JWT is validated, the connection is upgraded and
// registered in the hub WITHOUT a server subscription, and a welcome message
// is sent. The dashboard then subscribes to a server explicitly (either via a
// subscribe message or the server_id query parameter, which behaves like an
// immediate subscribe).
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

		// server_id is optional: when present it behaves like an immediate
		// subscribe (validated before upgrade); otherwise the dashboard
		// subscribes via a subscribe message after connecting.
		serverID := r.URL.Query().Get("server_id")
		if serverID != "" && !userHasServerAccess(db, userID, serverID) {
			http.Error(w, "server not found or access denied", http.StatusNotFound)
			return
		}

		// Upgrade to WebSocket
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Error("browser websocket upgrade", "user_id", userID, "error", err)
			return
		}

		connID, sendCh := hub.RegisterBrowser(userID, conn)
		watching := ""
		slog.Info("browser connected", "user_id", userID, "server_id", serverID, "connection_id", connID)

		// Welcome the dashboard with its connection id (Step 3A).
		welcome, _ := json.Marshal(map[string]interface{}{
			"type":          "connected",
			"connection_id": connID,
		})
		hub.SendToBrowser(connID, welcome)

		// URL-driven subscribe: same path as a subscribe message (Step 3B).
		if serverID != "" {
			hub.Subscribe(serverID, connID)
			watching = serverID
			hub.SendToBrowser(connID, buildServerStateSnapshot(db, serverID))
		}

		// Writer goroutine: forwards messages from hub to browser. It is the
		// ONLY goroutine that writes to the WebSocket.
		go func() {
			defer func() {
				// No-op if the reader already unregistered this connection.
				// (Note: `watching` is owned by the reader goroutine, so it must
				// not be read here.)
				hub.UnregisterBrowser(connID)
				slog.Info("browser disconnected", "user_id", userID)
			}()

			for msg := range sendCh {
				if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					slog.Warn("browser ws write", "user_id", userID, "error", err)
					return
				}
			}
		}()

		// Reader goroutine: receives messages from browser, routes via hub.
		go func() {
			defer func() {
				// On disconnect, stop any active log streams and forget the
				// recorded desires; the dashboard re-sends stream_logs on
				// reconnect.
				if watching != "" {
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
					hub.SendToAgent(watching, stopMsg)
					hub.ClearServerStreams(watching)
				}
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

				switch msg.Type {
				case "command":
					// User wants to do something (deploy, restart, ...). Forward
					// to the server currently being watched.
					if watching == "" {
						hub.SendToBrowser(connID, browserErrorMsg("not subscribed to a server"))
						continue
					}
					agentMsg, _ := json.Marshal(msg)
					// Track log-stream desires so active views are re-established
					// when the agent reconnects (Layer 4C 3B).
					trackStreamCommand(hub, watching, msg, agentMsg)
					if !hub.SendToAgent(watching, agentMsg) {
						slog.Warn("no agent connected for command", "user_id", userID, "server_id", watching)
						// Send error back to browser (through the writer).
						hub.SendToBrowser(connID, browserErrorMsg("agent not connected"))
					} else if cmdID := browserCommandID(msg); cmdID != "" {
						// Delivered: record that this browser is waiting for the
						// result so the hub can route ack/progress/result (and
						// disconnect failures) back to exactly this dashboard
						// (Layer 5B Step 2). Only tracked on success so an
						// offline agent does not leak a pending entry.
						hub.TrackPendingCommand(cmdID, connID, watching)
					}

				case "subscribe":
					// Step 3B: watch a server, validated against the team
					// permission model. Subscribing to a new server
					// auto-unsubscribes from the previous one.
					var sub struct {
						ServerID string `json:"server_id"`
					}
					if err := json.Unmarshal(data, &sub); err != nil || sub.ServerID == "" {
						continue
					}
					if !userHasServerAccess(db, userID, sub.ServerID) {
						hub.SendToBrowser(connID, browserErrorMsg("You do not have access to this server"))
						continue
					}
					hub.Subscribe(sub.ServerID, connID)
					watching = sub.ServerID
					// Immediate snapshot of the server's current state.
					hub.SendToBrowser(connID, buildServerStateSnapshot(db, sub.ServerID))

				case "unsubscribe":
					// Step 3C Way 1: browser navigates away.
					var un struct {
						ServerID string `json:"server_id"`
					}
					if err := json.Unmarshal(data, &un); err != nil || un.ServerID == "" {
						continue
					}
					hub.Unsubscribe(un.ServerID, connID)
					if watching == un.ServerID {
						watching = ""
					}

				case "ping":
					// Browser heartbeat: respond with pong (Step 3D).
					hub.SendToBrowser(connID, []byte(`{"type":"pong"}`))

				case "start_log_stream":
					// Step 3D: register stream routing and forward the start
					// command to the agent.
					if watching == "" || !userHasServerAccess(db, userID, watching) {
						hub.SendToBrowser(connID, browserErrorMsg("no access to start log stream"))
						continue
					}
					key := streamPayloadKey(msg.Payload)
					if key == "" {
						hub.SendToBrowser(connID, browserErrorMsg("log stream request is missing project_name or stream_id"))
						continue
					}
					agentCmd := buildStreamCommand("stream_logs", msg.Payload)
					if !hub.SendToAgent(watching, agentCmd) {
						hub.SendToBrowser(connID, browserErrorMsg("agent not connected"))
						continue
					}
					hub.RecordStreamCommand(watching, key, agentCmd)
					hub.RegisterLogStream(key, connID)

				case "stop_log_stream":
					// Step 3D: remove stream routing and tell the agent to stop.
					if watching == "" {
						continue
					}
					if key := streamPayloadKey(msg.Payload); key != "" {
						hub.UnregisterLogStream(key)
						hub.ClearStreamCommand(watching, key)
					}
					hub.SendToAgent(watching, buildStreamCommand("stop_stream_logs", msg.Payload))

				default:
					slog.Debug("browser message", "type", msg.Type, "user_id", userID)
				}
			}
		}()
	}
}

// userHasServerAccess reports whether the user owns the server or shares it
// through a team (Layer 5A permission model). Used to validate subscriptions.
func userHasServerAccess(db *sql.DB, userID, serverID string) bool {
	ids, err := queries.ListServersByUserID(db, userID)
	if err != nil {
		slog.Warn("server access check failed", "user_id", userID, "error", err)
		return false
	}
	for _, id := range ids {
		if id == serverID {
			return true
		}
	}
	return false
}

// browserErrorMsg builds a browser-facing error envelope.
func browserErrorMsg(message string) []byte {
	b, _ := json.Marshal(map[string]interface{}{
		"type": "error",
		"payload": map[string]interface{}{
			"message": message,
		},
	})
	return b
}

// buildServerStateSnapshot assembles the immediate server_state snapshot a
// browser receives when it subscribes (Layer 5B Step 3B): current connection
// status, latest container statuses, latest health metrics, and active alerts.
func buildServerStateSnapshot(db *sql.DB, serverID string) []byte {
	status, _ := queries.GetServerStatus(db, serverID)

	containers := []map[string]interface{}{}
	if rows, err := queries.GetServerContainers(db, serverID); err == nil {
		for _, c := range rows {
			health := ""
			if c.Health.Valid {
				health = c.Health.String
			}
			containers = append(containers, map[string]interface{}{
				"project":        c.Project,
				"role":           c.Role,
				"container_id":   c.ContainerID,
				"status":         c.Status,
				"health":         health,
				"cpu_percent":    c.CPUPercent,
				"ram_used_mb":    c.RAMUsedMB,
				"ram_limit_mb":   c.RAMLimitMB,
				"ram_percent":    c.RAMPercent,
				"restart_count":  c.RestartCount,
				"uptime_seconds": c.UptimeSecs,
			})
		}
	}

	metrics := map[string]interface{}{}
	if m, err := queries.GetLatestMetric(db, serverID); err == nil && m != nil {
		metrics = map[string]interface{}{
			"cpu_percent":        metricFloat(m.CPUPercent),
			"ram_percent":        metricFloat(m.RAMPercent),
			"disk_percent":       metricFloat(m.DiskPercent),
			"ram_used_mb":        metricInt(m.RAMUsedMB),
			"ram_total_mb":       metricInt(m.RAMTotalMB),
			"disk_used_gb":       metricFloat(m.DiskUsedGB),
			"disk_total_gb":      metricFloat(m.DiskTotalGB),
			"load_1min":          metricFloat(m.Load1Min),
			"caddy_running":      m.CaddyRunning != nil && *m.CaddyRunning,
			"caddy_routes_count": metricIntPtr(m.CaddyRoutesCount),
			"container_count":    metricIntPtr(m.ContainerCount),
		}
	}

	alerts := []map[string]interface{}{}
	if list, err := queries.ListAlertsByServer(db, serverID, 10); err == nil {
		for _, a := range list {
			if a.Status == "resolved" {
				continue
			}
			alerts = append(alerts, map[string]interface{}{
				"id":        a.ID,
				"severity":  a.Severity,
				"type":      a.Type,
				"status":    a.Status,
				"title":     a.Title,
				"message":   a.Message,
				"project":   a.Project,
				"container": a.Container,
				"fired_at":  a.FiredAt,
			})
		}
	}

	snapshot, _ := json.Marshal(map[string]interface{}{
		"type": "server_state",
		"payload": map[string]interface{}{
			"server": map[string]interface{}{
				"id":         serverID,
				"status":     status,
				"containers": containers,
				"metrics":    metrics,
				"alerts":     alerts,
				"routes":     []string{},
			},
		},
	})
	return snapshot
}

// metricFloat unwraps a nullable float for the snapshot payload.
func metricFloat(v *float64) interface{} {
	if v == nil {
		return nil
	}
	return *v
}

// metricInt unwraps a nullable int64 for the snapshot payload.
func metricInt(v *int64) interface{} {
	if v == nil {
		return nil
	}
	return *v
}

// metricIntPtr unwraps a nullable int for the snapshot payload.
func metricIntPtr(v *int) interface{} {
	if v == nil {
		return nil
	}
	return *v
}

// streamPayloadKey derives the routing identity for a start/stop log stream
// request. It prefers an explicit stream_id; otherwise it falls back to the
// stable project|roles signature. Agent log lines carry the same stream_id (or
// the project|roles signature) so stream-scoped routing lines up.
func streamPayloadKey(payload json.RawMessage) string {
	var p map[string]interface{}
	if err := json.Unmarshal(payload, &p); err != nil {
		return ""
	}
	if sid, _ := p["stream_id"].(string); sid != "" {
		return sid
	}
	return streamCommandKey(p)
}

// buildStreamCommand wraps a stream control request in the command envelope
// the agent expects (same shape as the existing stream_logs protocol).
func buildStreamCommand(innerType string, payload json.RawMessage) []byte {
	env, _ := json.Marshal(map[string]interface{}{
		"type": "command",
		"payload": map[string]interface{}{
			"type":    innerType,
			"payload": json.RawMessage(payload),
		},
	})
	return env
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

// browserCommandID extracts the id of a browser-issued command so its result
// can be routed back. Stream-control commands (stream_logs / stop_stream_logs)
// produce no result and are never tracked.
func browserCommandID(msg Message) string {
	var inner struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(msg.Payload, &inner); err != nil {
		return ""
	}
	if inner.Type == "stream_logs" || inner.Type == "stop_stream_logs" {
		return ""
	}
	return inner.ID
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
