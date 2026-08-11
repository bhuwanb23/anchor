//go:build !linux

package platform

import "runtime"

// detectPlatform returns a minimal result on non-Linux systems.
// The agent only runs on Linux in production; this exists for
// development and testing on macOS/Windows.
func detectPlatform() *PlatformInfo {
	return &PlatformInfo{
		IsArm64:          runtime.GOARCH == "arm64",
		RecommendedBuild: "arm64",
		RecommendedQuant: "Q4_K_S",
	}
}
