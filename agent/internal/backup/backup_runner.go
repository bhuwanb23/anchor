package backup

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"
)

// ComponentResult holds the result of backing up a single component.
type ComponentResult struct {
	Type      string `json:"type"`
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
	Status    string `json:"status"` // "success" | "failed"
}

// ProjectResult holds the backup result for a single project.
type ProjectResult struct {
	Name       string            `json:"name"`
	Status     string            `json:"status"` // "success" | "partial" | "failed"
	Components []ComponentResult `json:"components,omitempty"`
	Error      string            `json:"error,omitempty"`
}

// BackupRunResult holds the result of a manifest-driven backup.
type BackupRunResult struct {
	Manifest       *BackupManifest `json:"manifest"`
	SnapshotID     string          `json:"snapshot_id"`
	DumpResults    []*DumpResult   `json:"dump_results"`
	ProjectResults []ProjectResult `json:"project_results"`
	TotalBytes     int64           `json:"total_bytes"`
	Duration       time.Duration   `json:"duration"`
	StartedAt      time.Time       `json:"started_at"`
	Error          string          `json:"error,omitempty"`
	// Verification fields
	VerificationStatus string        `json:"verification_status"` // "verified" | "failed" | "skipped"
	VerificationError  string        `json:"verification_error,omitempty"`
	VerificationTime   time.Duration `json:"verification_time"`
}

// BackupRunner executes manifest-driven backups.
type BackupRunner struct {
	manager     *BackupManager
	manifestBuilder *BackupManifestBuilder
	dumper      *Dumper
	dumpDir     string
}

// NewBackupRunner creates a new backup runner.
func NewBackupRunner(manager *BackupManager, dockerClient DockerClient) *BackupRunner {
	dumpDir := defaultDumpDir
	return &BackupRunner{
		manager:         manager,
		manifestBuilder: NewBackupManifestBuilder(manager.stateMgr, dockerClient),
		dumper:          NewDumper(dockerClient, dumpDir),
		dumpDir:         dumpDir,
	}
}

