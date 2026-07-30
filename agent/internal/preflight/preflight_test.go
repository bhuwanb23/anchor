package preflight

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNewResult(t *testing.T) {
	r := NewResult()
	if r == nil {
		t.Fatal("NewResult() returned nil")
	}
	if r.Timestamp == "" {
		t.Error("expected Timestamp to be set")
	}
	if r.Checks == nil {
		t.Error("expected Checks slice to be initialized")
	}
	if r.AutoFixed == nil {
		t.Error("expected AutoFixed slice to be initialized")
	}
	if r.startTime.IsZero() {
		t.Error("expected startTime to be set")
	}
}

func TestAddCheck(t *testing.T) {
	r := NewResult()
	r.AddCheck(CheckResult{
		Name:        "test_check",
		DisplayName: "Test Check",
		Status:      StatusPass,
		Severity:    SeverityBlocking,
		Message:     "everything is fine",
	})
	if len(r.Checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(r.Checks))
	}
	if r.Checks[0].Name != "test_check" {
		t.Errorf("expected name 'test_check', got '%s'", r.Checks[0].Name)
	}
}

func TestAddCheckAutoFixed(t *testing.T) {
	r := NewResult()
	r.AddCheck(CheckResult{
		Name:        "auto_fixed_check",
		DisplayName: "Auto Fixed Check",
		Status:      StatusFixed,
		Severity:    SeverityBlocking,
		Message:     "was broken but we fixed it",
		AutoFixed:   true,
	})
	if len(r.AutoFixed) != 1 {
		t.Fatalf("expected 1 auto-fixed item, got %d", len(r.AutoFixed))
	}
	if r.AutoFixed[0] != "Auto Fixed Check" {
		t.Errorf("expected 'Auto Fixed Check', got '%s'", r.AutoFixed[0])
	}
}

func TestDoneCalculatesPassed(t *testing.T) {
	t.Run("all blocking pass", func(t *testing.T) {
		r := NewResult()
		r.AddCheck(CheckResult{Name: "a", Status: StatusPass, Severity: SeverityBlocking})
		r.AddCheck(CheckResult{Name: "b", Status: StatusPass, Severity: SeverityBlocking})
		r.Done()
		if !r.Passed {
			t.Error("expected Passed=true when all blocking checks pass")
		}
	})

	t.Run("blocking fail", func(t *testing.T) {
		r := NewResult()
		r.AddCheck(CheckResult{Name: "a", Status: StatusPass, Severity: SeverityBlocking})
		r.AddCheck(CheckResult{Name: "b", Status: StatusFail, Severity: SeverityBlocking})
		r.Done()
		if r.Passed {
			t.Error("expected Passed=false when a blocking check fails")
		}
	})

	t.Run("warning fail does not affect passed", func(t *testing.T) {
		r := NewResult()
		r.AddCheck(CheckResult{Name: "a", Status: StatusPass, Severity: SeverityBlocking})
		r.AddCheck(CheckResult{Name: "b", Status: StatusFail, Severity: SeverityWarning})
		r.Done()
		if !r.Passed {
			t.Error("expected Passed=true when only warning checks fail")
		}
	})

	t.Run("fixed blocking counts as pass", func(t *testing.T) {
		r := NewResult()
		r.AddCheck(CheckResult{Name: "a", Status: StatusFixed, Severity: SeverityBlocking})
		r.Done()
		if !r.Passed {
			t.Error("expected Passed=true when blocking checks were fixed")
		}
	})
}

func TestHasBlockingFailures(t *testing.T) {
	r := NewResult()
	r.AddCheck(CheckResult{Name: "a", Status: StatusPass, Severity: SeverityBlocking})
	r.AddCheck(CheckResult{Name: "b", Status: StatusFail, Severity: SeverityBlocking})
	r.Done()

	if !r.HasBlockingFailures() {
		t.Error("expected HasBlockingFailures()=true")
	}
}

func TestHasWarnings(t *testing.T) {
	t.Run("has warning", func(t *testing.T) {
		r := NewResult()
		r.AddCheck(CheckResult{Name: "a", Status: StatusWarn, Severity: SeverityWarning})
		if !r.HasWarnings() {
			t.Error("expected HasWarnings()=true")
		}
	})

	t.Run("no warning", func(t *testing.T) {
		r := NewResult()
		r.AddCheck(CheckResult{Name: "a", Status: StatusPass, Severity: SeverityBlocking})
		if r.HasWarnings() {
			t.Error("expected HasWarnings()=false")
		}
	})
}

