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

// Infer handles Anchor Infer endpoints.
type Infer struct {
	DB  *sql.DB
	Hub *ws.Hub
}

// DeployInferenceRequest is the dashboard request to deploy an inference server.
type DeployInferenceRequest struct {
	TemplateID string `json:"template_id"`
	Domain     string `json:"domain,omitempty"`
	APIKey     string `json:"api_key,omitempty"`
}

// DeployInference dispatches a deploy_inference command to the agent.
func (inf *Infer) DeployInference(w http.ResponseWriter, r *http.Request) {
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

	if !inf.userOwnsServer(w, r, userID, serverID) {
		return
	}

	var req DeployInferenceRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return
	}
	if req.TemplateID == "" {
		Respond400(w, r, "template_id is required")
		return
	}

	cmdID := "cmd-" + uuid.New().String()[:12]
	payload, _ := json.Marshal(map[string]interface{}{
		"template_id": req.TemplateID,
		"server_id":   serverID,
		"domain":      req.Domain,
		"api_key":     req.APIKey,
	})
	now := time.Now().UTC().Format(time.RFC3339)
	projectKey := "infer:" + req.TemplateID

	if dup, err := queries.HasInProgressCommand(inf.DB, serverID, "deploy_inference", projectKey); err == nil && dup {
		Respond400(w, r, "An inference deploy for this template is already in progress")
		return
	}

	if err := queries.InsertCommand(inf.DB, cmdID, serverID, "deploy_inference", string(payload), projectKey, "queued", userID, now); err != nil {
		slog.Error("insert deploy_inference command", "error", err)
		Respond500(w, r)
		return
	}

	cmd := map[string]interface{}{
		"id":      cmdID,
		"type":    "deploy_inference",
		"payload": json.RawMessage(payload),
	}
	if err := ws.QueueOrSendCommand(inf.Hub, inf.DB, serverID, cmd); err != nil {
		slog.Warn("queue/send deploy_inference", "error", err, "command_id", cmdID)
	}

	// Prefer issuer-based result routing when a browser is online.
	if connID := inf.Hub.FindBrowserByUser(userID); connID != "" {
		inf.Hub.TrackPendingCommand(cmdID, connID, serverID)
	}

	RespondJSON(w, http.StatusAccepted, map[string]interface{}{
		"command_id": cmdID,
		"status":     "queued",
	})
}

// ListTemplates returns the available inference templates.
func (inf *Infer) ListTemplates(w http.ResponseWriter, r *http.Request) {
	templates := []map[string]interface{}{
		{
			"id":          "llm-chat-kleidiai",
			"name":        "LLM Chat",
			"description": "Deploy a conversational AI endpoint powered by Llama 3.1 with OpenAI-compatible API",
			"category":    "Language Model",
			"model": map[string]interface{}{
				"family":          "Llama 3.1",
				"size":            "8B",
				"variant":         "Instruct",
				"default_quant":   "Q4_K_M",
				"fallback_quants": []string{"Q3_K_M", "Q2_K"},
			},
			"runtime": map[string]interface{}{
				"engine":        "llama.cpp",
				"internal_port": 8080,
				"api_format":    "openai-compatible",
				"api_path":      "/v1/chat/completions",
			},
			"resources": map[string]interface{}{
				"min_ram_gb":     6.0,
				"min_disk_gb":    5.0,
				"preferred_arch": "arm64",
			},
		},
	}
	RespondJSON(w, http.StatusOK, templates)
}

// GetInferenceStatus returns the latest deploy_inference command state for a server.
func (inf *Infer) GetInferenceStatus(w http.ResponseWriter, r *http.Request) {
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
	if !inf.userOwnsServer(w, r, userID, serverID) {
		return
	}

	var (
		cmdID, status, result string
		createdAt             string
	)
	err := inf.DB.QueryRow(`
		SELECT id, status, COALESCE(result,''), created_at
		FROM commands
		WHERE server_id = ? AND command_type = 'deploy_inference'
		ORDER BY created_at DESC LIMIT 1
	`, serverID).Scan(&cmdID, &status, &result, &createdAt)

	if err == sql.ErrNoRows {
		RespondJSON(w, http.StatusOK, map[string]interface{}{
			"deployed": false,
		})
		return
	}
	if err != nil {
		slog.Error("query infer status", "error", err)
		Respond500(w, r)
		return
	}

	out := map[string]interface{}{
		"deployed":   status == "success",
		"status":     status,
		"command_id": cmdID,
		"created_at": createdAt,
	}

	if result != "" {
		details := parseInferCommandResult(result)
		if details != nil {
			out["details"] = details
			if status == "success" {
				queries.PersistInferBenchmarksFromDetails(inf.DB, serverID, details)
			}
		}
	}

	RespondJSON(w, http.StatusOK, out)
}

// GetBenchmarks returns the latest generic/optimized comparison for a server.
func (inf *Infer) GetBenchmarks(w http.ResponseWriter, r *http.Request) {
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
	if !inf.userOwnsServer(w, r, userID, serverID) {
		return
	}

	optimized, generic, err := queries.GetBenchmarkComparison(inf.DB, serverID)
	if err != nil {
		slog.Error("get benchmark comparison", "error", err)
		Respond500(w, r)
		return
	}
	if optimized == nil && generic == nil {
		RespondJSON(w, http.StatusOK, map[string]interface{}{
			"available": false,
		})
		return
	}

	resp := map[string]interface{}{
		"available": true,
		"optimized": optimized,
		"generic":   generic,
	}
	if optimized != nil && generic != nil && generic.MedianTokensPerSecond > 0 {
		tpsImp := ((optimized.MedianTokensPerSecond - generic.MedianTokensPerSecond) / generic.MedianTokensPerSecond) * 100
		ttftImp := 0.0
		if generic.MedianTTFTMs > 0 {
			ttftImp = (float64(generic.MedianTTFTMs-optimized.MedianTTFTMs) / float64(generic.MedianTTFTMs)) * 100
		}
		resp["tokens_per_second_improvement_pct"] = tpsImp
		resp["ttft_improvement_pct"] = ttftImp
		resp["memory_difference_bytes"] = int64(optimized.PeakMemoryBytes) - int64(generic.PeakMemoryBytes)
	}
	RespondJSON(w, http.StatusOK, resp)
}

func (inf *Infer) userOwnsServer(w http.ResponseWriter, r *http.Request, userID, serverID string) bool {
	var ownerID string
	err := inf.DB.QueryRow("SELECT user_id FROM servers WHERE id = ?", serverID).Scan(&ownerID)
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

// parseInferCommandResult unwraps agent result envelopes into a details map.
func parseInferCommandResult(raw string) map[string]interface{} {
	var outer map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &outer); err != nil {
		return nil
	}
	if outStr, ok := outer["output"].(string); ok && outStr != "" {
		var inner map[string]interface{}
		if json.Unmarshal([]byte(outStr), &inner) == nil {
			return inner
		}
	}
	if details, ok := outer["details"].(map[string]interface{}); ok {
		return details
	}
	if _, hasEndpoint := outer["endpoint_url"]; hasEndpoint {
		return outer
	}
	if _, hasTpl := outer["template_id"]; hasTpl {
		return outer
	}
	return nil
}
