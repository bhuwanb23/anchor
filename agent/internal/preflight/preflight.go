package preflight

import (
	"log/slog"
)

// runGroupA runs system basics checks with short-circuit on A1 (OS) blocking failure.
func runGroupA(result *Result) {
	a1 := checkOS()
	result.AddCheck(a1)
	if a1.Severity == SeverityBlocking && a1.Status == StatusFail {
		slog.Warn("preflight short-circuit", "reason", "OS check failed — cannot determine system type, skipping remaining checks")
		return
	}
	result.AddCheck(checkArch())
	result.AddCheck(checkDisk())
	result.AddCheck(checkRAM())
	result.AddCheck(checkClock())
}

// runGroupB runs network checks with short-circuit on B1 (internet) blocking failure.
func runGroupB(result *Result) {
	b1 := checkInternet()
	result.AddCheck(b1)
	if b1.Severity == SeverityBlocking && b1.Status == StatusFail {
		slog.Warn("preflight short-circuit", "reason", "No internet connectivity — cannot reach control plane or pull images, skipping network checks")
		return
	}
	result.AddCheck(checkDNS())
	result.AddCheck(checkPort(80, "HTTP"))
	result.AddCheck(checkPort(443, "HTTPS"))
	result.AddCheck(checkControlPlaneConnect())
}

// runGroupC runs Docker checks with auto-fix and short-circuit.
// If C1 auto-fix fails → stop. If C2 auto-fix fails → stop.
func runGroupC(result *Result) {
	c1 := checkDockerInstalled()
	result.AddCheck(c1)
	if c1.Status == StatusFail && c1.Severity == SeverityBlocking {
		slog.Warn("preflight short-circuit", "reason", "Docker is not installed and auto-install failed — cannot run Docker checks")
		return
	}

	c2 := checkDockerDaemon()
	result.AddCheck(c2)
	if c2.Status == StatusFail && c2.Severity == SeverityBlocking {
		slog.Warn("preflight short-circuit", "reason", "Docker daemon could not be started — skipping Docker runtime checks")
		return
	}

	result.AddCheck(checkDockerVersion())
	result.AddCheck(checkDockerSocket())
	result.AddCheck(checkDockerPull())
}

// RunAll runs all pre-flight checks in the correct order with short-circuit logic.
func RunAll() *Result {
	result := NewResult()

	// Group A — System basics (short-circuit on A1 blocking failure)
	runGroupA(result)

	// Group B — Network checks (short-circuit on B1 blocking failure)
	runGroupB(result)

	// Group C — Docker checks (short-circuit on C1 or C2 auto-fix failure)
	runGroupC(result)

	// Group D — Runtime environment (collect all, no short-circuit)
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