func TestCountByStatus(t *testing.T) {
	r := NewResult()
	r.AddCheck(CheckResult{Name: "a", Status: StatusPass})
	r.AddCheck(CheckResult{Name: "b", Status: StatusPass})
	r.AddCheck(CheckResult{Name: "c", Status: StatusFail})
	r.AddCheck(CheckResult{Name: "d", Status: StatusWarn})

	if got := r.CountByStatus(StatusPass); got != 2 {
		t.Errorf("expected 2 pass, got %d", got)
	}
	if got := r.CountByStatus(StatusFail); got != 1 {
		t.Errorf("expected 1 fail, got %d", got)
	}
	if got := r.CountByStatus(StatusWarn); got != 1 {
		t.Errorf("expected 1 warn, got %d", got)
	}
}

func TestToJSON(t *testing.T) {
	r := NewResult()
	r.AddCheck(CheckResult{
		Name:        "test_check",
		DisplayName: "Test Check",
		Status:      StatusPass,
		Severity:    SeverityBlocking,
		Message:     "everything is fine",
	})
	r.SystemInfo = SystemInfo{
		OS:   "linux",
		Arch: "amd64",
	}
	r.Done()

	jsonStr, err := r.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() returned error: %v", err)
	}

	var parsed Result
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if len(parsed.Checks) != 1 {
		t.Errorf("expected 1 check in JSON, got %d", len(parsed.Checks))
	}
	if parsed.SystemInfo.OS != "linux" {
		t.Errorf("expected OS 'linux', got '%s'", parsed.SystemInfo.OS)
	}
	if parsed.Passed != true {
		t.Error("expected Passed=true in JSON")
	}
	if parsed.Timestamp == "" {
		t.Error("expected Timestamp to be set in JSON")
	}
	if parsed.Duration == "" {
		t.Error("expected Duration to be set in JSON")
	}
}

func TestTextOutput(t *testing.T) {
	r := NewResult()
	r.AddCheck(CheckResult{
		Name:        "os",
		DisplayName: "Operating System",
		Status:      StatusPass,
		Severity:    SeverityBlocking,
		Message:     "detected linux",
	})
	r.AddCheck(CheckResult{
		Name:           "docker_installed",
		DisplayName:    "Docker Installation",
		Status:         StatusFail,
		Severity:       SeverityBlocking,
		Message:        "Docker is not installed",
		FixInstruction: "Install Docker: curl ...",
	})
	r.AddCheck(CheckResult{
		Name:        "disk_space",
		DisplayName: "Disk Space",
		Status:      StatusWarn,
		Severity:    SeverityWarning,
		Message:     "disk is 95.0% full",
	})
	r.SystemInfo = SystemInfo{
		OS:   "linux",
		Arch: "amd64",
	}
	r.Done()

	text := r.Text()

	// Check basic structure
	if !strings.Contains(text, "Pre-flight Check Results") {
		t.Error("expected title in text output")
	}
	if !strings.Contains(text, "OS:") {
		t.Error("expected OS in text output")
	}
	if !strings.Contains(text, "Operating System") {
		t.Error("expected check name in text output")
	}
	if !strings.Contains(text, "[BLOCKING]") {
		t.Error("expected [BLOCKING] tag for blocking checks")
	}
	if !strings.Contains(text, "✗") {
		t.Error("expected failure icon for failed checks")
	}
	if !strings.Contains(text, "⚠") {
		t.Error("expected warning icon for warning checks")
	}
	if !strings.Contains(text, "Fix:") {
		t.Error("expected fix instruction in text output")
	}
	if !strings.Contains(text, "All blocking checks passed") {
		// Should NOT say this since docker failed
	}

	// Should say some blocking checks failed
	if !strings.Contains(text, "Some blocking checks failed") {
		t.Error("expected failure summary in text output")
	}

	// System info should be present
	if !strings.Contains(text, "linux") {
		t.Error("expected OS in system info")
	}
	if !strings.Contains(text, "amd64") {
		t.Error("expected arch in system info")
	}
}

func TestTextOutputAllPass(t *testing.T) {
	r := NewResult()
	r.AddCheck(CheckResult{
		Name:        "os",
		DisplayName: "Operating System",
		Status:      StatusPass,
		Severity:    SeverityBlocking,
		Message:     "detected linux",
	})
	r.AddCheck(CheckResult{
		Name:        "disk",
		DisplayName: "Disk Space",
		Status:      StatusPass,
		Severity:    SeverityBlocking,
		Message:     "50% used",
	})
	r.Done()

	text := r.Text()
	if !strings.Contains(text, "All blocking checks passed") {
		t.Error("expected success message when all pass")
	}
	if !strings.Contains(text, "✓") {
		t.Error("expected checkmark icon for passed checks")
	}
}

