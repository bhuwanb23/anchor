//go:build !linux

package platform

import "runtime"

// detectPlatform returns a minimal result on non-Linux systems.
// The agent only runs on Linux in production; this exists for
// development and testing on macOS/Windows.
func detectPlatform() *PlatformInfo {
	isArm := runtime.GOARCH == "arm64"
	info := &PlatformInfo{
		IsArm64: isArm,
		Memory: MemoryInfo{
			MemorySufficient: true,
			RecommendedModel: "7b",
			RecommendedQuant: "Q4_K_M",
		},
		Disk: DiskInfo{
			DiskSufficient: true,
		},
		Readiness: Readiness{
			CanRunInference: true,
		},
	}

	if isArm {
		info.Build = BuildSelection{
			ImageTag:         inferImageBase + ":arm64",
			OptimizationLabel: "Generic (Arm64)",
			ExpectedHardware: "Arm64 server",
		}
	} else {
		info.Build = BuildSelection{
			ImageTag:         inferImageBase + ":x86_64",
			OptimizationLabel: "No Arm optimization",
			ExpectedHardware: "x86_64 server",
		}
		info.Readiness.Notes = []string{"For best results, use an Arm64 server."}
	}

	return info
}
