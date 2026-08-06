package handlers

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/yourname/yourplatform/control-plane/internal/api/middleware"
	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
)

type Server struct {
	DB *sql.DB
}

func (s *Server) ListServers(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	rows, err := s.DB.Query(`
		SELECT id, name, status, connected_at, last_seen,
			os_info, os_version, os_pretty, arch,
			ram_mb, ram_available_mb,
			disk_gb, disk_total_gb, disk_available_gb, disk_used_percent,
			docker_version, ip_address
		FROM servers WHERE user_id = ?
	`, userID)
	if err != nil {
		slog.Error("query servers", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var servers []map[string]interface{}
	for rows.Next() {
		var id, name, status, connectedAt, lastSeen string
		var osInfo, osVersion, osPretty, arch, dockerVersion, ipAddress *string
		var ramMB, ramAvailableMB, diskGB, diskTotalGB, diskAvailableGB *int
		var diskUsedPercent *float64

		if err := rows.Scan(
			&id, &name, &status, &connectedAt, &lastSeen,
			&osInfo, &osVersion, &osPretty, &arch,
			&ramMB, &ramAvailableMB,
			&diskGB, &diskTotalGB, &diskAvailableGB, &diskUsedPercent,
			&dockerVersion, &ipAddress,
		); err != nil {
			slog.Error("scan server row", "error", err)
			continue
		}

		server := map[string]interface{}{
			"id":           id,
			"name":         name,
			"status":       status,
			"connected_at": connectedAt,
			"last_seen":    lastSeen,
		}
		if osInfo != nil {
			server["os_info"] = *osInfo
		}
		if osVersion != nil {
			server["os_version"] = *osVersion
		}
		if osPretty != nil {
			server["os_pretty"] = *osPretty
		}
		if arch != nil {
			server["arch"] = *arch
		}
		if ramMB != nil {
			server["ram_mb"] = *ramMB
		}
		if ramAvailableMB != nil {
			server["ram_available_mb"] = *ramAvailableMB
		}
		if diskGB != nil {
			server["disk_gb"] = *diskGB
		}
		if diskTotalGB != nil {
			server["disk_total_gb"] = *diskTotalGB
		}
		if diskAvailableGB != nil {
			server["disk_available_gb"] = *diskAvailableGB
		}
		if diskUsedPercent != nil {
			server["disk_used_percent"] = *diskUsedPercent
		}
		if dockerVersion != nil {
			server["docker_version"] = *dockerVersion
		}
		if ipAddress != nil {
			server["ip_address"] = *ipAddress
		}

		servers = append(servers, server)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(servers)
}

func (s *Server) ListEvents(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	if serverID == "" {
		http.Error(w, "server ID required", http.StatusBadRequest)
		return
	}

	rows, err := s.DB.Query(
		`SELECT id, event_type, check_name, message, details, created_at
		FROM server_events WHERE server_id = ?
		ORDER BY created_at DESC LIMIT 50`,
		serverID,
	)
	if err != nil {
		slog.Error("query server events", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var events []map[string]interface{}
	for rows.Next() {
		var id, eventType, createdAt string
		var checkName, message, details *string
		if err := rows.Scan(&id, &eventType, &checkName, &message, &details, &createdAt); err != nil {
			slog.Error("scan event row", "error", err)
			continue
		}
		event := map[string]interface{}{
			"id":         id,
			"event_type": eventType,
			"created_at": createdAt,
		}
		if checkName != nil {
			event["check_name"] = *checkName
		}
		if message != nil {
			event["message"] = *message
		}
		if details != nil {
			event["details"] = *details
		}
		events = append(events, event)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)
}

// ListAlerts returns the persisted Layer 4C Step 5 alerts for a server,
// newest first with active alerts floated to the top.
func (s *Server) ListAlerts(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	if serverID == "" {
		http.Error(w, "server ID required", http.StatusBadRequest)
		return
	}
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" || !s.serverOwnedBy(userID, serverID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	alerts, err := queries.ListAlertsByServer(s.DB, serverID, 100)
	if err != nil {
		slog.Error("query alerts", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(alerts)
}

// ListAllAlerts returns recent alerts across every server the user owns plus
// the unread count for the notification center bell.
func (s *Server) ListAllAlerts(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	alerts, err := queries.ListRecentAlertsForUser(s.DB, userID, 50)
	if err != nil {
		slog.Error("query recent alerts", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	unread, err := queries.UnreadAlertCountForUser(s.DB, userID)
	if err != nil {
		slog.Error("count unread alerts", "error", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"alerts":       alerts,
		"unread_count": unread,
	})
}

// MarkAllAlertsRead stamps read_at on the user's active alerts (called when
// the notification center is opened).
func (s *Server) MarkAllAlertsRead(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := queries.MarkAlertsReadForUser(s.DB, userID); err != nil {
		slog.Error("mark alerts read", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AcknowledgeAlert marks an active alert as acknowledged by the user.
func (s *Server) AcknowledgeAlert(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	alertID := chi.URLParam(r, "alertID")
	if serverID == "" || alertID == "" {
		http.Error(w, "server and alert IDs required", http.StatusBadRequest)
		return
	}
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !s.serverOwnedBy(userID, serverID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := queries.AcknowledgeAlert(s.DB, alertID, serverID, userID); err != nil {
		slog.Error("acknowledge alert", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// serverOwnedBy reports whether the given user owns the server.
func (s *Server) serverOwnedBy(userID, serverID string) bool {
	var count int
	err := s.DB.QueryRow("SELECT COUNT(*) FROM servers WHERE id = ? AND user_id = ?", serverID, userID).Scan(&count)
	return err == nil && count > 0
}

// getServerTeam returns the team that owns the given server.
func (s *Server) getServerTeam(serverID string) (string, error) {
	team, err := queries.GetServerTeam(s.DB, serverID)
	if err != nil {
		return "", err
	}
	return team.ID, nil
}

func (s *Server) CreateServer(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Name   string `json:"name"`
		TeamID string `json:"team_id"`
	}
	if err := DecodeJSON(w, r, &req); err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	serverID := uuid.New().String()
	token := uuid.New().String()

	_, err := s.DB.Exec(
		"INSERT INTO servers (id, user_id, name, token) VALUES (?, ?, ?, ?)",
		serverID, userID, req.Name, token,
	)
	if err != nil {
		slog.Error("insert server", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Link server to team if team_id provided, otherwise link to user's personal team
	teamID := req.TeamID
	if teamID == "" {
		// Get or create user's personal team
		var err error
		teamID, err = queries.EnsureUserPersonalTeam(s.DB, userID, "")
		if err != nil {
			slog.Error("failed to get personal team", "error", err, "user_id", userID)
		}
	}

	if teamID != "" {
		if err := queries.LinkServerToTeam(s.DB, serverID, teamID); err != nil {
			slog.Error("failed to link server to team", "error", err, "server_id", serverID, "team_id", teamID)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{
		"id":              serverID,
		"name":            req.Name,
		"token":           token,
		"install_command": "curl -fsSL https://get.yourplatform.com/install.sh | sudo sh -s -- --token=" + token,
	})
}

// DeleteServer removes a server. Requires admin role in the server's team.
func (s *Server) DeleteServer(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	if serverID == "" {
		http.Error(w, "server ID required", http.StatusBadRequest)
		return
	}

	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Check team-based access: user must be admin or owner in the server's team
	teamID, err := s.getServerTeam(serverID)
	if err != nil {
		http.Error(w, "server not found", http.StatusNotFound)
		return
	}

	role, err := queries.GetUserTeamRole(s.DB, teamID, userID)
	if err != nil || role == "" {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": "You do not have access to this server.",
			"code":  "forbidden",
		})
		return
	}
	if role != "owner" && role != "admin" {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": "Only the server owner can delete this server. Ask the owner if you need it removed.",
			"code":  "insufficient_permissions",
		})
		return
	}

	// Mark server as deleted
	if _, err := s.DB.Exec(
		"UPDATE servers SET status = 'deleted' WHERE id = ?",
		serverID,
	); err != nil {
		slog.Error("delete server", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
