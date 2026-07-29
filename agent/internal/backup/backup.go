package backup

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"time"
)

type BackupManager struct {
	destination string
}

func NewManager(destination string) *BackupManager {
	return &BackupManager{
		destination: destination,
	}
}

func (b *BackupManager) RunBackup(ctx context.Context, sourcePath string) error {
	slog.Info("starting backup", "source", sourcePath, "dest", b.destination)

	repoPath := fmt.Sprintf("%s/%s", b.destination, time.Now().UTC().Format("2006-01-02"))

	cmd := exec.CommandContext(ctx, "restic", "backup", sourcePath, "--repo", repoPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restic backup failed: %w\n%s", err, string(output))
	}

	slog.Info("backup completed", "source", sourcePath, "repo", repoPath)
	return nil
}

func (b *BackupManager) ListSnapshots(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "restic", "snapshots", "--repo", b.destination)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("restic snapshots failed: %w", err)
	}

	return parseSnapshots(string(output))
}

func (b *BackupManager) Restore(ctx context.Context, snapshotID, targetPath string) error {
	slog.Info("starting restore", "snapshot", snapshotID, "target", targetPath)

	cmd := exec.CommandContext(ctx, "restic", "restore", snapshotID, "--repo", b.destination, "--target", targetPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restic restore failed: %w\n%s", err, string(output))
	}

	slog.Info("restore completed", "snapshot", snapshotID)
	return nil
}

func parseSnapshots(output string) ([]string, error) {
	var snapshots []string
	// TODO: parse restic snapshots output properly
	_ = output
	return snapshots, nil
}