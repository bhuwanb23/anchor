package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/yourname/yourplatform/control-plane/internal/api/middleware"
	"github.com/yourname/yourplatform/control-plane/internal/auth"
	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
)

// Step5 holds the database dependency for all Step 5 handlers.
type Step5 struct {
	DB *sql.DB
}

// requireAccess checks if the user can access the server. Returns the role or "".
func (s *Step5) requireAccess(userID, serverID string) string {
	role, _ := queries.GetUserServerRole(s.DB, userID, serverID)
	return role
}

// ---------------------------------------------------------------------------
// Step 5B — Server Handlers
// ---------------------------------------------------------------------------

// GetServer returns full server detail including latest metrics.
func (s *Step5) GetServer(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		Respond401(w, r)
		return
	}
	if role := s.requireAccess(userID, serverID); role == "" {
		Respond403(w, r, "You do not have access to this server")
		return
	}

	server, err := queries.GetServer(s.DB, serverID)
	if err != nil {
		if err == sql.ErrNoRows {
			Respond404(w, r, "Server")
			return
		}
		slog.Error("get server", "error", err)
		Respond500(w, r)
		return
	}

	resp := ServerToResponse(*server)

	// Attach latest metrics if available.
	if m, err := queries.GetServerMetricsLatest(s.DB, serverID); err == nil && m != nil {
		snap := &MetricsSnapshot{}
		if m.CPUPercent != nil {
			snap.CPUPercent = *m.CPUPercent
		}
		if m.RAMUsedMB != nil {
			snap.RAMUsedMB = *m.RAMUsedMB
		}
		if m.RAMTotalMB != nil {
			snap.RAMTotalMB = *m.RAMTotalMB
		}
		if m.RAMPercent != nil {
			snap.RAMPercent = *m.RAMPercent
		}
		if m.DiskUsedGB != nil {
			snap.DiskUsedGB = *m.DiskUsedGB
		}
		if m.DiskTotalGB != nil {
			snap.DiskTotalGB = *m.DiskTotalGB
		}
		if m.Load1Min != nil {
			snap.Load1Min = *m.Load1Min
		}
		resp.Metrics = snap
	}

	RespondJSON(w, http.StatusOK, resp)
}

// CreateServerRegistrationToken generates a registration token for a specific server.
func (s *Step5) CreateServerRegistrationToken(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		Respond401(w, r)
		return
	}

	// Only owner can generate tokens.
	role := s.requireAccess(userID, serverID)
	if role != "owner" && role != "admin" {
		Respond403(w, r, "Only server owners can generate registration tokens")
		return
	}

	rawToken, hashedToken, err := auth.GenerateRegistrationToken()
	if err != nil {
		slog.Error("generate registration token", "error", err)
		Respond500(w, r)
		return
	}

	tokenID := uuid.New().String()
	expiresAt := time.Now().UTC().Add(1 * time.Hour).Format(time.RFC3339)

	if err := queries.CreateRegistrationToken(s.DB, tokenID, hashedToken, userID, serverID, expiresAt); err != nil {
		slog.Error("insert registration token", "error", err)
		Respond500(w, r)
		return
	}

	scheme := "http://"
	if r.TLS != nil {
		scheme = "https://"
	}
	baseURL := scheme + r.Host
	installCommand := fmt.Sprintf("curl -fsSL %s/install.sh | sudo sh -s -- --token=%s --base-url=%s", baseURL, rawToken, baseURL)

	RespondJSON(w, http.StatusOK, map[string]string{
		"token":           rawToken,
		"install_command": installCommand,
		"expires_at":      expiresAt,
	})
}

// ---------------------------------------------------------------------------
// Step 5C — App Handlers
// ---------------------------------------------------------------------------

// ListApps returns all apps for a server.
func (s *Step5) ListApps(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		Respond401(w, r)
		return
	}
	if role := s.requireAccess(userID, serverID); role == "" {
		Respond403(w, r, "You do not have access to this server")
		return
	}

	apps, err := queries.ListAppsByServer(s.DB, serverID)
	if err != nil {
		slog.Error("list apps", "error", err)
		Respond500(w, r)
		return
	}

	resp := make([]AppResponse, 0, len(apps))
	for _, a := range apps {
		resp = append(resp, AppToResponse(a))
	}
	RespondList(w, resp, len(resp), 1, len(resp))
}

