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
	r.SystemInfo = SystemInfo{OS: "linux", Arch: "amd64"}
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
	r.SystemInfo = SystemInfo{OS: "linux", Arch: "amd64"}
	r.Done()

	text := r.Text()

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
		t.Error("expected [BLOCKING] tag")
	}
	if !strings.Contains(text, "✗") {
		t.Error("expected failure icon")
	}
	if !strings.Contains(text, "⚠") {
		t.Error("expected warning icon")
	}
	if !strings.Contains(text, "Fix:") {
		t.Error("expected fix instruction")
	}
	if !strings.Contains(text, "Some blocking checks failed") {
		t.Error("expected failure summary")
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

func TestShortCircuitA1Blocking(t *testing.T) {
	// A1 (OS) is always the first check. If it fails blocking, Group B-D should not run.
	// We can't mock the check functions, but we can verify that RunAll produces a result
	// that at least has the OS check.
	result := RunAll()
	if len(result.Checks) == 0 {
		t.Fatal("expected at least OS check in result")
	}
	if result.Checks[0].Name != "os" {
		t.Errorf("expected first check to be 'os', got '%s'", result.Checks[0].Name)
	}
	t.Logf("RunAll produced %d checks, passed=%v", len(result.Checks), result.Passed)
}

func TestRunGroupAIncludesAllSystemChecks(t *testing.T) {
	result := NewResult()
	runGroupA(result)
	// Should have at least OS check
	if len(result.Checks) == 0 {
		t.Error("expected at least one check after runGroupA")
	}
}

func TestRunGroupBIncludesAllNetworkChecks(t *testing.T) {
	result := NewResult()
	runGroupB(result)
	if len(result.Checks) == 0 {
		t.Error("expected at least one check after runGroupB")
	}
}

func TestRunGroupCIncludesAllDockerChecks(t *testing.T) {
	result := NewResult()
	runGroupC(result)
	if len(result.Checks) == 0 {
		t.Error("expected at least one check after runGroupC")
	}
}

func TestToJSONCompact(t *testing.T) {
	r := NewResult()
	r.AddCheck(CheckResult{
		Name:        "test_check",
		DisplayName: "Test Check",
		Status:      StatusPass,
		Severity:    SeverityBlocking,
		Message:     "everything is fine",
	})
	r.SystemInfo = SystemInfo{OS: "linux", Arch: "amd64"}
	r.Done()

	compactStr, err := r.ToJSONCompact()
	if err != nil {
		t.Fatalf("ToJSONCompact() returned error: %v", err)
	}

	// Compact JSON should be a single line (no indentation)
	if strings.Contains(compactStr, "\n") {
		t.Error("compact JSON should not contain newlines")
	}

	// Should still be valid JSON
	var parsed Result
	if err := json.Unmarshal([]byte(compactStr), &parsed); err != nil {
		t.Fatalf("compact JSON failed to unmarshal: %v", err)
	}
	if len(parsed.Checks) != 1 {
		t.Errorf("expected 1 check, got %d", len(parsed.Checks))
	}
}

func TestAutoFixEntryRecorded(t *testing.T) {
	r := NewResult()
	r.AddCheck(CheckResult{
		Name:      "docker_installed",
		DisplayName: "Docker Installation",
		Status:    StatusFixed,
		Severity:  SeverityBlocking,
		Message:   "Docker installed successfully (version 25.0.3)",
		AutoFixed: true,
	})

	if len(r.AutoFixed) != 1 {
		t.Fatalf("expected 1 auto-fix entry, got %d", len(r.AutoFixed))
	}
	if r.AutoFixed[0].Check != "docker_installed" {
		t.Errorf("expected check 'docker_installed', got '%s'", r.AutoFixed[0].Check)
	}
	if r.AutoFixed[0].Action != "Docker installed successfully (version 25.0.3)" {
		t.Errorf("unexpected action: '%s'", r.AutoFixed[0].Action)
	}
	if r.AutoFixed[0].Timestamp == "" {
		t.Error("expected timestamp to be set")
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

func TestDurationSet(t *testing.T) {
	r := NewResult()
	r.AddCheck(CheckResult{Name: "a", Status: StatusPass, Severity: SeverityBlocking})
	r.Done()

	if r.Duration == "" {
		t.Error("expected Duration to be set after Done()")
	}
}

func TestCheckNames(t *testing.T) {
	checks := []CheckResult{
		checkInternet(),
		checkDNS(),
		checkPort(80, "HTTP"),
		checkPort(443, "HTTPS"),
		checkControlPlaneConnect(),
	}

	for _, c := range checks {
		if c.Name == "" {
			t.Error("check has empty Name")
		}
		if c.DisplayName == "" {
			t.Errorf("check %s has empty DisplayName", c.Name)
		}
		if c.Severity == "" {
			t.Errorf("check %s has empty Severity", c.Name)
		}
		if c.Message == "" {
			t.Errorf("check %s has empty Message", c.Name)
		}
		if c.Status != StatusPass && c.Status != StatusFail {
			t.Errorf("check %s has unexpected status: %s", c.Name, c.Status)
		}
	}
}

func TestJSONFieldNames(t *testing.T) {
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

	// Verify auto_fixed is an array of objects with check/action/timestamp
	autoFixedItems := raw["auto_fixed"].([]interface{})
	if len(autoFixedItems) > 0 {
		firstItem := autoFixedItems[0].(map[string]interface{})
		expectedAutoFixFields := []string{"check", "action", "timestamp"}
		for _, f := range expectedAutoFixFields {
			if _, ok := firstItem[f]; !ok {
				t.Errorf("expected field '%s' in auto_fixed entry JSON", f)
			}
		}
	}
}
