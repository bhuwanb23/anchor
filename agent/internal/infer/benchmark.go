package infer

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/yourname/yourplatform/agent/internal/docker"
)

// ---------------------------------------------------------------------------
// Benchmark prompt set
// ---------------------------------------------------------------------------

// benchmarkPrompts is the fixed set of 4 prompts used for every benchmark run.
// Identical prompts make results comparable across different runs and servers.
var benchmarkPrompts = []string{
	"What is the capital of France?",
	"Explain the difference between a REST API and a GraphQL API in two paragraphs.",
	"Write a detailed step-by-step tutorial on how to deploy a web application to a cloud server. Include at least 10 steps with detailed explanations for each step.",
	"Write a Python function that takes a list of integers and returns the top 3 most frequent elements, with their counts.",
}

// warmupRuns is the number of times each prompt is sent before measurement begins.
const warmupRuns = 1

// measuredRuns is the number of times each prompt is sent after warmup.
// If variance between the two exceeds 20%, a third run is performed.
const measuredRuns = 2

// varianceThreshold is the maximum allowed coefficient of variation (as a percentage)
// between measured runs. If exceeded, a third run is triggered.
const varianceThreshold = 20.0

// stabilizeWait is how long to wait after pausing other containers before benchmarking.
const stabilizeWait = 10 * time.Second

// ---------------------------------------------------------------------------
// Benchmark result types
// ---------------------------------------------------------------------------

// PromptResult holds the benchmark result for a single prompt run.
type PromptResult struct {
	Prompt           string        `json:"prompt"`
	Index            int           `json:"index"`
	Warmup           bool          `json:"warmup"`
	TimeToFirstToken time.Duration `json:"time_to_first_token_ms"`
	TotalTokens      int           `json:"total_tokens"`
	TotalTime        time.Duration `json:"total_time_ms"`
	TokensPerSecond  float64       `json:"tokens_per_second"`
}

// BenchmarkResult holds the full benchmark result for a deployment.
type BenchmarkResult struct {
	BuildLabel        string         `json:"build_label"`        // "optimized" or "generic"
	ImageTag          string         `json:"image_tag"`
	Prompts           []PromptResult `json:"prompts"`
	MedianTokensSec   float64        `json:"median_tokens_per_second"`
	MedianTTFT        time.Duration  `json:"median_time_to_first_token_ms"`
	PeakMemoryBytes   uint64         `json:"peak_memory_bytes"`
	TotalDuration     time.Duration  `json:"total_duration_ms"`
	// Variance info
	TokensSecRange    [2]float64     `json:"tokens_per_second_range"`   // [min, max] of measured runs
	TTFTRange         [2]int64       `json:"ttft_range_ms"`             // [min, max] of measured runs
	VarianceDetected  bool           `json:"variance_detected"`         // true if >20% variance triggered 3rd run
	ActualRuns        int            `json:"actual_runs"`               // number of measured runs (2 or 3)
	// Arm Performix results (nil if not available)
	Performix         *PerformixResult `json:"performix,omitempty"`
}

// PerformixResult holds results from Arm Performix benchmarking.
type PerformixResult struct {
	TokensPerSecond  float64       `json:"tokens_per_second"`
	TimeToFirstToken time.Duration `json:"time_to_first_token_ms"`
	PeakMemoryBytes  uint64        `json:"peak_memory_bytes"`
	TotalDuration    time.Duration `json:"total_duration_ms"`
	RawOutput        string        `json:"raw_output,omitempty"`
}

// BenchmarkComparison holds a side-by-side comparison of generic vs optimized builds.
type BenchmarkComparison struct {
	Optimized         *BenchmarkResult `json:"optimized"`
	Generic           *BenchmarkResult `json:"generic"`
	// Calculated improvements
	TokensSecImprovement   float64 `json:"tokens_per_second_improvement_pct"`
	TTFTImprovement        float64 `json:"ttft_improvement_pct"`         // positive = faster
	MemoryDifferenceBytes  int64   `json:"memory_difference_bytes"`
}

// ---------------------------------------------------------------------------
// Benchmark runner
// ---------------------------------------------------------------------------

