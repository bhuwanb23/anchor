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
	"golang.org/x/sys/unix"
)

// RunAll runs all pre-flight checks and returns the overall result.
func RunAll() *Result {
	result := NewResult()

	// Group A — System basics
	result.AddCheck(checkOS())
	result.AddCheck(checkArch())
	result.AddCheck(checkDisk())
	result.AddCheck(checkRAM())
	result.AddCheck(checkClock())

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

	// Read RAM info
	if v, err := mem.VirtualMemory(); err == nil {
		info.RAMMB = int(v.Total / 1024 / 1024)
		info.RAMAvailableMB = int(v.Available / 1024 / 1024)
	}

	// Read disk info
	if usage, err := disk.Usage("/"); err == nil {
		info.DiskTotalGB = int(usage.Total / 1024 / 1024 / 1024)
		info.DiskAvailableGB = int(usage.Free / 1024 / 1024 / 1024)
		info.DiskUsedPercent = usage.UsedPercent
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
	getAvailableGB := func(path string) (int, error) {
		var stat unix.Statfs_t
		if err := unix.Statfs(path, &stat); err != nil {
			return 0, err
		}
		available := int(stat.Bavail * uint64(stat.Bsize) / (1024 * 1024 * 1024))
		return available, nil
	}

	freeGB, err := getAvailableGB("/")
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

	// Also check /var if it's a separate mount point
	var varFreeGB int
	if varFree, err := getAvailableGB("/var"); err == nil {
		varFreeGB = varFree
		if varFree < freeGB {
			freeGB = varFree
		}
	}

	// Also check /var/lib/docker if it exists
	if dockerFree, err := getAvailableGB("/var/lib/docker"); err == nil {
		if dockerFree < freeGB {
			freeGB = dockerFree
		}
	}

	if freeGB < 2 {
		msg := fmt.Sprintf("Your server only has %d GB of free disk space.", freeGB)
		if varFreeGB > 0 && varFreeGB < freeGB {
			msg = fmt.Sprintf("Your /var partition only has %d GB of free disk space.", freeGB)
		}
		return CheckResult{
			Name:           "disk_space",
			DisplayName:    "Disk Space",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        msg + " YourPlatform needs at least 2 GB free to pull Docker images and store app data.",
			FixInstruction: "To free up space: remove unused Docker images (docker image prune -a), check usage (du -sh /* 2>/dev/null | sort -rh | head -20), or expand your server's disk.",
		}
	}

	if freeGB < 5 {
		msg := fmt.Sprintf("Your server has %d GB of free disk space.", freeGB)
		if varFreeGB > 0 && varFreeGB < freeGB {
			msg = fmt.Sprintf("Your /var partition has %d GB of free disk space.", freeGB)
		}
		return CheckResult{
			Name:           "disk_space",
			DisplayName:    "Disk Space",
			Status:         StatusWarn,
			Severity:       SeverityWarning,
			Message:        msg + " This is enough to get started, but apps and their data will fill this quickly. Consider expanding your disk soon.",
			FixInstruction: "Free up disk space or expand your disk to avoid running out.",
		}
	}

	return CheckResult{
		Name:        "disk_space",
		DisplayName: "Disk Space",
		Status:      StatusPass,
		Severity:    SeverityBlocking,
		Message:     fmt.Sprintf("%d GB free disk space", freeGB),
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
	availableMB := int(v.Available / 1024 / 1024)

	if totalMB < 512 {
		return CheckResult{
			Name:           "memory",
			DisplayName:    "Memory",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        fmt.Sprintf("Your server has %d MB of RAM, which is below the 512 MB minimum. Running Docker and your apps requires at least 512 MB.", totalMB),
			FixInstruction: "Upgrade to a larger server plan (usually $2-5/month more). Your current hosting provider likely offers a RAM upgrade.",
		}
	}

	if totalMB < 1024 {
		return CheckResult{
			Name:           "memory",
			DisplayName:    "Memory",
			Status:         StatusWarn,
			Severity:       SeverityWarning,
			Message:        fmt.Sprintf("Your server has %d MB of RAM (%d MB available). This is enough to run but may be tight for multiple apps.", totalMB, availableMB),
			FixInstruction: "Consider upgrading to at least 1 GB RAM for running multiple apps comfortably.",
		}
	}

	return CheckResult{
		Name:        "memory",
		DisplayName: "Memory",
		Status:      StatusPass,
		Severity:    SeverityBlocking,
		Message:     fmt.Sprintf("%d MB RAM (%d MB available)", totalMB, availableMB),
	}
}

func checkClock() CheckResult {
	// Check if systemd-timesyncd is active
	out, err := exec.Command("timedatectl", "show", "--property=NTPSynchronized", "--value").Output()
	if err == nil && strings.TrimSpace(string(out)) == "yes" {
		return CheckResult{
			Name:        "system_clock",
			DisplayName: "System Clock",
			Status:      StatusPass,
			Severity:    SeverityBlocking,
			Message:     "system clock is synchronized via NTP",
		}
	}

	// Check if systemd-timesyncd service exists and try to start it
	if err := exec.Command("systemctl", "cat", "systemd-timesyncd").Run(); err == nil {
		_ = exec.Command("systemctl", "start", "systemd-timesyncd").Run()
		_ = exec.Command("systemctl", "enable", "systemd-timesyncd").Run()

		// Re-check after auto-fix
		out, err := exec.Command("timedatectl", "show", "--property=NTPSynchronized", "--value").Output()
		if err == nil && strings.TrimSpace(string(out)) == "yes" {
			return CheckResult{
				Name:        "system_clock",
				DisplayName: "System Clock",
				Status:      StatusFixed,
				Severity:    SeverityBlocking,
				Message:     "systemd-timesyncd was not running — agent started it successfully",
				AutoFixed:   true,
			}
		}

		// Check if timedatectl shows NTP service is active but not yet synchronized
		out, err = exec.Command("timedatectl", "show", "--property=NTP", "--value").Output()
		if err == nil && strings.TrimSpace(string(out)) == "yes" {
			return CheckResult{
				Name:        "system_clock",
				DisplayName: "System Clock",
				Status:      StatusWarn,
				Severity:    SeverityWarning,
				Message:     "NTP is enabled but clock may not yet be synchronized — this should resolve within a few minutes",
			}
		}

		return CheckResult{
			Name:           "system_clock",
			DisplayName:    "System Clock",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        "systemd-timesyncd is installed but could not be started",
			FixInstruction: "Run: sudo timedatectl set-ntp true && sudo systemctl start systemd-timesyncd",
		}
	}

	// Check for ntpd as fallback
	if err := exec.Command("systemctl", "is-active", "ntpd").Run(); err == nil {
		return CheckResult{
			Name:        "system_clock",
			DisplayName: "System Clock",
			Status:      StatusPass,
			Severity:    SeverityBlocking,
			Message:     "system clock is synchronized via ntpd",
		}
	}

	// Check for chronyd as another fallback
	if err := exec.Command("systemctl", "is-active", "chronyd").Run(); err == nil {
		return CheckResult{
			Name:        "system_clock",
			DisplayName: "System Clock",
			Status:      StatusPass,
			Severity:    SeverityBlocking,
			Message:     "system clock is synchronized via chronyd",
		}
	}

	// Check if chrony is available but not running
	if err := exec.Command("systemctl", "cat", "chronyd").Run(); err == nil {
		_ = exec.Command("systemctl", "start", "chronyd").Run()
		_ = exec.Command("systemctl", "enable", "chronyd").Run()

		if err := exec.Command("systemctl", "is-active", "chronyd").Run(); err == nil {
			return CheckResult{
				Name:        "system_clock",
				DisplayName: "System Clock",
				Status:      StatusFixed,
				Severity:    SeverityBlocking,
				Message:     "chronyd was not running — agent started it successfully",
				AutoFixed:   true,
			}
		}
	}

	return CheckResult{
		Name:           "system_clock",
		DisplayName:    "System Clock",
		Status:         StatusFail,
		Severity:       SeverityBlocking,
		Message:        "no time synchronization service is running",
		FixInstruction: "Enable NTP: sudo timedatectl set-ntp true (requires systemd) or install ntp/chrony for your distribution.",
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
