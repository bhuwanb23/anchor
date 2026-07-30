package preflight

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
	"golang.org/x/sys/unix"
)

func collectSystemInfo() SystemInfo {
	info := SystemInfo{
		OS:   runtime.GOOS,
		Arch: runtime.GOARCH,
	}

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

	if out, err := exec.Command("uname", "-m").Output(); err == nil {
		info.Arch = strings.TrimSpace(string(out))
	}

	if v, err := mem.VirtualMemory(); err == nil {
		info.RAMMB = int(v.Total / 1024 / 1024)
		info.RAMAvailableMB = int(v.Available / 1024 / 1024)
	}

	if usage, err := disk.Usage("/"); err == nil {
		info.DiskTotalGB = int(usage.Total / 1024 / 1024 / 1024)
		info.DiskAvailableGB = int(usage.Free / 1024 / 1024 / 1024)
		info.DiskUsedPercent = usage.UsedPercent
	}

	return info
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

	var varFreeGB int
	if varFree, err := getAvailableGB("/var"); err == nil {
		varFreeGB = varFree
		if varFree < freeGB {
			freeGB = varFree
		}
	}

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

	if err := exec.Command("systemctl", "cat", "systemd-timesyncd").Run(); err == nil {
		_ = exec.Command("systemctl", "start", "systemd-timesyncd").Run()
		_ = exec.Command("systemctl", "enable", "systemd-timesyncd").Run()

		out, err := exec.Command("timedatectl", "show", "--property=NTPSynchronized", "--value").Output()
		if err == nil && strings.TrimSpace(string(out)) == "yes" {
			return CheckResult{
				Name:      "system_clock",
				DisplayName: "System Clock",
				Status:    StatusFixed,
				Severity:  SeverityBlocking,
				Message:   "systemd-timesyncd was not running — agent started it successfully",
				AutoFixed: true,
			}
		}

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

	if err := exec.Command("systemctl", "is-active", "ntpd").Run(); err == nil {
		return CheckResult{
			Name:        "system_clock",
			DisplayName: "System Clock",
			Status:      StatusPass,
			Severity:    SeverityBlocking,
			Message:     "system clock is synchronized via ntpd",
		}
	}

	if err := exec.Command("systemctl", "is-active", "chronyd").Run(); err == nil {
		return CheckResult{
			Name:        "system_clock",
			DisplayName: "System Clock",
			Status:      StatusPass,
			Severity:    SeverityBlocking,
			Message:     "system clock is synchronized via chronyd",
		}
	}

	if err := exec.Command("systemctl", "cat", "chronyd").Run(); err == nil {
		_ = exec.Command("systemctl", "start", "chronyd").Run()
		_ = exec.Command("systemctl", "enable", "chronyd").Run()

		if err := exec.Command("systemctl", "is-active", "chronyd").Run(); err == nil {
			return CheckResult{
				Name:      "system_clock",
				DisplayName: "System Clock",
				Status:    StatusFixed,
				Severity:  SeverityBlocking,
				Message:   "chronyd was not running — agent started it successfully",
				AutoFixed: true,
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
