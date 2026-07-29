package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/yourname/yourplatform/control-plane/internal/auth"
	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
)

type Agent struct {
	DB *sql.DB
}

func (a *Agent) Register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token      string `json:"token"`
		ServerInfo struct {
			OS        string `json:"os"`
			Arch      string `json:"arch"`
			RAMMB     int    `json:"ram_mb"`
			DiskGB    int    `json:"disk_gb"`
			IPAddress string `json:"ip_address"`
		} `json:"server_info"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Token == "" {
		http.Error(w, "token is required", http.StatusBadRequest)
		return
	}

	tokenHash := auth.HashRegistrationToken(req.Token)

	tokenID, userID, serverName, expiresAt, usedAt, err := queries.FindRegistrationTokenByHash(a.DB, tokenHash)
	if err == sql.ErrNoRows {
		http.Error(w, "invalid registration token", http.StatusUnauthorized)
		return
	}
	if err != nil {
		slog.Error("query registration token", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if usedAt.Valid {
		http.Error(w, "registration token has already been used", http.StatusConflict)
		return
	}

	expiry, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		slog.Error("parse expiry time", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if time.Now().After(expiry) {
		http.Error(w, "registration token has expired", http.StatusGone)
		return
	}

	serverID := uuid.New().String()
	agentID := uuid.New().String()

	agentSecret, err := generateAgentSecret()
	if err != nil {
		slog.Error("generate agent secret", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	agentSecretHash := hashAgentSecret(agentSecret)

	name := serverName
	if name == "" {
		name = "server-" + agentID[:8]
	}

	err = queries.InsertServerWithAgent(
		a.DB, serverID, userID, name, agentID, agentSecretHash,
		req.ServerInfo.OS, req.ServerInfo.Arch,
		req.ServerInfo.RAMMB, req.ServerInfo.DiskGB,
		req.ServerInfo.IPAddress,
	)
	if err != nil {
		slog.Error("insert server", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	ip := r.Header.Get("X-Real-IP")
	if ip == "" {
		ip = r.RemoteAddr
	}
	if err := queries.MarkRegistrationTokenUsed(a.DB, tokenID, ip); err != nil {
		slog.Error("mark token used", "error", err)
	}

	wsURL := fmt.Sprintf("ws://%s/ws/agent", r.Host)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{
		"agent_id":     agentID,
		"agent_secret": agentSecret,
		"server_id":    serverID,
		"ws_url":       wsURL,
	})
}

func generateAgentSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random secret: %w", err)
	}
	return "as_" + hex.EncodeToString(b), nil
}

func hashAgentSecret(secret string) string {
	hash := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(hash[:])
}
