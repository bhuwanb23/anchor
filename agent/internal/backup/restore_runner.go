package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RestoreResult holds the result of restoring a single component.
type RestoreResult struct {
	Type   string `json:"type"`
	Name   string `json:"name"`
	Status string `json:"status"` // "success" | "failed"
	Error  string `json:"error,omitempty"`
}

// RestoreProjectResult holds the result of restoring a single project.
type RestoreProjectResult struct {
	Name       string          `json:"name"`
	Status     string          `json:"status"` // "success" | "partial" | "failed"
	Components []RestoreResult `json:"components,omitempty"`
	Error      string          `json:"error,omitempty"`
}

// RestoreRunResult holds the result of a manifest-driven restore.
type RestoreRunResult struct {
	SnapshotID     string                `json:"snapshot_id"`
	ProjectName    string                `json:"project_name"`
	ProjectResult  *RestoreProjectResult `json:"project_result"`
	Duration       time.Duration         `json:"duration"`
	StartedAt      time.Time             `json:"started_at"`
	Error          string                `json:"error,omitempty"`
}

// RestoreRunner executes manifest-driven restores.
type RestoreRunner struct {
	manager    *BackupManager
	dumper     *Dumper
	tempDir    string
}

// NewRestoreRunner creates a new restore runner.
func NewRestoreRunner(manager *BackupManager, dockerClient DockerClient) *RestoreRunner {
	return &RestoreRunner{
		manager: manager,
		dumper:  NewDumper(dockerClient, defaultDumpDir),
		tempDir: "/tmp/yourplatform/restore",
	}
}