// CreateApp creates a new app record on a server.
func (s *Step5) CreateApp(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		Respond401(w, r)
		return
	}
	if role := s.requireAccess(userID, serverID); role == "" {
		Respond403(w, r, "You do not have access to this server")
		return
	}

	var req struct {
		ProjectName string `json:"project_name"`
	}
	if err := DecodeJSON(w, r, &req); err != nil {
		Respond400(w, r, err.Error())
		return
	}
	if req.ProjectName == "" {
		Respond400(w, r, "project_name is required")
		return
	}

	appID := uuid.New().String()
	if err := queries.InsertApp(s.DB, appID, serverID, req.ProjectName); err != nil {
		slog.Error("insert app", "error", err)
		Respond500(w, r)
		return
	}

	Respond201(w, map[string]string{"id": appID, "project_name": req.ProjectName})
}

// GetApp returns a single app by ID.
func (s *Step5) GetApp(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	appID := chi.URLParam(r, "appID")
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		Respond401(w, r)
		return
	}
	if role := s.requireAccess(userID, serverID); role == "" {
		Respond403(w, r, "You do not have access to this server")
		return
	}

	app, err := queries.GetAppByID(s.DB, appID)
	if err != nil {
		if err == sql.ErrNoRows {
			Respond404(w, r, "App")
			return
		}
		slog.Error("get app", "error", err)
		Respond500(w, r)
		return
	}
	if app.ServerID != serverID {
		Respond404(w, r, "App")
		return
	}

	RespondJSON(w, http.StatusOK, AppToResponse(*app))
}

// DeleteApp removes an app from a server.
func (s *Step5) DeleteApp(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	appID := chi.URLParam(r, "appID")
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		Respond401(w, r)
		return
	}
	role := s.requireAccess(userID, serverID)
	if role != "owner" && role != "admin" {
		Respond403(w, r, "Only admins can delete apps")
		return
	}

	app, err := queries.GetAppByID(s.DB, appID)
	if err != nil || app == nil || app.ServerID != serverID {
		Respond404(w, r, "App")
		return
	}

	if _, err := s.DB.Exec("DELETE FROM apps WHERE id = ?", appID); err != nil {
		slog.Error("delete app", "error", err)
		Respond500(w, r)
		return
	}

	RespondNoContent(w)
}

// ---------------------------------------------------------------------------
// Step 5C — Deployment Handlers
// ---------------------------------------------------------------------------

// DeployApp creates a deploy command and sends it to the agent.
func (s *Step5) DeployApp(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	appID := chi.URLParam(r, "appID")
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		Respond401(w, r)
		return
	}
	if role := s.requireAccess(userID, serverID); role == "" {
		Respond403(w, r, "You do not have access to this server")
		return
	}

	app, err := queries.GetAppByID(s.DB, appID)
	if err != nil || app == nil || app.ServerID != serverID {
		Respond404(w, r, "App")
		return
	}

	var req struct {
		Image           string `json:"image"`
		Port            int    `json:"port"`
		MemoryLimitMB   int    `json:"memory_limit_mb"`
		CPUQuotaPercent int    `json:"cpu_quota_percent"`
	}
	if err := DecodeJSON(w, r, &req); err != nil {
		Respond400(w, r, err.Error())
		return
	}
	if req.Image == "" {
		Respond400(w, r, "image is required")
		return
	}
	if req.Port <= 0 || req.Port > 65535 {
		Respond400(w, r, "port must be between 1 and 65535")
		return
	}

	depID := uuid.New().String()
	cmdID := uuid.New().String()

	// Create deployment record.
	if err := queries.InsertDeployment(s.DB, depID, serverID, app.ProjectName, req.Image, req.Port, ""); err != nil {
		slog.Error("insert deployment", "error", err)
		Respond500(w, r)
		return
	}

	// Enqueue command for the agent.
	payload := fmt.Sprintf(`{"type":"deploy","image":"%s","port":%d}`, req.Image, req.Port)
	if err := queries.EnqueuePendingCommand(s.DB, cmdID, serverID, "deploy", payload, app.ProjectName, ""); err != nil {
		slog.Error("enqueue deploy command", "error", err)
		Respond500(w, r)
		return
	}

	RespondJSON(w, http.StatusAccepted, map[string]string{
		"command_id":    cmdID,
		"deployment_id": depID,
		"message":       "Deploy started. Watch progress in the dashboard.",
	})
}

