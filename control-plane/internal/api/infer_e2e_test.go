package api

import (
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yourname/yourplatform/control-plane/internal/auth"
	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
)

// buildInferDeployOutput returns the deploy_inference command output JSON the
// agent produces (deploy details + benchmark_comparison with array ranges),
// as a marshaled string — matching the agent's resultMap shape exactly.
func buildInferDeployOutput(t *testing.T, templateID string) string {
	t.Helper()
	out := map[string]interface{}{
		"template_id":  templateID,
		"container_id": "ctn-infer-12345678",
		"image_tag":    "ghcr.io/yourplatform/infer:arm64-sve2-i8mm",
		"quantization": "Q4_K_M",
		"model_file":   "Meta-Llama-3.1-8B-Instruct-Q4_K_M.gguf",
		"internal_port": 8080,
		"domain":        "infer-" + templateID + ".srv-1.anchor.app",
		"endpoint_url":  "https://infer-" + templateID + ".srv-1.anchor.app",
		"api_key":       "sk-test-0123456789abcdef0123456789abcdef",
		"api_key_hash":  "deadbeef",
		"api_path":      "/v1/chat/completions",
		"optimization":  "Maximum (SVE2 + I8MM)",
		"memory_limit_mb": 8192,
		"model_size_gb": 4.7,
		"test_passed":   true,
		"benchmark_comparison": map[string]interface{}{
			"tokens_per_second_improvement_pct": 35.0,
			"ttft_improvement_pct":              33.333333333333336,
			"memory_difference_bytes":           400000000,
			"optimized": map[string]interface{}{
				"image_tag":                 "ghcr.io/yourplatform/infer:arm64-sve2-i8mm",
				"median_tokens_per_second":  13.5,
				"median_ttft_ms":            int64(200),
				"peak_memory_bytes":         uint64(6200000000),
				"total_duration_ms":         int64(45000),
				"tokens_per_second_range":   [2]float64{12.1, 14.9},
				"ttft_range_ms":             [2]int64{180, 240},
				"variance_detected":         true,
				"actual_runs":               5,
			},
			"generic": map[string]interface{}{
				"image_tag":                 "ghcr.io/yourplatform/infer:arm64",
				"median_tokens_per_second":  10.0,
				"median_ttft_ms":            int64(300),
				"peak_memory_bytes":         uint64(5800000000),
				"total_duration_ms":         int64(60000),
				"tokens_per_second_range":   [2]float64{9.2, 11.1},
				"ttft_range_ms":             [2]int64{260, 340},
				"variance_detected":         false,
				"actual_runs":               5,
			},
		},
	}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal output: %v", err)
	}
	return string(b)
}

// simulateAgentResult replicates exactly what the WS layer does when the
// agent's result message arrives (routeCommandResult + persistBenchmarkIfPresent):
// update the command row and persist benchmark rows from the output.
func (e *e2eEnv) simulateAgentResult(t *testing.T, serverID, cmdID, output string) {
	t.Helper()
	payload, err := json.Marshal(map[string]interface{}{
		"command_id": cmdID,
		"status":     "success",
		"output":     output,
	})
	if err != nil {
		t.Fatalf("marshal result payload: %v", err)
	}
	if err := queries.UpdateCommandStatus(e.db, cmdID, "success", string(payload)); err != nil {
		t.Fatalf("update command status: %v", err)
	}

	// Persist benchmark rows exactly like persistBenchmarkIfPresent.
	type benchSide struct {
		ImageTag             string    `json:"image_tag"`
		MedianTokensPerSec   float64   `json:"median_tokens_per_second"`
		MedianTTFTMs         int64     `json:"median_ttft_ms"`
		PeakMemoryBytes      uint64    `json:"peak_memory_bytes"`
		TotalDurationMs      int64     `json:"total_duration_ms"`
		TokensPerSecondRange [2]float64 `json:"tokens_per_second_range"`
		TTFTRangeMs          [2]int64   `json:"ttft_range_ms"`
		VarianceDetected     bool      `json:"variance_detected"`
		ActualRuns           int       `json:"actual_runs"`
	}
	var out struct {
		TemplateID   string `json:"template_id"`
		Quantization string `json:"quantization"`
		ArmFeatures  string `json:"arm_features"`
		Comparison   struct {
			Optimized benchSide `json:"optimized"`
			Generic   benchSide `json:"generic"`
		} `json:"benchmark_comparison"`
	}
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		t.Fatalf("parse output: %v", err)
	}
	insert := func(buildLabel string, m benchSide) {
		row := &queries.BenchmarkResult{
			ServerID:              serverID,
			TemplateID:            out.TemplateID,
			BuildLabel:            buildLabel,
			ImageTag:              m.ImageTag,
			Quantization:          out.Quantization,
			ArmFeatures:           out.ArmFeatures,
			MedianTokensPerSecond: m.MedianTokensPerSec,
			MedianTTFTMs:          m.MedianTTFTMs,
			PeakMemoryBytes:       m.PeakMemoryBytes,
			TotalDurationMs:       m.TotalDurationMs,
			PromptResults:         json.RawMessage("[]"),
			TokensSecRangeMin:     m.TokensPerSecondRange[0],
			TokensSecRangeMax:     m.TokensPerSecondRange[1],
			TTFTRangeMinMs:        m.TTFTRangeMs[0],
			TTFTRangeMaxMs:        m.TTFTRangeMs[1],
			VarianceDetected:      m.VarianceDetected,
			ActualRuns:            m.ActualRuns,
		}
		if err := queries.InsertBenchmarkResult(e.db, row); err != nil {
			t.Fatalf("insert %s benchmark row: %v", buildLabel, err)
		}
	}
	insert("optimized", out.Comparison.Optimized)
	insert("generic", out.Comparison.Generic)
}

