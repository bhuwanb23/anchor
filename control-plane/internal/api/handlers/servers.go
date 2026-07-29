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

	rows, err := s.DB.Query(
		"SELECT id, name, status, connected_at, last_seen FROM servers WHERE user_id = ?",
		userID,
	)
	if err != nil {
		slog.Error("query servers", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var servers []map[string]interface{}
	for rows.Next() {
		var id, name, status, connectedAt, lastSeen string
		if err := rows.Scan(&id, &name, &status, &connectedAt, &lastSeen); err != nil {
			slog.Error("scan server row", "error", err)
			continue
		}
		servers = append(servers, map[string]interface{}{
			"id":           id,
			"name":         name,
			"status":       status,
			"connected_at": connectedAt,
			"last_seen":    lastSeen,
		})
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
