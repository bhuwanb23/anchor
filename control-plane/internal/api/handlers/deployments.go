package handlers

import (
	"encoding/json"
	"net/http"
)

func DeployApp(w http.ResponseWriter, r *http.Request) {
	// TODO: validate input, get server_id from URL params
	var req struct {
		AppName  string `json:"app_name"`
		Image    string `json:"image"`
		Port     int    `json:"port"`
		Domain   string `json:"domain"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.AppName == "" || req.Image == "" || req.Port == 0 {
		http.Error(w, "app_name, image, and port are required", http.StatusBadRequest)
		return
	}

	// TODO: send deploy command to agent via WebSocket
	_ = req

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "queued"})
}

func RollbackApp(w http.ResponseWriter, r *http.Request) {
	// TODO: implement rollback
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "not implemented"})
}

func GetDeploymentStatus(w http.ResponseWriter, r *http.Request) {
	// TODO: get deployment status from database
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "unknown"})
}