package preflight

import (
	"testing"
)

func TestCheckSystemdStructure(t *testing.T) {
	c := checkSystemd()
	if c.Name != "systemd" {
		t.Errorf("expected name 'systemd', got '%s'", c.Name)
	}
	if c.Severity != SeverityBlocking {
		t.Errorf("expected blocking severity, got %s", c.Severity)
	}
	if c.DisplayName == "" {
		t.Error("expected DisplayName to be non-empty")
	}
	if c.Status != StatusPass && c.Status != StatusFail && c.Status != StatusWarn {
		t.Errorf("unexpected status: %s", c.Status)
	}
}

func TestCheckDirectoriesStructure(t *testing.T) {
	c := checkDirectories()
	if c.Name != "directories" {
		t.Errorf("expected name 'directories', got '%s'", c.Name)
	}
	if c.Severity != SeverityBlocking {
		t.Errorf("expected blocking severity, got %s", c.Severity)
	}
	if c.DisplayName == "" {
		t.Error("expected DisplayName to be non-empty")
	}
	t.Logf("checkDirectories status: %s, message: %s", c.Status, c.Message)
}

func TestCheckConflictingAgentStructure(t *testing.T) {
	c := checkConflictingAgent()
	if c.Name != "conflicting_agent" {
		t.Errorf("expected name 'conflicting_agent', got '%s'", c.Name)
	}
	if c.Severity != SeverityBlocking {
		t.Errorf("expected blocking severity, got %s", c.Severity)
	}
	if c.DisplayName == "" {
		t.Error("expected DisplayName to be non-empty")
	}
}

func TestCheckConfigStructure(t *testing.T) {
	c := checkConfig()
	if c.Name != "config" {
		t.Errorf("expected name 'config', got '%s'", c.Name)
	}
	if c.Severity != SeverityBlocking {
		t.Errorf("expected blocking severity, got %s", c.Severity)
	}
	if c.DisplayName == "" {
		t.Error("expected DisplayName to be non-empty")
	}
	t.Logf("checkConfig status: %s, message: %s", c.Status, c.Message)
}