// RollbackApp rolls back to a previous deployment.
func (s *Step5) RollbackApp(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	appID := chi.URLParam(r, "appID")
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		Respond401(w, r)
		return
	}
	if role := s.requireAccess(userID, serverID); role == "" {
		Respond403(w, r, "You do not have access to this server")
		return
	}

	app, err := queries.GetAppByID(s.DB, appID)
	if err != nil || app == nil || app.ServerID != serverID {
		Respond404(w, r, "App")
		return
	}

	var req struct {
		Target       string `json:"target"`        // "previous" or "specific"
		DeploymentID string `json:"deployment_id"` // required if target is "specific"
	}
	if err := DecodeJSON(w, r, &req); err != nil {
		Respond400(w, r, err.Error())
		return
	}
	if req.Target == "" {
		req.Target = "previous"
	}

	var image string
	switch req.Target {
	case "previous":
		deps, err := queries.ListDeploymentsByServer(s.DB, serverID, 10)
		if err != nil || len(deps) == 0 {
			Respond400(w, r, "No previous deployments found")
			return
		}
		// Find first successful deployment for this app.
		for _, d := range deps {
			if d.AppName == app.ProjectName && d.Status == "success" {
				image = d.Image
				break
			}
		}
		if image == "" {
			Respond400(w, r, "No successful deployment found to rollback to")
			return
		}
	case "specific":
		if req.DeploymentID == "" {
			Respond400(w, r, "deployment_id is required for specific rollback")
			return
		}
		// Verify deployment belongs to this app.
		deps, err := queries.ListDeploymentsByServer(s.DB, serverID, 100)
		if err != nil {
			Respond500(w, r)
			return
		}
		for _, d := range deps {
			if d.ID == req.DeploymentID && d.AppName == app.ProjectName {
				image = d.Image
				break
			}
		}
		if image == "" {
			Respond404(w, r, "Deployment")
			return
		}
	default:
		Respond400(w, r, "target must be 'previous' or 'specific'")
		return
	}

	cmdID := uuid.New().String()
	payload := fmt.Sprintf(`{"type":"deploy","image":"%s","rollback":true}`, image)
	if err := queries.EnqueuePendingCommand(s.DB, cmdID, serverID, "deploy", payload, app.ProjectName, ""); err != nil {
		slog.Error("enqueue rollback command", "error", err)
		Respond500(w, r)
		return
	}

	RespondJSON(w, http.StatusAccepted, map[string]string{
		"command_id": cmdID,
		"message":    "Rollback started.",
	})
}

// ListDeployments returns deployment history for an app.
func (s *Step5) ListDeployments(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		Respond401(w, r)
		return
	}
	if role := s.requireAccess(userID, serverID); role == "" {
		Respond403(w, r, "You do not have access to this server")
		return
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}

	deps, err := queries.ListDeploymentsByServer(s.DB, serverID, perPage)
	if err != nil {
		slog.Error("list deployments", "error", err)
		Respond500(w, r)
		return
	}

	resp := make([]DeploymentResponse, 0, len(deps))
	for _, d := range deps {
		resp = append(resp, DeploymentToResponse(d))
	}
	RespondList(w, resp, len(resp), page, perPage)
}

// ---------------------------------------------------------------------------
// Step 5C — App Lifecycle Handlers
// ---------------------------------------------------------------------------

