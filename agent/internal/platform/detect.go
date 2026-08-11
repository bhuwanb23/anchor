package platform

// Detect runs the full platform detection and returns a structured
// result describing the server's CPU, features, memory, disk, and
// recommended llama.cpp build configuration.
//
// The result is designed to be sent to the control plane once on
// agent connect and stored for later use by the Infer dashboard.
func Detect() *PlatformInfo {
	return detectPlatform()
}