// BenchmarkOpts configures a benchmark run.
type BenchmarkOpts struct {
	Image       string            // Docker image to benchmark
	VolumeName  string            // Model volume name
	VolumePath  string            // Mount path inside container
	ModelFile   string            // Model file path (relative to volume)
	Template    *Template         // Template with model/runtime specs
	Docker      *docker.Client    // Docker client
	Logger      *slog.Logger      // Logger
	OnProgress  func(msg string)  // Progress callback
	// Container pausing
	ManagedContainers []string    // Container IDs to pause during benchmark (non-empty = pause)
	PerformixImage    string      // Performix Docker image (empty = skip Performix)
}

// RunBenchmark starts a container with the given image, waits for it to be ready,
// runs the benchmark prompt set, and returns the results.
func RunBenchmark(ctx context.Context, opts BenchmarkOpts) (*BenchmarkResult, error) {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}

	result := &BenchmarkResult{
		BuildLabel: opts.Image,
		ImageTag:   opts.Image,
	}

	// Generate random container name to avoid conflicts.
	suffix := fmt.Sprintf("%06x", rand.Intn(0xffffff))
	containerName := "bench_" + suffix

	progress := func(msg string) {
		if opts.OnProgress != nil {
			opts.OnProgress(msg)
		}
	}

	// ─── Step 1: Pause non-essential containers ──────────────────────
	var stoppedContainers []string
	if len(opts.ManagedContainers) > 0 {
		progress(fmt.Sprintf("Pausing %d other containers for accurate benchmarking...", len(opts.ManagedContainers)))
		for _, cid := range opts.ManagedContainers {
			if err := opts.Docker.StopContainerGraceful(ctx, cid); err != nil {
				log.Warn("benchmark: failed to pause container", "id", cid[:12], "error", err)
				continue
			}
			stoppedContainers = append(stoppedContainers, cid)
			log.Info("benchmark: paused container", "id", cid[:12])
		}

		progress(fmt.Sprintf("Waiting %s for resource stabilization...", stabilizeWait))
		time.Sleep(stabilizeWait)
	}

	// Ensure containers are restarted when done.
	defer func() {
		if len(stoppedContainers) > 0 {
			progress("Restarting paused containers...")
			for _, cid := range stoppedContainers {
				if err := opts.Docker.StartContainer(ctx, cid); err != nil {
					log.Warn("benchmark: failed to restart container", "id", cid[:12], "error", err)
				} else {
					log.Info("benchmark: restarted container", "id", cid[:12])
				}
			}
		}
	}()

	// ─── Step 2: Start benchmark container ───────────────────────────
	progress("Starting benchmark container...")
	log.Info("benchmark: starting container", "image", opts.Image, "name", containerName)

	containerID, err := opts.Docker.CreateContainer(ctx, docker.CreateContainerOpts{
		Name:  containerName,
		Image: opts.Image,
		Env: []string{
			"MODEL=/models/" + opts.ModelFile,
			"HOST=0.0.0.0",
			"PORT=8080",
		},
		ExposedPorts: nat.PortSet{
			"8080/tcp": struct{}{},
		},
		VolumeMounts: []docker.VolumeMount{
			{
				Name:      opts.VolumeName,
				MountPath: "/models",
				ReadOnly:  true,
			},
		},
		Labels: map[string]string{
			"managed-by": "yourplatform",
			"purpose":    "benchmark",
		},
		RestartPolicy: "no",
	})
	if err != nil {
		return nil, fmt.Errorf("create benchmark container: %w", err)
	}

	defer func() {
		progress("Cleaning up benchmark container...")
		_ = opts.Docker.StopContainerGraceful(ctx, containerID)
		_ = opts.Docker.RemoveContainer(ctx, containerID)
	}()

	if err := opts.Docker.StartContainer(ctx, containerID); err != nil {
		return nil, fmt.Errorf("start benchmark container: %w", err)
	}

	// Get container port mapping.
	info, err := opts.Docker.InspectContainer(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("inspect benchmark container: %w", err)
	}

	port := "8080"
	if bindings, ok := info.NetworkSettings.Ports["8080/tcp"]; ok && len(bindings) > 0 {
		port = bindings[0].HostPort
	}

	endpoint := "http://127.0.0.1:" + port

	// Wait for server to be ready.
	progress("Waiting for model to load (this may take 30-90 seconds)...")
	log.Info("benchmark: waiting for server", "endpoint", endpoint)

	readyCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	if err := waitForReady(readyCtx, endpoint); err != nil {
		return nil, fmt.Errorf("server not ready: %w", err)
	}

	progress("Server ready. Running benchmark prompts...")

	// ─── Step 3: Run prompts with variance detection ─────────────────
	measured, varianceDetected, err := runMeasuredPrompts(ctx, endpoint, progress, log)
	if err != nil {
		return nil, err
	}
	result.Prompts = measured
	result.VarianceDetected = varianceDetected
	result.ActualRuns = len(measured) / len(benchmarkPrompts)

	// Calculate aggregate metrics.
	result.MedianTokensSec = medianTokensPerSecond(measured)
	result.MedianTTFT = medianTTFT(measured)

	// Calculate ranges.
	result.TokensSecRange = tokensSecRange(measured)
	result.TTFTRange = ttftRange(measured)

	// Sample peak memory.
	progress("Collecting peak memory usage...")
	stats, err := opts.Docker.GetContainerStats(ctx, containerID)
	if err == nil {
		result.PeakMemoryBytes = stats.RAMUsedBytes
	}

	// ─── Step 4: Run Arm Performix (if available) ───────────────────
	if opts.PerformixImage != "" {
		progress("Running Arm Performix benchmark...")
		performixResult, err := runPerformix(ctx, opts, containerID, port, progress, log)
		if err != nil {
			log.Warn("benchmark: Performix failed (continuing with custom results)", "error", err)
			progress("Arm Performix unavailable, using custom benchmark results")
		} else {
			result.Performix = performixResult
			log.Info("benchmark: Performix complete",
				"tok_sec", performixResult.TokensPerSecond,
				"ttft_ms", performixResult.TimeToFirstToken.Milliseconds(),
			)
		}
	}

	progress(fmt.Sprintf("Benchmark complete. Median: %.1f tok/s, TTFT: %dms",
		result.MedianTokensSec, result.MedianTTFT.Milliseconds()))

	return result, nil
}