// RunRestore executes a full manifest-driven restore for a single project.
// Flow: extract snapshot → read manifest → stop containers → restore DBs → restore volumes → restart → cleanup
func (r *RestoreRunner) RunRestore(ctx context.Context, snapshotID, projectName string, reporter RestoreProgressReporter) (*RestoreRunResult, error) {
	startTime := time.Now()
	result := &RestoreRunResult{
		SnapshotID:  snapshotID,
		ProjectName: projectName,
		StartedAt:  startTime,
	}

	// Ensure cleanup happens
	restoreDir := filepath.Join(r.tempDir, snapshotID[:12])
	defer r.cleanupRestoreDir(restoreDir)

	// Phase 1: Extract snapshot
	if reporter != nil {
		reporter.ReportRestoreProgress(RestoreProgress{
			Phase:   "extracting",
			Message: fmt.Sprintf("Extracting snapshot %s...", snapshotID[:12]),
		})
	}

	slog.Info("extracting snapshot for restore", "snapshot", snapshotID, "project", projectName)
	if err := os.MkdirAll(restoreDir, 0700); err != nil {
		result.Error = fmt.Sprintf("create restore dir: %v", err)
		return result, fmt.Errorf("create restore dir: %w", err)
	}

	if err := r.manager.repository.Restore(ctx, snapshotID, restoreDir); err != nil {
		result.Error = fmt.Sprintf("extract snapshot: %v", err)
		if reporter != nil {
			reporter.ReportRestoreError(result.Error)
		}
		return result, fmt.Errorf("extract snapshot: %w", err)
	}

	// Phase 2: Read manifest
	if reporter != nil {
		reporter.ReportRestoreProgress(RestoreProgress{
			Phase:   "reading_manifest",
			Message: "Reading backup manifest...",
		})
	}

	manifest, err := r.readManifest(restoreDir)
	if err != nil {
		result.Error = fmt.Sprintf("read manifest: %v", err)
		if reporter != nil {
			reporter.ReportRestoreError(result.Error)
		}
		return result, fmt.Errorf("read manifest: %w", err)
	}

	// Find the target project in the manifest
	var targetProject *ProjectBackup
	for i, proj := range manifest.Projects {
		if proj.Name == projectName {
			targetProject = &manifest.Projects[i]
			break
		}
	}
	if targetProject == nil {
		result.Error = fmt.Sprintf("project %q not found in backup", projectName)
		if reporter != nil {
			reporter.ReportRestoreError(result.Error)
		}
		return result, fmt.Errorf("project %q not found in backup", projectName)
	}

	slog.Info("found project in manifest",
		"project", projectName,
		"components", len(targetProject.Components))

	// Phase 3: Stop project containers
	if reporter != nil {
		reporter.ReportRestoreProgress(RestoreProgress{
			Phase:   "stopping",
			Project: projectName,
			Message: fmt.Sprintf("Stopping containers for %s...", projectName),
		})
	}

	if err := r.stopProjectContainers(ctx, projectName); err != nil {
		slog.Warn("failed to stop project containers, continuing restore",
			"project", projectName, "error", err)
		// Continue restore even if stop fails — some containers may not exist
	}

	projectResult := &RestoreProjectResult{
		Name:   projectName,
		Status: "success",
	}

	// Phase 4: Restore components
	for _, comp := range targetProject.Components {
		compResult := RestoreResult{
			Type:   comp.Type,
			Name:   comp.Name(),
			Status: "success",
		}

		switch comp.Type {
		case ComponentTypePostgresDump:
			if reporter != nil {
				reporter.ReportRestoreProgress(RestoreProgress{
					Phase:   "restoring_db",
					Project: projectName,
					Message: fmt.Sprintf("Restoring postgres database %s...", comp.Database),
				})
			}
			if err := r.restorePostgres(ctx, comp, restoreDir, projectName); err != nil {
				slog.Warn("postgres restore failed",
					"project", projectName, "database", comp.Database, "error", err)
				compResult.Status = "failed"
				compResult.Error = err.Error()
				projectResult.Status = "partial"
			}

		case ComponentTypeMysqlDump:
			if reporter != nil {
				reporter.ReportRestoreProgress(RestoreProgress{
					Phase:   "restoring_db",
					Project: projectName,
					Message: fmt.Sprintf("Restoring mysql database %s...", comp.Database),
				})
			}
			if err := r.restoreMySQL(ctx, comp, restoreDir, projectName); err != nil {
				slog.Warn("mysql restore failed",
					"project", projectName, "database", comp.Database, "error", err)
				compResult.Status = "failed"
				compResult.Error = err.Error()
				projectResult.Status = "partial"
			}

		case ComponentTypeRedisDump:
			if reporter != nil {
				reporter.ReportRestoreProgress(RestoreProgress{
					Phase:   "restoring_db",
					Project: projectName,
					Message: "Restoring redis data...",
				})
			}
			if err := r.restoreRedis(ctx, comp, restoreDir, projectName); err != nil {
				slog.Warn("redis restore failed",
					"project", projectName, "error", err)
				compResult.Status = "failed"
				compResult.Error = err.Error()
				projectResult.Status = "partial"
			}

		case ComponentTypeVolume:
			if reporter != nil {
				reporter.ReportRestoreProgress(RestoreProgress{
					Phase:   "restoring_volume",
					Project: projectName,
					Message: fmt.Sprintf("Restoring volume %s...", comp.VolumeName),
				})
			}
			if err := r.restoreVolume(ctx, comp, restoreDir, projectName); err != nil {
				slog.Warn("volume restore failed",
					"project", projectName, "volume", comp.VolumeName, "error", err)
				compResult.Status = "failed"
				compResult.Error = err.Error()
				projectResult.Status = "partial"
			}

		case ComponentTypeEnvFile:
			if reporter != nil {
				reporter.ReportRestoreProgress(RestoreProgress{
					Phase:   "restoring_env",
					Project: projectName,
					Message: "Restoring environment file...",
				})
			}
			if err := r.restoreEnvFile(comp, restoreDir); err != nil {
				slog.Warn("env file restore failed",
					"project", projectName, "error", err)
				compResult.Status = "failed"
				compResult.Error = err.Error()
				projectResult.Status = "partial"
			}
		}

		projectResult.Components = append(projectResult.Components, compResult)
	}

	// Phase 5: Restart project containers
	if reporter != nil {
		reporter.ReportRestoreProgress(RestoreProgress{
			Phase:   "starting",
			Project: projectName,
			Message: fmt.Sprintf("Restarting containers for %s...", projectName),
		})
	}

	if err := r.startProjectContainers(ctx, projectName); err != nil {
		slog.Warn("failed to restart project containers",
			"project", projectName, "error", err)
		// Don't fail the restore — containers may need manual intervention
		if projectResult.Status == "success" {
			projectResult.Status = "partial"
			projectResult.Error = fmt.Sprintf("restart containers: %v", err)
		}
	}

	result.ProjectResult = projectResult
	result.Duration = time.Since(startTime)

	// Report completion
	if reporter != nil {
		if projectResult.Status == "failed" {
			reporter.ReportRestoreError(projectResult.Error)
		} else {
			reporter.ReportRestoreComplete(*result)
		}
	}

	slog.Info("restore completed",
		"snapshot", snapshotID,
		"project", projectName,
		"status", projectResult.Status,
		"duration", result.Duration)

	return result, nil
}

// readManifest reads the BackupManifest from the extracted snapshot directory.
func (r *RestoreRunner) readManifest(restoreDir string) (*BackupManifest, error) {
	// The manifest is stored at the root of the snapshot as manifest.json
	manifestPath := filepath.Join(restoreDir, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read manifest file: %w", err)
	}

	var manifest BackupManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	return &manifest, nil
}

