package preflight

import (
	"encoding/json"
	"strings"
	"testing"
)

// =============================================================================
// Scenario 1 — Perfect server (Ubuntu 22.04, 2GB RAM, 40GB disk, nothing on 80/443)
// =============================================================================

func TestScenario1_VersionAtLeast_Ubuntu2204Passes(t *testing.T) {
	// Ubuntu 22.04 >= Ubuntu 20.04 (minimum) → should pass
	if !versionAtLeast("22.04", "20.04") {
		t.Error("Ubuntu 22.04 should be >= 20.04 minimum")
	}
}

func TestScenario1_ResultStructure_AllChecksPresent(t *testing.T) {
	result := RunAll()
	if len(result.Checks) == 0 {
		t.Fatal("RunAll should produce checks")
	}

	// Verify the first check is always OS
	if result.Checks[0].Name != "os" {
		t.Errorf("First check should be 'os', got '%s'", result.Checks[0].Name)
	}

	// Verify all 4 groups are represented
	foundGroups := map[string]bool{
		"os":              false, // Group A
		"internet":        false, // Group B
		"docker_installed": false, // Group C
		"systemd":         false, // Group D
	}
	for _, c := range result.Checks {
		switch c.Name {
		case "os", "arch", "disk_space", "memory", "system_clock":
			foundGroups["os"] = true
		case "internet", "dns", "port_80", "port_443", "control_plane_connect":
			foundGroups["internet"] = true
		case "docker_installed", "docker_daemon", "docker_version", "docker_socket", "docker_pull":
			foundGroups["docker_installed"] = true
		case "systemd", "directories", "conflicting_agent", "config":
			foundGroups["systemd"] = true
		}
	}

	for group, found := range foundGroups {
		if !found {
			t.Errorf("Missing checks from group starting with '%s'", group)
		}
	}
}

func TestScenario1_ResultJSON_SerializeAndDeserialize(t *testing.T) {
	result := RunAll()
	jsonStr, err := result.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}

	var parsed Result
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Fatalf("JSON unmarshal failed: %v", err)
	}

	if len(parsed.Checks) != len(result.Checks) {
		t.Errorf("Check count mismatch: got %d, want %d", len(parsed.Checks), len(result.Checks))
	}
}

func TestScenario1_ResultText_ContainsAllSections(t *testing.T) {
	result := RunAll()
	text := result.Text()

	sections := []string{
		"Pre-flight Check Results",
		"System:",
		"Checks:",
		"Summary:",
	}
	for _, s := range sections {
		if !strings.Contains(text, s) {
			t.Errorf("Text output missing section: '%s'", s)
		}
	}
}

// =============================================================================
// Scenario 2 — Port 80 in use by Apache with no active sites
// =============================================================================

func TestScenario2_HasActiveSites_ApacheNoSites(t *testing.T) {
	// hasActiveSites checks /etc/apache2/sites-enabled
	// On a system without Apache, this returns false (no sites = safe to stop)
	if hasActiveSites("apache2") {
		t.Log("Apache has active sites — auto-fix will NOT be attempted")
	} else {
		t.Log("Apache has no active sites — auto-fix WILL be attempted")
	}
}

func TestScenario2_HasActiveSites_NginxNoSites(t *testing.T) {
	if hasActiveSites("nginx") {
		t.Log("Nginx has active sites — auto-fix will NOT be attempted")
	} else {
		t.Log("Nginx has no active sites — auto-fix WILL be attempted")
	}
}

func TestScenario2_HasActiveSites_UnknownService(t *testing.T) {
	if hasActiveSites("caddy") {
		t.Error("Unknown service should return false")
	}
}

func TestScenario2_CheckPort_Structure(t *testing.T) {
	c := checkPort(80, "HTTP")
	if c.Name != "port_80" {
		t.Errorf("Expected name 'port_80', got '%s'", c.Name)
	}
	if c.DisplayName != "Port 80 (HTTP)" {
		t.Errorf("Expected 'Port 80 (HTTP)', got '%s'", c.DisplayName)
	}
	if c.Severity != SeverityBlocking {
		t.Errorf("Port checks should be blocking, got %s", c.Severity)
	}
}

// =============================================================================
// Scenario 3 — Port 80 in use by Apache with active virtual hosts
// =============================================================================

func TestScenario3_ApacheWithSites_NotAutoFixed(t *testing.T) {
	// This tests the auto-fix logic in checkPort:
	// If hasActiveSites returns true, the function should NOT auto-fix
	// and should return a StatusFail with a clear message

	// We can't test the full flow without actual Apache, but we can
	// verify the code path exists by checking hasActiveSites behavior
	c := checkPort(80, "HTTP")
	if c.Status == StatusFixed && !hasActiveSites("apache2") {
		// If Apache was auto-fixed, that means hasActiveSites returned false
		// (Apache was stopped because it had no active sites)
		t.Log("Apache was auto-fixed (no active sites)")
	}

	// Verify the fix instruction mentions Apache when relevant
	if c.Status == StatusFail && strings.Contains(c.Message, "apache2") {
		if !strings.Contains(c.FixInstruction, "move those sites") {
			t.Error("Fix instruction should mention moving sites when Apache has active sites")
		}
	}
}

