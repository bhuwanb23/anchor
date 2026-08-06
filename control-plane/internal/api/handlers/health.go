package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/yourname/yourplatform/control-plane/internal/ws"
)

const healthVersion = "1.0.0"

var healthStartTime = time.Now()

// Health is the handler for GET /health. It checks database connectivity and
// reports hub connection counts so load balancers can route away from degraded
// instances.
type Health struct {
	DB  *sql.DB
	Hub *ws.Hub
}

type healthResponse struct {
	Status           string  `json:"status"`
	Version          string  `json:"version"`
	Database         string  `json:"database"`
	UptimeSeconds    int64   `json:"uptime_seconds"`
	ConnectedAgents  int     `json:"connected_agents"`
	ConnectedBrowsers int    `json:"connected_browsers"`
	Error            string  `json:"error,omitempty"`
}

func (h *Health) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	resp := healthResponse{
		Status:  "ok",
		Version: healthVersion,
	}

	// Database check with 1-second timeout.
	ctx, cancel := context.WithTimeout(r.Context(), 1*time.Second)
	defer cancel()
	if err := h.DB.PingContext(ctx); err != nil {
		resp.Status = "degraded"
		resp.Database = "error"
		resp.Error = "database unavailable"
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(resp)
		return
	}
	resp.Database = "ok"

	resp.UptimeSeconds = int64(time.Since(healthStartTime).Seconds())

	if h.Hub != nil {
		stats := h.Hub.Stats()
		resp.ConnectedAgents = stats.AgentConnections
		resp.ConnectedBrowsers = stats.BrowserConnections
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
