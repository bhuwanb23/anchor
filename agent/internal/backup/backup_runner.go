package backup

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// BackupRunResult holds the result of a manifest-driven backup.
type BackupRunResult struct {
	Manifest    *BackupManifest `json:"manifest"`
	SnapshotID  string          `json:"snapshot_id"`
	DumpResults []*DumpResult   `json:"dump_results"`
	TotalBytes  int64           `json:"total_bytes"`
	Duration    time.Duration   `json:"duration"`
	Error       string          `json:"error,omitempty"`
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

	result := &BackupRunResult{}

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

	// Dump databases for each project
	for _, project := range manifest.Projects {
		for _, comp := range project.Components {
			var dumpResult *DumpResult

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
				}

			case ComponentTypeRedisDump:
				slog.Info("dumping redis database",
					"project", project.Name,
					"container", comp.Container)
				dumpResult, err = r.dumper.DumpRedis(ctx, comp.Container, project.Name)
				if err != nil {
					slog.Warn("redis dump failed",
						"project", project.Name, "error", err)
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