func (s *Step5) appLifecycleCommand(w http.ResponseWriter, r *http.Request, commandType string) {
	serverID := chi.URLParam(r, "serverID")
	appID := chi.URLParam(r, "appID")
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		Respond401(w, r)
		return
	}
	if role := s.requireAccess(userID, serverID); role == "" {
		Respond403(w, r, "You do not have access to this server")
		return
	}

	app, err := queries.GetAppByID(s.DB, appID)
	if err != nil || app == nil || app.ServerID != serverID {
		Respond404(w, r, "App")
		return
	}

	cmdID := uuid.New().String()
	payload := fmt.Sprintf(`{"type":"%s","project":"%s"}`, commandType, app.ProjectName)
	if err := queries.EnqueuePendingCommand(s.DB, cmdID, serverID, commandType, payload, app.ProjectName, ""); err != nil {
		slog.Error("enqueue lifecycle command", "error", err)
		Respond500(w, r)
		return
	}

	RespondJSON(w, http.StatusAccepted, map[string]string{"command_id": cmdID})
}

// StartApp sends a start command to the agent.
func (s *Step5) StartApp(w http.ResponseWriter, r *http.Request) {
	s.appLifecycleCommand(w, r, "start")
}

// StopApp sends a stop command to the agent.
func (s *Step5) StopApp(w http.ResponseWriter, r *http.Request) {
	s.appLifecycleCommand(w, r, "stop")
}

// RestartApp sends a restart command to the agent.
func (s *Step5) RestartApp(w http.ResponseWriter, r *http.Request) {
	s.appLifecycleCommand(w, r, "restart")
}

// GetAppLogs returns recent log lines for an app container.
func (s *Step5) GetAppLogs(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	appID := chi.URLParam(r, "appID")
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		Respond401(w, r)
		return
	}
	if role := s.requireAccess(userID, serverID); role == "" {
		Respond403(w, r, "You do not have access to this server")
		return
	}

	app, err := queries.GetAppByID(s.DB, appID)
	if err != nil || app == nil || app.ServerID != serverID {
		Respond404(w, r, "App")
		return
	}

	lines, _ := strconv.Atoi(r.URL.Query().Get("lines"))
	if lines <= 0 || lines > 1000 {
		lines = 200
	}

	// Return placeholder — real implementation requires hub round-trip to agent.
	RespondJSON(w, http.StatusOK, map[string]interface{}{
		"lines":      []interface{}{},
		"container":  app.ProjectName + "_app",
		"total_lines": lines,
	})
}

// ---------------------------------------------------------------------------
// Step 5D — Environment Variable Handlers
// ---------------------------------------------------------------------------

// ListEnvVars returns env var keys for an app (never values).
func (s *Step5) ListEnvVars(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	appID := chi.URLParam(r, "appID")
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		Respond401(w, r)
		return
	}
	if role := s.requireAccess(userID, serverID); role == "" {
		Respond403(w, r, "You do not have access to this server")
		return
	}

	keys, err := queries.ListEnvVarKeysByApp(s.DB, appID)
	if err != nil {
		slog.Error("list env vars", "error", err)
		Respond500(w, r)
		return
	}

	resp := make([]EnvVarKeyResponse, 0, len(keys))
	for _, k := range keys {
		resp = append(resp, EnvVarKeyToResponse(k))
	}

	RespondJSON(w, http.StatusOK, map[string]interface{}{
		"keys": resp,
	})
}

