package queries

import (
	"database/sql"
	"encoding/json"
)

// BenchmarkResult holds the benchmark result for a single run.
type BenchmarkResult struct {
	ID                     string          `json:"id"`
	ServerID               string          `json:"server_id"`
	DeploymentID           string          `json:"deployment_id,omitempty"`
	TemplateID             string          `json:"template_id"`
	BuildLabel             string          `json:"build_label"`       // "optimized" or "generic"
	ImageTag               string          `json:"image_tag"`
	Quantization           string          `json:"quantization"`
	MedianTokensPerSecond  float64         `json:"median_tokens_per_second"`
	MedianTTFTMs           int64           `json:"median_ttft_ms"`
	PeakMemoryBytes        uint64          `json:"peak_memory_bytes"`
	TotalDurationMs        int64           `json:"total_duration_ms"`
	PromptResults          json.RawMessage `json:"prompt_results"`
	CreatedAt              string          `json:"created_at"`
	UpdatedAt              string          `json:"updated_at"`
}

// InsertBenchmarkResult stores a new benchmark result.
func InsertBenchmarkResult(db *sql.DB, b *BenchmarkResult) error {
	result, err := db.Exec(`
		INSERT INTO benchmark_results (
			server_id, deployment_id, template_id, build_label, image_tag, quantization,
			median_tokens_per_second, median_ttft_ms, peak_memory_bytes, total_duration_ms,
			prompt_results
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b.ServerID, nullStr(b.DeploymentID), b.TemplateID, b.BuildLabel, b.ImageTag, b.Quantization,
		b.MedianTokensPerSecond, b.MedianTTFTMs, b.PeakMemoryBytes, b.TotalDurationMs,
		b.PromptResults,
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
		       median_tokens_per_second, median_ttft_ms, peak_memory_bytes, total_duration_ms,
		       prompt_results, created_at, updated_at
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
			&b.MedianTokensPerSecond, &b.MedianTTFTMs, &b.PeakMemoryBytes, &b.TotalDurationMs,
			&b.PromptResults, &b.CreatedAt, &b.UpdatedAt,
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
		       median_tokens_per_second, median_ttft_ms, peak_memory_bytes, total_duration_ms,
		       prompt_results, created_at, updated_at
		FROM benchmark_results
		WHERE server_id = ? AND build_label = ?
		ORDER BY created_at DESC
		LIMIT 1`, serverID, buildLabel).Scan(
		&b.ID, &b.ServerID, &b.DeploymentID, &b.TemplateID, &b.BuildLabel, &b.ImageTag, &b.Quantization,
		&b.MedianTokensPerSecond, &b.MedianTTFTMs, &b.PeakMemoryBytes, &b.TotalDurationMs,
		&b.PromptResults, &b.CreatedAt, &b.UpdatedAt,
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
	generic, err = GetLatestBenchmarkByServerAndBuild(db, serverID, "generic")
	if err != nil && err != sql.ErrNoRows {
		return nil, nil, err
	}
	return optimized, generic, nil
}
