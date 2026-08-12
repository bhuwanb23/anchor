package executor

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/yourname/yourplatform/agent/internal/docker"
	"github.com/yourname/yourplatform/agent/internal/infer"
	"github.com/yourname/yourplatform/agent/internal/platform"
	"github.com/yourname/yourplatform/agent/internal/state"
)

// Volume purpose constant for model weights.
const volumePurposeModelWeights = "model-weights"

// Downloader image used to fetch GGUF files from HuggingFace.
const downloaderImage = "python:3.11-slim"

// executeDeployInference deploys an inference server using a template.
// Implements the full Step 3A–3I deploy sequence.
func (e *Executor) executeDeployInference(ctx context.Context, cmd Command, result *Result) error {
	var p DeployInferencePayload
	if err := json.Unmarshal(cmd.Payload, &p); err != nil {
		return fmt.Errorf("invalid deploy_inference payload: %w", err)
	}

	if p.TemplateID == "" {
		return fmt.Errorf("template_id is required")
	}

	// Look up template
	if e.inferRegistry == nil {
		return fmt.Errorf("inference template registry not configured")
	}
	tmpl := e.inferRegistry.Get(p.TemplateID)
	if tmpl == nil {
		return fmt.Errorf("template not found: %s", p.TemplateID)
	}

	slog.Info("deploy_inference started",
		"template", tmpl.ID,
		"model", tmpl.Model.Family+" "+tmpl.Model.Size,
	)

	// ─── Step 3A: Validate readiness ──────────────────────────────────
	SendProgress(e.progressSender, cmd.ID, "validating", "Checking server capabilities...", 5)
	plat := platform.Detect()

	availableRAMGB := float64(plat.Memory.AvailableMB) / 1024.0
	quantKey, quantInfo := tmpl.SelectQuantization(availableRAMGB)
	if quantInfo == nil {
		// Cross-phase error contract: specific, actionable, non-technical.
		minGB := 0.0
		for _, q := range tmpl.Model.Quantizations {
			if q.MinRAMGB > 0 && (minGB == 0 || q.MinRAMGB < minGB) {
				minGB = q.MinRAMGB
			}
		}
		return fmt.Errorf(
			"Your server has %.1fGB of available memory. Deploying %s %s requires at least %.1fGB. Options: stop other running apps to free memory, or use a larger server.",
			availableRAMGB, tmpl.Model.Family, tmpl.Model.Size, minGB,
		)
	}
	if !plat.Readiness.CanRunInference {
		return fmt.Errorf("server not ready: %s", plat.Readiness.BlockReason)
	}
	if plat.Disk.AvailableGB < quantInfo.SizeGB+1.0 {
		return fmt.Errorf(
			"Not enough free disk space: this model needs %.1fGB, but only %.1fGB is available. Free up space and try again, or use a larger server.",
			quantInfo.SizeGB+1.0, plat.Disk.AvailableGB,
		)
	}

	slog.Info("readiness validated",
		"quant", quantKey,
		"ram_gb", availableRAMGB,
		"disk_gb", plat.Disk.AvailableGB,
	)

	// ─── Step 3B: Pull Docker images ──────────────────────────────────
	SendProgress(e.progressSender, cmd.ID, "pulling", "Pulling inference runtime images...", 10)

	optimizedImage := plat.Build.ImageTag
	genericImage := platform.InferImageBase + ":arm64"
	if !plat.IsArm64 {
		genericImage = platform.InferImageBase + ":x86_64"
	}

	// Pull optimized image (primary)
	if e.docker != nil {
		progressFn := func(p docker.PullProgress) error {
			SendProgress(e.progressSender, cmd.ID, "pulling", fmt.Sprintf("Pulling optimized image: %s", p.Status), 10)
			return nil
		}
		if _, _, err := e.docker.PullImageIfNeeded(ctx, optimizedImage, e.imageCache, progressFn); err != nil {
			return fmt.Errorf("pull optimized image: %w", err)
		}
	}

	// Pull generic image (for benchmarks — non-fatal if it fails)
	if e.docker != nil && genericImage != optimizedImage {
		progressFn := func(p docker.PullProgress) error {
			SendProgress(e.progressSender, cmd.ID, "pulling", fmt.Sprintf("Pulling generic image: %s", p.Status), 20)
			return nil
		}
		if _, _, err := e.docker.PullImageIfNeeded(ctx, genericImage, e.imageCache, progressFn); err != nil {
			slog.Warn("failed to pull generic benchmark image (non-fatal)", "error", err)
		}
	}

	// Pull downloader image
	if e.docker != nil {
		if _, _, err := e.docker.PullImageIfNeeded(ctx, downloaderImage, e.imageCache, nil); err != nil {
			slog.Warn("failed to pull downloader image", "error", err)
		}
	}

	// ─── Step 3C: Create model volume ─────────────────────────────────
	SendProgress(e.progressSender, cmd.ID, "volume", "Preparing model storage...", 25)

	projectName := fmt.Sprintf("infer-%s", p.TemplateID)
	volName := docker.VolumeName(projectName, volumePurposeModelWeights)
	modelAlreadyExists := false

	if e.docker != nil {
		_, err := e.docker.EnsureVolume(ctx, projectName, volumePurposeModelWeights)
		if err != nil {
			return fmt.Errorf("create model volume: %w", err)
		}
		// Check if model already exists by running a marker test
		// Use a temporary container to check
		markerCheck, err := e.docker.CreateContainer(ctx, docker.CreateContainerOpts{
			Name:           "infer-check-" + p.TemplateID[:8],
			Image:          downloaderImage,
			VolumeMounts:   []docker.VolumeMount{{Name: volName, MountPath: "/models"}},
			RestartPolicy:  "no",
		})
		if err == nil {
			_ = e.docker.StartContainerWithWait(ctx, markerCheck, 5*time.Second)
			output, _ := e.docker.ExecInContainer(ctx, markerCheck, []string{"test", "-f", "/models/.download_complete"})
			if output == "" {
				modelAlreadyExists = true
				slog.Info("model already exists in volume, skipping download", "quant", quantKey)
			}
			_ = e.docker.StopContainerGraceful(ctx, markerCheck)
			_ = e.docker.RemoveContainer(ctx, markerCheck)
		}
	}

	// ─── Step 3D: Download model file (skip if already exists) ────────
	if !modelAlreadyExists {
		SendProgress(e.progressSender, cmd.ID, "downloading", fmt.Sprintf("Downloading model (%.1fGB)...", quantInfo.SizeGB), 30)
		slog.Info("model download started",
			"file", quantInfo.FileName,
			"size_gb", quantInfo.SizeGB,
			"note", "This is a one-time download. Future deployments will start instantly.",
		)

		if err := e.downloadModel(ctx, cmd.ID, volName, tmpl, quantInfo); err != nil {
			return fmt.Errorf("download model: %w", err)
		}
	} else {
		SendProgress(e.progressSender, cmd.ID, "downloading", "Model already downloaded, skipping...", 45)
	}

	// ─── Step 3E: Generate API credentials ────────────────────────────
	SendProgress(e.progressSender, cmd.ID, "credentials", "Generating API credentials...", 46)

	apiKey, err := generateAPIKey()
	if err != nil {
		return fmt.Errorf("generate API key: %w", err)
	}
	apiKeyHash := hashAPIKey(apiKey)

	// Store in env file
	if e.envManager != nil {
		envVars := map[string]string{
			"API_KEY":      apiKey,
			"API_KEY_HASH": apiKeyHash,
		}
		if err := e.envManager.WriteEnvFile(projectName, envVars); err != nil {
			slog.Warn("failed to write API key to env file", "error", err)
		}
	}

	slog.Info("API credentials generated", "project", projectName)

	// ─── Step 3J: Run baseline benchmark (generic build) ─────────────
	// Collect non-inference containers to pause during benchmarking.
	var managedContainers []string
	if e.stateManager != nil {
		state := e.stateManager.GetState()
		for projName, proj := range state.Projects {
			if projName == projectName {
				continue // Don't pause our own project's containers.
			}
			for role, ct := range proj.Containers {
				if role != "inference" && ct.ContainerID != "" && ct.Status == "running" {
					managedContainers = append(managedContainers, ct.ContainerID)
				}
			}
		}
	}

	var baselineResult *infer.BenchmarkResult
	if e.docker != nil && e.inferRegistry != nil {
		SendProgress(e.progressSender, cmd.ID, "benchmarking", "Running baseline benchmark (generic build)...", 47)

		genericImage := platform.InferImageBase + ":arm64"
		if !plat.IsArm64 {
			genericImage = platform.InferImageBase + ":x86_64"
		}

		baselineResult, err = infer.RunBenchmark(ctx, infer.BenchmarkOpts{
			Image:             genericImage,
			VolumeName:        volName,
			VolumePath:        "/models",
			ModelFile:         quantInfo.FileName,
			Template:          tmpl,
			Docker:            e.docker,
			Logger:            slog.Default(),
			ManagedContainers: managedContainers,
			OnProgress: func(msg string) {
				SendProgress(e.progressSender, cmd.ID, "benchmarking", msg, 47)
			},
		})
		if err != nil {
			// Benchmark failure is non-fatal — log and continue.
			slog.Warn("baseline benchmark failed (continuing deployment)", "error", err)
			SendProgress(e.progressSender, cmd.ID, "benchmarking", "Baseline benchmark failed, continuing...", 47)
		} else {
			slog.Info("baseline benchmark complete",
				"median_tok_sec", baselineResult.MedianTokensSec,
				"median_ttft_ms", baselineResult.MedianTTFT.Milliseconds(),
				"peak_memory", infer.FormatBytes(baselineResult.PeakMemoryBytes),
				"variance_detected", baselineResult.VarianceDetected,
				"actual_runs", baselineResult.ActualRuns,
			)
		}
	}

	// ─── Step 3F: Start inference server ───────────────────────────────
	SendProgress(e.progressSender, cmd.ID, "starting", "Starting inference server...", 50)

	containerName := fmt.Sprintf("infer-%s", p.TemplateID)
	if e.docker != nil {
		_, _ = e.docker.ReplaceExistingContainer(ctx, containerName)
	}

	// Port mapping
	portSpec := &docker.AppPortSpec{
		ContainerPort: tmpl.Runtime.InternalPort,
		BindAddress:   "127.0.0.1",
	}
	portMap, exposedPorts := docker.PortMapping(docker.ContainerTypeApp, portSpec)

	// Resource limits — use 80% of available RAM
	memLimitMB := int64(plat.Memory.AvailableMB * 80 / 100)
	if memLimitMB < 2048 {
		memLimitMB = 2048
	}
	rlimits := &docker.ResourceLimits{
		MemoryHard: memLimitMB * 1024 * 1024,
		MemorySoft: memLimitMB * 512 * 1024,
	}

	// Model path inside the container
	modelPath := fmt.Sprintf("/models/%s", quantInfo.FileName)

	// Build environment variables
	envVars := []string{
		fmt.Sprintf("MODEL_DIR=/models"),
		fmt.Sprintf("MODEL_FILE=%s", quantInfo.FileName),
		fmt.Sprintf("MODEL_PATH=%s", modelPath),
		fmt.Sprintf("CONTEXT_SIZE=%d", tmpl.Runtime.ContextWindow),
		fmt.Sprintf("MAX_CONCURRENT=%d", tmpl.Runtime.MaxConcurrent),
		fmt.Sprintf("API_KEY=%s", apiKey),
		fmt.Sprintf("PORT=%d", tmpl.Runtime.InternalPort),
	}

	var containerID string
	if e.docker != nil {
		id, err := e.docker.CreateContainer(ctx, docker.CreateContainerOpts{
			Name:           containerName,
			Image:          optimizedImage,
			PortBindings:   portMap,
			ExposedPorts:   exposedPorts,
			Env:            envVars,
			VolumeMounts:   []docker.VolumeMount{{Name: volName, MountPath: "/models"}},
			Labels: map[string]string{
				"yourplatform.type":     "inference",
				"yourplatform.template": tmpl.ID,
				"yourplatform.quant":    quantKey,
			},
			ResourceLimits: rlimits,
			RestartPolicy:  "unless-stopped",
			HealthCheck: &docker.HealthCheckConfig{
				Test:     []string{"CMD-SHELL", fmt.Sprintf("curl -sf http://localhost:%d/health || exit 1", tmpl.Runtime.InternalPort)},
				Interval: 10 * time.Second,
				Timeout:  5 * time.Second,
				Retries:  3,
			},
		})
		if err != nil {
			return fmt.Errorf("create container: %w", err)
		}
		containerID = id
	}

	if e.docker != nil && containerID != "" {
		if err := e.docker.StartContainerWithWait(ctx, containerID, 60*time.Second); err != nil {
			return fmt.Errorf("start container: %w", err)
		}
	}

	// ─── Step 3G: Wait for server ready ────────────────────────────────
	SendProgress(e.progressSender, cmd.ID, "loading", "Waiting for model to load into memory...", 60)

	if e.docker != nil && containerID != "" {
		if err := e.waitForInferenceReady(ctx, containerID, tmpl.Runtime.InternalPort, 3*time.Minute); err != nil {
			return fmt.Errorf("inference server not ready: %w", err)
		}
	}

	// ─── Step 3H: Configure HTTPS routing ──────────────────────────────
	domain := p.Domain
	if domain == "" {
		domain = fmt.Sprintf("infer-%s.%s.anchor.app", p.TemplateID, p.ServerID)
	}

	if e.caddy != nil {
		SendProgress(e.progressSender, cmd.ID, "routing", "Configuring HTTPS endpoint...", 80)
		routeID := "infer-" + p.TemplateID
		upstream := fmt.Sprintf("127.0.0.1:%d", tmpl.Runtime.InternalPort)
		domains := []string{domain}
		if err := e.caddy.SetRouteByID(routeID, domains, upstream); err != nil {
			slog.Warn("failed to set Caddy route", "error", err)
		}
	}

	// ─── Step 3I: Test the endpoint ────────────────────────────────────
	SendProgress(e.progressSender, cmd.ID, "testing", "Testing endpoint...", 85)

	testPassed := false
	if e.docker != nil && containerID != "" {
		if err := e.testInferenceEndpoint(ctx, tmpl.Runtime.InternalPort, apiKey); err != nil {
			slog.Warn("endpoint test failed (deploy marked as failed)", "error", err)
			return fmt.Errorf("endpoint test failed: %w", err)
		}
		testPassed = true
	}

	// ─── Step 3K: Run optimized benchmark ────────────────────────────
	var optimizedResult *infer.BenchmarkResult
	var benchmarkComparison *infer.BenchmarkComparison
	if e.docker != nil && e.inferRegistry != nil {
		SendProgress(e.progressSender, cmd.ID, "benchmarking", "Running optimized benchmark...", 88)

		optimizedResult, err = infer.RunBenchmark(ctx, infer.BenchmarkOpts{
			Image:             optimizedImage,
			VolumeName:        volName,
			VolumePath:        "/models",
			ModelFile:         quantInfo.FileName,
			Template:          tmpl,
			Docker:            e.docker,
			Logger:            slog.Default(),
			ManagedContainers: managedContainers,
			// Performix image (if available in the platform image set).
			PerformixImage: "ghcr.io/arm-performix/performix:latest",
			OnProgress: func(msg string) {
				SendProgress(e.progressSender, cmd.ID, "benchmarking", msg, 88)
			},
		})
		if err != nil {
			slog.Warn("optimized benchmark failed (continuing deployment)", "error", err)
			SendProgress(e.progressSender, cmd.ID, "benchmarking", "Optimized benchmark failed, continuing...", 88)
		} else {
			slog.Info("optimized benchmark complete",
				"median_tok_sec", optimizedResult.MedianTokensSec,
				"median_ttft_ms", optimizedResult.MedianTTFT.Milliseconds(),
				"peak_memory", infer.FormatBytes(optimizedResult.PeakMemoryBytes),
				"variance_detected", optimizedResult.VarianceDetected,
				"actual_runs", optimizedResult.ActualRuns,
				"performix_available", optimizedResult.Performix != nil,
			)

			// Build comparison if baseline is available.
			if baselineResult != nil {
				benchmarkComparison = infer.CompareResults(optimizedResult, baselineResult)
				SendProgress(e.progressSender, cmd.ID, "benchmarking",
					fmt.Sprintf("Benchmark complete: %s improvement in throughput",
						infer.FormatImprovement(benchmarkComparison.TokensSecImprovement, true)), 92)
			}
		}
	}

	// ─── Persist state ────────────────────────────────────────────────
	if e.stateManager != nil {
		_ = e.stateManager.SetContainer(projectName, "inference", &state.ContainerState{
			ContainerID:   containerID,
			Image:         optimizedImage,
			Status:        "running",
			Domain:        domain,
			HostPort:      tmpl.Runtime.InternalPort,
			RestartPolicy: "unless-stopped",
		})
	}

	// ─── Step 4: Return endpoint information ───────────────────────────
	SendProgress(e.progressSender, cmd.ID, "complete", "Inference server deployed successfully!", 100)

	endpointURL := fmt.Sprintf("https://%s", domain)

	// Build response with benchmark comparison if available.
	resultMap := map[string]interface{}{
		"template_id":     tmpl.ID,
		"container_id":    containerID,
		"image_tag":       optimizedImage,
		"quantization":    quantKey,
		"model_file":      quantInfo.FileName,
		"internal_port":   tmpl.Runtime.InternalPort,
		"domain":          domain,
		"endpoint_url":    endpointURL,
		"api_key":         apiKey,
		"api_key_hash":    apiKeyHash,
		"api_path":        tmpl.Runtime.APIPath,
		"optimization":    plat.Build.OptimizationLabel,
		"memory_limit_mb": memLimitMB,
		"model_size_gb":   quantInfo.SizeGB,
		"test_passed":     testPassed,
		"arm_features":    plat.Build.OptimizationLabel,
	}

	// Include benchmark comparison if both runs succeeded.
	if benchmarkComparison != nil {
		resultMap["benchmark_comparison"] = map[string]interface{}{
			"tokens_per_second_improvement_pct": benchmarkComparison.TokensSecImprovement,
			"ttft_improvement_pct":              benchmarkComparison.TTFTImprovement,
			"memory_difference_bytes":           benchmarkComparison.MemoryDifferenceBytes,
			"optimized": map[string]interface{}{
				"median_tokens_per_second":   optimizedResult.MedianTokensSec,
				"median_ttft_ms":             optimizedResult.MedianTTFT.Milliseconds(),
				"peak_memory_bytes":          optimizedResult.PeakMemoryBytes,
				"tokens_per_second_range":    optimizedResult.TokensSecRange,
				"ttft_range_ms":              optimizedResult.TTFTRange,
				"variance_detected":          optimizedResult.VarianceDetected,
				"actual_runs":                optimizedResult.ActualRuns,
				"performix":                  optimizedResult.Performix,
			},
			"generic": map[string]interface{}{
				"median_tokens_per_second":   baselineResult.MedianTokensSec,
				"median_ttft_ms":             baselineResult.MedianTTFT.Milliseconds(),
				"peak_memory_bytes":          baselineResult.PeakMemoryBytes,
				"tokens_per_second_range":    baselineResult.TokensSecRange,
				"ttft_range_ms":              baselineResult.TTFTRange,
				"variance_detected":          baselineResult.VarianceDetected,
				"actual_runs":                baselineResult.ActualRuns,
				"performix":                  baselineResult.Performix,
			},
		}
	} else if baselineResult != nil {
		// Fallback: only baseline available.
		resultMap["baseline_benchmark"] = map[string]interface{}{
			"median_tokens_per_second": baselineResult.MedianTokensSec,
			"median_ttft_ms":           baselineResult.MedianTTFT.Milliseconds(),
			"peak_memory_bytes":        baselineResult.PeakMemoryBytes,
			"total_duration_ms":        baselineResult.TotalDuration.Milliseconds(),
		}
	}

	output, _ := json.Marshal(resultMap)
	result.Status = "success"
	result.Output = string(output)

	slog.Info("deploy_inference complete",
		"template", tmpl.ID,
		"container", containerID,
		"quant", quantKey,
		"endpoint", endpointURL,
	)
	return nil
}

