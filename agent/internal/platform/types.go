package platform

// PlatformInfo holds the full server capability profile.
// Determined once at startup and sent to the control plane on connect.
type PlatformInfo struct {
	IsArm64           bool         `json:"is_arm64"`
	CPU               CPUInfo      `json:"cpu"`
	Features          CPUFeatures  `json:"features"`
	Memory            MemoryInfo   `json:"memory"`
	Disk              DiskInfo     `json:"disk"`
	RecommendedBuild  string       `json:"recommended_build"`
	RecommendedQuant  string       `json:"recommended_quantization"`
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

// MemoryInfo reports physical memory and a recommended model size ceiling.
type MemoryInfo struct {
	TotalMB          int64  `json:"total_mb"`
	AvailableMB      int64  `json:"available_mb"`
	RecommendedModel string `json:"recommended_max_model_size"` // "7b", "13b", "70b"
}

// DiskInfo reports storage available for model weights.
type DiskInfo struct {
	TotalGB     float64 `json:"total_gb"`
	AvailableGB float64 `json:"available_gb"`
}
