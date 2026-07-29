package handlers

import (
	"encoding/json"
	"net/http"
)

func AgentRegister(w http.ResponseWriter, r *http.Request) {
	// TODO: validate token, register server
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}