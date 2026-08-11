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

	// Verify access
	var ownerID string
	err := inf.DB.QueryRow("SELECT user_id FROM servers WHERE id = ?", serverID).Scan(&ownerID)
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

	var req DeployInferenceRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return
	}

	if req.TemplateID == "" {
		Respond400(w, r, "template_id is required")
		return
	}

	// Create command
	cmdID := "cmd-" + uuid.New().String()[:12]
	payload, _ := json.Marshal(map[string]interface{}{
		"template_id": req.TemplateID,
		"server_id":   serverID,
		"domain":      req.Domain,
		"api_key":     req.APIKey,
	})

	// Enqueue command
	if err := queries.EnqueuePendingCommand(inf.DB, cmdID, serverID, "deploy_inference", string(payload), "infer", ""); err != nil {
		slog.Error("enqueue deploy_inference", "error", err)
		Respond500(w, r)
		return
	}

	// Try to send immediately if agent is connected
	cmd := map[string]interface{}{
		"id":      cmdID,
		"type":    "deploy_inference",
		"payload": json.RawMessage(payload),
	}
	_ = ws.QueueOrSendCommand(inf.Hub, inf.DB, serverID, cmd)

	RespondJSON(w, http.StatusAccepted, map[string]interface{}{
		"command_id": cmdID,
		"status":     "queued",
	})
}

// ListTemplates returns the available inference templates.
// This is a static list from the agent's embedded templates.
func (inf *Infer) ListTemplates(w http.ResponseWriter, r *http.Request) {
	// Templates are static; return the known set
	templates := []map[string]interface{}{
		{
			"id":          "llm-chat-kleidiai",
			"name":        "LLM Chat",
			"description": "Deploy a conversational AI endpoint powered by Llama 3.1 with OpenAI-compatible API",
			"category":    "Language Model",
			"model": map[string]interface{}{
				"family":         "Llama 3.1",
				"size":           "8B",
				"variant":        "Instruct",
				"default_quant":  "Q4_K_M",
				"fallback_quants": []string{"Q3_K_M", "Q2_K"},
			},
			"runtime": map[string]interface{}{
				"engine":        "llama.cpp",
				"internal_port": 8080,
				"api_format":    "openai-compatible",
				"api_path":      "/v1/chat/completions",
			},
			"resources": map[string]interface{}{
				"min_ram_gb":   6.0,
				"min_disk_gb":  5.0,
				"preferred_arch": "arm64",
			},
		},
	}

	RespondJSON(w, http.StatusOK, templates)
}

// GetInferenceStatus returns the current state of an inference deployment.
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

	// Verify access
	var ownerID string
	err := inf.DB.QueryRow("SELECT user_id FROM servers WHERE id = ?", serverID).Scan(&ownerID)
	if err == sql.ErrNoRows {
		Respond404(w, r, "server not found")
		return
	}
	if err != nil {
		Respond500(w, r)
		return
	}
	if ownerID != userID {
		Respond403(w, r, "you do not have access to this server")
		return
	}

	// Query latest inference command status
	var status, output string
	var createdAt time.Time
	err = inf.DB.QueryRow(`
		SELECT status, output, created_at FROM pending_commands
		WHERE server_id = ? AND type = 'deploy_inference'
		ORDER BY created_at DESC LIMIT 1
	`, serverID).Scan(&status, &output, &createdAt)

	if err == sql.ErrNoRows {
		RespondJSON(w, http.StatusOK, map[string]interface{}{
			"deployed": false,
		})
		return
	}
	if err != nil {
		Respond500(w, r)
		return
	}

	result := map[string]interface{}{
		"deployed":   status == "completed",
		"status":     status,
		"created_at": createdAt,
	}

	if status == "completed" && output != "" {
		var out map[string]interface{}
		if json.Unmarshal([]byte(output), &out) == nil {
			result["details"] = out
		}
	}

	RespondJSON(w, http.StatusOK, result)
}