// RunManifestBackup executes a full manifest-driven backup.
// It discovers projects, dumps databases, collects volumes, and runs restic.
func (r *BackupRunner) RunManifestBackup(ctx context.Context, serverID string) (*BackupRunResult, error) {
	startTime := time.Now()

	result := &BackupRunResult{
		StartedAt: startTime,
	}

	// Build manifest
	slog.Info("building backup manifest", "server_id", serverID)
	manifest, err := r.manifestBuilder.BuildManifest(ctx, serverID)
	if err != nil {
		result.Error = fmt.Sprintf("build manifest: %v", err)
		return result, fmt.Errorf("build manifest: %w", err)
	}
	result.Manifest = manifest

	// Create temp dump directory
	if err := os.MkdirAll(r.dumpDir, 0700); err != nil {
		result.Error = fmt.Sprintf("create dump dir: %v", err)
		return result, fmt.Errorf("create dump dir: %w", err)
	}

	// Ensure cleanup happens
	defer func() {
		slog.Info("cleaning up dump files")
		r.dumper.CleanupAllDumps()
	}()

	// Dump databases for each project and build per-project results
	for _, project := range manifest.Projects {
		projectResult := ProjectResult{
			Name:   project.Name,
			Status: "success",
		}

		for _, comp := range project.Components {
			var dumpResult *DumpResult
			compResult := ComponentResult{
				Type:   comp.Type,
				Name:   comp.Name(),
				Status: "success",
			}

			switch comp.Type {
			case ComponentTypePostgresDump:
				slog.Info("dumping postgres database",
					"project", project.Name,
					"container", comp.Container,
					"database", comp.Database)
				dumpResult, err = r.dumper.DumpPostgres(ctx, comp.Container, project.Name, comp.Database)
				if err != nil {
					slog.Warn("postgres dump failed",
						"project", project.Name, "error", err)
					compResult.Status = "failed"
					projectResult.Status = "partial"
				}

			case ComponentTypeMysqlDump:
				slog.Info("dumping mysql database",
					"project", project.Name,
					"container", comp.Container,
					"database", comp.Database)
				dumpResult, err = r.dumper.DumpMySQL(ctx, comp.Container, project.Name, comp.Database)
				if err != nil {
					slog.Warn("mysql dump failed",
						"project", project.Name, "error", err)
					compResult.Status = "failed"
					projectResult.Status = "partial"
				}

			case ComponentTypeRedisDump:
				slog.Info("dumping redis database",
					"project", project.Name,
					"container", comp.Container)
				dumpResult, err = r.dumper.DumpRedis(ctx, comp.Container, project.Name)
				if err != nil {
					slog.Warn("redis dump failed",
						"project", project.Name, "error", err)
					compResult.Status = "failed"
					projectResult.Status = "partial"
				}
			}

			if dumpResult != nil {
				result.DumpResults = append(result.DumpResults, dumpResult)
				result.TotalBytes += dumpResult.SizeBytes
				compResult.SizeBytes = dumpResult.SizeBytes
			}

			projectResult.Components = append(projectResult.Components, compResult)
		}

		result.ProjectResults = append(result.ProjectResults, projectResult)
	}

	// Collect all paths to back up
	backupPaths := r.manifestBuilder.CollectBackupPaths(manifest, r.dumpDir)

	// Filter out paths that don't exist
	var validPaths []string
	for _, path := range backupPaths {
		if _, err := os.Stat(path); err == nil {
			validPaths = append(validPaths, path)
		} else {
			slog.Debug("backup path not found, skipping", "path", path)
		}
	}

	if len(validPaths) == 0 {
		slog.Warn("no valid backup paths found")
		result.Error = "no valid backup paths found"
		return result, fmt.Errorf("no valid backup paths found")
	}

	// Run restic backup with all collected paths
	slog.Info("starting restic backup",
		"paths", len(validPaths),
		"server_id", serverID)

	// Create a tag with server ID and timestamp
	tag := fmt.Sprintf("server-%s", serverID)

	// Build restic command with all paths
	args := r.manager.repoArgs()
	args = append(args, "backup")
	for _, path := range validPaths {
		args = append(args, path)
	}
	args = append(args, "--tag", tag, "--tag", "manifest")

	// Execute restic backup
	snapshotID, err := r.executeResticBackup(ctx, args)
	if err != nil {
		result.Error = fmt.Sprintf("restic backup failed: %v", err)
		return result, fmt.Errorf("restic backup: %w", err)
	}
	result.SnapshotID = snapshotID

	// Verify the snapshot (post-backup verification)
	verifyStart := time.Now()
	if r.manager.verifier != nil {
		verifyResult := r.manager.verifier.VerifyPostBackup(ctx, snapshotID)
		if verifyResult != nil {
			result.VerificationStatus = verifyResult.Status
			result.VerificationError = verifyResult.Error
			result.VerificationTime = verifyResult.Duration
		}
	} else {
		result.VerificationStatus = "skipped"
		slog.Debug("verification skipped: no verifier configured")
	}
	verifyDuration := time.Since(verifyStart)
	if result.VerificationTime == 0 {
		result.VerificationTime = verifyDuration
	}

	// Run prune to clean up old backups
	if err := r.manager.Prune(ctx); err != nil {
		slog.Warn("backup prune failed", "error", err)
		// Non-fatal: backup succeeded, prune is cleanup
	}

	result.Duration = time.Since(startTime)

	slog.Info("manifest backup completed",
		"snapshot", result.SnapshotID,
		"duration", result.Duration,
		"total_bytes", result.TotalBytes,
		"dumps", len(result.DumpResults))

	return result, nil
}