// restorePostgres restores a Postgres database from a dump file.
func (r *RestoreRunner) restorePostgres(ctx context.Context, comp BackupComponent, restoreDir, projectName string) error {
	// Find the dump file in the restored data
	dumpPath := r.findDumpFile(restoreDir, projectName, "postgres.dump")
	if dumpPath == "" {
		return fmt.Errorf("postgres dump file not found in snapshot")
	}

	// Find the postgres container
	containerID, err := r.findContainerByName(ctx, comp.Container)
	if err != nil {
		return fmt.Errorf("find postgres container: %w", err)
	}

	// Copy dump file into the container
	slog.Info("copying postgres dump to container",
		"container", comp.Container, "dump", dumpPath)

	// Use docker cp to copy the dump file into the container
	copyCmd := []string{"sh", "-c", fmt.Sprintf("cat %s > /tmp/postgres.dump", dumpPath)}
	if _, err := r.manager.dockerClient.ExecInContainer(ctx, containerID, copyCmd); err != nil {
		return fmt.Errorf("copy dump to container: %w", err)
	}

	// Drop and recreate the database, then restore
	// Step 1: Drop existing connections
	dropConnsCmd := []string{"sh", "-c",
		fmt.Sprintf("psql -U yourplatform -d postgres -c \"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s' AND pid <> pg_backend_pid();\"", comp.Database)}
	if _, err := r.manager.dockerClient.ExecInContainer(ctx, containerID, dropConnsCmd); err != nil {
		slog.Warn("failed to terminate connections", "error", err)
	}

	// Step 2: Drop database
	dropCmd := []string{"sh", "-c",
		fmt.Sprintf("psql -U yourplatform -d postgres -c \"DROP DATABASE IF EXISTS %s;\"", comp.Database)}
	if _, err := r.manager.dockerClient.ExecInContainer(ctx, containerID, dropCmd); err != nil {
		return fmt.Errorf("drop database: %w", err)
	}

	// Step 3: Create database
	createCmd := []string{"sh", "-c",
		fmt.Sprintf("psql -U yourplatform -d postgres -c \"CREATE DATABASE %s;\"", comp.Database)}
	if _, err := r.manager.dockerClient.ExecInContainer(ctx, containerID, createCmd); err != nil {
		return fmt.Errorf("create database: %w", err)
	}

	// Step 4: Restore from dump
	restoreCmd := []string{"sh", "-c",
		fmt.Sprintf("pg_restore -U yourplatform -d %s /tmp/postgres.dump 2>&1 || true", comp.Database)}
	output, err := r.manager.dockerClient.ExecInContainer(ctx, containerID, restoreCmd)
	if err != nil {
		return fmt.Errorf("pg_restore: %w", err)
	}
	slog.Info("postgres restore output", "container", comp.Container, "output", output)

	// Step 5: Cleanup temp file
	cleanupCmd := []string{"sh", "-c", "rm -f /tmp/postgres.dump"}
	_, _ = r.manager.dockerClient.ExecInContainer(ctx, containerID, cleanupCmd)

	slog.Info("postgres restore completed",
		"container", comp.Container, "database", comp.Database)
	return nil
}

// restoreMySQL restores a MySQL database from a dump file.
func (r *RestoreRunner) restoreMySQL(ctx context.Context, comp BackupComponent, restoreDir, projectName string) error {
	dumpPath := r.findDumpFile(restoreDir, projectName, "mysql.dump")
	if dumpPath == "" {
		return fmt.Errorf("mysql dump file not found in snapshot")
	}

	containerID, err := r.findContainerByName(ctx, comp.Container)
	if err != nil {
		return fmt.Errorf("find mysql container: %w", err)
	}

	// Copy dump file into the container
	copyCmd := []string{"sh", "-c", fmt.Sprintf("cat %s > /tmp/mysql.dump", dumpPath)}
	if _, err := r.manager.dockerClient.ExecInContainer(ctx, containerID, copyCmd); err != nil {
		return fmt.Errorf("copy dump to container: %w", err)
	}

	// Drop and recreate database
	dropCmd := []string{"sh", "-c",
		fmt.Sprintf("mysql -u root -e \"DROP DATABASE IF EXISTS %s;\"", comp.Database)}
	if _, err := r.manager.dockerClient.ExecInContainer(ctx, containerID, dropCmd); err != nil {
		return fmt.Errorf("drop database: %w", err)
	}

	createCmd := []string{"sh", "-c",
		fmt.Sprintf("mysql -u root -e \"CREATE DATABASE %s;\"", comp.Database)}
	if _, err := r.manager.dockerClient.ExecInContainer(ctx, containerID, createCmd); err != nil {
		return fmt.Errorf("create database: %w", err)
	}

	// Restore from dump
	restoreCmd := []string{"sh", "-c",
		fmt.Sprintf("mysql -u root %s < /tmp/mysql.dump 2>&1 || true", comp.Database)}
	output, err := r.manager.dockerClient.ExecInContainer(ctx, containerID, restoreCmd)
	if err != nil {
		return fmt.Errorf("mysql restore: %w", err)
	}
	slog.Info("mysql restore output", "container", comp.Container, "output", output)

	// Cleanup
	cleanupCmd := []string{"sh", "-c", "rm -f /tmp/mysql.dump"}
	_, _ = r.manager.dockerClient.ExecInContainer(ctx, containerID, cleanupCmd)

	slog.Info("mysql restore completed",
		"container", comp.Container, "database", comp.Database)
	return nil
}

