package queries

import (
	"database/sql"
	"encoding/json"
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
		b.ID = string(rune(id))
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
		// deployment_id, performix_raw_output are nullable; prompt_results is a
		// TEXT column that the driver can't scan directly into json.RawMessage.
		var depID, perfRaw, promptsRaw sql.NullString
		if err := rows.Scan(
			&b.ID, &b.ServerID, &depID, &b.TemplateID, &b.BuildLabel, &b.ImageTag, &b.Quantization,
			&b.ArmFeatures,
			&b.MedianTokensPerSecond, &b.MedianTTFTMs, &b.PeakMemoryBytes, &b.TotalDurationMs,
			&promptsRaw,
			&b.TokensSecRangeMin, &b.TokensSecRangeMax, &b.TTFTRangeMinMs, &b.TTFTRangeMaxMs,
			&b.VarianceDetected, &b.ActualRuns,
			&b.PerformixTokensPerSecond, &b.PerformixTTFTMs, &b.PerformixPeakMemoryBytes, &perfRaw,
			&b.CreatedAt, &b.UpdatedAt,
		); err != nil {
			return nil, err
		}
		b.DeploymentID = depID.String
		b.PerformixRawOutput = perfRaw.String
		b.PromptResults = json.RawMessage(promptsRaw.String)
		results = append(results, b)
	}
	return results, nil
}

// GetLatestBenchmarkByServerAndBuild returns the most recent benchmark result
// for a server and build label (e.g. "optimized" or "generic").
func GetLatestBenchmarkByServerAndBuild(db *sql.DB, serverID, buildLabel string) (*BenchmarkResult, error) {
	var b BenchmarkResult
	var depID, perfRaw, promptsRaw sql.NullString
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
		&b.ID, &b.ServerID, &depID, &b.TemplateID, &b.BuildLabel, &b.ImageTag, &b.Quantization,
		&b.ArmFeatures,
		&b.MedianTokensPerSecond, &b.MedianTTFTMs, &b.PeakMemoryBytes, &b.TotalDurationMs,
		&promptsRaw,
		&b.TokensSecRangeMin, &b.TokensSecRangeMax, &b.TTFTRangeMinMs, &b.TTFTRangeMaxMs,
		&b.VarianceDetected, &b.ActualRuns,
		&b.PerformixTokensPerSecond, &b.PerformixTTFTMs, &b.PerformixPeakMemoryBytes, &perfRaw,
		&b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	b.DeploymentID = depID.String
	b.PerformixRawOutput = perfRaw.String
	b.PromptResults = json.RawMessage(promptsRaw.String)
	return &b, nil
}

// GetBenchmarkComparison returns the latest optimized and generic results for a server.
func GetBenchmarkComparison(db *sql.DB, serverID string) (optimized, generic *BenchmarkResult, err error) {
	optimized, err = GetLatestBenchmarkByServerAndBuild(db, serverID, "optimized")
	if err != nil && err != sql.ErrNoRows {
		return nil, nil, err
	}
	generic, err = GetLatestBenchmarkByServerAndBuild(db, serverID, "generic")
	if err != nil && err != sql.ErrNoRows {
		return nil, nil, err
	}
	return optimized, generic, nil
}