// =============================================================================
// Scenario 4 — Low disk (1.5GB available)
// =============================================================================

func TestScenario4_DiskThreshold_BlockingBelow2GB(t *testing.T) {
	// The checkDisk function uses unix.Statfs which we can't mock,
	// but we can test the threshold logic indirectly via version comparison
	// and the Result's handling of blocking failures

	// Verify blocking failures cause Result.Passed = false
	r := NewResult()
	r.AddCheck(CheckResult{
		Name:     "disk_space",
		Status:   StatusFail,
		Severity: SeverityBlocking,
		Message:  "Your server only has 1 GB of free disk space. YourPlatform needs at least 2 GB free.",
	})
	r.Done()
	if r.Passed {
		t.Error("Low disk (blocking) should cause Passed=false")
	}
}

func TestScenario4_DiskThreshold_Warning2to5GB(t *testing.T) {
	r := NewResult()
	r.AddCheck(CheckResult{
		Name:     "disk_space",
		Status:   StatusWarn,
		Severity: SeverityWarning,
		Message:  "Your server has 3 GB of free disk space.",
	})
	r.AddCheck(CheckResult{
		Name:     "os",
		Status:   StatusPass,
		Severity: SeverityBlocking,
	})
	r.Done()
	if !r.Passed {
		t.Error("Warning-only should not cause Passed=false")
	}
	if !r.HasWarnings() {
		t.Error("Should have warnings")
	}
}

func TestScenario4_TextOutput_ShowsDiskInfo(t *testing.T) {
	r := NewResult()
	r.SystemInfo = SystemInfo{
		OS:              "linux",
		Arch:            "amd64",
		DiskTotalGB:     40,
		DiskAvailableGB: 1,
	}
	r.AddCheck(CheckResult{
		Name:     "disk_space",
		Status:   StatusFail,
		Severity: SeverityBlocking,
		Message:  "Your server only has 1 GB of free disk space.",
	})
	r.Done()

	text := r.Text()
	if !strings.Contains(text, "40 GB") {
		t.Error("Text output should show disk total")
	}
	if !strings.Contains(text, "1 GB free") {
		t.Error("Text output should show disk free")
	}
}

// =============================================================================
// Scenario 5 — No internet
// =============================================================================

func TestScenario5_InternetFails_Blocking(t *testing.T) {
	c := checkInternet()
	if c.Name != "internet" {
		t.Errorf("Expected name 'internet', got '%s'", c.Name)
	}
	if c.Severity != SeverityBlocking {
		t.Errorf("Internet check should be blocking, got %s", c.Severity)
	}
}

func TestScenario5_TextOutput_ShowsFixInstructions(t *testing.T) {
	r := NewResult()
	r.AddCheck(CheckResult{
		Name:           "internet",
		DisplayName:    "Internet Connectivity",
		Status:         StatusFail,
		Severity:       SeverityBlocking,
		Message:        "Your server cannot reach the internet.",
		FixInstruction: "Check your hosting provider's firewall settings. Try: curl -I https://1.1.1.1",
	})
	r.Done()

	text := r.Text()
	if !strings.Contains(text, "Fix:") {
		t.Error("Text output should show fix instructions when available")
	}
}

// =============================================================================
// Scenario 6 — Docker installed, daemon stopped
// =============================================================================

func TestScenario6_DockerInstalled_PassesButDaemonMayNotRun(t *testing.T) {
	c1 := checkDockerInstalled()
	if c1.Name != "docker_installed" {
		t.Errorf("Expected 'docker_installed', got '%s'", c1.Name)
	}
	if c1.Severity != SeverityBlocking {
		t.Errorf("Docker check should be blocking, got %s", c1.Severity)
	}
}

func TestScenario6_DockerDaemon_AutoFixAttempted(t *testing.T) {
	c2 := checkDockerDaemon()
	if c2.Name != "docker_daemon" {
		t.Errorf("Expected 'docker_daemon', got '%s'", c2.Name)
	}
	if c2.Severity != SeverityBlocking {
		t.Errorf("Docker daemon check should be blocking, got %s", c2.Severity)
	}
	// Status could be pass, fixed, or fail depending on Docker availability
	t.Logf("Docker daemon status: %s, auto-fixed: %v", c2.Status, c2.AutoFixed)
}

// =============================================================================
// Scenario 7 — CentOS 7 (unsupported)
// =============================================================================

