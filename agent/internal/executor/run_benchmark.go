package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/yourname/yourplatform/agent/internal/docker"
	"github.com/yourname/yourplatform/agent/internal/infer"
	"github.com/yourname/yourplatform/agent/internal/platform"
)

// RunBenchmarkPayload is the payload for run_benchmark commands.
// TemplateID identifies the existing inference deployment to benchmark.
type RunBenchmarkPayload struct {
	TemplateID string `json:"template_id"`
	ServerID   string `json:"server_id"`
}

// executeRunBenchmark re-runs the benchmark pipeline (generic + optimized
// builds) against an existing inference deployment. It reuses the same
// model volume and model file as the original deploy, so results are
// directly comparable. Non-fatal when benchmark infrastructure is missing —
// the deployed endpoint stays up either way.
func (e *Executor) executeRunBenchmark(ctx context.Context, cmd Command, result *Result) error {
	var p RunBenchmarkPayload
	if err := json.Unmarshal(cmd.Payload, &p); err != nil {
		return fmt.Errorf("invalid run_benchmark payload: %w", err)
	}
	if p.TemplateID == "" {
		return fmt.Errorf("template_id is required")
	}
	if e.inferRegistry == nil {
		return fmt.Errorf("inference template registry not configured")
	}
	tmpl := e.inferRegistry.Get(p.TemplateID)
	if tmpl == nil {
		return fmt.Errorf("template not found: %s", p.TemplateID)
	}
	if e.docker == nil {
		return fmt.Errorf("docker client not configured")
	}

	// Confirm the inference deployment actually exists. The container is
	// named infer-{templateID} by the deploy sequence.
	containerName := fmt.Sprintf("infer-%s", p.TemplateID)
	insp, err := e.docker.InspectContainer(ctx, containerName)
	if err != nil {
		return fmt.Errorf("no inference deployment found for %s (deploy it first): %w", p.TemplateID, err)
	}
	if insp.Config == nil || insp.Config.Labels == nil {
		return fmt.Errorf("inference container %s has no deployment labels", containerName)
	}

	// Recover the quantization used at deploy time from the container
	// labels; fall back to the template default recommendation.
	quant := insp.Config.Labels["yourplatform.quant"]
	var quantInfo *infer.QuantInfo
	if q, ok := tmpl.Model.Quantizations[quant]; ok && q.FileName != "" {
		quantInfo = &q
	} else {
		plat0 := platform.Detect()
		availableRAMGB := float64(plat0.Memory.AvailableMB) / 1024.0
		_, quantInfo = tmpl.SelectQuantization(availableRAMGB)
	}
	if quantInfo == nil || quantInfo.FileName == "" {
		return fmt.Errorf("no suitable quantization found for this server")
	}

	projectName := fmt.Sprintf("infer-%s", p.TemplateID)
	volName := docker.VolumeName(projectName, volumePurposeModelWeights)

	// Build the image set from the current platform detection — the same
	// selection logic the deploy sequence uses.
	plat := platform.Detect()
	optimizedImage := plat.Build.ImageTag
	genericImage := platform.InferImageBase + ":arm64"
	if !plat.IsArm64 {
		genericImage = platform.InferImageBase + ":x86_64"
	}

	// Collect non-inference containers to pause during benchmarking.
	var managedContainers []string
	if e.stateManager != nil {
		state := e.stateManager.GetState()
		for projName, proj := range state.Projects {
			if projName == projectName {
				continue
			}
			for role, ct := range proj.Containers {
				if role != "inference" && ct.ContainerID != "" && ct.Status == "running" {
					managedContainers = append(managedContainers, ct.ContainerID)
				}
			}
		}
	}

	// ─── Baseline: generic build ───────────────────────────────────────
	SendProgress(e.progressSender, cmd.ID, "benchmarking", "Running baseline benchmark (generic build)...", 10)
	baselineResult, err := infer.RunBenchmark(ctx, infer.BenchmarkOpts{
		Image:             genericImage,
		VolumeName:        volName,
		VolumePath:        "/models",
		ModelFile:         quantInfo.FileName,
		Template:          tmpl,
		Docker:            e.docker,
		Logger:            slog.Default(),
		ManagedContainers: managedContainers,
		OnProgress: func(msg string) {
			SendProgress(e.progressSender, cmd.ID, "benchmarking", msg, 10)
		},
	})
	if err != nil {
		return fmt.Errorf("baseline benchmark failed: %w", err)
	}
	SendProgress(e.progressSender, cmd.ID, "benchmarking", "Baseline complete. Benchmarking optimized build...", 60)

	// ─── Optimized: the KleidiAI-optimized build ───────────────────────
	optimizedResult, err := infer.RunBenchmark(ctx, infer.BenchmarkOpts{
		Image:             optimizedImage,
		VolumeName:        volName,
		VolumePath:        "/models",
		ModelFile:         quantInfo.FileName,
		Template:          tmpl,
		Docker:            e.docker,
		Logger:            slog.Default(),
		ManagedContainers: managedContainers,
		PerformixImage:    "ghcr.io/arm-performix/performix:latest",
		OnProgress: func(msg string) {
			SendProgress(e.progressSender, cmd.ID, "benchmarking", msg, 80)
		},
	})
	if err != nil {
		return fmt.Errorf("optimized benchmark failed: %w", err)
	}

	comparison := infer.CompareResults(optimizedResult, baselineResult)
	if comparison == nil {
		return fmt.Errorf("could not compare benchmark results")
	}

	SendProgress(e.progressSender, cmd.ID, "complete", "Benchmark complete", 100)

	result.Status = "success"
	result.Output = benchmarkResultJSON(p.TemplateID, quant, plat, optimizedResult, baselineResult, comparison)
	slog.Info("run_benchmark complete",
		"template", p.TemplateID,
		"tok_sec_improvement", comparison.TokensSecImprovement,
		"ttft_improvement", comparison.TTFTImprovement,
	)
	return nil
}

