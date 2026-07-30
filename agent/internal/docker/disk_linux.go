//go:build linux

package docker

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// checkDiskPressure performs a real disk usage check on the given directory
// using unix.Statfs. Returns pressure level (0, 1, or 2) and a descriptive
// message (empty string when pressure is normal).
func checkDiskPressure(dir string) (int, string) {
	var stat unix.Statfs_t
	if err := unix.Statfs(dir, &stat); err != nil {
		slog.Warn("failed to statfs for disk pressure check", "dir", dir, "error", err)
		return 0, ""
	}

	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bavail * uint64(stat.Bsize)

	if total == 0 {
		return 0, ""
	}

	usedPercent := 100.0 - (float64(free)/float64(total))*100.0

	slog.Debug("disk pressure check",
		"dir", dir,
		"total_gb", total/(1024*1024*1024),
		"free_gb", free/(1024*1024*1024),
		"used_percent", fmt.Sprintf("%.1f%%", usedPercent),
	)

	if usedPercent >= 85.0 {
		return 2, fmt.Sprintf("Disk is %.0f%% full on %s — critical", usedPercent, dir)
	}
	if usedPercent >= 70.0 {
		return 1, fmt.Sprintf("Disk is %.0f%% full on %s — elevated", usedPercent, dir)
	}

	return 0, ""
}

// GetTotalRAMMB reads total physical RAM from /proc/meminfo on Linux.
// Returns 0 if the value cannot be determined.
func GetTotalRAMMB() int64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		slog.Warn("failed to open /proc/meminfo for RAM check", "error", err)
		return 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			var kb int64
			fmt.Sscanf(strings.TrimPrefix(line, "MemTotal:"), "%d kB", &kb)
			return kb / 1024 // convert kB to MB
		}
	}

	return 0
}