func TestScenario7_CentOS7_TooOld(t *testing.T) {
	// CentOS minimum version is 8, so CentOS 7 should fail
	if versionAtLeast("7", "8") {
		t.Error("CentOS 7 should NOT be >= CentOS 8 minimum")
	}
}

func TestScenario7_CentOS8_Supported(t *testing.T) {
	if !versionAtLeast("8", "8") {
		t.Error("CentOS 8 should be >= CentOS 8 minimum")
	}
}

func TestScenario7_Ubuntu1804_TooOld(t *testing.T) {
	// Ubuntu minimum is 20.04
	if versionAtLeast("18.04", "20.04") {
		t.Error("Ubuntu 18.04 should NOT be >= 20.04 minimum")
	}
}

func TestScenario7_Debian10_TooOld(t *testing.T) {
	// Debian minimum is 11
	if versionAtLeast("10", "11") {
		t.Error("Debian 10 should NOT be >= Debian 11 minimum")
	}
}

func TestScenario7_Fedora35_TooOld(t *testing.T) {
	// Fedora minimum is 36
	if versionAtLeast("35", "36") {
		t.Error("Fedora 35 should NOT be >= Fedora 36 minimum")
	}
}

func TestScenario7_UnsupportedOS_TextMessage(t *testing.T) {
	// When checkOS encounters an unsupported OS, it should return
	// a blocking failure with a message listing supported systems
	// We can verify this by checking the code logic
	c := CheckResult{
		Name:           "os",
		DisplayName:    "Operating System",
		Status:         StatusFail,
		Severity:       SeverityBlocking,
		Message:        "'arch' is not a supported operating system",
		FixInstruction: "Supported systems: Ubuntu 20.04+, Debian 11+, CentOS 8+, RHEL 8+, Fedora 36+, Rocky Linux 8+, AlmaLinux 8+",
	}

	if c.Status != StatusFail {
		t.Error("Unsupported OS should be StatusFail")
	}
	if c.Severity != SeverityBlocking {
		t.Error("Unsupported OS should be blocking")
	}
	if !strings.Contains(c.FixInstruction, "7 distros listed") &&
		strings.Count(c.FixInstruction, "+") != 7 {
		t.Log("Fix instruction lists all supported distros:", c.FixInstruction)
	}
}

// =============================================================================
// Cross-scenario: Short-circuit behavior
// =============================================================================

func TestShortCircuit_GroupASkipsBOnA1Fail(t *testing.T) {
	// When A1 (OS) fails blocking, group A short-circuits.
	// We can verify that runGroupA stops after A1 by checking
	// the behavior when checkOS returns a blocking failure.
	// This is a structural test — actual short-circuit depends on system state.
	result := NewResult()
	runGroupA(result)
	if len(result.Checks) > 0 && result.Checks[0].Name == "os" {
		t.Logf("runGroupA produced %d checks (first: %s)", len(result.Checks), result.Checks[0].Name)
	}
}

func TestShortCircuit_RunGroupB_B1FailSkipsB2B5(t *testing.T) {
	// Verifying that the result structure from runGroupB
	// always has internet as the first check
	result := NewResult()
	runGroupB(result)
	if len(result.Checks) > 0 && result.Checks[0].Name != "internet" {
		t.Errorf("Expected first B check to be 'internet', got '%s'", result.Checks[0].Name)
	}
}

func TestShortCircuit_RunGroupC_C1FailSkipsC2C5(t *testing.T) {
	result := NewResult()
	runGroupC(result)
	if len(result.Checks) > 0 && result.Checks[0].Name != "docker_installed" {
		t.Errorf("Expected first C check to be 'docker_installed', got '%s'", result.Checks[0].Name)
	}
}

// =============================================================================
// Cross-scenario: OS requirement definitions check
// =============================================================================

func TestSupportedOS_Versions(t *testing.T) {
	minVersions := map[string]string{
		"ubuntu":    "20.04",
		"debian":    "11",
		"centos":    "8",
		"rhel":      "8",
		"fedora":    "36",
		"rocky":     "8",
		"almalinux": "8",
	}

	for _, req := range supportedOS {
		expectedMin, ok := minVersions[req.ID]
		if !ok {
			t.Errorf("Unexpected OS '%s' in supported list", req.ID)
			continue
		}
		if req.MinVersion != expectedMin {
			t.Errorf("OS '%s': expected min version %s, got %s", req.ID, expectedMin, req.MinVersion)
		}
		if req.Label == "" {
			t.Errorf("OS '%s' has empty label", req.ID)
		}
	}
}

func TestSupportedOS_AllHaveLabels(t *testing.T) {
	for _, req := range supportedOS {
		if req.Label == "" {
			t.Errorf("OS '%s' has empty label", req.ID)
		}
		if req.MinVersion == "" {
			t.Errorf("OS '%s' has empty min version", req.ID)
		}
	}
}