// SetEnvVar sets an environment variable for an app.
func (s *Step5) SetEnvVar(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	appID := chi.URLParam(r, "appID")
	key := chi.URLParam(r, "key")
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		Respond401(w, r)
		return
	}
	if role := s.requireAccess(userID, serverID); role == "" {
		Respond403(w, r, "You do not have access to this server")
		return
	}

	if key == "" {
		Respond400(w, r, "env var key is required")
		return
	}

	var req struct {
		Value        string `json:"value"`
		RestartAfter bool   `json:"restart_after"`
	}
	if err := DecodeJSON(w, r, &req); err != nil {
		Respond400(w, r, err.Error())
		return
	}

	app, err := queries.GetAppByID(s.DB, appID)
	if err != nil || app == nil || app.ServerID != serverID {
		Respond404(w, r, "App")
		return
	}

	// Enqueue set_env command.
	cmdID := uuid.New().String()
	payload := fmt.Sprintf(`{"type":"set_env","key":"%s","value":"%s","project":"%s"}`, key, req.Value, app.ProjectName)
	if err := queries.EnqueuePendingCommand(s.DB, cmdID, serverID, "set_env", payload, app.ProjectName, ""); err != nil {
		slog.Error("enqueue set_env command", "error", err)
		Respond500(w, r)
		return
	}

	// Upsert key record.
	envID := uuid.New().String()
	if err := queries.InsertEnvVarKey(s.DB, envID, appID, serverID, key, false); err != nil {
		slog.Error("insert env var key", "error", err)
	}

	if req.RestartAfter {
		restartCmdID := uuid.New().String()
		restartPayload := fmt.Sprintf(`{"type":"restart","project":"%s"}`, app.ProjectName)
		_ = queries.EnqueuePendingCommand(s.DB, restartCmdID, serverID, "restart", restartPayload, app.ProjectName, "")
	}

	RespondJSON(w, http.StatusAccepted, map[string]string{
		"command_id": cmdID,
		"message":    "Variable updated",
	})
}

// DeleteEnvVar removes an environment variable from an app.
func (s *Step5) DeleteEnvVar(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	appID := chi.URLParam(r, "appID")
	key := chi.URLParam(r, "key")
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		Respond401(w, r)
		return
	}
	if role := s.requireAccess(userID, serverID); role == "" {
		Respond403(w, r, "You do not have access to this server")
		return
	}

	app, err := queries.GetAppByID(s.DB, appID)
	if err != nil || app == nil || app.ServerID != serverID {
		Respond404(w, r, "App")
		return
	}

	cmdID := uuid.New().String()
	payload := fmt.Sprintf(`{"type":"delete_env","key":"%s","project":"%s"}`, key, app.ProjectName)
	if err := queries.EnqueuePendingCommand(s.DB, cmdID, serverID, "delete_env", payload, app.ProjectName, ""); err != nil {
		slog.Error("enqueue delete_env command", "error", err)
		Respond500(w, r)
		return
	}

	RespondJSON(w, http.StatusAccepted, map[string]string{"command_id": cmdID})
}

// ---------------------------------------------------------------------------
// Step 5E — Metrics Handlers
// ---------------------------------------------------------------------------

// GetServerMetrics returns current metrics for a server.
func (s *Step5) GetServerMetrics(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		Respond401(w, r)
		return
	}
	if role := s.requireAccess(userID, serverID); role == "" {
		Respond403(w, r, "You do not have access to this server")
		return
	}

	type serverMetrics struct {
		CPUPercent  float64 `json:"cpu_percent"`
		RAMUsedMB   int64   `json:"ram_used_mb"`
		RAMTotalMB  int64   `json:"ram_total_mb"`
		RAMPercent  float64 `json:"ram_percent"`
		DiskUsedGB  float64 `json:"disk_used_gb"`
		DiskTotalGB float64 `json:"disk_total_gb"`
		DiskPercent float64 `json:"disk_percent"`
		Load1Min    float64 `json:"load_1min"`
		RecordedAt  string  `json:"recorded_at"`
	}
	type containerMetrics struct {
		Project      string  `json:"project"`
		Role         string  `json:"role"`
		Status       string  `json:"status"`
		CPUPercent   float64 `json:"cpu_percent"`
		RAMUsedMB    int64   `json:"ram_used_mb"`
		RAMLimitMB   int64   `json:"ram_limit_mb"`
		RestartCount int     `json:"restart_count"`
	}

	m, err := queries.GetServerMetricsLatest(s.DB, serverID)
	if err != nil {
		Respond404(w, r, "Metrics")
		return
	}

	sm := serverMetrics{RecordedAt: m.RecordedAt}
	if m.CPUPercent != nil {
		sm.CPUPercent = *m.CPUPercent
	}
	if m.RAMUsedMB != nil {
		sm.RAMUsedMB = *m.RAMUsedMB
	}
	if m.RAMTotalMB != nil {
		sm.RAMTotalMB = *m.RAMTotalMB
	}
	if m.DiskUsedGB != nil {
		sm.DiskUsedGB = *m.DiskUsedGB
	}
	if m.DiskTotalGB != nil {
		sm.DiskTotalGB = *m.DiskTotalGB
	}
	if m.Load1Min != nil {
		sm.Load1Min = *m.Load1Min
	}
	if sm.RAMTotalMB > 0 {
		sm.RAMPercent = float64(sm.RAMUsedMB) / float64(sm.RAMTotalMB) * 100
	}
	if sm.DiskTotalGB > 0 {
		sm.DiskPercent = sm.DiskUsedGB / sm.DiskTotalGB * 100
	}

	containers, _ := queries.GetServerContainers(s.DB, serverID)
	resp := make([]containerMetrics, 0, len(containers))
	for _, c := range containers {
		resp = append(resp, containerMetrics{
			Project:      c.Project,
			Role:         c.Role,
			Status:       c.Status,
			CPUPercent:   c.CPUPercent,
			RAMUsedMB:    c.RAMUsedMB,
			RAMLimitMB:   c.RAMLimitMB,
			RestartCount: c.RestartCount,
		})
	}

	RespondJSON(w, http.StatusOK, map[string]interface{}{
		"server":     sm,
		"containers": resp,
	})
}

