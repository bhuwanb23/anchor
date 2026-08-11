package handlers

import (
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/yourplatform/control-plane/internal/api/middleware"
	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
)

type Platform struct {
	DB *sql.DB
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

	// Verify the user has access to this server
	var ownerID string
	err := p.DB.QueryRow("SELECT user_id FROM servers WHERE id = ?", serverID).Scan(&ownerID)
	if err == sql.ErrNoRows {
		Respond404(w, r, "server not found")
		return
	}
	if err != nil {
		slog.Error("query server owner", "error", err)
		Respond500(w, r)
		return
	}
	if ownerID != userID {
		Respond403(w, r, "you do not have access to this server")
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