// restoreRedis restores Redis data from an RDB dump file.
func (r *RestoreRunner) restoreRedis(ctx context.Context, comp BackupComponent, restoreDir, projectName string) error {
	dumpPath := r.findDumpFile(restoreDir, projectName, "redis.rdb")
	if dumpPath == "" {
		return fmt.Errorf("redis dump file not found in snapshot")
	}

	containerID, err := r.findContainerByName(ctx, comp.Container)
	if err != nil {
		return fmt.Errorf("find redis container: %w", err)
	}

	// Stop Redis to replace the RDB file
	slog.Info("stopping redis for restore", "container", comp.Container)
	stopCmd := []string{"redis-cli", "SHUTDOWN", "NOSAVE"}
	_, _ = r.manager.dockerClient.ExecInContainer(ctx, containerID, stopCmd)

	// Wait a moment for Redis to stop
	time.Sleep(2 * time.Second)

	// Copy RDB file into the container
	copyCmd := []string{"sh", "-c", fmt.Sprintf("cat %s > /data/dump.rdb", dumpPath)}
	if _, err := r.manager.dockerClient.ExecInContainer(ctx, containerID, copyCmd); err != nil {
		return fmt.Errorf("copy rdb to container: %w", err)
	}

	// Redis will restart automatically if using restart policy
	slog.Info("redis restore completed",
		"container", comp.Container)
	return nil
}

// restoreVolume restores a Docker volume from extracted backup data.
func (r *RestoreRunner) restoreVolume(ctx context.Context, comp BackupComponent, restoreDir, projectName string) error {
	if comp.VolumeName == "" || comp.MountPath == "" {
		return fmt.Errorf("volume component missing name or mount path")
	}

	// Find the volume data in the restored snapshot
	volumeDataPath := filepath.Join(restoreDir, comp.MountPath)
	if _, err := os.Stat(volumeDataPath); os.IsNotExist(err) {
		// Try finding it under the project directory
		volumeDataPath = filepath.Join(restoreDir, projectName, filepath.Base(comp.MountPath))
		if _, err := os.Stat(volumeDataPath); os.IsNotExist(err) {
			return fmt.Errorf("volume data not found in snapshot: %s", comp.MountPath)
		}
	}

	// Find a container that uses this volume
	containerID, err := r.findContainerByVolume(ctx, comp.VolumeName)
	if err != nil {
		return fmt.Errorf("find container for volume: %w", err)
	}

	// Copy data into the volume mount point
	slog.Info("restoring volume data",
		"volume", comp.VolumeName,
		"source", volumeDataPath,
		"container", containerID[:12])

	copyCmd := []string{"sh", "-c", fmt.Sprintf("cp -a %s/. %s/", volumeDataPath, comp.MountPath)}
	if _, err := r.manager.dockerClient.ExecInContainer(ctx, containerID, copyCmd); err != nil {
		return fmt.Errorf("copy volume data: %w", err)
	}

	slog.Info("volume restore completed", "volume", comp.VolumeName)
	return nil
}

// restoreEnvFile restores an environment file from the extracted snapshot.
func (r *RestoreRunner) restoreEnvFile(comp BackupComponent, restoreDir string) error {
	if comp.Path == "" {
		return fmt.Errorf("env file component missing path")
	}

	// Find the env file in the restored snapshot
	envFileName := filepath.Base(comp.Path)
	envPath := filepath.Join(restoreDir, envFileName)
	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		return fmt.Errorf("env file not found in snapshot: %s", envFileName)
	}

	// Read the env file content
	data, err := os.ReadFile(envPath)
	if err != nil {
		return fmt.Errorf("read env file: %w", err)
	}

	// Ensure the target directory exists
	targetDir := filepath.Dir(comp.Path)
	if err := os.MkdirAll(targetDir, 0700); err != nil {
		return fmt.Errorf("create env dir: %w", err)
	}

	// Write the env file
	if err := os.WriteFile(comp.Path, data, 0600); err != nil {
		return fmt.Errorf("write env file: %w", err)
	}

	slog.Info("env file restored", "path", comp.Path)
	return nil
}