func TestHasErrors(t *testing.T) {
	t.Run("no errors", func(t *testing.T) {
		r := NewResult()
		r.AddCheck(CheckResult{Name: "a", Status: StatusPass, Severity: SeverityBlocking})
		r.AddCheck(CheckResult{Name: "b", Status: StatusWarn, Severity: SeverityWarning})
		r.Done()
		if HasErrors(r) {
			t.Error("expected HasErrors()=false")
		}
	})

	t.Run("has blocking failure", func(t *testing.T) {
		r := NewResult()
		r.AddCheck(CheckResult{Name: "a", Status: StatusFail, Severity: SeverityBlocking})
		r.Done()
		if !HasErrors(r) {
			t.Error("expected HasErrors()=true")
		}
	})

	t.Run("warning-only not an error", func(t *testing.T) {
		r := NewResult()
		r.AddCheck(CheckResult{Name: "a", Status: StatusWarn, Severity: SeverityWarning})
		r.Done()
		if HasErrors(r) {
			t.Error("expected HasErrors()=false for warnings only")
		}
	})
}

func TestVersionAtLeast(t *testing.T) {
	tests := []struct {
		version    string
		minVersion string
		expected   bool
	}{
		{"22.04", "20.04", true},   // newer
		{"20.04", "20.04", true},   // equal
		{"20.04", "22.04", false},  // older
		{"18.04", "20.04", false},  // much older
		{"11", "11", true},         // equal simple
		{"12", "11", true},         // newer simple
		{"10", "11", false},        // older simple
		{"22.04.3", "20.04", true}, // newer with patch
		{"20.04", "20.04.3", false}, // older than patch
		{"36", "36", true},         // fedora equal
		{"40", "36", true},         // fedora newer
		{"35", "36", false},        // fedora older
		{"8", "8", true},           // centos equal
		{"9", "8", true},           // centos newer
		{"7", "8", false},          // centos older
	}

	for _, tt := range tests {
		t.Run(tt.version+">="+tt.minVersion, func(t *testing.T) {
			result := versionAtLeast(tt.version, tt.minVersion)
			if result != tt.expected {
				t.Errorf("versionAtLeast(%q, %q) = %v, want %v", tt.version, tt.minVersion, result, tt.expected)
			}
		})
	}
}

func TestDurationSet(t *testing.T) {
	r := NewResult()
	r.AddCheck(CheckResult{Name: "a", Status: StatusPass, Severity: SeverityBlocking})
	r.Done()

	if r.Duration == "" {
		t.Error("expected Duration to be set after Done()")
	}
}

func TestJSONFieldNames(t *testing.T) {
	// Verify the JSON field names match what consumers expect
	r := NewResult()
	r.AddCheck(CheckResult{
		Name:           "test",
		DisplayName:    "Test",
		Status:         StatusPass,
		Severity:       SeverityBlocking,
		Message:        "ok",
		FixInstruction: "do nothing",
		AutoFixed:      false,
	})
	r.SystemInfo = SystemInfo{OS: "linux", Arch: "amd64"}
	r.Done()

	data, _ := json.Marshal(r)
	var raw map[string]interface{}
	json.Unmarshal(data, &raw)

	expectedFields := []string{"passed", "checks", "auto_fixed", "system_info", "timestamp", "duration"}
	for _, f := range expectedFields {
		if _, ok := raw[f]; !ok {
			t.Errorf("expected field '%s' in JSON output", f)
		}
	}

	checks := raw["checks"].([]interface{})[0].(map[string]interface{})
	expectedCheckFields := []string{"name", "display_name", "status", "severity", "message", "fix_instruction", "auto_fixed"}
	for _, f := range expectedCheckFields {
		if _, ok := checks[f]; !ok {
			t.Errorf("expected field '%s' in check JSON", f)
		}
	}

	sysInfo := raw["system_info"].(map[string]interface{})
	expectedSysFields := []string{"os", "os_version", "os_pretty", "arch", "ram_mb", "disk_gb"}
	for _, f := range expectedSysFields {
		if _, ok := sysInfo[f]; !ok {
			t.Errorf("expected field '%s' in system_info JSON", f)
		}
	}
}
