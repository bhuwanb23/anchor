package handlers

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"

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
