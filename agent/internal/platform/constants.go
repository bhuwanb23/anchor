package platform

// InferImageBase is the base Docker image tag for the Infer server.
const InferImageBase = "ghcr.io/yourname/infer"

// Model file size estimates (GB).
const (
	modelSize7BQ4KM = 4.1
	modelSize7BQ2K  = 2.9
	modelSize3BQ4KM = 1.9
	modelBufferGB   = 1.0 // headroom for logs, temp files
)