// TestE2E_InferFullFlow drives the whole Anchor Infer path the demo relies on:
// templates → deploy dispatch → agent result → status restore (with api_key)
// → benchmark comparison.
func TestE2E_InferFullFlow(t *testing.T) {
	e := setupE2E(t)
	e.seedUser(t, "usr-infer", "infer@test.com", "InferUser", func() string {
		h, _ := auth.HashPassword("pass1234!")
		return h
	}())
	token := e.mintToken(t, "usr-infer", "sess-infer", "infer@test.com", "InferUser", time.Hour)

	// Create the server
	w := e.doWithJWT(http.MethodPost, "/api/v1/servers", token, map[string]string{"name": "infer-server"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create server = %d: %s", w.Code, w.Body.String())
	}
	var srv struct{ ID string }
	decodeJSON(t, w, &srv)

	// 1. Templates — the dashboard's model picker source
	w = e.doWithJWT(http.MethodGet, "/api/v1/infer/templates", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("templates = %d, want 200: %s", w.Code, w.Body.String())
	}
	var templates []map[string]interface{}
	decodeJSON(t, w, &templates)
	found := false
	for _, tmpl := range templates {
		if id, _ := tmpl["id"].(string); id == "llm-chat-kleidiai" {
			found = true
		}
	}
	if !found {
		t.Fatal("templates missing llm-chat-kleidiai (agent registry + control plane must agree)")
	}

	// 2. Deploy → 202 + command_id
	w = e.doWithJWT(http.MethodPost, "/api/v1/servers/"+srv.ID+"/infer/deploy", token, map[string]string{
		"template_id": "llm-chat-kleidiai",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("deploy = %d, want 202: %s", w.Code, w.Body.String())
	}
	var deployResp struct {
		CommandID string `json:"command_id"`
		Status    string `json:"status"`
	}
	decodeJSON(t, w, &deployResp)
	if deployResp.CommandID == "" {
		t.Fatal("deploy command_id is empty")
	}

	// 3. Agent completes: status restore must include the API key so the
	//    dashboard's Section 3 (test prompt) works on page load.
	output := buildInferDeployOutput(t, "llm-chat-kleidiai")
	e.simulateAgentResult(t, srv.ID, deployResp.CommandID, output)

	w = e.doWithJWT(http.MethodGet, "/api/v1/servers/"+srv.ID+"/infer/status", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var statusResp struct {
		Deployed bool                   `json:"deployed"`
		Status   string                 `json:"status"`
		APIKey   string                 `json:"api_key"`
		Details  map[string]interface{} `json:"details"`
	}
	decodeJSON(t, w, &statusResp)
	if !statusResp.Deployed {
		t.Fatal("status.deployed = false, want true after successful deploy")
	}
	if statusResp.APIKey == "" {
		t.Error("status.api_key is empty — Section 3 test prompt will not work on restore")
	}
	if statusResp.APIKey != "sk-test-0123456789abcdef0123456789abcdef" {
		t.Errorf("status.api_key = %q, want the agent's key", statusResp.APIKey)
	}
	if ep, _ := statusResp.Details["endpoint_url"].(string); ep != "https://infer-llm-chat-kleidiai.srv-1.anchor.app" {
		t.Errorf("details.endpoint_url = %q, want https://infer-llm-chat-kleidiai.srv-1.anchor.app", ep)
	}

	// 4. Benchmark comparison — restored pre-run results drive the card
	w = e.doWithJWT(http.MethodGet, "/api/v1/servers/"+srv.ID+"/infer/benchmark", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("benchmark = %d, want 200: %s", w.Code, w.Body.String())
	}
	var bench map[string]interface{}
	decodeJSON(t, w, &bench)
	if bench["optimized"] == nil || bench["generic"] == nil {
		t.Fatalf("benchmark comparison missing sides: %s", w.Body.String())
	}
	opt := bench["optimized"].(map[string]interface{})
	gen := bench["generic"].(map[string]interface{})
	if opt["median_tokens_per_second"].(float64) != 13.5 {
		t.Errorf("optimized tps = %v, want 13.5", opt["median_tokens_per_second"])
	}
	if gen["median_tokens_per_second"].(float64) != 10.0 {
		t.Errorf("generic tps = %v, want 10.0", gen["median_tokens_per_second"])
	}
	// Ranges must survive persistence (regression: array → flat fields).
	if rng, ok := opt["tokens_per_second_range"].([]interface{}); !ok || len(rng) != 2 || rng[0].(float64) != 12.1 {
		t.Errorf("optimized tps range lost in persistence: %v", opt["tokens_per_second_range"])
	}
	if pct := bench["tokens_per_second_improvement_pct"].(float64); math.Abs(pct-35.0) > 0.01 {
		t.Errorf("tps improvement = %v, want 35.0", pct)
	}
	if pct := bench["ttft_improvement_pct"].(float64); math.Abs(pct-33.33) > 0.1 {
		t.Errorf("ttft improvement = %v, want ~33.33", pct)
	}
	if mem := bench["memory_difference_bytes"].(float64); mem != 400000000 {
		t.Errorf("memory difference = %v, want 400000000", mem)
	}

	// 5. Missing template_id → 400
	w = e.doWithJWT(http.MethodPost, "/api/v1/servers/"+srv.ID+"/infer/deploy", token, map[string]string{})
	if w.Code != http.StatusBadRequest {
		t.Errorf("deploy without template_id = %d, want 400: %s", w.Code, w.Body.String())
	}

	// 6. Another user cannot deploy to this server → 403
	e.seedUser(t, "usr-other-infer", "other-infer@test.com", "OtherInfer", func() string {
		h, _ := auth.HashPassword("pass1234!")
		return h
	}())
	otherToken := e.mintToken(t, "usr-other-infer", "sess-other-infer", "other-infer@test.com", "OtherInfer", time.Hour)
	w = e.doWithJWT(http.MethodPost, "/api/v1/servers/"+srv.ID+"/infer/deploy", otherToken, map[string]string{
		"template_id": "llm-chat-kleidiai",
	})
	if w.Code != http.StatusForbidden {
		t.Errorf("cross-user deploy = %d, want 403: %s", w.Code, w.Body.String())
	}

	// 7. Nonexistent server → 404
	w = e.doWithJWT(http.MethodGet, "/api/v1/servers/does-not-exist/infer/status", token, nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("status for nonexistent server = %d, want 404: %s", w.Code, w.Body.String())
	}
}

// TestE2E_InferBenchmarkRerun verifies the run_benchmark dispatch mirrors the
// deploy dispatch (202 + command_id), and that the benchmark endpoint reflects
// a refreshed run.
func TestE2E_InferBenchmarkRerun(t *testing.T) {
	e := setupE2E(t)
	e.seedUser(t, "usr-bench2", "bench2@test.com", "Bench2", func() string {
		h, _ := auth.HashPassword("pass1234!")
		return h
	}())
	token := e.mintToken(t, "usr-bench2", "sess-bench2", "bench2@test.com", "Bench2", time.Hour)

	w := e.doWithJWT(http.MethodPost, "/api/v1/servers", token, map[string]string{"name": "bench-server"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create server = %d: %s", w.Code, w.Body.String())
	}
	var srv struct{ ID string }
	decodeJSON(t, w, &srv)

	// Re-run benchmark against the existing deployment
	w = e.doWithJWT(http.MethodPost, "/api/v1/servers/"+srv.ID+"/infer/benchmark", token, map[string]string{
		"template_id": "llm-chat-kleidiai",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("run_benchmark = %d, want 202: %s", w.Code, w.Body.String())
	}
	var resp struct {
		CommandID string `json:"command_id"`
	}
	decodeJSON(t, w, &resp)
	if !strings.HasPrefix(resp.CommandID, "cmd-") {
		t.Errorf("command_id = %q, want cmd- prefix", resp.CommandID)
	}

	// No benchmark rows yet → empty object, not an error
	w = e.doWithJWT(http.MethodGet, "/api/v1/servers/"+srv.ID+"/infer/benchmark", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("empty benchmark = %d, want 200: %s", w.Code, w.Body.String())
	}
	if strings.TrimSpace(w.Body.String()) != "{}" {
		t.Errorf("empty benchmark body = %s, want {}", w.Body.String())
	}
}
