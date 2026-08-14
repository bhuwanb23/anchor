package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_AgentTokenAlias(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := []byte("control_plane_url: http://localhost:8080\nagent_token: reg_test_token\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RegistrationToken != "reg_test_token" {
		t.Fatalf("expected agent_token alias mapped, got %q", cfg.RegistrationToken)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestLoad_RegistrationTokenPreferred(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := []byte("control_plane_url: http://localhost:8080\nregistration_token: reg_a\nagent_token: reg_b\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RegistrationToken != "reg_a" {
		t.Fatalf("expected registration_token to win, got %q", cfg.RegistrationToken)
	}
}
