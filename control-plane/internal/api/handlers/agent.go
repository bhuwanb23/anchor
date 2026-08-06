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
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yourname/yourplatform/control-plane/internal/auth"
	"github.com/yourname/yourplatform/control-plane/internal/config"
	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
	"github.com/yourname/yourplatform/control-plane/internal/dns"
)

const controlPlaneVersion = "1.0.0"

type Agent struct {
	DB      *sql.DB
	DNS     *dns.Client
	Config  *config.Config
}

func (a *Agent) Register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token     string `json:"token"`
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
		AgentVersion string          `json:"agent_version"`
		IPAddress    string          `json:"ip_address"`
		Warnings     json.RawMessage `json:"warnings,omitempty"`
		AutoFixed    json.RawMessage `json:"auto_fixed,omitempty"`
	}

	if err := DecodeJSON(w, r, &req); err != nil {
		Respond400(w, r, err.Error())
		return
	}

	if req.Token == "" {
		Respond400(w, r, "token is required")
		return
	}

	if !strings.HasPrefix(req.Token, "reg_") {
		RespondError(w, r, http.StatusUnauthorized, "invalid_token", "Invalid registration token")
		return
	}

	tokenHash := auth.HashRegistrationToken(req.Token)

	tokenID, userID, serverName, expiresAt, usedAt, err := queries.FindRegistrationTokenByHash(a.DB, tokenHash)
	if err == sql.ErrNoRows {
		RespondError(w, r, http.StatusUnauthorized, "invalid_token", "Invalid registration token")
		return
	}
	if err != nil {
		slog.Error("query registration token", "error", err)
		Respond500(w, r)
		return
	}

	if usedAt.Valid {
		RespondError(w, r, http.StatusUnauthorized, "token_already_used", "Registration token already used")
		return
	}

	expiry, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		slog.Error("parse expiry time", "error", err)
		Respond500(w, r)
		return
	}

	if time.Now().After(expiry) {
		RespondError(w, r, http.StatusUnauthorized, "token_expired", "Registration token expired. Generate a new one.")
		return
	}

	serverID := uuid.New().String()

	agentID, err := generateAgentID()
	if err != nil {
		slog.Error("generate agent id", "error", err)
		Respond500(w, r)
		return
	}

	agentSecret, err := generateAgentSecret()
	if err != nil {
		slog.Error("generate agent secret", "error", err)
		Respond500(w, r)
		return
	}
	agentSecretHash := hashAgentSecret(agentSecret)

	name := serverName
	if name == "" {
		name = "server-" + agentID[4:12]
	}

	err = queries.InsertServerWithAgent(
		a.DB, serverID, userID, name, agentID, agentSecretHash,
		req.SystemInfo.OS, req.SystemInfo.Arch,
		req.SystemInfo.RAMMB, req.SystemInfo.DiskTotalGB,
		req.IPAddress,
	)
	if err != nil {
		slog.Error("insert server", "error", err)
		Respond500(w, r)
		return
	}

	_ = queries.UpdateServerSystemInfo(a.DB, serverID,
		req.SystemInfo.OSVersion, req.SystemInfo.OSPretty, req.SystemInfo.DockerVersion,
		req.SystemInfo.RAMAvailableMB, req.SystemInfo.DiskTotalGB, req.SystemInfo.DiskAvailableGB,
		req.SystemInfo.DiskUsedPercent,
	)

	if a.DNS != nil && a.Config != nil && req.IPAddress != "" {
		subdomain := "srv-" + serverID[:8]
		if err := a.DNS.UpsertWildcard(r.Context(), subdomain, req.IPAddress); err != nil {
			slog.Error("create wildcard DNS record", "error", err, "server_id", serverID, "ip", req.IPAddress)
		} else {
			slog.Info("created wildcard DNS record", "subdomain", subdomain, "ip", req.IPAddress)
		}
	}

	teamID, err := queries.EnsureUserPersonalTeam(a.DB, userID, "")
	if err != nil {
		slog.Error("failed to get personal team for agent registration", "error", err, "user_id", userID)
	} else if err := queries.LinkServerToTeam(a.DB, serverID, teamID); err != nil {
		slog.Error("failed to link server to team on agent registration", "error", err, "server_id", serverID, "team_id", teamID)
	}

	_ = queries.InsertServerEvent(a.DB, uuid.New().String(), serverID, "server_registered", "registration", "Server registered", "")

	if len(req.AutoFixed) > 0 {
		var fixes []struct {
			Check  string `json:"check"`
			Action string `json:"action"`
		}
		if err := json.Unmarshal(req.AutoFixed, &fixes); err == nil {
			for _, fix := range fixes {
				_ = queries.InsertServerEvent(a.DB, uuid.New().String(), serverID, "auto_fixed", fix.Check, fix.Action, "")
			}
		}
	}

	ip := r.Header.Get("X-Real-IP")
	if ip == "" {
		ip = r.RemoteAddr
	}
	if err := queries.MarkRegistrationTokenUsed(a.DB, tokenID, ip); err != nil {
		slog.Error("mark token used", "error", err)
	}

	scheme := "ws://"
	if r.TLS != nil {
		scheme = "wss://"
	}
	wsHost := r.Host
	if a.Config != nil && a.Config.BaseDomain != "" {
		wsHost = "ws." + a.Config.BaseDomain
	}
	wsURL := scheme + wsHost + "/ws/agent"

	RespondJSON(w, http.StatusCreated, map[string]string{
		"agent_id":              agentID,
		"agent_secret":          agentSecret,
		"server_id":             serverID,
		"websocket_url":         wsURL,
		"control_plane_version": controlPlaneVersion,
	})
}

func generateAgentID() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random agent id: %w", err)
	}
	return "agt-" + hex.EncodeToString(b), nil
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