// downloadModel fetches the GGUF file from HuggingFace using a temporary container.
func (e *Executor) downloadModel(ctx context.Context, cmdID, volName string, tmpl *infer.Template, quantInfo *infer.QuantInfo) error {
	if e.docker == nil {
		return fmt.Errorf("docker client not configured")
	}

	downloaderName := fmt.Sprintf("infer-dl-%s", cmdID[:8])

	// Python script to download from HuggingFace
	downloadScript := fmt.Sprintf(`
import os
from huggingface_hub import hf_hub_download
path = hf_hub_download(
    repo_id="%s",
    filename="%s",
    local_dir="/models"
)
# Write marker file
with open("/models/.download_complete", "w") as f:
    f.write(path)
print(f"Downloaded to: {path}")
`, tmpl.Model.Source.Repository, quantInfo.FileName)

	id, err := e.docker.CreateContainer(ctx, docker.CreateContainerOpts{
		Name:  downloaderName,
		Image: downloaderImage,
		Env: []string{
			"PIP_QUIET=1",
		},
		VolumeMounts: []docker.VolumeMount{
			{Name: volName, MountPath: "/models"},
		},
		Labels: map[string]string{
			"yourplatform.type": "inference-downloader",
		},
		RestartPolicy: "no",
	})
	if err != nil {
		return fmt.Errorf("create downloader container: %w", err)
	}

	if err := e.docker.StartContainerWithWait(ctx, id, 10*time.Second); err != nil {
		return fmt.Errorf("start downloader: %w", err)
	}

	// Execute the download command: pip install huggingface_hub then run script
	cmd := []string{"sh", "-c", fmt.Sprintf("pip install -q huggingface_hub && python3 -c '%s'", downloadScript)}
	if _, err := e.docker.ExecInContainer(ctx, id, cmd); err != nil {
		_ = e.docker.StopContainerGraceful(ctx, id)
		_ = e.docker.RemoveContainer(ctx, id)
		// Cross-phase error contract: the download resumes from where it
		// stopped (huggingface_hub reuses the partial file on retry).
		return fmt.Errorf(
			"The model download was interrupted. This is usually a temporary network issue — the download will resume from where it stopped if you try again. (%w)",
			err,
		)
	}

	_ = e.docker.StopContainerGraceful(ctx, id)
	_ = e.docker.RemoveContainer(ctx, id)

	slog.Info("model download complete", "file", quantInfo.FileName)
	return nil
}