// RunManifestBackupWithProgress executes a full manifest-driven backup with
// real-time progress reporting, snapshot verification, and configurable retention.
func (r *BackupRunner) RunManifestBackupWithProgress(ctx context.Context, serverID string, reporter ProgressReporter) (*BackupRunResult, error) {
	startTime := time.Now()
	result := &BackupRunResult{}

	// Report initial status
	if reporter != nil {
		reporter.ReportProgress(BackupProgress{
			Phase:   "dumping",
			Message: "Building backup manifest...",
		})
	}

	// Build manifest
	slog.Info("building backup manifest", "server_id", serverID)
	manifest, err := r.manifestBuilder.BuildManifest(ctx, serverID)
	if err != nil {
		result.Error = fmt.Sprintf("build manifest: %v", err)
		if reporter != nil {
			reporter.ReportError("", result.Error)
		}
		return result, fmt.Errorf("build manifest: %w", err)
	}
	result.Manifest = manifest

	// Create temp dump directory
	if err := os.MkdirAll(r.dumpDir, 0700); err != nil {
		result.Error = fmt.Sprintf("create dump dir: %v", err)
		if reporter != nil {
			reporter.ReportError("", result.Error)
		}
		return result, fmt.Errorf("create dump dir: %w", err)
	}

	// Ensure cleanup happens
	defer func() {
		slog.Info("cleaning up dump files")
		r.dumper.CleanupAllDumps()
	}()

	// Dump databases for each project
	for _, project := range manifest.Projects {
		for _, comp := range project.Components {
			var dumpResult *DumpResult

			// Report dumping progress
			if reporter != nil {
				reporter.ReportProgress(BackupProgress{
					Phase:   "dumping",
					Project: project.Name,
					Message: fmt.Sprintf("Dumping %s database for %s...", comp.Type, project.Name),
				})
			}

			switch comp.Type {
			case ComponentTypePostgresDump:
				dumpResult, err = r.dumper.DumpPostgres(ctx, comp.Container, project.Name, comp.Database)
				if err != nil {
					slog.Warn("postgres dump failed", "project", project.Name, "error", err)
				}

			case ComponentTypeMysqlDump:
				dumpResult, err = r.dumper.DumpMySQL(ctx, comp.Container, project.Name, comp.Database)
				if err != nil {
					slog.Warn("mysql dump failed", "project", project.Name, "error", err)
				}

			case ComponentTypeRedisDump:
				dumpResult, err = r.dumper.DumpRedis(ctx, comp.Container, project.Name)
				if err != nil {
					slog.Warn("redis dump failed", "project", project.Name, "error", err)
				}
			}

			if dumpResult != nil {
				result.DumpResults = append(result.DumpResults, dumpResult)
				result.TotalBytes += dumpResult.SizeBytes
			}
		}
	}

	// Collect all paths to back up
	backupPaths := r.manifestBuilder.CollectBackupPaths(manifest, r.dumpDir)

	// Filter out paths that don't exist
	var validPaths []string
	for _, path := range backupPaths {
		if _, err := os.Stat(path); err == nil {
			validPaths = append(validPaths, path)
		}
	}

	if len(validPaths) == 0 {
		result.Error = "no valid backup paths found"
		if reporter != nil {
			reporter.ReportError("", result.Error)
		}
		return result, fmt.Errorf("no valid backup paths found")
	}

	// Report backing up phase
	if reporter != nil {
		reporter.ReportProgress(BackupProgress{
			Phase:   "backing_up",
			Message: fmt.Sprintf("Backing up %d paths...", len(validPaths)),
		})
	}

	// Run restic backup with JSON progress
	snapshotID, err := r.executeResticBackupJSON(ctx, validPaths, reporter, serverID)
	if err != nil {
		result.Error = fmt.Sprintf("restic backup failed: %v", err)
		if reporter != nil {
			reporter.ReportError("", result.Error)
		}
		return result, fmt.Errorf("restic backup: %w", err)
	}
	result.SnapshotID = snapshotID

	// Report verification phase
	if reporter != nil {
		reporter.ReportProgress(BackupProgress{
			Phase:   "verifying",
			Message: "Verifying backup integrity...",
		})
	}

	// Verify the snapshot
	if r.manager.verifier != nil {
		verifyResult := r.manager.verifier.VerifyPostBackup(ctx, snapshotID)
		if verifyResult != nil {
			result.VerificationStatus = verifyResult.Status
			result.VerificationError = verifyResult.Error
			result.VerificationTime = verifyResult.Duration
		}
	} else {
		// Fallback to legacy verification
		if err := r.VerifySnapshot(ctx); err != nil {
			slog.Warn("snapshot verification failed", "error", err)
			result.VerificationStatus = "failed"
			result.VerificationError = err.Error()
		} else {
			result.VerificationStatus = "verified"
		}
	}

	// Apply retention policy
	if reporter != nil {
		reporter.ReportProgress(BackupProgress{
			Phase:   "pruning",
			Message: "Cleaning up old snapshots...",
		})
	}

	// Get retention config from backup manager
	keepDaily := 7
	keepWeekly := 4
	keepMonthly := 12
	if r.manager.config != nil {
		if r.manager.config.RetentionDaily > 0 {
			keepDaily = r.manager.config.RetentionDaily
		}
		if r.manager.config.RetentionWeekly > 0 {
			keepWeekly = r.manager.config.RetentionWeekly
		}
		if r.manager.config.RetentionMonthly > 0 {
			keepMonthly = r.manager.config.RetentionMonthly
		}
	}

	if err := r.manager.repository.PruneWithConfig(ctx, keepDaily, keepWeekly, keepMonthly); err != nil {
		slog.Warn("backup prune failed", "error", err)
	}

	result.Duration = time.Since(startTime)

	// Report completion
	if reporter != nil {
		reporter.ReportComplete(*result)
	}

	slog.Info("manifest backup completed",
		"snapshot", result.SnapshotID,
		"duration", result.Duration,
		"total_bytes", result.TotalBytes,
		"dumps", len(result.DumpResults))

	return result, nil
}

