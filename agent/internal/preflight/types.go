package preflight

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Status represents the result of a single check.
type Status string

const (
	StatusPass  Status = "pass"
	StatusFail  Status = "fail"
	StatusWarn  Status = "warn"
	StatusFixed Status = "fixed"
)

// Severity represents how serious a check failure is.
type Severity string

const (
	SeverityBlocking Severity = "blocking"
	SeverityWarning  Severity = "warning"
)

// AutoFixEntry records a single auto-fix action taken by the agent.
type AutoFixEntry struct {
	Check     string `json:"check"`
	Action    string `json:"action"`
	Timestamp string `json:"timestamp"`
}

// CheckResult is a single pre-flight check result.
type CheckResult struct {
	Name           string   `json:"name"`
	DisplayName    string   `json:"display_name"`
	Status         Status   `json:"status"`
	Severity       Severity `json:"severity"`
	Message        string   `json:"message"`
	FixInstruction string   `json:"fix_instruction"`
	AutoFixed      bool     `json:"auto_fixed"`
}

// SystemInfo collects system information gathered during checks.
type SystemInfo struct {
	OS              string  `json:"os"`
	OSVersion       string  `json:"os_version"`
	OSPretty        string  `json:"os_pretty,omitempty"`
	Arch            string  `json:"arch"`
	RAMMB           int     `json:"ram_mb"`
	RAMAvailableMB  int     `json:"ram_available_mb"`
	DiskTotalGB     int     `json:"disk_total_gb"`
	DiskAvailableGB int     `json:"disk_available_gb"`
	DiskUsedPercent float64 `json:"disk_used_percent"`
	DockerVersion   string  `json:"docker_version,omitempty"`
}

// Result is the overall pre-flight check result.
type Result struct {
	Passed     bool           `json:"passed"`
	Checks     []CheckResult  `json:"checks"`
	AutoFixed  []AutoFixEntry `json:"auto_fixed"`
	SystemInfo SystemInfo     `json:"system_info"`
	Timestamp  string         `json:"timestamp"`
	Duration   string         `json:"duration"`
	startTime  time.Time      // internal, not serialized
}

// NewResult creates a new Result with the current timestamp and starts the timer.
func NewResult() *Result {
	return &Result{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		startTime: time.Now(),
		AutoFixed: make([]AutoFixEntry, 0),
		Checks:    make([]CheckResult, 0),
	}
}

// Done finalizes the result: calculates Passed and Duration.
// Must be called after all checks are added.
func (r *Result) Done() {
	r.Duration = time.Since(r.startTime).Round(time.Millisecond).String()
	r.Passed = r.allBlockingPassed()
}

// AddCheck adds a single check result and tracks auto-fixes.
func (r *Result) AddCheck(cr CheckResult) {
	r.Checks = append(r.Checks, cr)
	if cr.AutoFixed {
		r.AutoFixed = append(r.AutoFixed, AutoFixEntry{
			Check:     cr.Name,
			Action:    cr.Message,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		})
	}
}

// allBlockingPassed returns true if all blocking checks passed or were fixed.
func (r *Result) allBlockingPassed() bool {
	for _, c := range r.Checks {
		if c.Severity == SeverityBlocking && c.Status == StatusFail {
			return false
		}
	}
	return true
}

// HasBlockingFailures returns true if any blocking check failed.
func (r *Result) HasBlockingFailures() bool {
	return !r.allBlockingPassed()
}

// HasWarnings returns true if any warning-level check has a warning status.
func (r *Result) HasWarnings() bool {
	return len(r.Warnings()) > 0
}

// Warnings returns all checks with a warning status.
func (r *Result) Warnings() []CheckResult {
	var warns []CheckResult
	for _, c := range r.Checks {
		if c.Status == StatusWarn {
			warns = append(warns, c)
		}
	}
	return warns
}

// CountByStatus returns the number of checks with the given status.
func (r *Result) CountByStatus(status Status) int {
	count := 0
	for _, c := range r.Checks {
		if c.Status == status {
			count++
		}
	}
	return count
}

// ToJSON serializes the result as indented human-readable JSON.
func (r *Result) ToJSON() (string, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal preflight result: %w", err)
	}
	return string(data), nil
}