// ---------------------------------------------------------------------------
// Measured prompts with variance detection
// ---------------------------------------------------------------------------

// runMeasuredPrompts runs the benchmark prompts with warmup + measured runs.
// If variance exceeds the threshold, a third run is performed.
func runMeasuredPrompts(ctx context.Context, endpoint string, progress func(string), log *slog.Logger) ([]PromptResult, bool, error) {
	var allMeasured []PromptResult
	varianceDetected := false

	for i, prompt := range benchmarkPrompts {
		// Phase 1: Warmup run (discard).
		progress(fmt.Sprintf("Prompt %d/4, warmup run...", i+1))
		_, err := runPrompt(ctx, endpoint, prompt, i, true)
		if err != nil {
			log.Warn("benchmark: warmup prompt failed", "prompt", i, "error", err)
			continue
		}
		time.Sleep(500 * time.Millisecond)

		// Phase 2: First measured run.
		progress(fmt.Sprintf("Prompt %d/4, measured run 1/2...", i+1))
		pr1, err := runPrompt(ctx, endpoint, prompt, i, false)
		if err != nil {
			log.Warn("benchmark: measured prompt 1 failed", "prompt", i, "error", err)
			continue
		}
		allMeasured = append(allMeasured, *pr1)
		time.Sleep(500 * time.Millisecond)

		// Phase 3: Second measured run.
		progress(fmt.Sprintf("Prompt %d/4, measured run 2/2...", i+1))
		pr2, err := runPrompt(ctx, endpoint, prompt, i, false)
		if err != nil {
			log.Warn("benchmark: measured prompt 2 failed", "prompt", i, "error", err)
			continue
		}
		allMeasured = append(allMeasured, *pr2)

		// Phase 4: Variance check — run third if needed.
		if pr1.TokensPerSecond > 0 && pr2.TokensPerSecond > 0 {
			cv := coefficientOfVariation(pr1.TokensPerSecond, pr2.TokensPerSecond)
			if cv > varianceThreshold {
				varianceDetected = true
				log.Warn("benchmark: high variance detected, running third measurement",
					"prompt", i, "cv_pct", fmt.Sprintf("%.1f", cv))
				progress(fmt.Sprintf("Prompt %d/4, high variance (%.0f%%), running 3rd measurement...", i+1, cv))

				pr3, err := runPrompt(ctx, endpoint, prompt, i, false)
				if err != nil {
					log.Warn("benchmark: third measurement failed", "prompt", i, "error", err)
				} else {
					allMeasured = append(allMeasured, *pr3)
				}
			}
		}

		time.Sleep(500 * time.Millisecond)
	}

	return allMeasured, varianceDetected, nil
}

