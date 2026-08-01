package handlers

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/yourname/yourplatform/control-plane/internal/config"
	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
	"github.com/yourname/yourplatform/control-plane/internal/domain"
	"github.com/yourname/yourplatform/control-plane/internal/ws"
)

func MakeDeployApp(db *sql.DB, cfg *config.Config, hub *ws.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ServerID string `json:"server_id"`
			AppName  string `json:"app_name"`
			Image    string `json:"image"`
			Port     int    `json:"port"`
			Domain   string `json:"domain,omitempty"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		if req.ServerID == "" || req.AppName == "" || req.Image == "" || req.Port == 0 {
			http.Error(w, "server_id, app_name, image, and port are required", http.StatusBadRequest)
			return
		}

		// Verify server exists and get IP
		serverName, ipAddress, err := queries.GetServerByID(db, req.ServerID)
		if err == sql.ErrNoRows {
			http.Error(w, "server not found", http.StatusNotFound)
			return
		}
		if err != nil {
			slog.Error("query server", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		_ = serverName

		// Collision detection: check if this app is already deployed to this server
		existingID, err := queries.GetDeploymentByServerAndApp(db, req.ServerID, req.AppName)
		if err != nil && err != sql.ErrNoRows {
			slog.Error("check existing deployment", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		if existingID != "" {
			http.Error(w, "app already deployed to this server (stop it first)", http.StatusConflict)
			return
		}

		// Auto-generate domain if not provided
		domainStr := req.Domain
		if domainStr == "" && cfg.BaseDomain != "" && ipAddress != "" {
			domainStr, err = domain.GenerateDomain(req.AppName, req.ServerID, cfg.BaseDomain)
			if err != nil {
				slog.Error("generate domain", "error", err)
				http.Error(w, "failed to generate domain", http.StatusInternalServerError)
				return
			}
		}

		// Create deployment record
		deploymentID := uuid.New().String()
		if err := queries.InsertDeployment(db, deploymentID, req.ServerID, req.AppName, req.Image, req.Port, domainStr); err != nil {
			slog.Error("insert deployment", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		// Send deploy command to agent via WebSocket
		deployMsg := map[string]interface{}{
			"type":       "deploy",
			"deployment": deploymentID,
			"app_name":   req.AppName,
			"image":      req.Image,
			"port":       req.Port,
			"domain":     domainStr,
		}
		msgBytes, _ := json.Marshal(deployMsg)
		if !hub.SendToAgent(req.ServerID, msgBytes) {
			slog.Warn("agent not connected, deploy queued", "server_id", req.ServerID)
		}

		slog.Info("deploy initiated", "deployment_id", deploymentID, "server_id", req.ServerID, "app", req.AppName, "domain", domainStr)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]string{
			"status":        "queued",
			"deployment_id": deploymentID,
			"domain":        domainStr,
		})
	}
}

func RollbackApp(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "not implemented"})
}

func GetDeploymentStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "unknown"})
}