// benchmarkResultJSON builds the same result shape the deploy sequence
// returns, so the control plane and dashboard handle both commands identically.
func benchmarkResultJSON(templateID, quant string, plat *platform.PlatformInfo, optimized, baseline *infer.BenchmarkResult, comparison *infer.BenchmarkComparison) string {
	out := map[string]interface{}{
		"template_id":  templateID,
		"quantization": quant,
		"optimization": plat.Build.OptimizationLabel,
		"arm_features": plat.Build.OptimizationLabel,
		"benchmark_comparison": map[string]interface{}{
			"tokens_per_second_improvement_pct": comparison.TokensSecImprovement,
			"ttft_improvement_pct":              comparison.TTFTImprovement,
			"memory_difference_bytes":           comparison.MemoryDifferenceBytes,
			"optimized":                         benchmarkResultMap(optimized),
			"generic":                           benchmarkResultMap(baseline),
		},
		"benchmarked_at": time.Now().UTC().Format(time.RFC3339),
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// benchmarkResultMap flattens an infer.BenchmarkResult into the JSON shape
// the dashboard BenchmarkComparison type expects.
func benchmarkResultMap(r *infer.BenchmarkResult) map[string]interface{} {
	m := map[string]interface{}{
		"build_label":               r.BuildLabel,
		"image_tag":                 r.ImageTag,
		"median_tokens_per_second":  r.MedianTokensSec,
		"median_ttft_ms":            r.MedianTTFT.Milliseconds(),
		"peak_memory_bytes":         r.PeakMemoryBytes,
		"total_duration_ms":         r.TotalDuration.Milliseconds(),
		"tokens_per_second_range":   r.TokensSecRange,
		"ttft_range_ms":             r.TTFTRange,
		"variance_detected":         r.VarianceDetected,
		"actual_runs":               r.ActualRuns,
		"prompts":                   r.Prompts,
	}
	if r.Performix != nil {
		m["performix"] = map[string]interface{}{
			"tokens_per_second":     r.Performix.TokensPerSecond,
			"time_to_first_token_ms": r.Performix.TimeToFirstToken.Milliseconds(),
			"peak_memory_bytes":     r.Performix.PeakMemoryBytes,
			"total_duration_ms":     r.Performix.TotalDuration.Milliseconds(),
		}
	}
	return m
}
