package queries

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// BenchmarkResult holds the benchmark result for a single run.
type BenchmarkResult struct {
	ID                       string          `json:"id"`
	ServerID                 string          `json:"server_id"`
	DeploymentID             string          `json:"deployment_id,omitempty"`
	TemplateID               string          `json:"template_id"`
	BuildLabel               string          `json:"build_label"`       // "optimized" or "generic"
	ImageTag                 string          `json:"image_tag"`
	Quantization             string          `json:"quantization"`
	ArmFeatures              string          `json:"arm_features,omitempty"` // comma-separated: I8MM,SVE,SVE2,...
	MedianTokensPerSecond    float64         `json:"median_tokens_per_second"`
	MedianTTFTMs             int64           `json:"median_ttft_ms"`
	PeakMemoryBytes          uint64          `json:"peak_memory_bytes"`
	TotalDurationMs          int64           `json:"total_duration_ms"`
	PromptResults            json.RawMessage `json:"prompt_results"`
	// Range info
	TokensSecRangeMin        float64         `json:"tokens_sec_range_min"`
	TokensSecRangeMax        float64         `json:"tokens_sec_range_max"`
	TTFTRangeMinMs           int64           `json:"ttft_range_min_ms"`
	TTFTRangeMaxMs           int64           `json:"ttft_range_max_ms"`
	VarianceDetected         bool            `json:"variance_detected"`
	ActualRuns               int             `json:"actual_runs"`
	// Arm Performix
	PerformixTokensPerSecond float64        `json:"performix_tokens_per_second,omitempty"`
	PerformixTTFTMs          int64          `json:"performix_ttft_ms,omitempty"`
	PerformixPeakMemoryBytes uint64         `json:"performix_peak_memory_bytes,omitempty"`
	PerformixRawOutput       string         `json:"performix_raw_output,omitempty"`
	CreatedAt                string         `json:"created_at"`
	UpdatedAt                string         `json:"updated_at"`
}

