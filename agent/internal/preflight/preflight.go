package preflight

import (
	"log/slog"
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

	// Group B — Network checks
	result.AddCheck(checkInternet())
	result.AddCheck(checkDNS())
	result.AddCheck(checkPort(80, "HTTP"))
	result.AddCheck(checkPort(443, "HTTPS"))
	result.AddCheck(checkControlPlaneConnect())

	// Group C — Docker checks
	c1 := checkDockerInstalled()
	result.AddCheck(c1)
	if c1.Status == StatusPass || c1.Status == StatusFixed {
		result.AddCheck(checkDockerDaemon())
		result.AddCheck(checkDockerVersion())
		result.AddCheck(checkDockerSocket())
		result.AddCheck(checkDockerPull())
	}

	// Group D — Runtime environment
	result.AddCheck(checkSystemd())
	result.AddCheck(checkDirectories())
	result.AddCheck(checkConflictingAgent())
	result.AddCheck(checkConfig())

	result.SystemInfo = collectSystemInfo()
	result.Done()
	return result
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
