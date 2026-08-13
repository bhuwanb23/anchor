package handlers

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/yourname/yourplatform/control-plane/internal/api/middleware"
	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
	"github.com/yourname/yourplatform/control-plane/internal/ws"
)

type Platform struct {
	DB  *sql.DB
	Hub *ws.Hub
}

// GetServerPlatform returns the platform detection result for a server.
func (p *Platform) GetServerPlatform(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		Respond401(w, r)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	if serverID == "" {
		Respond400(w, r, "serverID is required")
		return
	}

	if !p.userOwnsServer(w, r, userID, serverID) {
		return
	}

	platform, err := queries.GetServerPlatform(p.DB, serverID)
	if err != nil {
		slog.Error("query server platform", "error", err)
		Respond500(w, r)
		return
	}
	if platform == nil {
		Respond404(w, r, "platform info not available yet — agent will report on next connect")
		return
	}

	RespondJSON(w, http.StatusOK, platform)
}

// DetectPlatform queues a detect_platform command so the agent re-runs readiness
// detection and the control plane stores the result.
func (p *Platform) DetectPlatform(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		Respond401(w, r)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	if serverID == "" {
		Respond400(w, r, "serverID is required")
		return
	}

	if !p.userOwnsServer(w, r, userID, serverID) {
		return
	}

	if p.Hub == nil {
		Respond500(w, r)
		return
	}

	projectKey := "infer:detect"
	if dup, err := queries.HasInProgressCommand(p.DB, serverID, "detect_platform", projectKey); err == nil && dup {
		Respond400(w, r, "Platform detection is already in progress")
		return
	}

	cmdID := "cmd-" + uuid.New().String()[:12]
	payload, _ := json.Marshal(map[string]interface{}{"server_id": serverID})
	now := time.Now().UTC().Format(time.RFC3339)

	if err := queries.InsertCommand(p.DB, cmdID, serverID, "detect_platform", string(payload), projectKey, "queued", userID, now); err != nil {
		slog.Error("insert detect_platform command", "error", err)
		Respond500(w, r)
		return
	}

	cmd := map[string]interface{}{
		"id":      cmdID,
		"type":    "detect_platform",
		"payload": json.RawMessage(payload),
	}
	if err := ws.QueueOrSendCommand(p.Hub, p.DB, serverID, cmd); err != nil {
		slog.Warn("queue/send detect_platform", "error", err, "command_id", cmdID)
	}

	if connID := p.Hub.FindBrowserByUser(userID); connID != "" {
		p.Hub.TrackPendingCommand(cmdID, connID, serverID)
	}

	RespondJSON(w, http.StatusAccepted, map[string]interface{}{
		"command_id": cmdID,
		"status":     "queued",
	})
}

func (p *Platform) userOwnsServer(w http.ResponseWriter, r *http.Request, userID, serverID string) bool {
	var ownerID string
	err := p.DB.QueryRow("SELECT user_id FROM servers WHERE id = ?", serverID).Scan(&ownerID)
	if err == sql.ErrNoRows {
		Respond404(w, r, "server not found")
		return false
	}
	if err != nil {
		slog.Error("query server owner", "error", err)
		Respond500(w, r)
		return false
	}
	if ownerID != userID {
		Respond403(w, r, "you do not have access to this server")
		return false
	}
	return true
}
