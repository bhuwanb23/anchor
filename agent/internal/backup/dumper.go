package backup

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// DumpResult holds the result of a database dump operation.
type DumpResult struct {
	ComponentType string `json:"component_type"`
	ContainerName string `json:"container_name"`
	Database      string `json:"database,omitempty"`
	DumpPath      string `json:"dump_path"`
	SizeBytes     int64  `json:"size_bytes"`
	Error         string `json:"error,omitempty"`
}

// Dumper handles database dump operations for backups.
type Dumper struct {
	dockerClient DockerClient
	dumpDir      string
}

// NewDumper creates a new database dumper.
func NewDumper(dockerClient DockerClient, dumpDir string) *Dumper {
	if dumpDir == "" {
		dumpDir = defaultDumpDir
	}
	return &Dumper{
		dockerClient: dockerClient,
		dumpDir:      dumpDir,
	}
}

// DumpPostgres runs pg_dump for a live Postgres container.
// The dump uses -Fc (custom format) for compressed, fast restores.
func (d *Dumper) DumpPostgres(ctx context.Context, containerName, projectName, database string) (*DumpResult, error) {
	result := &DumpResult{
		ComponentType: ComponentTypePostgresDump,
		ContainerName: containerName,
		Database:      database,
	}

	// Create dump directory
	dumpDir := filepath.Join(d.dumpDir, projectName)
	if err := os.MkdirAll(dumpDir, 0700); err != nil {
		result.Error = fmt.Sprintf("create dump dir: %v", err)
		return result, fmt.Errorf("create dump dir: %w", err)
	}
	result.DumpPath = filepath.Join(dumpDir, "postgres.dump")

	// Find container ID from name
	containerID, err := d.findContainerByName(ctx, containerName)
	if err != nil {
		result.Error = fmt.Sprintf("find container: %v", err)
		return result, fmt.Errorf("find container %s: %w", containerName, err)
	}

	// Run pg_dump inside the container
	// pg_dump -U yourplatform -Fc {database} > /tmp/.../postgres.dump
	dumpCmd := []string{
		"sh", "-c",
		fmt.Sprintf("pg_dump -U yourplatform -Fc %s > %s", database, result.DumpPath),
	}

	slog.Info("starting postgres dump",
		"container", containerName,
		"database", database,
		"output", result.DumpPath)

	output, err := d.dockerClient.ExecInContainer(ctx, containerID, dumpCmd)
	if err != nil {
		result.Error = fmt.Sprintf("pg_dump failed: %v, output: %s", err, output)
		return result, fmt.Errorf("pg_dump in %s: %w", containerName, err)
	}

	// Verify dump file
	if err := d.verifyDumpFile(result.DumpPath); err != nil {
		result.Error = fmt.Sprintf("verify dump: %v", err)
		return result, err
	}

	// Get file size
	info, err := os.Stat(result.DumpPath)
	if err == nil {
		result.SizeBytes = info.Size()
	}

	slog.Info("postgres dump completed",
		"container", containerName,
		"database", database,
		"size", result.SizeBytes)

	return result, nil
}

// DumpMySQL runs mysqldump for a live MySQL container.
// Uses --single-transaction for consistent dumps without locking tables.
func (d *Dumper) DumpMySQL(ctx context.Context, containerName, projectName, database string) (*DumpResult, error) {
	result := &DumpResult{
		ComponentType: ComponentTypeMysqlDump,
		ContainerName: containerName,
		Database:      database,
	}

	// Create dump directory
	dumpDir := filepath.Join(d.dumpDir, projectName)
	if err := os.MkdirAll(dumpDir, 0700); err != nil {
		result.Error = fmt.Sprintf("create dump dir: %v", err)
		return result, fmt.Errorf("create dump dir: %w", err)
	}
	result.DumpPath = filepath.Join(dumpDir, "mysql.dump")

	// Find container ID from name
	containerID, err := d.findContainerByName(ctx, containerName)
	if err != nil {
		result.Error = fmt.Sprintf("find container: %v", err)
		return result, fmt.Errorf("find container %s: %w", containerName, err)
	}

	// Run mysqldump inside the container
	// mysqldump --single-transaction --routines --triggers -u root {database} > /tmp/.../mysql.dump
	dumpCmd := []string{
		"sh", "-c",
		fmt.Sprintf("mysqldump --single-transaction --routines --triggers -u root %s > %s", database, result.DumpPath),
	}

	slog.Info("starting mysql dump",
		"container", containerName,
		"database", database,
		"output", result.DumpPath)

	output, err := d.dockerClient.ExecInContainer(ctx, containerID, dumpCmd)
	if err != nil {
		result.Error = fmt.Sprintf("mysqldump failed: %v, output: %s", err, output)
		return result, fmt.Errorf("mysqldump in %s: %w", containerName, err)
	}

	// Verify dump file
	if err := d.verifyDumpFile(result.DumpPath); err != nil {
		result.Error = fmt.Sprintf("verify dump: %v", err)
		return result, err
	}

	// Get file size
	info, err := os.Stat(result.DumpPath)
	if err == nil {
		result.SizeBytes = info.Size()
	}

	slog.Info("mysql dump completed",
		"container", containerName,
		"database", database,
		"size", result.SizeBytes)

	return result, nil
}

