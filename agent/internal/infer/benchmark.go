package infer

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
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
const measuredRuns = 2

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
	BuildLabel       string         `json:"build_label"`       // "optimized" or "generic"
	ImageTag         string         `json:"image_tag"`
	Prompts          []PromptResult `json:"prompts"`
	MedianTokensSec  float64        `json:"median_tokens_per_second"`
	MedianTTFT       time.Duration  `json:"median_time_to_first_token_ms"`
	PeakMemoryBytes  uint64         `json:"peak_memory_bytes"`
	TotalDuration    time.Duration  `json:"total_duration_ms"`
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

	progress("Starting benchmark container...")
	log.Info("benchmark: starting container", "image", opts.Image, "name", containerName)

	// Start the container.
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

	// Run prompts: warmup runs + measured runs.
	totalRuns := warmupRuns + measuredRuns
	var measured []PromptResult

	for i, prompt := range benchmarkPrompts {
		for run := 0; run < totalRuns; run++ {
			isWarmup := run < warmupRuns
			runLabel := "measured"
			if isWarmup {
				runLabel = "warmup"
			}

			progress(fmt.Sprintf("Prompt %d/4, run %d/%d (%s)...", i+1, run+1, totalRuns, runLabel))

			pr, err := runPrompt(ctx, endpoint, prompt, i, isWarmup)
			if err != nil {
				log.Warn("benchmark: prompt failed", "prompt", i, "run", run, "error", err)
				continue
			}

			if !isWarmup {
				measured = append(measured, *pr)
			}

			// Brief pause between requests.
			time.Sleep(500 * time.Millisecond)
		}
	}

	// Calculate aggregate metrics.
	result.Prompts = measured
	result.MedianTokensSec = medianTokensPerSecond(measured)
	result.MedianTTFT = medianTTFT(measured)

	// Sample peak memory.
	progress("Collecting peak memory usage...")
	stats, err := opts.Docker.GetContainerStats(ctx, containerID)
	if err == nil {
		result.PeakMemoryBytes = stats.RAMUsedBytes
	}

	progress(fmt.Sprintf("Benchmark complete. Median: %.1f tok/s, TTFT: %dms",
		result.MedianTokensSec, result.MedianTTFT.Milliseconds()))

	return result, nil
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

		// Skip empty lines and non-data lines.
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

		// Record time of first token with content.
		if firstTokenTime.IsZero() && choice.Delta.Content != "" {
			firstTokenTime = time.Now()
		}

		// Count tokens (approximate: 1 chunk ≈ 1 token).
		if choice.Delta.Content != "" {
			totalTokens++
		}

		if chunk.Usage != nil {
			lastUsage = chunk.Usage
		}
	}

	totalTime := time.Since(start)

	// If we got usage info from the server, use the accurate count.
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

// medianFloat64 returns the median of a float64 slice.
func medianFloat64(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	// Simple sort for small slices.
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
