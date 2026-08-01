package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
)

type BackupManager struct {
	destination string
	password    string
}

type Snapshot struct {
	ID       string
	Time     string
	Paths    string
	Tags     string
	Hostname string
}

func NewManager(destination string) *BackupManager {
	return &BackupManager{
		destination: destination,
		password:    os.Getenv("RESTIC_PASSWORD"),
	}
}

func (b *BackupManager) repoArgs() []string {
	args := []string{"--repo", b.destination}
	if b.password != "" {
		args = append(args, "--password", b.password)
	}
	return args
}

func (b *BackupManager) ensureRepo(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "restic", append(b.repoArgs(), "cat", "config")...)
	if err := cmd.Run(); err == nil {
		return nil
	}

	slog.Info("initializing restic repository", "dest", b.destination)
	initArgs := append(b.repoArgs(), "init")
	cmd = exec.CommandContext(ctx, "restic", initArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restic init failed: %w\n%s", err, string(output))
	}
	return nil
}

func (b *BackupManager) RunBackup(ctx context.Context, sourcePath string) error {
	slog.Info("starting backup", "source", sourcePath, "dest", b.destination)

	if err := b.ensureRepo(ctx); err != nil {
		return err
	}

	args := append(b.repoArgs(), "backup", sourcePath, "--tag", "agent")
	cmd := exec.CommandContext(ctx, "restic", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restic backup failed: %w\n%s", err, string(output))
	}

	slog.Info("backup completed", "source", sourcePath)
	return nil
}

func (b *BackupManager) ListSnapshots(ctx context.Context) ([]Snapshot, error) {
	args := append(b.repoArgs(), "snapshots", "--json")
	cmd := exec.CommandContext(ctx, "restic", args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("restic snapshots failed: %w", err)
	}

	var raw []struct {
		ID       string   `json:"id"`
		Time     string   `json:"time"`
		Paths    []string `json:"paths"`
		Tags     []string `json:"tags"`
		Hostname string   `json:"hostname"`
	}
	if err := json.Unmarshal(output, &raw); err != nil {
		return nil, fmt.Errorf("parse snapshots json: %w", err)
	}

	var snapshots []Snapshot
	for _, s := range raw {
		snapshots = append(snapshots, Snapshot{
			ID:       s.ID,
			Time:     s.Time,
			Paths:    strings.Join(s.Paths, ", "),
			Tags:     strings.Join(s.Tags, ", "),
			Hostname: s.Hostname,
		})
	}

	return snapshots, nil
}

func (b *BackupManager) Restore(ctx context.Context, snapshotID, targetPath string) error {
	slog.Info("starting restore", "snapshot", snapshotID, "target", targetPath)

	args := append(b.repoArgs(), "restore", snapshotID, "--target", targetPath)
	cmd := exec.CommandContext(ctx, "restic", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restic restore failed: %w\n%s", err, string(output))
	}

	slog.Info("restore completed", "snapshot", snapshotID)
	return nil
}

func (b *BackupManager) Prune(ctx context.Context) error {
	slog.Info("pruning old backups")

	args := append(b.repoArgs(), "forget", "--keep-daily", "7", "--keep-weekly", "4", "--prune")
	cmd := exec.CommandContext(ctx, "restic", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restic prune failed: %w\n%s", err, string(output))
	}

	slog.Info("backup pruning completed")
	return nil
}

// BackupConfig holds paths for default backup sources.
type BackupConfig struct {
	DataDir    string // agent data directory (state.json, config)
	CertDir    string // certificate storage directory
	ProjectsDir string // project data directory
}

// DefaultBackupPaths returns the default directories to back up,
// including certificates, state, and project data.
func DefaultBackupPaths(cfg BackupConfig) []string {
	var paths []string
	if cfg.DataDir != "" {
		paths = append(paths, cfg.DataDir)
	}
	if cfg.CertDir != "" {
		paths = append(paths, cfg.CertDir)
	}
	if cfg.ProjectsDir != "" {
		paths = append(paths, cfg.ProjectsDir)
	}
	return paths
}