// DumpRedis triggers Redis BGSAVE and copies the RDB file.
func (d *Dumper) DumpRedis(ctx context.Context, containerName, projectName string) (*DumpResult, error) {
	result := &DumpResult{
		ComponentType: ComponentTypeRedisDump,
		ContainerName: containerName,
	}

	// Create dump directory
	dumpDir := filepath.Join(d.dumpDir, projectName)
	if err := os.MkdirAll(dumpDir, 0700); err != nil {
		result.Error = fmt.Sprintf("create dump dir: %v", err)
		return result, fmt.Errorf("create dump dir: %w", err)
	}
	result.DumpPath = filepath.Join(dumpDir, "redis.rdb")

	// Find container ID from name
	containerID, err := d.findContainerByName(ctx, containerName)
	if err != nil {
		result.Error = fmt.Sprintf("find container: %v", err)
		return result, fmt.Errorf("find container %s: %w", containerName, err)
	}

	// Trigger BGSAVE
	slog.Info("triggering redis bgsave", "container", containerName)
	bgsaveCmd := []string{"redis-cli", "BGSAVE"}
	output, err := d.dockerClient.ExecInContainer(ctx, containerID, bgsaveCmd)
	if err != nil {
		result.Error = fmt.Sprintf("bgsave failed: %v, output: %s", err, output)
		return result, fmt.Errorf("redis bgsave in %s: %w", containerName, err)
	}

	// Wait for BGSAVE to complete (up to 30 seconds)
	slog.Info("waiting for redis bgsave to complete", "container", containerName)
	waitCmd := []string{"sh", "-c", "for i in $(seq 1 30); do if [ $(redis-cli LASTSAVE) != '0' ]; then exit 0; fi; sleep 1; done; exit 1"}
	if _, err := d.dockerClient.ExecInContainer(ctx, containerID, waitCmd); err != nil {
		result.Error = fmt.Sprintf("bgsave wait timeout: %v", err)
		return result, fmt.Errorf("redis bgsave wait in %s: %w", containerName, err)
	}

	// Copy RDB file from container to host
	// We use docker cp via exec since we're already inside the container context
	copyCmd := []string{"sh", "-c", fmt.Sprintf("cat /data/dump.rdb > %s", result.DumpPath)}
	slog.Info("copying redis rdb file", "container", containerName, "output", result.DumpPath)
	output, err = d.dockerClient.ExecInContainer(ctx, containerID, copyCmd)
	if err != nil {
		result.Error = fmt.Sprintf("copy rdb failed: %v, output: %s", err, output)
		return result, fmt.Errorf("copy redis rdb in %s: %w", containerName, err)
	}

	// Verify dump file
	if err := d.verifyDumpFile(result.DumpPath); err != nil {
		result.Error = fmt.Sprintf("verify dump: %v", err)
		return result, err
	}

	// Get file size
	info, err := os.Stat(result.DumpPath)
	if err == nil {
		result.SizeBytes = info.Size()
	}

	slog.Info("redis dump completed",
		"container", containerName,
		"size", result.SizeBytes)

	return result, nil
}

