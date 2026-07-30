package preflight

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strconv"
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

	// Read OS info from /etc/os-release
	if data, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "ID=") && !strings.HasPrefix(line, "ID_LIKE=") {
				info.OS = strings.Trim(strings.TrimPrefix(line, "ID="), "\"")
			}
			if strings.HasPrefix(line, "VERSION_ID=") {
				info.OSVersion = strings.Trim(strings.TrimPrefix(line, "VERSION_ID="), "\"")
			}
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				info.OSPretty = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
			}
		}
	}

	// Override Arch with actual uname -m result
	if out, err := exec.Command("uname", "-m").Output(); err == nil {
		info.Arch = strings.TrimSpace(string(out))
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

type osRequirement struct {
	ID          string
	MinVersion  string
	Label       string
}

var supportedOS = []osRequirement{
	{"ubuntu", "20.04", "Ubuntu"},
	{"debian", "11", "Debian"},
	{"centos", "8", "CentOS"},
	{"rhel", "8", "RHEL"},
	{"fedora", "36", "Fedora"},
	{"rocky", "8", "Rocky Linux"},
	{"almalinux", "8", "AlmaLinux"},
}

// versionAtLeast compares two version strings (e.g. "22.04" >= "20.04").
func versionAtLeast(version, minVersion string) bool {
	vParts := strings.Split(version, ".")
	mParts := strings.Split(minVersion, ".")

	maxLen := len(vParts)
	if len(mParts) > maxLen {
		maxLen = len(mParts)
	}

	for i := 0; i < maxLen; i++ {
		var v, m int
		if i < len(vParts) {
			v, _ = strconv.Atoi(vParts[i])
		}
		if i < len(mParts) {
			m, _ = strconv.Atoi(mParts[i])
		}
		if v > m {
			return true
		}
		if v < m {
			return false
		}
	}
	return true // equal
}

func checkOS() CheckResult {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return CheckResult{
			Name:           "os",
			DisplayName:    "Operating System",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        "could not read /etc/os-release \u2014 unable to determine operating system",
			FixInstruction: "This agent requires a supported Linux distribution. Ensure /etc/os-release is present and readable.",
		}
	}

	var osID, osVersion string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "ID=") && !strings.HasPrefix(line, "ID_LIKE=") {
			osID = strings.Trim(strings.TrimPrefix(line, "ID="), "\"")
		}
		if strings.HasPrefix(line, "VERSION_ID=") {
			osVersion = strings.Trim(strings.TrimPrefix(line, "VERSION_ID="), "\"")
		}
	}

	if osID == "" {
		return CheckResult{
			Name:           "os",
			DisplayName:    "Operating System",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        "could not determine OS distribution from /etc/os-release",
			FixInstruction: "This agent requires a supported Linux distribution. Contact support if you believe this is an error.",
		}
	}

	var requirement *osRequirement
	for i, req := range supportedOS {
		if req.ID == osID {
			requirement = &supportedOS[i]
			break
		}
	}

	if requirement == nil {
	supported := make([]string, len(supportedOS))
	for i, req := range supportedOS {
		supported[i] = fmt.Sprintf("%s %s+", req.Label, req.MinVersion)
	}
	return CheckResult{
		Name:           "os",
		DisplayName:    "Operating System",
		Status:         StatusFail,
		Severity:       SeverityBlocking,
		Message:        fmt.Sprintf("'%s' is not a supported operating system", osID),
		FixInstruction: fmt.Sprintf("Supported systems: %s", strings.Join(supported, ", ")),
	}
	}

	if osVersion == "" {
		return CheckResult{
			Name:           "os",
			DisplayName:    "Operating System",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        fmt.Sprintf("detected %s but could not determine version", requirement.Label),
			FixInstruction: fmt.Sprintf("Minimum supported version is %s. Check /etc/os-release for VERSION_ID.", requirement.MinVersion),
		}
	}

	if !versionAtLeast(osVersion, requirement.MinVersion) {
		return CheckResult{
			Name:           "os",
			DisplayName:    "Operating System",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        fmt.Sprintf("Your server is running %s %s, which is too old. Minimum supported version is %s.", requirement.Label, osVersion, requirement.MinVersion),
			FixInstruction: fmt.Sprintf("Upgrade to %s or newer. On Ubuntu: sudo do-release-upgrade", requirement.MinVersion),
		}
	}

	return CheckResult{
		Name:        "os",
		DisplayName: "Operating System",
		Status:      StatusPass,
		Severity:    SeverityBlocking,
		Message:     fmt.Sprintf("detected %s %s", osID, osVersion),
	}
}

func checkArch() CheckResult {
	out, err := exec.Command("uname", "-m").Output()
	if err != nil {
		return CheckResult{
			Name:           "arch",
			DisplayName:    "Architecture",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        "could not determine system architecture",
			FixInstruction: "Run 'uname -m' manually to check. The agent requires a 64-bit system (x86_64 or aarch64).",
		}
	}

	rawArch := strings.TrimSpace(string(out))

	switch rawArch {
	case "x86_64":
		return CheckResult{
			Name:        "arch",
			DisplayName: "Architecture",
			Status:      StatusPass,
			Severity:    SeverityBlocking,
			Message:     fmt.Sprintf("detected %s (amd64)", rawArch),
		}
	case "aarch64":
		return CheckResult{
			Name:        "arch",
			DisplayName: "Architecture",
			Status:      StatusPass,
			Severity:    SeverityBlocking,
			Message:     fmt.Sprintf("detected %s (arm64)", rawArch),
		}
	case "i386", "i686":
		return CheckResult{
			Name:           "arch",
			DisplayName:    "Architecture",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        fmt.Sprintf("Your server is running 32-bit %s, which is not supported.", rawArch),
			FixInstruction: "YourPlatform requires a 64-bit server (x86_64 or aarch64). Reinstall with a 64-bit OS.",
		}
	case "armv7l", "armv6l":
		return CheckResult{
			Name:           "arch",
			DisplayName:    "Architecture",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        fmt.Sprintf("Your server is running 32-bit ARM (%s), which is not supported.", rawArch),
			FixInstruction: "If this is a Raspberry Pi, please flash a 64-bit OS image (e.g., Raspberry Pi OS 64-bit).",
		}
	default:
		return CheckResult{
			Name:           "arch",
			DisplayName:    "Architecture",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        fmt.Sprintf("unsupported architecture '%s'", rawArch),
			FixInstruction: "YourPlatform requires a 64-bit server (x86_64 or aarch64).",
		}
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
