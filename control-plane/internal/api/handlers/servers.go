package handlers

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type Server struct {
	DB *sql.DB
}

func (s *Server) ListServers(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value("user_id").(string)
	if !ok || userID == "" {
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

func (s *Server) CreateServer(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value("user_id").(string)
	if !ok || userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{
		"id":              serverID,
		"name":            req.Name,
		"token":           token,
		"install_command": "curl -fsSL https://get.yourplatform.com/install.sh | sudo sh -s -- --token=" + token,
	})
}
