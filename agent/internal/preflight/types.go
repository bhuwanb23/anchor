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
	OS            string `json:"os"`
	OSVersion     string `json:"os_version"`
	OSPretty      string `json:"os_pretty,omitempty"`
	Arch          string `json:"arch"`
	RAMMB         int    `json:"ram_mb"`
	DiskGB        int    `json:"disk_gb"`
	DockerVersion string `json:"docker_version,omitempty"`
}

// Result is the overall pre-flight check result.
type Result struct {
	Passed     bool          `json:"passed"`
	Checks     []CheckResult `json:"checks"`
	AutoFixed  []string      `json:"auto_fixed"`
	SystemInfo SystemInfo    `json:"system_info"`
	Timestamp  string        `json:"timestamp"`
	Duration   string        `json:"duration"`
	startTime  time.Time     // internal, not serialized
}

// NewResult creates a new Result with the current timestamp and starts the timer.
func NewResult() *Result {
	return &Result{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		startTime: time.Now(),
		AutoFixed: make([]string, 0),
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
		r.AutoFixed = append(r.AutoFixed, cr.DisplayName)
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
	for _, c := range r.Checks {
		if c.Status == StatusWarn {
			return true
		}
	}
	return false
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

// ToJSON serializes the result as indented JSON.
func (r *Result) ToJSON() (string, error) {
	data, err := json.MarshalIndent(r, "", "  ")
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
		b.WriteString(fmt.Sprintf("  RAM:         %d MB\n", r.SystemInfo.RAMMB))
	}
	if r.SystemInfo.DiskGB > 0 {
		b.WriteString(fmt.Sprintf("  Disk:        %d GB\n", r.SystemInfo.DiskGB))
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
			b.WriteString(fmt.Sprintf("  • %s\n", fix))
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
