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

	// Record in the commands audit table (drives GetInferenceStatus + result
	// routing). QueueOrSendCommand below handles both the live-send path and
	// enqueueing for offline delivery — enqueueing here too would create a
	// duplicate pending_commands row when the agent is disconnected.
	if err := queries.InsertCommand(inf.DB, cmdID, serverID, "deploy_inference", string(payload), "infer", "queued", userID, time.Now().UTC().Format(time.RFC3339)); err != nil {
		slog.Error("insert deploy_inference command", "error", err)
		Respond500(w, r)
		return
	}

	// Send immediately if the agent is connected, otherwise queue for delivery
	// on reconnect.
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

// RunBenchmarkRequest is the dashboard request to re-run the benchmark
// pipeline against an existing inference deployment.
type RunBenchmarkRequest struct {
	TemplateID string `json:"template_id"`
}

// RunBenchmark dispatches a run_benchmark command to the agent. The agent
// reuses the deployed model volume, so results are directly comparable.
func (inf *Infer) RunBenchmark(w http.ResponseWriter, r *http.Request) {
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

	var req RunBenchmarkRequest
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
	})

	if err := queries.InsertCommand(inf.DB, cmdID, serverID, "run_benchmark", string(payload), "infer", "queued", userID, time.Now().UTC().Format(time.RFC3339)); err != nil {
		slog.Error("insert run_benchmark command", "error", err)
		Respond500(w, r)
		return
	}

	cmd := map[string]interface{}{
		"id":      cmdID,
		"type":    "run_benchmark",
		"payload": json.RawMessage(payload),
	}
	_ = ws.QueueOrSendCommand(inf.Hub, inf.DB, serverID, cmd)

	RespondJSON(w, http.StatusAccepted, map[string]interface{}{
		"command_id": cmdID,
		"status":     "queued",
	})
}

// GetBenchmarkResults returns the latest stored benchmark comparison for a
// server, so the dashboard can restore pre-computed results on page load.
func (inf *Infer) GetBenchmarkResults(w http.ResponseWriter, r *http.Request) {
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

	optimized, generic, err := queries.GetBenchmarkComparison(inf.DB, serverID)
	if err != nil {
		slog.Error("get benchmark comparison", "error", err)
		Respond500(w, r)
		return
	}
	if optimized == nil || generic == nil {
		RespondJSON(w, http.StatusOK, map[string]interface{}{})
		return
	}

	RespondJSON(w, http.StatusOK, map[string]interface{}{
		"tokens_per_second_improvement_pct": improvementPct(generic.MedianTokensPerSecond, optimized.MedianTokensPerSecond),
		"ttft_improvement_pct":              ttftImprovementPct(generic.MedianTTFTMs, optimized.MedianTTFTMs),
		"memory_difference_bytes":           int64(optimized.PeakMemoryBytes) - int64(generic.PeakMemoryBytes),
		"optimized":                         benchmarkResultResponse(optimized),
		"generic":                           benchmarkResultResponse(generic),
		"benchmarked_at":                    optimized.CreatedAt,
	})
}

// improvementPct returns the percentage change from baseline to value
// (positive = improvement, negative = regression).
func improvementPct(baseline, value float64) float64 {
	if baseline == 0 {
		return 0
	}
	return ((value - baseline) / baseline) * 100
}

// ttftImprovementPct mirrors the agent's latency formula: lower TTFT is
// better, so improvement is measured against the generic baseline.
// ((generic - optimized) / generic) × 100 → positive = faster.
func ttftImprovementPct(genericMS, optimizedMS int64) float64 {
	if genericMS == 0 {
		return 0
	}
	return (float64(genericMS) - float64(optimizedMS)) / float64(genericMS) * 100
}

// benchmarkResultResponse maps a stored benchmark row to the same shape the
// agent returns in the command output, so the dashboard handles both paths
// identically.
func benchmarkResultResponse(b *queries.BenchmarkResult) map[string]interface{} {
	return map[string]interface{}{
		"build_label":              b.BuildLabel,
		"image_tag":                b.ImageTag,
		"median_tokens_per_second": b.MedianTokensPerSecond,
		"median_ttft_ms":           b.MedianTTFTMs,
		"peak_memory_bytes":        b.PeakMemoryBytes,
		"total_duration_ms":        b.TotalDurationMs,
		"tokens_per_second_range":  [2]float64{b.TokensSecRangeMin, b.TokensSecRangeMax},
		"ttft_range_ms":            [2]int64{b.TTFTRangeMinMs, b.TTFTRangeMaxMs},
		"variance_detected":        b.VarianceDetected,
		"actual_runs":              b.ActualRuns,
	}
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

	// Query the latest deploy_inference command from the commands audit table.
	// The agent's result payload is stored in `result` as JSON with an `output`
	// field holding the deploy details JSON string.
	// Note: created_at is stored as TEXT (RFC3339) by the agent, so we scan it
	// as a string and parse — the SQLite driver can't scan TEXT into time.Time.
	var status, resultJSON, createdAtRaw string
	err = inf.DB.QueryRow(`
		SELECT status, result, created_at FROM commands
		WHERE server_id = ? AND command_type = 'deploy_inference'
		ORDER BY created_at DESC LIMIT 1
	`, serverID).Scan(&status, &resultJSON, &createdAtRaw)

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

	result := map[string]interface{}{
		"deployed":   status == "success",
		"status":     status,
		"created_at": createdAtRaw,
	}

	if status == "success" && resultJSON != "" {
		// resultJSON is the agent payload {command_id, status, output, error};
		// `output` is the deploy details JSON string.
		var payload struct {
			Output string `json:"output"`
		}
		if json.Unmarshal([]byte(resultJSON), &payload) == nil && payload.Output != "" {
			var out map[string]interface{}
			if json.Unmarshal([]byte(payload.Output), &out) == nil {
				result["details"] = out
				// The API key lives in the deploy result output. Returning it on
				// status lets the dashboard restore a pre-run deployment with a
				// fully working Section 3 (copy/reveal key + live test prompt)
				// on page load, which the demo relies on.
				if key, ok := out["api_key"].(string); ok && key != "" {
					result["api_key"] = key
				}
			}
		}
	}

	RespondJSON(w, http.StatusOK, result)
}
