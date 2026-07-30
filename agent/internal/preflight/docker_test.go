package preflight

import (
	"testing"
)

func TestDockerCheckStructures(t *testing.T) {
	t.Run("checkDockerInstalled", func(t *testing.T) {
		c := checkDockerInstalled()
		if c.Name != "docker_installed" {
			t.Errorf("expected name 'docker_installed', got '%s'", c.Name)
		}
		if c.Severity != SeverityBlocking {
			t.Errorf("expected blocking severity, got %s", c.Severity)
		}
		if c.DisplayName == "" {
			t.Error("expected DisplayName to be non-empty")
		}
	})

	t.Run("checkDockerDaemon", func(t *testing.T) {
		c := checkDockerDaemon()
		if c.Name != "docker_daemon" {
			t.Errorf("expected name 'docker_daemon', got '%s'", c.Name)
		}
		if c.Severity != SeverityBlocking {
			t.Errorf("expected blocking severity, got %s", c.Severity)
		}
	})

	t.Run("checkDockerVersion", func(t *testing.T) {
		c := checkDockerVersion()
		if c.Name != "docker_version" {
			t.Errorf("expected name 'docker_version', got '%s'", c.Name)
		}
	})

	t.Run("checkDockerSocket", func(t *testing.T) {
		c := checkDockerSocket()
		if c.Name != "docker_socket" {
			t.Errorf("expected name 'docker_socket', got '%s'", c.Name)
		}
		if c.Severity != SeverityBlocking {
			t.Errorf("expected blocking severity, got %s", c.Severity)
		}
	})

	t.Run("checkDockerPull", func(t *testing.T) {
		c := checkDockerPull()
		if c.Name != "docker_pull" {
			t.Errorf("expected name 'docker_pull', got '%s'", c.Name)
		}
		if c.Severity != SeverityBlocking {
			t.Errorf("expected blocking severity, got %s", c.Severity)
		}
	})
}

func TestArchForRepo(t *testing.T) {
	arch := archForRepo()
	if arch != "amd64" && arch != "arm64" {
		t.Errorf("expected amd64 or arm64, got '%s'", arch)
	}
}