// coefficientOfVariation returns the CV (in percentage) of two values.
func coefficientOfVariation(a, b float64) float64 {
	if a == 0 && b == 0 {
		return 0
	}
	mean := (a + b) / 2
	if mean == 0 {
		return 0
	}
	stddev := math.Sqrt(((a-mean)*(a-mean) + (b-mean)*(b-mean)) / 2)
	return (stddev / mean) * 100
}

// ---------------------------------------------------------------------------
// Arm Performix runner
// ---------------------------------------------------------------------------

const (
	performixImageDefault = "arm/perf:latest"
	performixTimeout      = 10 * time.Minute
)

// runPerformix pulls and runs the Arm Performix container against the inference server.
func runPerformix(ctx context.Context, opts BenchmarkOpts, serverContainerID, serverPort string, progress func(string), log *slog.Logger) (*PerformixResult, error) {
	image := opts.PerformixImage
	if image == "" {
		image = performixImageDefault
	}

	// Pull the Performix image.
	progress("Pulling Arm Performix image...")
	pullCtx, pullCancel := context.WithTimeout(ctx, 2*time.Minute)
	defer pullCancel()

	_, err := opts.Docker.PullImage(pullCtx, image, nil)
	if err != nil {
		return nil, fmt.Errorf("pull performix image: %w", err)
	}

	// Get the server container's IP on the Docker network so Performix can reach it.
	serverInfo, err := opts.Docker.InspectContainer(ctx, serverContainerID)
	if err != nil {
		return nil, fmt.Errorf("inspect server container: %w", err)
	}

	// Find the network IP.
	var serverIP string
	for _, net := range serverInfo.NetworkSettings.Networks {
		if net.IPAddress != "" {
			serverIP = net.IPAddress
			break
		}
	}
	if serverIP == "" {
		serverIP = "host.docker.internal"
	}

	// Run Performix container.
	suffix := fmt.Sprintf("%06x", rand.Intn(0xffffff))
	performixName := "performix_" + suffix

	performixCtx, performixCancel := context.WithTimeout(ctx, performixTimeout)
	defer performixCancel()

	containerID, err := opts.Docker.CreateContainer(performixCtx, docker.CreateContainerOpts{
		Name:  performixName,
		Image: image,
		Env: []string{
			fmt.Sprintf("TARGET_URL=http://%s:%s", serverIP, serverPort),
			"MODEL_NAME=local",
		},
		Labels: map[string]string{
			"managed-by": "yourplatform",
			"purpose":    "performix-benchmark",
		},
		RestartPolicy: "no",
	})
	if err != nil {
		return nil, fmt.Errorf("create performix container: %w", err)
	}

	defer func() {
		_ = opts.Docker.StopContainerGraceful(ctx, containerID)
		_ = opts.Docker.RemoveContainer(ctx, containerID)
	}()

	if err := opts.Docker.StartContainer(performixCtx, containerID); err != nil {
		return nil, fmt.Errorf("start performix container: %w", err)
	}

	progress("Performix running (this may take several minutes)...")

	// Wait for Performix to finish by monitoring container status.
	deadline := time.Now().Add(performixTimeout)
	for time.Now().Before(deadline) {
		select {
		case <-performixCtx.Done():
			return nil, performixCtx.Err()
		case <-time.After(5 * time.Second):
		}

		stats, err := opts.Docker.GetContainerStats(performixCtx, containerID)
		if err != nil {
			// Container may have exited.
			break
		}
		_ = stats
	}

	// Collect Performix output.
	output, err := opts.Docker.GetContainerLogsTail(performixCtx, containerID, 200)
	if err != nil {
		return nil, fmt.Errorf("get performix logs: %w", err)
	}

	// Parse Performix output for metrics.
	result := parsePerformixOutput(output)
	if result == nil {
		return nil, fmt.Errorf("could not parse performix output")
	}
	result.RawOutput = output

	return result, nil
}

