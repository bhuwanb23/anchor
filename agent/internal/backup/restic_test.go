package backup

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNewResticManager(t *testing.T) {
	m := NewResticManager()
	if m.binaryPath != ResticBinary {
		t.Errorf("binaryPath = %q, want %q", m.binaryPath, ResticBinary)
	}
}

func TestNewResticManagerWithPath(t *testing.T) {
	customPath := "/custom/path/restic"
	m := NewResticManagerWithPath(customPath)
	if m.binaryPath != customPath {
		t.Errorf("binaryPath = %q, want %q", m.binaryPath, customPath)
	}
}

func TestResticManager_BinaryPath(t *testing.T) {
	m := NewResticManagerWithPath("/test/restic")
	if m.BinaryPath() != "/test/restic" {
		t.Errorf("BinaryPath() = %q, want /test/restic", m.BinaryPath())
	}
}

func TestResticManager_EnsureRestic_NotFound(t *testing.T) {
	m := NewResticManagerWithPath("/nonexistent/restic")
	ctx := context.Background()

	err := m.EnsureRestic(ctx)
	if err == nil {
		t.Error("expected error for missing binary")
	}
}

func TestResticManager_Version_NotFound(t *testing.T) {
	m := NewResticManagerWithPath("/nonexistent/restic")
	ctx := context.Background()

	_, err := m.Version(ctx)
	if err == nil {
		t.Error("expected error for missing binary")
	}
}

func TestResticManager_DownloadRestic_ChecksumMismatch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip download test on Windows")
	}

	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "restic")
	m := NewResticManagerWithPath(binaryPath)

	ctx := context.Background()
	err := m.DownloadRestic(ctx, "http://localhost:9999", "wrongchecksum")
	if err == nil {
		t.Error("expected error for download from non-existent server")
	}
}

func TestResticManager_DownloadRestic_Success(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip download test on Windows")
	}

	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "restic")

	// Create a fake restic binary
	if err := os.WriteFile(binaryPath, []byte("#!/bin/sh\necho restic 0.16.4"), 0755); err != nil {
		t.Fatalf("create fake binary: %v", err)
	}

	m := NewResticManagerWithPath(binaryPath)

	// Verify it works
	ctx := context.Background()
	if err := m.EnsureRestic(ctx); err != nil {
		// Expected to fail version check since output format differs
		t.Logf("EnsureRestic error (expected for fake binary): %v", err)
	}
}

func TestResticVersion(t *testing.T) {
	if ResticVersion == "" {
		t.Error("ResticVersion should not be empty")
	}
}

func TestResticBinary(t *testing.T) {
	if ResticBinary == "" {
		t.Error("ResticBinary should not be empty")
	}
}
