package docker

import (
	"fmt"

	"github.com/docker/docker/api/types/container"
)

// ResourceLimits defines memory and CPU constraints for a container.
// All values are in bytes for memory and CPU shares for CPU.
type ResourceLimits struct {
	MemorySoft    int64 // Soft limit (memory reservation) — Docker tries to keep under this
	MemoryHard    int64 // Hard limit — container is OOM-killed if exceeded
	MemorySwap    int64 // Swap limit — 0 disables swap
	CPUShares     int64 // CPU shares (default 1024, 512 = half priority)
	CPUQuota      int64 // CPU quota in microseconds (0 = no hard limit)
	CPUPeriod     int64 // CPU period in microseconds (default 100000)
}

// ToDockerResources converts the limits to Docker's container.Resources format.
func (rl *ResourceLimits) ToDockerResources() container.Resources {
	return container.Resources{
		Memory:            rl.MemoryHard,
		MemoryReservation: rl.MemorySoft,
		MemorySwap:        rl.MemorySwap,
		CPUShares:         rl.CPUShares,
		CPUQuota:          rl.CPUQuota,
		CPUPeriod:         rl.CPUPeriod,
	}
}

// ---------------------------------------------------------------------------
// Default limit profiles per container type
// ---------------------------------------------------------------------------

// DefaultResourceLimits returns the appropriate resource limits for a container type.
//
//	App:      256MB soft, 512MB hard, no swap, 512 CPU shares
//	Postgres: 512MB soft, 1GB hard, no swap, 512 CPU shares
//	MySQL:    512MB soft, 1GB hard, no swap, 512 CPU shares
//	Redis:    128MB soft, 256MB hard, no swap, 512 CPU shares
func DefaultResourceLimits(ct ContainerType) *ResourceLimits {
	switch ct {
	case ContainerTypeApp:
		return &ResourceLimits{
			MemorySoft: 256 * mb,
			MemoryHard: 512 * mb,
			MemorySwap: 0, // disable swap — deadly slow on small VPS
			CPUShares:  512,
		}
	case ContainerTypePostgres, ContainerTypeMySQL:
		return &ResourceLimits{
			MemorySoft: 512 * mb,
			MemoryHard: 1 * gb,
			MemorySwap: 0,
			CPUShares:  512,
		}
	case ContainerTypeRedis:
		return &ResourceLimits{
			MemorySoft: 128 * mb,
			MemoryHard: 256 * mb,
			MemorySwap: 0,
			CPUShares:  512,
		}
	default:
		return DefaultResourceLimits(ContainerTypeApp)
	}
}

// byte multipliers
const (
	kb = 1024
	mb = 1024 * kb
	gb = 1024 * mb
)

// ValidateResourceLimits checks that the limits are within sane bounds
// given the available server memory.
//
//   - Cannot set limits higher than available server RAM
//   - Must leave at least 512MB for the system
func ValidateResourceLimits(rl *ResourceLimits, totalRAMMB int64) error {
	totalRAM := totalRAMMB * mb

	if rl.MemoryHard > totalRAM-512*mb {
		return fmt.Errorf(
			"memory limit of %s exceeds available capacity (need at least 512MB for system, have %dMB total)",
			formatBytes(rl.MemoryHard), totalRAMMB)
	}

	if rl.MemorySoft > rl.MemoryHard {
		return fmt.Errorf(
			"soft memory limit (%s) cannot exceed hard memory limit (%s)",
			formatBytes(rl.MemorySoft), formatBytes(rl.MemoryHard))
	}

	return nil
}

// formatBytes returns a human-readable string for byte values.
func formatBytes(b int64) string {
	switch {
	case b >= gb:
		return fmt.Sprintf("%.1fGB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.0fMB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.0fKB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%dB", b)
	}
}