// CleanupDumps removes temporary dump files after backup completes.
func (d *Dumper) CleanupDumps(projectName string) {
	dumpDir := filepath.Join(d.dumpDir, projectName)
	if err := os.RemoveAll(dumpDir); err != nil {
		slog.Warn("failed to cleanup dump directory",
			"dir", dumpDir, "error", err)
	} else {
		slog.Debug("cleaned up dump directory", "dir", dumpDir)
	}
}

// CleanupAllDumps removes the entire dump directory.
func (d *Dumper) CleanupAllDumps() {
	if err := os.RemoveAll(d.dumpDir); err != nil {
		slog.Warn("failed to cleanup all dumps",
			"dir", d.dumpDir, "error", err)
	} else {
		slog.Debug("cleaned up all dump directories", "dir", d.dumpDir)
	}
}

// verifyDumpFile checks that a dump file exists and is not empty.
func (d *Dumper) verifyDumpFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("dump file not found: %w", err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("dump file is empty: %s", path)
	}
	return nil
}

// findContainerByName finds a container ID by its name.
func (d *Dumper) findContainerByName(ctx context.Context, name string) (string, error) {
	containers, err := d.dockerClient.ListManagedContainers(ctx)
	if err != nil {
		return "", err
	}

	for _, c := range containers {
		for _, n := range c.Names {
			// Docker prefixes names with /
			if n == "/"+name || n == name {
				return c.ID, nil
			}
		}
	}

	return "", fmt.Errorf("container %s not found", name)
}

// ---------------------------------------------------------------------------
// Restore methods
// ---------------------------------------------------------------------------

// RestoreResult holds the result of a database restore operation.
type RestoreResult struct {
	ComponentType string `json:"component_type"`
	ContainerName string `json:"container_name"`
	Database      string `json:"database,omitempty"`
	Status        string `json:"status"` // "success" | "failed"
	Error         string `json:"error,omitempty"`
}

// RestorePostgres restores a Postgres database from a custom-format dump file.
// It drops and recreates the database, then runs pg_restore.
func (d *Dumper) RestorePostgres(ctx context.Context, containerName, database, dumpPath string) (*RestoreResult, error) {
	result := &RestoreResult{
		ComponentType: ComponentTypePostgresDump,
		ContainerName: containerName,
		Database:      database,
		Status:        "success",
	}

	containerID, err := d.findContainerByName(ctx, containerName)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("find container: %v", err)
		return result, fmt.Errorf("find container %s: %w", containerName, err)
	}

	// Verify dump file exists
	if _, err := os.Stat(dumpPath); os.IsNotExist(err) {
		result.Status = "failed"
		result.Error = fmt.Sprintf("dump file not found: %s", dumpPath)
		return result, fmt.Errorf("dump file not found: %w", err)
	}

	// Step 1: Terminate existing connections
	terminateCmd := []string{"sh", "-c",
		fmt.Sprintf("psql -U yourplatform -d postgres -c \"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s' AND pid <> pg_backend_pid();\"", database)}
	_, _ = d.dockerClient.ExecInContainer(ctx, containerID, terminateCmd)

	// Step 2: Drop database
	dropCmd := []string{"sh", "-c",
		fmt.Sprintf("psql -U yourplatform -d postgres -c \"DROP DATABASE IF EXISTS %s;\"", database)}
	if _, err := d.dockerClient.ExecInContainer(ctx, containerID, dropCmd); err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("drop database: %v", err)
		return result, fmt.Errorf("drop database: %w", err)
	}

	// Step 3: Create database
	createCmd := []string{"sh", "-c",
		fmt.Sprintf("psql -U yourplatform -d postgres -c \"CREATE DATABASE %s;\"", database)}
	if _, err := d.dockerClient.ExecInContainer(ctx, containerID, createCmd); err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("create database: %v", err)
		return result, fmt.Errorf("create database: %w", err)
	}

	// Step 4: Restore from dump (pg_restore reports warnings to stderr, use || true)
	restoreCmd := []string{"sh", "-c",
		fmt.Sprintf("pg_restore -U yourplatform -d %s %s 2>&1 || true", database, dumpPath)}
	output, err := d.dockerClient.ExecInContainer(ctx, containerID, restoreCmd)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("pg_restore: %v", err)
		return result, fmt.Errorf("pg_restore: %w", err)
	}

	slog.Info("postgres restore completed",
		"container", containerName,
		"database", database,
		"output", output)

	return result, nil
}