// GetServerMetricsHistory returns metrics history for charting.
func (s *Step5) GetServerMetricsHistory(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		Respond401(w, r)
		return
	}
	if role := s.requireAccess(userID, serverID); role == "" {
		Respond403(w, r, "You do not have access to this server")
		return
	}

	period := r.URL.Query().Get("period")
	if period == "" {
		period = "24h"
	}
	metric := r.URL.Query().Get("metric")
	if metric == "" {
		metric = "cpu"
	}

	var cutoff string
	var granularity string
	switch period {
	case "1h":
		cutoff = time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339)
		granularity = "raw"
	case "24h":
		cutoff = time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)
		granularity = "hourly"
	case "7d":
		cutoff = time.Now().Add(-7 * 24 * time.Hour).UTC().Format(time.RFC3339)
		granularity = "hourly"
	case "30d":
		cutoff = time.Now().Add(-30 * 24 * time.Hour).UTC().Format(time.RFC3339)
		granularity = "daily"
	default:
		cutoff = time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)
		granularity = "hourly"
	}

	rows, err := s.DB.Query(
		`SELECT recorded_at, cpu_percent, ram_used_mb, disk_used_gb, load_1min
		 FROM metrics_history
		 WHERE server_id = ? AND recorded_at >= ?
		 ORDER BY recorded_at ASC`,
		serverID, cutoff,
	)
	if err != nil {
		slog.Error("query metrics history", "error", err)
		Respond500(w, r)
		return
	}
	defer rows.Close()

	type dataPoint struct {
		Timestamp string  `json:"timestamp"`
		Value     float64 `json:"value"`
	}
	var data []dataPoint
	for rows.Next() {
		var ts string
		var cpu sql.NullFloat64
		var ram sql.NullInt64
		var disk sql.NullFloat64
		var load sql.NullFloat64
		if err := rows.Scan(&ts, &cpu, &ram, &disk, &load); err != nil {
			continue
		}
		var val float64
		switch metric {
		case "cpu":
			if cpu.Valid {
				val = cpu.Float64
			}
		case "ram":
			if ram.Valid {
				val = float64(ram.Int64)
			}
		case "disk":
			if disk.Valid {
				val = disk.Float64
			}
		default:
			if load.Valid {
				val = load.Float64
			}
		}
		data = append(data, dataPoint{Timestamp: ts, Value: val})
	}

	RespondJSON(w, http.StatusOK, map[string]interface{}{
		"metric":      metric,
		"period":      period,
		"granularity": granularity,
		"data":        data,
	})
}

// ---------------------------------------------------------------------------
// Step 5F — Backup Plan Handlers
// ---------------------------------------------------------------------------