// waitForInferenceReady polls the inference server's /health endpoint until
// it responds or the timeout is reached.
func (e *Executor) waitForInferenceReady(ctx context.Context, containerID string, port int, timeout time.Duration) error {
	if e.docker == nil {
		return nil
	}

	deadline := time.Now().Add(timeout)
	pollInterval := 10 * time.Second

	for time.Now().Before(deadline) {
		cmd := []string{"curl", "-sf", fmt.Sprintf("http://localhost:%d/health", port)}
		output, err := e.docker.ExecInContainer(ctx, containerID, cmd)
		if err == nil && strings.TrimSpace(output) != "" {
			slog.Info("inference server is healthy", "output", strings.TrimSpace(output))
			return nil
		}

		slog.Debug("waiting for inference server...", "container", containerID)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}

	// Cross-phase error contract: name the symptom, give the cause, tell the
	// user what to do next.
	return fmt.Errorf(
		"The inference server took too long to start (%d minutes). This can happen on servers with slow disk I/O. Try again, or check your server's disk performance.",
		int(timeout.Minutes()),
	)
}

// testInferenceEndpoint sends a simple test prompt to verify the full path works.
func (e *Executor) testInferenceEndpoint(ctx context.Context, port int, apiKey string) error {
	if e.docker == nil {
		return nil
	}

	testPayload := `{"messages":[{"role":"user","content":"Say hello in one word."}],"max_tokens":10}`
	cmd := []string{"curl", "-sf",
		"-X", "POST",
		"-H", "Content-Type: application/json",
		"-H", fmt.Sprintf("Authorization: Bearer %s", apiKey),
		"-d", testPayload,
		fmt.Sprintf("http://localhost:%d/v1/chat/completions", port),
	}

	output, err := e.docker.ExecInContainer(ctx, fmt.Sprintf("test-infer-%d", time.Now().UnixNano()), cmd)
	if err != nil {
		return fmt.Errorf("test request failed: %w", err)
	}

	if !strings.Contains(output, "choices") && !strings.Contains(output, "content") {
		return fmt.Errorf("unexpected response: %s", output)
	}

	slog.Info("endpoint test passed", "response_preview", truncate(output, 100))
	return nil
}

// generateAPIKey creates a random 32-byte hex-encoded API key.
func generateAPIKey() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// hashAPIKey returns the SHA-256 hex digest of an API key.
func hashAPIKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}

// truncate shortens a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
