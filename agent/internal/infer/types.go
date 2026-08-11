package infer

// Template describes everything needed to deploy a specific model.
// Templates are stored as JSON files in the embedded filesystem.
type Template struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Category    string           `json:"category"`
	Model       ModelSpec        `json:"model"`
	Runtime     RuntimeSpec      `json:"runtime"`
	Resources   ResourceSpec     `json:"resources"`
	Benchmark   BenchmarkSpec    `json:"benchmark"`
}

// ModelSpec describes the model to download and run.
type ModelSpec struct {
	Family            string            `json:"family"`              // "Llama 3.1"
	Size              string            `json:"size"`                // "8B"
	Variant           string            `json:"variant"`             // "Instruct"
	Source            ModelSource       `json:"source"`              // where to download from
	DefaultQuant      string            `json:"default_quant"`      // "Q4_K_M"
	FallbackQuants    []string          `json:"fallback_quants"`    // ["Q3_K_M", "Q2_K"]
	Format            string            `json:"format"`              // "GGUF"
	Quantizations     map[string]QuantInfo `json:"quantizations"`   // size info per quant
}

// ModelSource describes where to fetch the model weights.
type ModelSource struct {
	Repository string `json:"repository"` // "bartowski/Meta-Llama-3.1-8B-Instruct-GGUF"
	Registry   string `json:"registry"`   // "huggingface"
}

// QuantInfo holds size and memory info for a specific quantization.
type QuantInfo struct {
	FileName    string  `json:"file_name"`     // "Meta-Llama-3.1-8B-Instruct-Q4_K_M.gguf"
	SizeGB      float64 `json:"size_gb"`       // 4.9
	MinRAMGB    float64 `json:"min_ram_gb"`    // 6.0
}

// RuntimeSpec describes how to run the inference server.
type RuntimeSpec struct {
	Engine          string `json:"engine"`           // "llama.cpp"
	ServerMode      string `json:"server_mode"`     // "--server"
	InternalPort    int    `json:"internal_port"`    // 8080
	APIFormat      string `json:"api_format"`       // "openai-compatible"
	APIPath        string `json:"api_path"`         // "/v1/chat/completions"
	ContextWindow   int    `json:"context_window"`  // 4096
	MaxConcurrent   int    `json:"max_concurrent"`  // 4
}

// ResourceSpec describes hardware requirements.
type ResourceSpec struct {
	MinRAMGB       float64 `json:"min_ram_gb"`        // 6.0
	MinDiskGB      float64 `json:"min_disk_gb"`       // 5.0
	RecommendedRAMGB float64 `json:"recommended_ram_gb"` // 10.0
	PreferredArch  string  `json:"preferred_arch"`    // "arm64"
}

// BenchmarkSpec defines how to benchmark this model.
type BenchmarkSpec struct {
	Prompts     []string `json:"prompts"`      // fixed prompt set
	WarmupRuns  int      `json:"warmup_runs"`  // 1
	MeasuredRuns int     `json:"measured_runs"` // 2
	Metrics     []string `json:"metrics"`      // ["tokens_per_sec", "ttft", "memory_mb"]
}