// ListBackupsPlan returns paginated backup history for a server.
func (s *Step5) ListBackupsPlan(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		Respond401(w, r)
		return
	}
	if role := s.requireAccess(userID, serverID); role == "" {
		Respond403(w, r, "You do not have access to this server")
		return
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}

	jobs, err := queries.GetBackupJobsByServer(s.DB, serverID, perPage)
	if err != nil {
		slog.Error("list backups", "error", err)
		Respond500(w, r)
		return
	}

	type backupItem struct {
		ID              string  `json:"id"`
		Status          string  `json:"status"`
		SizeNewBytes    int64   `json:"size_new_bytes"`
		SizeTotalBytes  int64   `json:"size_total_bytes"`
		StartedAt       string  `json:"started_at"`
		CompletedAt     string  `json:"completed_at,omitempty"`
		DurationSeconds int64   `json:"duration_seconds"`
		Verified        bool    `json:"verified"`
	}

	data := make([]backupItem, 0, len(jobs))
	for _, j := range jobs {
		item := backupItem{
			ID:       j.ID,
			Status:   j.Status,
			StartedAt: j.CreatedAt,
		}
		if j.SizeNewBytes.Valid {
			item.SizeNewBytes = j.SizeNewBytes.Int64
		}
		if j.SizeTotalBytes.Valid {
			item.SizeTotalBytes = j.SizeTotalBytes.Int64
		}
		if j.CompletedAt.Valid {
			item.CompletedAt = j.CompletedAt.String
		}
		if j.DurationSeconds.Valid {
			item.DurationSeconds = j.DurationSeconds.Int64
		}
		if j.VerificationStatus.Valid && j.VerificationStatus.String == "verified" {
			item.Verified = true
		}
		data = append(data, item)
	}

	RespondList(w, data, len(data), page, perPage)
}

// TriggerBackupPlan sends a backup command to the agent.
func (s *Step5) TriggerBackupPlan(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		Respond401(w, r)
		return
	}
	if role := s.requireAccess(userID, serverID); role == "" {
		Respond403(w, r, "You do not have access to this server")
		return
	}

	cmdID := uuid.New().String()
	if err := queries.EnqueuePendingCommand(s.DB, cmdID, serverID, "backup_trigger", `{"type":"backup_trigger"}`, "", ""); err != nil {
		slog.Error("enqueue backup command", "error", err)
		Respond500(w, r)
		return
	}

	RespondJSON(w, http.StatusAccepted, map[string]string{
		"command_id": cmdID,
		"message":    "Backup started",
	})
}

// RestoreBackupPlan sends a restore command to the agent.
func (s *Step5) RestoreBackupPlan(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	backupID := chi.URLParam(r, "backupID")
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		Respond401(w, r)
		return
	}

	// Restore is owner-only (destructive).
	role := s.requireAccess(userID, serverID)
	if role != "owner" && role != "admin" {
		Respond403(w, r, "Only server owners can restore backups")
		return
	}

	var req struct {
		ProjectName string `json:"project_name"`
		Confirmed   bool   `json:"confirmed"`
	}
	if err := DecodeJSON(w, r, &req); err != nil {
		Respond400(w, r, err.Error())
		return
	}
	if !req.Confirmed {
		Respond400(w, r, "confirmed must be true to restore a backup")
		return
	}
	if req.ProjectName == "" {
		Respond400(w, r, "project_name is required")
		return
	}

	cmdID := uuid.New().String()
	payload := fmt.Sprintf(`{"type":"backup_restore","snapshot_id":"%s","project":"%s"}`, backupID, req.ProjectName)
	if err := queries.EnqueuePendingCommand(s.DB, cmdID, serverID, "backup_restore", payload, req.ProjectName, ""); err != nil {
		slog.Error("enqueue restore command", "error", err)
		Respond500(w, r)
		return
	}

	RespondJSON(w, http.StatusAccepted, map[string]string{
		"command_id": cmdID,
		"message":    "Restore started",
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func generateRandomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Unused but available for future use.
var _ = strings.TrimSpace
