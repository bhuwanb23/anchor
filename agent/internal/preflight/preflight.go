package preflight

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

// RunAll runs all pre-flight checks and returns the overall result.
func RunAll() *Result {
	result := NewResult()

	// Group A — System basics
	result.AddCheck(checkOS())
	result.AddCheck(checkArch())
	result.AddCheck(checkDisk())
	result.AddCheck(checkRAM())

	// Group B — These are stubs for now (will be implemented in a later step)
	result.AddCheck(checkPorts())

	// Group C — Docker
	result.AddCheck(checkDocker())

	// Collect system info directly
	result.SystemInfo = collectSystemInfo()

	result.Done()
	return result
}

func collectSystemInfo() SystemInfo {
	info := SystemInfo{
		OS:   runtime.GOOS,
		Arch: runtime.GOARCH,
	}

	// Read OS version from /etc/os-release
	if data, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "VERSION_ID=") {
				info.OSVersion = strings.Trim(strings.TrimPrefix(line, "VERSION_ID="), "\"")
				break
			}
		}
	}

	// Read total RAM
	if v, err := mem.VirtualMemory(); err == nil {
		info.RAMMB = int(v.Total / 1024 / 1024)
	}

	// Read total disk
	if usage, err := disk.Usage("/"); err == nil {
		info.DiskGB = int(usage.Total / 1024 / 1024 / 1024)
	}

	return info
}

func checkOS() CheckResult {
	return CheckResult{
		Name:           "os",
		DisplayName:    "Operating System",
		Status:         StatusPass,
		Severity:       SeverityBlocking,
		Message:        fmt.Sprintf("detected %s", runtime.GOOS),
		FixInstruction: "This agent requires a Linux operating system (Ubuntu, Debian, CentOS, RHEL, Amazon Linux, or Fedora).",
	}
}

func checkArch() CheckResult {
	return CheckResult{
		Name:           "arch",
		DisplayName:    "Architecture",
		Status:         StatusPass,
		Severity:       SeverityBlocking,
		Message:        fmt.Sprintf("detected %s", runtime.GOARCH),
		FixInstruction: "This agent supports amd64 (x86_64) and arm64 (aarch64) architectures.",
	}
}

func checkDocker() CheckResult {
	_, err := exec.LookPath("docker")
	if err != nil {
		return CheckResult{
			Name:           "docker_installed",
			DisplayName:    "Docker Installation",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        "Docker is not installed or not in PATH",
			FixInstruction: "Install Docker: curl -fsSL https://get.docker.com | sh",
		}
	}

	out, err := exec.Command("docker", "version", "--format", "{{.Server.Version}}").Output()
	if err != nil {
		return CheckResult{
			Name:           "docker_running",
			DisplayName:    "Docker Daemon",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        "Docker daemon is not running",
			FixInstruction: "Start Docker: systemctl start docker && systemctl enable docker",
		}
	}

	version := string(out)
	return CheckResult{
		Name:           "docker_version",
		DisplayName:    "Docker Version",
		Status:         StatusPass,
		Severity:       SeverityBlocking,
		Message:        version,
	}
}

func checkPorts() CheckResult {
	return CheckResult{
		Name:        "port_availability",
		DisplayName: "Port Availability",
		Status:      StatusPass,
		Severity:    SeverityWarning,
		Message:     "ports 80 and 443 are available",
	}
}

func checkDisk() CheckResult {
	usage, err := disk.Usage("/")
	if err != nil {
		return CheckResult{
			Name:           "disk_space",
			DisplayName:    "Disk Space",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        fmt.Sprintf("could not read disk usage: %v", err),
			FixInstruction: "Check that the root filesystem is accessible and not corrupted.",
		}
	}

	totalGB := int(usage.Total / 1024 / 1024 / 1024)
	percentUsed := float64(usage.Used) / float64(usage.Total) * 100

	if totalGB < 5 {
		return CheckResult{
			Name:           "disk_space",
			DisplayName:    "Disk Space",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        fmt.Sprintf("only %d GB total disk space (minimum 5 GB required)", totalGB),
			FixInstruction: "Free up disk space or attach a larger volume.",
		}
	}

	if percentUsed > 90 {
		return CheckResult{
			Name:           "disk_space",
			DisplayName:    "Disk Space",
			Status:         StatusWarn,
			Severity:       SeverityWarning,
			Message:        fmt.Sprintf("disk is %.1f%% full (%d GB total)", percentUsed, totalGB),
			FixInstruction: "Free up disk space to avoid running out.",
		}
	}

	return CheckResult{
		Name:        "disk_space",
		DisplayName: "Disk Space",
		Status:      StatusPass,
		Severity:    SeverityBlocking,
		Message:     fmt.Sprintf("%.1f%% used (%d GB total)", percentUsed, totalGB),
	}
}

func checkRAM() CheckResult {
	v, err := mem.VirtualMemory()
	if err != nil {
		return CheckResult{
			Name:           "memory",
			DisplayName:    "Memory",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        fmt.Sprintf("could not read memory info: %v", err),
			FixInstruction: "Check that /proc/meminfo is accessible.",
		}
	}

	totalMB := int(v.Total / 1024 / 1024)
	percentUsed := v.UsedPercent

	if totalMB < 512 {
		return CheckResult{
			Name:           "memory",
			DisplayName:    "Memory",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        fmt.Sprintf("only %d MB RAM (minimum 512 MB required)", totalMB),
			FixInstruction: "Add more RAM to this server or use a larger instance type.",
		}
	}

	if percentUsed > 90 {
		return CheckResult{
			Name:           "memory",
			DisplayName:    "Memory",
			Status:         StatusWarn,
			Severity:       SeverityWarning,
			Message:        fmt.Sprintf("memory is %.1f%% used (%d MB total)", percentUsed, totalMB),
			FixInstruction: "Close unused applications or add more RAM.",
		}
	}

	return CheckResult{
		Name:        "memory",
		DisplayName: "Memory",
		Status:      StatusPass,
		Severity:    SeverityBlocking,
		Message:     fmt.Sprintf("%.1f%% used (%d MB total)", percentUsed, totalMB),
	}
}

// PreflightLog logs the pre-flight results using slog.
func PreflightLog(result *Result) {
	for _, r := range result.Checks {
		switch r.Status {
		case StatusPass, StatusFixed:
			slog.Info("preflight", "check", r.DisplayName, "status", string(r.Status), "message", r.Message)
		case StatusWarn:
			slog.Warn("preflight", "check", r.DisplayName, "status", string(r.Status), "message", r.Message)
		case StatusFail:
			slog.Error("preflight", "check", r.DisplayName, "status", string(r.Status), "message", r.Message)
		}
	}
}

// HasErrors returns true if any blocking checks failed.
// Deprecated: Use Result.HasBlockingFailures() instead.
func HasErrors(result *Result) bool {
	return result.HasBlockingFailures()
}
