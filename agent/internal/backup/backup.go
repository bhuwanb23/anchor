package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"
)

type BackupManager struct {
	destination  string
	password     string
	restic       *ResticManager
	repository   *RepositoryManager
	dataDir      string
	serverID     string
	config       *RepositoryConfig
	stateMgr     StateManager // StateManager interface for manifest-based backups
	dockerClient DockerClient // DockerClient interface for container operations
	verifier     *VerificationManager // Verification manager for backup verification
}

// StateManager interface for state operations.
type StateManager interface {
	GetState() *StateData
	GetLastBackupTime() time.Time
	RecordBackupCompletion(snapshotID string, duration time.Duration, totalBytes int64) error
}

// StateData holds project state for backup manifest building.
type StateData struct {
	Projects map[string]interface{}
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
		restic:      NewResticManager(),
		dataDir:     "/var/lib/yourplatform",
	}
}

// NewManagerWithConfig creates a backup manager with full configuration.
func NewManagerWithConfig(cfg BackupConfig) *BackupManager {
	m := &BackupManager{
		destination: cfg.Destination,
		dataDir:     cfg.DataDir,
		serverID:    cfg.ServerID,
		restic:      NewResticManager(),
	}

	// Load existing config if available
	if cfg.DataDir != "" {
		config, err := LoadConfig(cfg.DataDir)
		if err == nil && config != nil {
			m.config = config
			m.password = config.Password
			m.destination = config.Destination
		} else {
			// Create new config
			m.config = &RepositoryConfig{
				Destination: cfg.Destination,
			}
		}
	}

	// Initialize repository and verifier
	if m.config != nil {
		m.repository = NewRepositoryManager(*m.config, m.restic.BinaryPath(), m.dataDir)
		m.verifier = NewVerificationManager(m.repository)
	}

	return m
}

func (b *BackupManager) repoArgs() []string {
	args := []string{"--repo", b.destination}
	if b.password != "" {
		args = append(args, "--password", b.password)
	}
	return args
}

// Initialize performs first-time backup setup.
func (b *BackupManager) Initialize(ctx context.Context) error {
	slog.Info("initializing backup system")

	// Verify restic binary
	if err := b.restic.EnsureRestic(ctx); err != nil {
		return fmt.Errorf("restic binary check: %w", err)
	}

	// Generate password if not set
	if b.password == "" {
		pwd, err := GeneratePassword()
		if err != nil {
			return fmt.Errorf("generate password: %w", err)
		}
		b.password = pwd
		b.config.Password = pwd

		// Save password securely
		if err := SavePassword(b.dataDir, pwd); err != nil {
			return fmt.Errorf("save password: %w", err)
		}
	}

	// Initialize repository
	b.repository = NewRepositoryManager(*b.config, b.restic.BinaryPath(), b.dataDir)
	if err := b.repository.InitRepository(ctx); err != nil {
		return fmt.Errorf("init repository: %w", err)
	}

	// Save config
	if err := SaveConfig(b.dataDir, b.config); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	slog.Info("backup system initialized", "dest", b.destination)
	return nil
}

// CheckHealth verifies the backup system is functional.
func (b *BackupManager) CheckHealth(ctx context.Context) error {
	if err := b.restic.EnsureRestic(ctx); err != nil {
		return fmt.Errorf("restic binary: %w", err)
	}

	if b.repository == nil {
		b.repository = NewRepositoryManager(*b.config, b.restic.BinaryPath(), b.dataDir)
	}

	if err := b.repository.VerifyRepository(ctx); err != nil {
		return fmt.Errorf("repository: %w", err)
	}

	return nil
}

// GetStatus returns the current backup system status.
func (b *BackupManager) GetStatus(ctx context.Context) (*BackupStatus, error) {
	status := &BackupStatus{
		ResticVersion: ResticVersion,
	}

	if err := b.restic.EnsureRestic(ctx); err != nil {
		status.RepositoryOK = false
		status.Error = err.Error()
		return status, nil
	}

	if b.repository == nil {
		b.repository = NewRepositoryManager(*b.config, b.restic.BinaryPath(), b.dataDir)
	}

	snapshots, err := b.repository.ListSnapshots(ctx)
	if err != nil {
		status.RepositoryOK = false
		status.Error = err.Error()
		return status, nil
	}

	status.RepositoryOK = true
	status.SnapshotCount = len(snapshots)

	if len(snapshots) > 0 {
		status.LastBackup = snapshots[0].Time
	}

	return status, nil
}

// BackupStatus holds the current backup system status.
type BackupStatus struct {
	ResticVersion string `json:"restic_version"`
	RepositoryOK  bool   `json:"repository_ok"`
	SnapshotCount int    `json:"snapshot_count"`
	LastBackup    string `json:"last_backup,omitempty"`
	Error         string `json:"error,omitempty"`
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
	DataDir        string // agent data directory (state.json, config)
	CertDir        string // certificate storage directory
	ProjectsDir    string // project data directory
	Destination    string // restic repository destination
	ServerID       string // server ID for control plane
	ControlPlaneURL string // control plane URL for binary downloads
	AgentID        string // agent ID for authentication
	AgentSecret    string // agent secret for authentication
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

// WithStateManager sets the state manager for manifest-based backups.
func (b *BackupManager) WithStateManager(stateMgr StateManager) *BackupManager {
	b.stateMgr = stateMgr
	return b
}

// WithDockerClient sets the Docker client for manifest-based backups.
func (b *BackupManager) WithDockerClient(client DockerClient) *BackupManager {
	b.dockerClient = client
	return b
}

// GetRepository returns the repository manager for verification operations.
func (b *BackupManager) GetRepository() *RepositoryManager {
	if b.repository == nil && b.config != nil {
		b.repository = NewRepositoryManager(*b.config, b.restic.BinaryPath(), b.dataDir)
	}
	return b.repository
}

// RunManifestBackup executes a manifest-driven backup if state and Docker are available.
// Falls back to legacy RunBackup if dependencies are not set.
func (b *BackupManager) RunManifestBackup(ctx context.Context, serverID string) (*BackupRunResult, error) {
	if b.stateMgr == nil || b.dockerClient == nil {
		return nil, fmt.Errorf("state manager and docker client required for manifest backup")
	}

	runner := NewBackupRunner(b, b.dockerClient)
	return runner.RunManifestBackup(ctx, serverID)
}

// RunRestore executes a manifest-driven restore for a single project.
// Requires Docker client to be set via WithDockerClient.
func (b *BackupManager) RunRestore(ctx context.Context, snapshotID, projectName string, reporter RestoreProgressReporter) (*RestoreRunResult, error) {
	if b.dockerClient == nil {
		return nil, fmt.Errorf("docker client required for restore")
	}

	if b.repository == nil {
		b.repository = NewRepositoryManager(*b.config, b.restic.BinaryPath(), b.dataDir)
	}

	runner := NewRestoreRunner(b, b.dockerClient)
	return runner.RunRestore(ctx, snapshotID, projectName, reporter)
}
