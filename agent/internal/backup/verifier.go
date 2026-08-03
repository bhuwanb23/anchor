package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// VerificationStatus represents the result of a backup verification check.
type VerificationStatus struct {
	Status      string        `json:"status"`       // "verified" | "failed" | "skipped"
	Subset      string        `json:"subset"`       // "5%", "25%", "100%"
	SnapshotID  string        `json:"snapshot_id"`
	StartedAt   time.Time     `json:"started_at"`
	CompletedAt time.Time     `json:"completed_at"`
	Duration    time.Duration `json:"duration"`
	Error       string        `json:"error,omitempty"`
	FilesCount  int           `json:"files_count,omitempty"` // number of files found in snapshot
}

// VerificationManager handles backup verification at multiple levels.
// Post-backup: 5% subset (every backup)
// Weekly:       25% subset (Sunday 3am)
// Monthly:     100% subset (1st of month)
type VerificationManager struct {
	repository *RepositoryManager
}

// NewVerificationManager creates a new verification manager.
func NewVerificationManager(repository *RepositoryManager) *VerificationManager {
	return &VerificationManager{
		repository: repository,
	}
}

// VerifyPostBackup runs a quick 5% verification after every backup.
// Checks data integrity and confirms snapshot is readable.
func (v *VerificationManager) VerifyPostBackup(ctx context.Context, snapshotID string) *VerificationStatus {
	return v.verify(ctx, snapshotID, "5%")
}

// VerifyDeep runs a 25% verification (weekly schedule).
// More thorough than post-backup, catches most corruption.
func (v *VerificationManager) VerifyDeep(ctx context.Context, snapshotID string) *VerificationStatus {
	return v.verify(ctx, snapshotID, "25%")
}

// VerifyFull runs a 100% verification (monthly schedule).
// Complete verification of entire repository. May take several minutes.
func (v *VerificationManager) VerifyFull(ctx context.Context, snapshotID string) *VerificationStatus {
	return v.verify(ctx, snapshotID, "100%")
}

// verify runs restic check with the given subset and optionally lists snapshot files.
func (v *VerificationManager) verify(ctx context.Context, snapshotID, subset string) *VerificationStatus {
	startedAt := time.Now()

	status := &VerificationStatus{
		SnapshotID: snapshotID,
		Subset:     subset,
		StartedAt:  startedAt,
	}

	if v.repository == nil {
		status.Status = "failed"
		status.Error = "repository not initialized"
		status.CompletedAt = time.Now()
		status.Duration = status.CompletedAt.Sub(startedAt)
		return status
	}

	slog.Info("starting backup verification",
		"snapshot", snapshotID,
		"subset", subset)

	// Step 1: Run restic check with read-data-subset
	if err := v.runCheck(ctx, subset); err != nil {
		status.Status = "failed"
		status.Error = fmt.Sprintf("restic check failed: %v", err)
		status.CompletedAt = time.Now()
		status.Duration = status.CompletedAt.Sub(startedAt)
		slog.Error("backup verification failed",
			"snapshot", snapshotID,
			"subset", subset,
			"error", err)
		return status
	}

	// Step 2: List snapshot files to verify index is readable
	filesCount, err := v.listSnapshotFiles(ctx, snapshotID)
	if err != nil {
		status.Status = "failed"
		status.Error = fmt.Sprintf("snapshot listing failed: %v", err)
		status.CompletedAt = time.Now()
		status.Duration = status.CompletedAt.Sub(startedAt)
		slog.Error("snapshot listing failed during verification",
			"snapshot", snapshotID,
			"error", err)
		return status
	}

	status.Status = "verified"
	status.FilesCount = filesCount
	status.CompletedAt = time.Now()
	status.Duration = status.CompletedAt.Sub(startedAt)

	slog.Info("backup verification passed",
		"snapshot", snapshotID,
		"subset", subset,
		"files", filesCount,
		"duration", status.Duration)

	return status
}

// runCheck executes restic check with the given data subset percentage.
func (v *VerificationManager) runCheck(ctx context.Context, subset string) error {
	args := v.repository.repoArgs()
	args = append(args, "check", "--read-data-subset="+subset)

	cmd := exec.CommandContext(ctx, v.repository.resticBin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restic check failed: %w\n%s", err, string(output))
	}
	return nil
}

// listSnapshotFiles runs restic ls to verify the snapshot index is readable.
// Returns the number of files found.
func (v *VerificationManager) listSnapshotFiles(ctx context.Context, snapshotID string) (int, error) {
	args := v.repository.repoArgs()
	args = append(args, "ls", "--json", snapshotID)

	cmd := exec.CommandContext(ctx, v.repository.resticBin, args...)
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("restic ls failed: %w", err)
	}

	// restic ls --json outputs one JSON object per line.
	// Each line represents a file or directory in the snapshot.
	var filesCount int
	lines := splitLines(string(output))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Each line is a JSON object with "name" field
		var entry struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err == nil && entry.Name != "" {
			filesCount++
		}
	}

	return filesCount, nil
}

// VerifySnapshot runs a quick verification on a snapshot (legacy interface).
// Uses 1% subset for fastest possible check.
func (v *VerificationManager) VerifySnapshot(ctx context.Context, snapshotID string) error {
	status := v.verify(ctx, snapshotID, "1%")
	if status.Status == "failed" {
		return fmt.Errorf("verification failed: %s", status.Error)
	}
	return nil
}
