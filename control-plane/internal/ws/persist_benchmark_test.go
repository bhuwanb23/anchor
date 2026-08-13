package ws

import (
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/yourname/yourplatform/control-plane/internal/db"
	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
)

// TestPersistBenchmarkIfPresent_RangeArrays verifies that an agent deploy
// result carrying benchmark ranges as arrays (the actual agent wire format:
// tokens_per_second_range / ttft_range_ms) is persisted with the ranges
// intact. Regression test for the flat-field-only parser that silently
// dropped range data (variance display).
func TestPersistBenchmarkIfPresent_RangeArrays(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	database.SetMaxOpenConns(1)
	defer database.Close()
	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	serverID := "srv-bench-1"
	output := map[string]interface{}{
		"template_id":  "llm-chat-kleidiai",
		"quantization": "Q4_K_M",
		"arm_features": "Full (SVE + I8MM)",
		"benchmark_comparison": map[string]interface{}{
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
	outputJSON, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("marshal output: %v", err)
	}
	payload, err := json.Marshal(map[string]interface{}{
		"command_id": "cmd-bench-1",
		"status":     "success",
		"output":     string(outputJSON),
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	persistBenchmarkIfPresent(database, serverID, payload)

	opt, err := queries.GetLatestBenchmarkByServerAndBuild(database, serverID, "optimized")
	if err != nil {
		t.Fatalf("get optimized row: %v", err)
	}
	gen, err := queries.GetLatestBenchmarkByServerAndBuild(database, serverID, "generic")
	if err != nil {
		t.Fatalf("get generic row: %v", err)
	}

	if opt.MedianTokensPerSecond != 13.5 {
		t.Errorf("optimized median_tps = %v, want 13.5", opt.MedianTokensPerSecond)
	}
	if opt.TokensSecRangeMin != 12.1 || opt.TokensSecRangeMax != 14.9 {
		t.Errorf("optimized tps range = [%v,%v], want [12.1,14.9]", opt.TokensSecRangeMin, opt.TokensSecRangeMax)
	}
	if opt.TTFTRangeMinMs != 180 || opt.TTFTRangeMaxMs != 240 {
		t.Errorf("optimized ttft range = [%v,%v], want [180,240]", opt.TTFTRangeMinMs, opt.TTFTRangeMaxMs)
	}
	if !opt.VarianceDetected || opt.ActualRuns != 5 {
		t.Errorf("optimized variance/runs = %v/%d, want true/5", opt.VarianceDetected, opt.ActualRuns)
	}

	if gen.MedianTokensPerSecond != 10.0 {
		t.Errorf("generic median_tps = %v, want 10.0", gen.MedianTokensPerSecond)
	}
	if gen.TokensSecRangeMin != 9.2 || gen.TokensSecRangeMax != 11.1 {
		t.Errorf("generic tps range = [%v,%v], want [9.2,11.1]", gen.TokensSecRangeMin, gen.TokensSecRangeMax)
	}
	if gen.TTFTRangeMinMs != 260 || gen.TTFTRangeMaxMs != 340 {
		t.Errorf("generic ttft range = [%v,%v], want [260,340]", gen.TTFTRangeMinMs, gen.TTFTRangeMaxMs)
	}
}

// TestPersistBenchmarkIfPresent_IgnoresFailed ensures a failed command result
// never writes benchmark rows.
func TestPersistBenchmarkIfPresent_IgnoresFailed(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	database.SetMaxOpenConns(1)
	defer database.Close()
	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"command_id": "cmd-fail",
		"status":     "failed",
		"error":      "something broke",
	})
	persistBenchmarkIfPresent(database, "srv-bench-fail", payload)

	rows, err := database.Query("SELECT COUNT(*) FROM benchmark_results WHERE server_id = ?", "srv-bench-fail")
	if err != nil {
		t.Fatalf("count rows: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("no count row")
	}
	var n int
	if err := rows.Scan(&n); err != nil {
		t.Fatalf("scan count: %v", err)
	}
	if n != 0 {
		t.Errorf("benchmark rows = %d, want 0 for failed command", n)
	}
}