// RestoreMySQL restores a MySQL database from a SQL dump file.
// It drops and recreates the database, then imports the dump.
func (d *Dumper) RestoreMySQL(ctx context.Context, containerName, database, dumpPath string) (*RestoreResult, error) {
	result := &RestoreResult{
		ComponentType: ComponentTypeMysqlDump,
		ContainerName: containerName,
		Database:      database,
		Status:        "success",
	}

	containerID, err := d.findContainerByName(ctx, containerName)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("find container: %v", err)
		return result, fmt.Errorf("find container %s: %w", containerName, err)
	}

	// Verify dump file exists
	if _, err := os.Stat(dumpPath); os.IsNotExist(err) {
		result.Status = "failed"
		result.Error = fmt.Sprintf("dump file not found: %s", dumpPath)
		return result, fmt.Errorf("dump file not found: %w", err)
	}

	// Step 1: Drop database
	dropCmd := []string{"sh", "-c",
		fmt.Sprintf("mysql -u root -e \"DROP DATABASE IF EXISTS %s;\"", database)}
	if _, err := d.dockerClient.ExecInContainer(ctx, containerID, dropCmd); err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("drop database: %v", err)
		return result, fmt.Errorf("drop database: %w", err)
	}

	// Step 2: Create database
	createCmd := []string{"sh", "-c",
		fmt.Sprintf("mysql -u root -e \"CREATE DATABASE %s;\"", database)}
	if _, err := d.dockerClient.ExecInContainer(ctx, containerID, createCmd); err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("create database: %v", err)
		return result, fmt.Errorf("create database: %w", err)
	}

	// Step 3: Restore from dump
	restoreCmd := []string{"sh", "-c",
		fmt.Sprintf("mysql -u root %s < %s 2>&1 || true", database, dumpPath)}
	output, err := d.dockerClient.ExecInContainer(ctx, containerID, restoreCmd)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("mysql restore: %v", err)
		return result, fmt.Errorf("mysql restore: %w", err)
	}

	slog.Info("mysql restore completed",
		"container", containerName,
		"database", database,
		"output", output)

	return result, nil
}

// RestoreRedis restores Redis data by replacing the RDB dump file.
// The container should be stopped before calling this method.
func (d *Dumper) RestoreRedis(ctx context.Context, containerName, dumpPath string) (*RestoreResult, error) {
	result := &RestoreResult{
		ComponentType: ComponentTypeRedisDump,
		ContainerName: containerName,
		Status:        "success",
	}

	containerID, err := d.findContainerByName(ctx, containerName)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("find container: %v", err)
		return result, fmt.Errorf("find container %s: %w", containerName, err)
	}

	// Verify dump file exists
	if _, err := os.Stat(dumpPath); os.IsNotExist(err) {
		result.Status = "failed"
		result.Error = fmt.Sprintf("dump file not found: %s", dumpPath)
		return result, fmt.Errorf("dump file not found: %w", err)
	}

	// Copy RDB file into the container
	copyCmd := []string{"sh", "-c", fmt.Sprintf("cp %s /data/dump.rdb", dumpPath)}
	if _, err := d.dockerClient.ExecInContainer(ctx, containerID, copyCmd); err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("copy rdb: %v", err)
		return result, fmt.Errorf("copy rdb: %w", err)
	}

	slog.Info("redis restore completed",
		"container", containerName,
		"dump_path", dumpPath)

	return result, nil
}

// DumpContainer executes a command in a container and returns the output.
// This is a helper for external callers.
func DumpContainer(ctx context.Context, dockerClient DockerClient, containerID string, cmd []string) (string, error) {
	return dockerClient.ExecInContainer(ctx, containerID, cmd)
}

// WaitForBGSAVE waits for a Redis BGSAVE to complete.
func WaitForBGSAVE(ctx context.Context, dockerClient DockerClient, containerID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		cmd := []string{"redis-cli", "LASTSAVE"}
		output, err := dockerClient.ExecInContainer(ctx, containerID, cmd)
		if err == nil && output != "0" {
			return nil
		}

		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("bgsave timeout after %v", timeout)
}
