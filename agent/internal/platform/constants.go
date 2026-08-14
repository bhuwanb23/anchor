package platform

import "os"

// DefaultInferImageBase is the default registry/repo for Infer runtime images.
// Override at runtime with ANCHOR_INFER_IMAGE_BASE (e.g. ghcr.io/bhuwanb23/anchor-infer).
const DefaultInferImageBase = "ghcr.io/yourname/infer"

// InferImageBase is kept for compatibility; prefer ImageBase() so env overrides apply.
const InferImageBase = DefaultInferImageBase

// ImageBase returns the Docker image repository for Infer runtimes.
// Set ANCHOR_INFER_IMAGE_BASE to point agents at published images.
func ImageBase() string {
	if v := os.Getenv("ANCHOR_INFER_IMAGE_BASE"); v != "" {
		return v
	}
	return DefaultInferImageBase
}

// Model file size estimates (GB).
const (
	modelSize7BQ4KM = 4.1
	modelSize7BQ2K  = 2.9
	modelSize3BQ4KM = 1.9
	modelBufferGB   = 1.0 // headroom for logs, temp files
)