// parsePerformixOutput extracts metrics from Performix container logs.
// Performix output format varies by version; this parser handles common patterns.
func parsePerformixOutput(output string) *PerformixResult {
	result := &PerformixResult{}

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Look for tokens/sec patterns.
		// Common formats:
		//   "Tokens/sec: 31.5"
		//   "tok/s: 31.5"
		//   "throughput: 31.5 tokens/s"
		lower := strings.ToLower(line)

		if idx := strings.Index(lower, "tokens/sec"); idx >= 0 || strings.Index(lower, "tok/s") >= 0 || strings.Index(lower, "tokens/s") >= 0 {
			if val := extractFloat(line); val > 0 {
				result.TokensPerSecond = val
			}
		}

		if strings.Contains(lower, "time to first token") || strings.Contains(lower, "ttft") || strings.Contains(lower, "first token latency") {
			if val := extractFloat(line); val > 0 {
				// Assume milliseconds.
				result.TimeToFirstToken = time.Duration(int64(val)) * time.Millisecond
			}
		}

		if strings.Contains(lower, "peak memory") || strings.Contains(lower, "max memory") || strings.Contains(lower, "memory usage") {
			if val := extractFloat(line); val > 0 {
				// Assume MB.
				result.PeakMemoryBytes = uint64(val * 1024 * 1024)
			}
		}
	}

	// If we didn't find any metrics, return nil.
	if result.TokensPerSecond == 0 && result.TimeToFirstToken == 0 {
		return nil
	}

	return result
}

// extractFloat pulls the first floating-point number from a line.
func extractFloat(line string) float64 {
	// Find the last colon or equals sign, then extract the number.
	var numStr string
	for _, sep := range []string{":", "=", "is"} {
		if idx := strings.LastIndex(strings.ToLower(line), sep); idx >= 0 {
			rest := strings.TrimSpace(line[idx+len(sep):])
			// Extract leading number.
			for i, ch := range rest {
				if (ch < '0' || ch > '9') && ch != '.' && ch != '-' {
					if i > 0 {
						numStr = rest[:i]
					}
					break
				}
				if i == len(rest)-1 {
					numStr = rest
				}
			}
			break
		}
	}

	if numStr == "" {
		return 0
	}
	val, _ := strconv.ParseFloat(numStr, 64)
	return val
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

// chatRequest is the OpenAI-compatible chat completion request.
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

// chatMessage is a single message in the chat completion request.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatChunk is a streaming chat completion chunk.
type chatChunk struct {
	Choices []chatChoice `json:"choices"`
	Usage   *chatUsage   `json:"usage,omitempty"`
}

// chatChoice is a single choice in a streaming chunk.
type chatChoice struct {
	Delta struct {
		Content string `json:"content"`
	} `json:"delta"`
	FinishReason *string `json:"finish_reason"`
}

// chatUsage contains token usage information.
type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// runPrompt sends a single prompt to the server and measures performance.
func runPrompt(ctx context.Context, endpoint, prompt string, index int, warmup bool) (*PromptResult, error) {
	reqBody := chatRequest{
		Model: "local",
		Messages: []chatMessage{
			{Role: "user", Content: prompt},
		},
		Stream: true,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint+"/v1/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	// Parse streaming response.
	scanner := bufio.NewScanner(resp.Body)
	var firstTokenTime time.Time
	var totalTokens int
	var lastUsage *chatUsage

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk chatChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]

		if firstTokenTime.IsZero() && choice.Delta.Content != "" {
			firstTokenTime = time.Now()
		}

		if choice.Delta.Content != "" {
			totalTokens++
		}

		if chunk.Usage != nil {
			lastUsage = chunk.Usage
		}
	}

	totalTime := time.Since(start)

	if lastUsage != nil && lastUsage.CompletionTokens > 0 {
		totalTokens = lastUsage.CompletionTokens
	}

	ttft := time.Duration(0)
	if !firstTokenTime.IsZero() {
		ttft = firstTokenTime.Sub(start)
	}

	tokensPerSecond := 0.0
	if totalTime.Seconds() > 0 {
		tokensPerSecond = float64(totalTokens) / totalTime.Seconds()
	}

	return &PromptResult{
		Prompt:           prompt,
		Index:            index,
		Warmup:           warmup,
		TimeToFirstToken: ttft,
		TotalTokens:      totalTokens,
		TotalTime:        totalTime,
		TokensPerSecond:  tokensPerSecond,
	}, nil
}