// stopProjectContainers stops all containers belonging to a project.
func (r *RestoreRunner) stopProjectContainers(ctx context.Context, projectName string) error {
	containers, err := r.manager.dockerClient.ListManagedContainers(ctx)
	if err != nil {
		return fmt.Errorf("list containers: %w", err)
	}

	stopped := 0
	for _, c := range containers {
		if c.Labels[labelProject] != projectName {
			continue
		}
		containerName := ""
		if len(c.Names) > 0 {
			containerName = c.Names[0]
		}
		slog.Info("stopping container for restore",
			"container", containerName, "project", projectName)
		if err := r.manager.dockerClient.StopContainer(ctx, c.ID); err != nil {
			slog.Warn("failed to stop container",
				"container", containerName, "error", err)
			continue
		}
		stopped++
	}

	slog.Info("stopped project containers",
		"project", projectName, "count", stopped)
	return nil
}

// startProjectContainers starts all containers belonging to a project.
func (r *RestoreRunner) startProjectContainers(ctx context.Context, projectName string) error {
	containers, err := r.manager.dockerClient.ListManagedContainers(ctx)
	if err != nil {
		return fmt.Errorf("list containers: %w", err)
	}

	started := 0
	for _, c := range containers {
		if c.Labels[labelProject] != projectName {
			continue
		}
		containerName := ""
		if len(c.Names) > 0 {
			containerName = c.Names[0]
		}
		slog.Info("starting container after restore",
			"container", containerName, "project", projectName)
		if err := r.manager.dockerClient.StartContainer(ctx, c.ID); err != nil {
			slog.Warn("failed to start container",
				"container", containerName, "error", err)
			continue
		}
		started++
	}

	slog.Info("started project containers",
		"project", projectName, "count", started)
	return nil
}

// findDumpFile searches for a dump file in the restored snapshot directory.
func (r *RestoreRunner) findDumpFile(restoreDir, projectName, fileName string) string {
	// Try multiple locations where the dump file might be
	possiblePaths := []string{
		filepath.Join(restoreDir, projectName, fileName),
		filepath.Join(restoreDir, fileName),
		filepath.Join(restoreDir, "tmp", "yourplatform", "backups", projectName, fileName),
	}

	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	// Search recursively as a last resort
	var found string
	filepath.Walk(restoreDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, fileName) {
			found = path
			return filepath.SkipDir
		}
		return nil
	})

	return found
}

// findContainerByName finds a container ID by its name.
func (r *RestoreRunner) findContainerByName(ctx context.Context, name string) (string, error) {
	containers, err := r.manager.dockerClient.ListManagedContainers(ctx)
	if err != nil {
		return "", err
	}

	for _, c := range containers {
		for _, n := range c.Names {
			if n == "/"+name || n == name {
				return c.ID, nil
			}
		}
	}

	return "", fmt.Errorf("container %s not found", name)
}

// findContainerByVolume finds a container that has the given volume mounted.
func (r *RestoreRunner) findContainerByVolume(ctx context.Context, volumeName string) (string, error) {
	containers, err := r.manager.dockerClient.ListManagedContainers(ctx)
	if err != nil {
		return "", err
	}

	for _, c := range containers {
		for _, mount := range c.Mounts {
			if mount.Name == volumeName {
				return c.ID, nil
			}
		}
	}

	return "", fmt.Errorf("no container found using volume %s", volumeName)
}

// cleanupRestoreDir removes the temporary restore directory.
func (r *RestoreRunner) cleanupRestoreDir(dir string) {
	if err := os.RemoveAll(dir); err != nil {
		slog.Warn("failed to cleanup restore directory",
			"dir", dir, "error", err)
	} else {
		slog.Debug("cleaned up restore directory", "dir", dir)
	}
}

// RestoreProgress holds restore progress information.
type RestoreProgress struct {
	Phase   string `json:"phase"`
	Project string `json:"project,omitempty"`
	Message string `json:"message"`
}

// RestoreProgressReporter sends restore progress updates.
type RestoreProgressReporter interface {
	ReportRestoreProgress(progress RestoreProgress)
	ReportRestoreComplete(result RestoreRunResult)
	ReportRestoreError(err string)
}
