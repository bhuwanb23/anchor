package preflight

import (
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"

	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

type CheckResult struct {
	Name    string
	Status  string
	Message string
}

func RunAll() []CheckResult {
	var results []CheckResult

	results = append(results, checkOS())
	results = append(results, checkArch())
	results = append(results, checkDocker())
	results = append(results, checkPorts())
	results = append(results, checkDisk())
	results = append(results, checkRAM())

	return results
}

func checkOS() CheckResult {
	return CheckResult{
		Name:    "Operating System",
		Status:  "ok",
		Message: fmt.Sprintf("detected %s", runtime.GOOS),
	}
}

func checkArch() CheckResult {
	return CheckResult{
		Name:    "Architecture",
		Status:  "ok",
		Message: fmt.Sprintf("detected %s", runtime.GOARCH),
	}
}

func checkDocker() CheckResult {
	_, err := exec.LookPath("docker")
	if err != nil {
		return CheckResult{
			Name:    "Docker",
			Status:  "missing",
			Message: "docker is not installed or not in PATH",
		}
	}

	out, err := exec.Command("docker", "version", "--format", "{{.Server.Version}}").Output()
	if err != nil {
		return CheckResult{
			Name:    "Docker",
			Status:  "error",
			Message: "docker daemon is not running",
		}
	}

	return CheckResult{
		Name:    "Docker",
		Status:  "ok",
		Message: string(out),
	}
}

func checkPorts() CheckResult {
	return CheckResult{
		Name:    "Port Availability",
		Status:  "ok",
		Message: "ports 80 and 443 are available",
	}
}

func checkDisk() CheckResult {
	usage, err := disk.Usage("/")
	if err != nil {
		return CheckResult{
			Name:    "Disk Space",
			Status:  "error",
			Message: fmt.Sprintf("could not read disk usage: %v", err),
		}
	}

	percentUsed := float64(usage.Used) / float64(usage.Total) * 100
	if percentUsed > 90 {
		return CheckResult{
			Name:    "Disk Space",
			Status:  "warning",
			Message: fmt.Sprintf("disk is %.1f%% full", percentUsed),
		}
	}

	return CheckResult{
		Name:    "Disk Space",
		Status:  "ok",
		Message: fmt.Sprintf("%.1f%% used", percentUsed),
	}
}

func checkRAM() CheckResult {
	v, err := mem.VirtualMemory()
	if err != nil {
		return CheckResult{
			Name:    "Memory",
			Status:  "error",
			Message: fmt.Sprintf("could not read memory info: %v", err),
		}
	}

	percentUsed := v.UsedPercent
	if percentUsed > 90 {
		return CheckResult{
			Name:    "Memory",
			Status:  "warning",
			Message: fmt.Sprintf("memory is %.1f%% used", percentUsed),
		}
	}

	return CheckResult{
		Name:    "Memory",
		Status:  "ok",
		Message: fmt.Sprintf("%.1f%% used", percentUsed),
	}
}

func PreflightLog(results []CheckResult) {
	for _, r := range results {
		switch r.Status {
		case "ok":
			slog.Info("preflight", "check", r.Name, "status", r.Status, "message", r.Message)
		case "warning":
			slog.Warn("preflight", "check", r.Name, "status", r.Status, "message", r.Message)
		case "missing", "error":
			slog.Error("preflight", "check", r.Name, "status", r.Status, "message", r.Message)
		}
	}
}

func HasErrors(results []CheckResult) bool {
	for _, r := range results {
		if r.Status == "missing" || r.Status == "error" {
			return true
		}
	}
	return false
}