// waitForReady polls the /health endpoint until the server responds 200.
func waitForReady(ctx context.Context, endpoint string) error {
	client := &http.Client{Timeout: 5 * time.Second}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			resp, err := client.Get(endpoint + "/health")
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return nil
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Comparison helpers
// ---------------------------------------------------------------------------

// CompareResults calculates improvements between generic and optimized builds.
func CompareResults(optimized, generic *BenchmarkResult) *BenchmarkComparison {
	if optimized == nil || generic == nil {
		return nil
	}

	comp := &BenchmarkComparison{
		Optimized: optimized,
		Generic:   generic,
	}

	// Tokens/sec improvement: ((optimized - generic) / generic) × 100
	if generic.MedianTokensSec > 0 {
		comp.TokensSecImprovement = ((optimized.MedianTokensSec - generic.MedianTokensSec) / generic.MedianTokensSec) * 100
	}

	// TTFT improvement: ((generic - optimized) / generic) × 100
	// Positive means optimized is faster (lower TTFT).
	if generic.MedianTTFT > 0 {
		comp.TTFTImprovement = ((float64(generic.MedianTTFT) - float64(optimized.MedianTTFT)) / float64(generic.MedianTTFT)) * 100
	}

	// Memory difference (usually near zero, same model).
	comp.MemoryDifferenceBytes = int64(optimized.PeakMemoryBytes) - int64(generic.PeakMemoryBytes)

	return comp
}

// FormatImprovement returns a human-readable improvement string.
func FormatImprovement(pct float64, higherIsBetter bool) string {
	if higherIsBetter {
		if pct > 0 {
			return fmt.Sprintf("+%.1f%%", pct)
		}
		return fmt.Sprintf("%.1f%%", pct)
	}
	// For TTFT, positive improvement means lower is better.
	if pct > 0 {
		return fmt.Sprintf("%.1f%% faster", pct)
	}
	return fmt.Sprintf("%.1f%% slower", -pct)
}

// ---------------------------------------------------------------------------
// Aggregate helpers
// ---------------------------------------------------------------------------

// medianTokensPerSecond returns the median tokens/sec across all measured results.
func medianTokensPerSecond(results []PromptResult) float64 {
	if len(results) == 0 {
		return 0
	}
	vals := make([]float64, len(results))
	for i, r := range results {
		vals[i] = r.TokensPerSecond
	}
	return medianFloat64(vals)
}

// medianTTFT returns the median time-to-first-token across all measured results.
func medianTTFT(results []PromptResult) time.Duration {
	if len(results) == 0 {
		return 0
	}
	vals := make([]float64, len(results))
	for i, r := range results {
		vals[i] = float64(r.TimeToFirstToken.Milliseconds())
	}
	median := medianFloat64(vals)
	return time.Duration(int64(median)) * time.Millisecond
}

// tokensSecRange returns the [min, max] of tokens/sec across measured results.
func tokensSecRange(results []PromptResult) [2]float64 {
	if len(results) == 0 {
		return [2]float64{0, 0}
	}
	min, max := results[0].TokensPerSecond, results[0].TokensPerSecond
	for _, r := range results[1:] {
		if r.TokensPerSecond < min {
			min = r.TokensPerSecond
		}
		if r.TokensPerSecond > max {
			max = r.TokensPerSecond
		}
	}
	return [2]float64{min, max}
}

// ttftRange returns the [min, max] TTFT in milliseconds across measured results.
func ttftRange(results []PromptResult) [2]int64 {
	if len(results) == 0 {
		return [2]int64{0, 0}
	}
	min, max := results[0].TimeToFirstToken.Milliseconds(), results[0].TimeToFirstToken.Milliseconds()
	for _, r := range results[1:] {
		ms := r.TimeToFirstToken.Milliseconds()
		if ms < min {
			min = ms
		}
		if ms > max {
			max = ms
		}
	}
	return [2]int64{min, max}
}

// medianFloat64 returns the median of a float64 slice.
func medianFloat64(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sorted := make([]float64, len(vals))
	copy(sorted, vals)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j] < sorted[j-1]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	mid := len(sorted) / 2
	if len(sorted)%2 == 0 {
		return (sorted[mid-1] + sorted[mid]) / 2
	}
	return sorted[mid]
}

// FormatBytes formats bytes as a human-readable string.
func FormatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return strconv.FormatUint(b, 10) + " B"
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
