package backup

import (
	"context"
	"testing"
	"time"
)

func TestNewVerificationManager(t *testing.T) {
	rm := &RepositoryManager{
		config:    RepositoryConfig{Destination: "local:/tmp/test-repo"},
		resticBin: "/usr/bin/restic",
		dataDir:   t.TempDir(),
	}
	vm := NewVerificationManager(rm)
	if vm == nil {
		t.Fatal("expected non-nil VerificationManager")
	}
	if vm.repository != rm {
		t.Error("repository not set correctly")
	}
}

func TestNewVerificationManager_NilRepository(t *testing.T) {
	vm := NewVerificationManager(nil)
	if vm == nil {
		t.Fatal("expected non-nil VerificationManager")
	}
}

func TestVerificationStatus_Structure(t *testing.T) {
	now := time.Now()
	status := &VerificationStatus{
		Status:      "verified",
		Subset:      "5%",
		SnapshotID:  "abc123def456",
		StartedAt:   now,
		CompletedAt: now.Add(2 * time.Second),
		Duration:    2 * time.Second,
		FilesCount:  42,
	}

	if status.Status != "verified" {
		t.Errorf("Status = %q, want verified", status.Status)
	}
	if status.Subset != "5%" {
		t.Errorf("Subset = %q, want 5%%", status.Subset)
	}
	if status.SnapshotID != "abc123def456" {
		t.Errorf("SnapshotID = %q, want abc123def456", status.SnapshotID)
	}
	if status.FilesCount != 42 {
		t.Errorf("FilesCount = %d, want 42", status.FilesCount)
	}
}

func TestVerificationStatus_WithError(t *testing.T) {
	status := &VerificationStatus{
		Status:     "failed",
		Error:      "restic check failed",
		Subset:     "5%",
		SnapshotID: "abc123",
	}

	if status.Status != "failed" {
		t.Errorf("Status = %q, want failed", status.Status)
	}
	if status.Error != "restic check failed" {
		t.Errorf("Error = %q, want 'restic check failed'", status.Error)
	}
}

func TestVerifyPostBackup_NilRepository(t *testing.T) {
	vm := NewVerificationManager(nil)
	ctx := context.Background()

	status := vm.VerifyPostBackup(ctx, "abc123def456")
	if status.Status != "failed" {
		t.Errorf("Status = %q, want failed", status.Status)
	}
	if status.Error != "repository not initialized" {
		t.Errorf("Error = %q, want 'repository not initialized'", status.Error)
	}
	if status.Subset != "5%" {
		t.Errorf("Subset = %q, want 5%%", status.Subset)
	}
	if status.SnapshotID != "abc123def456" {
		t.Errorf("SnapshotID = %q, want abc123def456", status.SnapshotID)
	}
	// Duration should be set (may be 0 for instant failure)
	if status.CompletedAt.IsZero() {
		t.Error("expected CompletedAt to be set")
	}
}

func TestVerifyDeep_NilRepository(t *testing.T) {
	vm := NewVerificationManager(nil)
	ctx := context.Background()

	status := vm.VerifyDeep(ctx, "abc123def456")
	if status.Status != "failed" {
		t.Errorf("Status = %q, want failed", status.Status)
	}
	if status.Subset != "25%" {
		t.Errorf("Subset = %q, want 25%%", status.Subset)
	}
}

func TestVerifyFull_NilRepository(t *testing.T) {
	vm := NewVerificationManager(nil)
	ctx := context.Background()

	status := vm.VerifyFull(ctx, "abc123def456")
	if status.Status != "failed" {
		t.Errorf("Status = %q, want failed", status.Status)
	}
	if status.Subset != "100%" {
		t.Errorf("Subset = %q, want 100%%", status.Subset)
	}
}

func TestVerifySnapshot_NilRepository(t *testing.T) {
	vm := NewVerificationManager(nil)
	ctx := context.Background()

	err := vm.VerifySnapshot(ctx, "abc123def456")
	if err == nil {
		t.Fatal("expected error for nil repository")
	}
	if err.Error() != "verification failed: repository not initialized" {
		t.Errorf("error = %q, want 'verification failed: repository not initialized'", err.Error())
	}
}

func TestVerificationStatus_TimingFields(t *testing.T) {
	startedAt := time.Now()
	completedAt := startedAt.Add(5 * time.Second)
	status := &VerificationStatus{
		Status:      "verified",
		StartedAt:   startedAt,
		CompletedAt: completedAt,
		Duration:    completedAt.Sub(startedAt),
	}

	if status.Duration != 5*time.Second {
		t.Errorf("Duration = %v, want 5s", status.Duration)
	}
	if !status.StartedAt.Equal(startedAt) {
		t.Error("StartedAt mismatch")
	}
	if !status.CompletedAt.Equal(completedAt) {
		t.Error("CompletedAt mismatch")
	}
}

func TestVerificationStatus_FieldsJSON(t *testing.T) {
	status := &VerificationStatus{
		Status:     "verified",
		Subset:     "5%",
		SnapshotID: "abc123",
		FilesCount: 10,
		Error:      "",
	}

	if status.Error != "" {
		t.Errorf("Error should be empty for verified status, got %q", status.Error)
	}
	if status.FilesCount != 10 {
		t.Errorf("FilesCount = %d, want 10", status.FilesCount)
	}
}

func TestVerifyPostBackup_CancelledContext(t *testing.T) {
	rm := &RepositoryManager{
		config:    RepositoryConfig{Destination: "local:/tmp/test-repo"},
		resticBin: "/usr/bin/restic",
		dataDir:   t.TempDir(),
	}
	vm := NewVerificationManager(rm)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	status := vm.VerifyPostBackup(ctx, "abc123def456")
	// Should fail because restic binary doesn't exist
	if status.Status != "failed" {
		t.Errorf("Status = %q, want failed (restic binary not found)", status.Status)
	}
}