// ToJSONCompact serializes the result as compact JSON (single line, no indent).
// Used by the install script via the --json flag.
func (r *Result) ToJSONCompact() (string, error) {
	data, err := json.Marshal(r)
	if err != nil {
		return "", fmt.Errorf("marshal preflight result: %w", err)
	}
	return string(data), nil
}

// Text returns a human-readable summary of the pre-flight result.
func (r *Result) Text() string {
	var b strings.Builder

	b.WriteString("Pre-flight Check Results\n")
	b.WriteString("───────────────────────\n\n")

	// System info
	b.WriteString("System:\n")
	if r.SystemInfo.OSPretty != "" {
		b.WriteString(fmt.Sprintf("  OS:          %s\n", r.SystemInfo.OSPretty))
	} else {
		b.WriteString(fmt.Sprintf("  OS:          %s %s\n", r.SystemInfo.OS, r.SystemInfo.OSVersion))
	}
	b.WriteString(fmt.Sprintf("  Arch:        %s\n", r.SystemInfo.Arch))
	if r.SystemInfo.RAMMB > 0 {
		b.WriteString(fmt.Sprintf("  RAM:         %d MB total", r.SystemInfo.RAMMB))
		if r.SystemInfo.RAMAvailableMB > 0 {
			b.WriteString(fmt.Sprintf(" (%d MB free)", r.SystemInfo.RAMAvailableMB))
		}
		b.WriteString("\n")
	}
	if r.SystemInfo.DiskTotalGB > 0 {
		b.WriteString(fmt.Sprintf("  Disk:        %d GB total", r.SystemInfo.DiskTotalGB))
		if r.SystemInfo.DiskAvailableGB > 0 {
			b.WriteString(fmt.Sprintf(" (%d GB free)", r.SystemInfo.DiskAvailableGB))
		}
		b.WriteString("\n")
	}
	if r.SystemInfo.DockerVersion != "" {
		b.WriteString(fmt.Sprintf("  Docker:      %s\n", r.SystemInfo.DockerVersion))
	}
	b.WriteString("\n")

	// Checks
	b.WriteString("Checks:\n")
	for _, c := range r.Checks {
		icon := "✓"
		switch c.Status {
		case StatusPass:
			icon = "✓"
		case StatusFail:
			icon = "✗"
		case StatusWarn:
			icon = "⚠"
		case StatusFixed:
			icon = "🔧"
		}

		severityTag := ""
		if c.Severity == SeverityBlocking {
			severityTag = " [BLOCKING]"
		}

		autoFixTag := ""
		if c.AutoFixed {
			autoFixTag = " (auto-fixed)"
		}

		b.WriteString(fmt.Sprintf("  %s %s%s%s\n", icon, c.DisplayName, severityTag, autoFixTag))
		b.WriteString(fmt.Sprintf("       %s\n", c.Message))
		if c.FixInstruction != "" && c.Status == StatusFail {
			b.WriteString(fmt.Sprintf("       Fix: %s\n", c.FixInstruction))
		}
	}
	b.WriteString("\n")

	// Auto-fixed summary
	if len(r.AutoFixed) > 0 {
		b.WriteString("Auto-fixed:\n")
		for _, fix := range r.AutoFixed {
			b.WriteString(fmt.Sprintf("  • %s — %s\n", fix.Check, fix.Action))
		}
		b.WriteString("\n")
	}

	// Summary
	b.WriteString("Summary:\n")
	total := len(r.Checks)
	passed := r.CountByStatus(StatusPass)
	failed := r.CountByStatus(StatusFail)
	warnings := r.CountByStatus(StatusWarn)
	fixed := r.CountByStatus(StatusFixed)

	b.WriteString(fmt.Sprintf("  Total:   %d\n", total))
	b.WriteString(fmt.Sprintf("  Passed:  %d\n", passed))
	b.WriteString(fmt.Sprintf("  Failed:  %d\n", failed))
	if warnings > 0 {
		b.WriteString(fmt.Sprintf("  Warnings: %d\n", warnings))
	}
	if fixed > 0 {
		b.WriteString(fmt.Sprintf("  Fixed:   %d\n", fixed))
	}
	b.WriteString(fmt.Sprintf("  Duration: %s\n", r.Duration))

	if r.Passed {
		b.WriteString("\n  ✓ All blocking checks passed.\n")
	} else {
		b.WriteString("\n  ✗ Some blocking checks failed. Fix the issues above and try again.\n")
	}

	return b.String()
}