// InsertBenchmarkResult stores a new benchmark result.
func InsertBenchmarkResult(db *sql.DB, b *BenchmarkResult) error {
	result, err := db.Exec(`
		INSERT INTO benchmark_results (
			server_id, deployment_id, template_id, build_label, image_tag, quantization,
			arm_features,
			median_tokens_per_second, median_ttft_ms, peak_memory_bytes, total_duration_ms,
			prompt_results,
			tokens_sec_range_min, tokens_sec_range_max, ttft_range_min_ms, ttft_range_max_ms,
			variance_detected, actual_runs,
			performix_tokens_per_second, performix_ttft_ms, performix_peak_memory_bytes, performix_raw_output
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b.ServerID, nullStr(b.DeploymentID), b.TemplateID, b.BuildLabel, b.ImageTag, b.Quantization,
		b.ArmFeatures,
		b.MedianTokensPerSecond, b.MedianTTFTMs, b.PeakMemoryBytes, b.TotalDurationMs,
		b.PromptResults,
		b.TokensSecRangeMin, b.TokensSecRangeMax, b.TTFTRangeMinMs, b.TTFTRangeMaxMs,
		b.VarianceDetected, b.ActualRuns,
		b.PerformixTokensPerSecond, b.PerformixTTFTMs, b.PerformixPeakMemoryBytes, nullStr(b.PerformixRawOutput),
	)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err == nil {
		b.ID = fmt.Sprintf("%d", id)
	}
	return nil
}

// GetBenchmarkResultsByServer returns all benchmark results for a server, newest first.
func GetBenchmarkResultsByServer(db *sql.DB, serverID string) ([]BenchmarkResult, error) {
	rows, err := db.Query(`
		SELECT id, server_id, deployment_id, template_id, build_label, image_tag, quantization,
		       arm_features,
		       median_tokens_per_second, median_ttft_ms, peak_memory_bytes, total_duration_ms,
		       prompt_results,
		       tokens_sec_range_min, tokens_sec_range_max, ttft_range_min_ms, ttft_range_max_ms,
		       variance_detected, actual_runs,
		       performix_tokens_per_second, performix_ttft_ms, performix_peak_memory_bytes, performix_raw_output,
		       created_at, updated_at
		FROM benchmark_results
		WHERE server_id = ?
		ORDER BY created_at DESC`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []BenchmarkResult
	for rows.Next() {
		var b BenchmarkResult
		if err := rows.Scan(
			&b.ID, &b.ServerID, &b.DeploymentID, &b.TemplateID, &b.BuildLabel, &b.ImageTag, &b.Quantization,
			&b.ArmFeatures,
			&b.MedianTokensPerSecond, &b.MedianTTFTMs, &b.PeakMemoryBytes, &b.TotalDurationMs,
			&b.PromptResults,
			&b.TokensSecRangeMin, &b.TokensSecRangeMax, &b.TTFTRangeMinMs, &b.TTFTRangeMaxMs,
			&b.VarianceDetected, &b.ActualRuns,
			&b.PerformixTokensPerSecond, &b.PerformixTTFTMs, &b.PerformixPeakMemoryBytes, &b.PerformixRawOutput,
			&b.CreatedAt, &b.UpdatedAt,
		); err != nil {
			return nil, err
		}
		results = append(results, b)
	}
	return results, nil
}

// GetLatestBenchmarkByServerAndBuild returns the most recent benchmark result
// for a server and build label (e.g. "optimized" or "generic").
func GetLatestBenchmarkByServerAndBuild(db *sql.DB, serverID, buildLabel string) (*BenchmarkResult, error) {
	var b BenchmarkResult
	err := db.QueryRow(`
		SELECT id, server_id, deployment_id, template_id, build_label, image_tag, quantization,
		       arm_features,
		       median_tokens_per_second, median_ttft_ms, peak_memory_bytes, total_duration_ms,
		       prompt_results,
		       tokens_sec_range_min, tokens_sec_range_max, ttft_range_min_ms, ttft_range_max_ms,
		       variance_detected, actual_runs,
		       performix_tokens_per_second, performix_ttft_ms, performix_peak_memory_bytes, performix_raw_output,
		       created_at, updated_at
		FROM benchmark_results
		WHERE server_id = ? AND build_label = ?
		ORDER BY created_at DESC
		LIMIT 1`, serverID, buildLabel).Scan(
		&b.ID, &b.ServerID, &b.DeploymentID, &b.TemplateID, &b.BuildLabel, &b.ImageTag, &b.Quantization,
		&b.ArmFeatures,
		&b.MedianTokensPerSecond, &b.MedianTTFTMs, &b.PeakMemoryBytes, &b.TotalDurationMs,
		&b.PromptResults,
		&b.TokensSecRangeMin, &b.TokensSecRangeMax, &b.TTFTRangeMinMs, &b.TTFTRangeMaxMs,
		&b.VarianceDetected, &b.ActualRuns,
		&b.PerformixTokensPerSecond, &b.PerformixTTFTMs, &b.PerformixPeakMemoryBytes, &b.PerformixRawOutput,
		&b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// GetBenchmarkComparison returns the latest optimized and generic results for a server.
func GetBenchmarkComparison(db *sql.DB, serverID string) (optimized, generic *BenchmarkResult, err error) {
	optimized, err = GetLatestBenchmarkByServerAndBuild(db, serverID, "optimized")
	if err != nil && err != sql.ErrNoRows {
		return nil, nil, err
	}
	if err == sql.ErrNoRows {
		optimized = nil
		err = nil
	}
	generic, err = GetLatestBenchmarkByServerAndBuild(db, serverID, "generic")
	if err != nil && err != sql.ErrNoRows {
		return nil, nil, err
	}
	if err == sql.ErrNoRows {
		generic = nil
		err = nil
	}
	return optimized, generic, nil
}

// MaybePersistInferBenchmarks stores benchmark rows from a deploy_inference
// command result when present. Safe to call for any command payload.
func MaybePersistInferBenchmarks(db *sql.DB, serverID, cmdID, rawPayload string) {
	if rec, _ := GetCommandByID(db, cmdID); rec != nil && rec.CommandType != "" && rec.CommandType != "deploy_inference" {
		return
	}
	details := parseInferDeployDetails(rawPayload)
	if details == nil {
		return
	}
	PersistInferBenchmarksFromDetails(db, serverID, details)
}

func parseInferDeployDetails(raw string) map[string]interface{} {
	var outer map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &outer); err != nil {
		return nil
	}
	if outStr, ok := outer["output"].(string); ok && outStr != "" {
		var inner map[string]interface{}
		if json.Unmarshal([]byte(outStr), &inner) == nil {
			outer = inner
		}
	}
	if _, ok := outer["benchmark_comparison"]; ok {
		return outer
	}
	if _, ok := outer["endpoint_url"]; ok {
		return outer
	}
	if _, ok := outer["template_id"]; ok {
		return outer
	}
	return nil
}

// PersistInferBenchmarksFromDetails writes generic/optimized rows from deploy output.
func PersistInferBenchmarksFromDetails(db *sql.DB, serverID string, details map[string]interface{}) {
	cmp, _ := details["benchmark_comparison"].(map[string]interface{})
	if cmp == nil {
		return
	}
	templateID, _ := details["template_id"].(string)
	quant, _ := details["quantization"].(string)
	armFeatures, _ := details["arm_features"].(string)
	if armFeatures == "" {
		armFeatures, _ = details["optimization"].(string)
	}

	persistOne := func(label string, src map[string]interface{}) {
		if src == nil {
			return
		}
		imageTag, _ := src["image_tag"].(string)
		if imageTag == "" {
			imageTag, _ = details["image_tag"].(string)
		}
		prompts, _ := json.Marshal(src["prompts"])
		if string(prompts) == "null" {
			prompts = []byte("[]")
		}
		b := &BenchmarkResult{
			ServerID:              serverID,
			TemplateID:            templateID,
			BuildLabel:            label,
			ImageTag:              imageTag,
			Quantization:          quant,
			ArmFeatures:           armFeatures,
			MedianTokensPerSecond: jsonFloat(src["median_tokens_per_second"]),
			MedianTTFTMs:          jsonInt64(src["median_ttft_ms"]),
			PeakMemoryBytes:       uint64(jsonInt64(src["peak_memory_bytes"])),
			TotalDurationMs:       jsonInt64(src["total_duration_ms"]),
			PromptResults:         prompts,
			ActualRuns:            int(jsonInt64(src["actual_runs"])),
			VarianceDetected:      jsonBool(src["variance_detected"]),
		}
		if rng, ok := src["tokens_per_second_range"].([]interface{}); ok && len(rng) == 2 {
			b.TokensSecRangeMin = jsonFloat(rng[0])
			b.TokensSecRangeMax = jsonFloat(rng[1])
		}
		if rng, ok := src["ttft_range_ms"].([]interface{}); ok && len(rng) == 2 {
			b.TTFTRangeMinMs = jsonInt64(rng[0])
			b.TTFTRangeMaxMs = jsonInt64(rng[1])
		}
		if pf, ok := src["performix"].(map[string]interface{}); ok && pf != nil {
			b.PerformixTokensPerSecond = jsonFloat(pf["tokens_per_second"])
			b.PerformixTTFTMs = jsonInt64(pf["time_to_first_token_ms"])
			b.PerformixPeakMemoryBytes = uint64(jsonInt64(pf["peak_memory_bytes"]))
		}
		_ = InsertBenchmarkResult(db, b)
	}

	if opt, ok := cmp["optimized"].(map[string]interface{}); ok {
		persistOne("optimized", opt)
	}
	if gen, ok := cmp["generic"].(map[string]interface{}); ok {
		persistOne("generic", gen)
	}
}

func jsonFloat(v interface{}) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case json.Number:
		f, _ := t.Float64()
		return f
	default:
		return 0
	}
}

func jsonInt64(v interface{}) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case int:
		return int64(t)
	case int64:
		return t
	case json.Number:
		i, _ := t.Int64()
		return i
	default:
		return 0
	}
}

func jsonBool(v interface{}) bool {
	switch t := v.(type) {
	case bool:
		return t
	case float64:
		return t != 0
	case int:
		return t != 0
	default:
		return false
	}
}