// executeResticBackupJSON runs restic with JSON output for progress reporting.
func (r *BackupRunner) executeResticBackupJSON(ctx context.Context, paths []string, reporter ProgressReporter, serverID string) (string, error) {
	if r.manager.repository == nil {
		r.manager.repository = NewRepositoryManager(
			*r.manager.config,
			r.manager.restic.BinaryPath(),
			r.manager.dataDir,
		)
	}

	// Run backup for all paths combined
	var allPaths string
	for i, path := range paths {
		if i > 0 {
			allPaths += " "
		}
		allPaths += path
	}

	// Use BackupJSON for progress reporting
	var lastSnapshotID string
	for _, path := range paths {
		progressFn := func(line []byte) {
			if reporter != nil {
				progress := ParseResticProgress(string(line), "backing_up", "")
				if progress != nil {
					reporter.ReportProgress(*progress)
				}
			}
		}

		snapshotID, err := r.manager.repository.BackupJSON(ctx, path, []string{"manifest", "server-" + serverID}, progressFn)
		if err != nil {
			return "", fmt.Errorf("backup %s: %w", path, err)
		}
		lastSnapshotID = snapshotID
	}

	return lastSnapshotID, nil
}

// executeResticBackup runs the restic backup command and extracts the snapshot ID.
func (r *BackupRunner) executeResticBackup(ctx context.Context, args []string) (string, error) {
	// Ensure repository manager exists
	if r.manager.repository == nil {
		r.manager.repository = NewRepositoryManager(
			*r.manager.config,
			r.manager.restic.BinaryPath(),
			r.manager.dataDir,
		)
	}

	// Extract source paths from args (everything after "backup" and before flags)
	var paths []string
	inBackup := false
	for _, arg := range args {
		if arg == "backup" {
			inBackup = true
			continue
		}
		if inBackup && len(arg) > 0 && arg[0] != '-' {
			paths = append(paths, arg)
		}
	}

	// Use repository manager for each path
	var lastSnapshotID string
	for _, path := range paths {
		snapshotID, err := r.manager.repository.Backup(ctx, path, []string{"manifest"})
		if err != nil {
			return "", fmt.Errorf("backup %s: %w", path, err)
		}
		lastSnapshotID = snapshotID
	}

	return lastSnapshotID, nil
}

// PrepareVolumesForBackup runs database-specific commands before backup.
func (r *BackupRunner) PrepareVolumesForBackup(ctx context.Context, manifest *BackupManifest) error {
	for _, project := range manifest.Projects {
		for _, comp := range project.Components {
			if comp.Type == ComponentTypeVolume && comp.VolumeName != "" {
				// Determine DB type from component metadata
				dbType := ""
				switch {
				case contains(comp.VolumeName, "postgres"):
					dbType = "postgres"
				case contains(comp.VolumeName, "mysql"):
					dbType = "mysql"
				case contains(comp.VolumeName, "redis"):
					dbType = "redis"
				}

				info := DockerBackupInfo{
					VolumeName: comp.VolumeName,
					MountPath:  comp.MountPath,
					Project:    project.Name,
					DBType:     dbType,
				}

				if err := r.manager.dockerClient.PrepareVolumeForBackup(ctx, info); err != nil {
					slog.Warn("failed to prepare volume for backup",
						"volume", comp.VolumeName, "error", err)
					// Continue with other volumes
				}
			}
		}
	}
	return nil
}

// FinishVolumesAfterBackup runs database-specific commands after backup.
func (r *BackupRunner) FinishVolumesAfterBackup(ctx context.Context, manifest *BackupManifest) error {
	for _, project := range manifest.Projects {
		for _, comp := range project.Components {
			if comp.Type == ComponentTypeVolume && comp.VolumeName != "" {
				dbType := ""
				switch {
				case contains(comp.VolumeName, "postgres"):
					dbType = "postgres"
				case contains(comp.VolumeName, "mysql"):
					dbType = "mysql"
				case contains(comp.VolumeName, "redis"):
					dbType = "redis"
				}

				info := DockerBackupInfo{
					VolumeName: comp.VolumeName,
					MountPath:  comp.MountPath,
					Project:    project.Name,
					DBType:     dbType,
				}

				if err := r.manager.dockerClient.FinishVolumeForBackup(ctx, info); err != nil {
					slog.Warn("failed to finish volume backup",
						"volume", comp.VolumeName, "error", err)
				}
			}
		}
	}
	return nil
}

// GetDumpDir returns the dump directory path.
func (r *BackupRunner) GetDumpDir() string {
	return r.dumpDir
}

// GetManifestBuilder returns the manifest builder.
func (r *BackupRunner) GetManifestBuilder() *BackupManifestBuilder {
	return r.manifestBuilder
}

// GetDumper returns the database dumper.
func (r *BackupRunner) GetDumper() *Dumper {
	return r.dumper
}

// parseSnapshotIDFromOutput extracts the snapshot ID from restic backup output.
func parseSnapshotIDFromOutput(output string) string {
	// Look for line: "snapshot abc123 saved!"
	lines := splitLines(output)
	for _, line := range lines {
		if contains(line, "snapshot") && contains(line, "saved") {
			words := splitWords(line)
			for i, word := range words {
				if word == "snapshot" && i+1 < len(words) {
					return words[i+1]
				}
			}
		}
	}
	return ""
}
