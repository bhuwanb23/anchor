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
