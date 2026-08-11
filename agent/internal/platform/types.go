package platform

// PlatformInfo holds the full server capability profile.
// Determined once at startup and sent to the control plane on connect.
type PlatformInfo struct {
	IsArm64              bool         `json:"is_arm64"`
	CPU                  CPUInfo      `json:"cpu"`
	Features             CPUFeatures  `json:"features"`
	Memory               MemoryInfo   `json:"memory"`
	Disk                 DiskInfo     `json:"disk"`
	Build                BuildSelection `json:"build"`
	Readiness            Readiness    `json:"readiness"`
}

// CPUInfo describes the physical CPU on the server.
type CPUInfo struct {
	ModelName           string  `json:"model_name"`
	VendorID            string  `json:"vendor_id,omitempty"`
	Microarchitecture   string  `json:"microarchitecture,omitempty"`
	CPUPartCode         string  `json:"cpu_part_code,omitempty"`
	CloudProviderHint   string  `json:"cloud_provider_hint,omitempty"`
	DetectionConfidence string  `json:"detection_confidence"` // "high" or "low"
	Cores               int     `json:"cores"`
	Mhz                 float64 `json:"mhz,omitempty"`
}

// CPUFeatures tracks which ARM instruction set extensions are available.
type CPUFeatures struct {
	Dotprod bool `json:"dotprod"`
	I8mm    bool `json:"i8mm"`
	Sve     bool `json:"sve"`
	Sve2    bool `json:"sve2"`
	Bf16    bool `json:"bf16"`
}

// BuildSelection holds the chosen Docker image and optimization metadata.
type BuildSelection struct {
	ImageTag         string `json:"image_tag"`          // e.g. "ghcr.io/yourname/infer:arm64-i8mm-sve"
	OptimizationLabel string `json:"optimization_label"` // e.g. "Full (SVE + I8MM)"
	ExpectedHardware string `json:"expected_hardware"`  // e.g. "Graviton 3"
}

// MemoryInfo reports physical memory and model/quantization recommendations.
type MemoryInfo struct {
	TotalMB           int64  `json:"total_mb"`
	AvailableMB       int64  `json:"available_mb"`
	AvailableGB       float64 `json:"available_gb"`
	RecommendedModel  string `json:"recommended_model"`   // "7b", "3b", or ""
	RecommendedQuant  string `json:"recommended_quantization"` // "Q4_K_M", "Q3_K_M", etc.
	MemoryNote        string `json:"memory_note,omitempty"`
	MemorySufficient  bool   `json:"memory_sufficient"`
}

// DiskInfo reports storage available for model weights.
type DiskInfo struct {
	TotalGB         float64 `json:"total_gb"`
	AvailableGB     float64 `json:"available_gb"`
	ModelRequiredGB float64 `json:"model_required_gb"` // estimated model file size
	DiskSufficient  bool    `json:"disk_sufficient"`
	DiskNote        string  `json:"disk_note,omitempty"`
}

// Readiness summarizes whether the server can run inference.
type Readiness struct {
	CanRunInference bool   `json:"can_run_inference"`
	BlockReason     string `json:"block_reason,omitempty"`
	Notes           []string `json:"notes,omitempty"`
}
