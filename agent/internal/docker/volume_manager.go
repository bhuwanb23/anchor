package docker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/volume"
)

// ---------------------------------------------------------------------------
// Volume naming
// ---------------------------------------------------------------------------

// volumeNamePrefix is the prefix for all volumes managed by the agent.
const volumeNamePrefix = "yourplatform_"

// VolumeName constructs a consistent, predictable volume name.
//
//	VolumeName("My Shop!", "postgres-data") → "yourplatform_my-shop_postgres-data"
//	VolumeName("blog", "uploads")           → "yourplatform_blog_uploads"
func VolumeName(projectName, purpose string) string {
	return volumeNamePrefix + SanitizeProjectName(projectName) + "_" + purpose
}

// ---------------------------------------------------------------------------
// Volume creation / reuse
// ---------------------------------------------------------------------------

// VolumeLabels returns the standard labels for a project volume.
func VolumeLabels(projectName string, purpose string) map[string]string {
	return map[string]string{
		labelOwner:            labelOwnerValue,
		"yourplatform.project":  SanitizeProjectName(projectName),
		"yourplatform.purpose":  purpose,
		"yourplatform.created":  time.Now().UTC().Format(time.RFC3339),
	}
}

// EnsureVolume creates a Docker volume if it doesn't exist,
// or returns the existing volume. Idempotent — safe to call
// on every deploy. Existing data is preserved on redeploy.
func (c *Client) EnsureVolume(ctx context.Context, projectName, purpose string) (*types.Volume, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, fmt.Errorf("docker unavailable: %w", err)
	}

	volName := VolumeName(projectName, purpose)

	// Check if volume already exists (reuse = preserve existing data)
	existing, err := c.findVolumeByName(ctx, volName)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		slog.Debug("reusing existing volume",
			"volume", volName,
			"project", SanitizeProjectName(projectName),
			"purpose", purpose,
		)
		return existing, nil
	}

	// Create the volume with labels
	projectSafe := SanitizeProjectName(projectName)
	resp, err := c.cliUnsafe().VolumeCreate(ctx, volume.VolumeCreateBody{
		Driver: "local",
		Labels: VolumeLabels(projectName, purpose),
		Name:   volName,
	})
	if err != nil {
		return nil, fmt.Errorf("create volume %s: %w", volName, err)
	}

	slog.Info("created volume",
		"volume", volName,
		"project", projectSafe,
		"purpose", purpose,
		"driver", resp.Driver,
		"mountpoint", resp.Mountpoint,
	)

	return &resp, nil
}

// ---------------------------------------------------------------------------
// Volume mount config
// ---------------------------------------------------------------------------

// VolumeMount describes a volume to mount into a container.
type VolumeMount struct {
	Name      string // Docker volume name
	MountPath string // Absolute path inside the container (e.g., /var/lib/postgresql/data)
	ReadOnly  bool   // Mount as read-only
}

// Purpose constants for standard mount paths.
const (
	VolumePurposePostgresData = "postgres-data"
	VolumePurposeMySQLData    = "mysql-data"
	VolumePurposeRedisData    = "redis-data"
	VolumePurposeUploads      = "uploads"
	VolumePurposeAppData      = "app-data"
)

// DBVolumePurpose returns the standard volume purpose string for a database type.
func DBVolumePurpose(dbType ContainerType) string {
	switch dbType {
	case ContainerTypePostgres:
		return VolumePurposePostgresData
	case ContainerTypeMySQL:
		return VolumePurposeMySQLData
	case ContainerTypeRedis:
		return VolumePurposeRedisData
	default:
		return VolumePurposeAppData
	}
}

// DBVolumeMountPath returns the standard data directory inside a database container.
func DBVolumeMountPath(dbType ContainerType) string {
	switch dbType {
	case ContainerTypePostgres:
		return "/var/lib/postgresql/data"
	case ContainerTypeMySQL:
		return "/var/lib/mysql"
	case ContainerTypeRedis:
		return "/data"
	default:
		return "/data"
	}
}

// EnsureDBVolume is a convenience that creates a volume for a database container
// and returns a VolumeMount config with the correct mount path.
func (c *Client) EnsureDBVolume(ctx context.Context, projectName string, dbType ContainerType) (*VolumeMount, error) {
	purpose := DBVolumePurpose(dbType)
	vol, err := c.EnsureVolume(ctx, projectName, purpose)
	if err != nil {
		return nil, err
	}

	return &VolumeMount{
		Name:      vol.Name,
		MountPath: DBVolumeMountPath(dbType),
		ReadOnly:  false,
	}, nil
}

// ---------------------------------------------------------------------------
// Backup integration (Layer 3C interface)
// ---------------------------------------------------------------------------

// BackupInfo provides Layer 3C with the information needed to back up a volume.
// Layer 3A stores this per deployment; Layer 3C queries it via the deployment record.
type BackupInfo struct {
	VolumeName string        `json:"volume_name"`
	MountPath  string        `json:"mount_path"`
	Project    string        `json:"project"`
	DBType     ContainerType `json:"db_type,omitempty"`
}

// PrepareVolumeForBackup runs database-specific commands to ensure a consistent
// backup state. For Postgres: CHECKPOINT. For MySQL: FLUSH TABLES WITH READ LOCK.
// For Redis: BGSAVE. For generic volumes: nothing needed.
func (c *Client) PrepareVolumeForBackup(ctx context.Context, info BackupInfo) error {
	if info.DBType == "" || info.DBType == ContainerTypeApp {
		slog.Debug("no backup preparation needed for app volume", "volume", info.VolumeName)
		return nil
	}

	if err := c.ensureConnected(ctx); err != nil {
		return fmt.Errorf("docker unavailable: %w", err)
	}

	// Find the container that has this volume mounted
	containerID, err := c.findContainerByVolume(ctx, info.VolumeName)
	if err != nil {
		return fmt.Errorf("find container for volume %s: %w", info.VolumeName, err)
	}
	if containerID == "" {
		slog.Warn("no running container found for volume, skipping backup prep",
			"volume", info.VolumeName)
		return nil
	}

	switch info.DBType {
	case ContainerTypePostgres:
		return c.execSQL(ctx, containerID, "CHECKPOINT;")
	case ContainerTypeMySQL:
		return c.execSQL(ctx, containerID, "FLUSH TABLES WITH READ LOCK;")
	case ContainerTypeRedis:
		return c.execSave(ctx, containerID)
	default:
		slog.Debug("no backup preparation for unknown db type", "db_type", info.DBType)
		return nil
	}
}

// FinishVolumeBackup runs database-specific commands to resume normal
// operation after backup. For MySQL: UNLOCK TABLES.
func (c *Client) FinishVolumeBackup(ctx context.Context, info BackupInfo) error {
	if info.DBType != ContainerTypeMySQL {
		return nil // Postgres CHECKPOINT and Redis BGSAVE don't need unlock
	}

	if err := c.ensureConnected(ctx); err != nil {
		return fmt.Errorf("docker unavailable: %w", err)
	}

	containerID, err := c.findContainerByVolume(ctx, info.VolumeName)
	if err != nil {
		return fmt.Errorf("find container for volume %s: %w", info.VolumeName, err)
	}
	if containerID == "" {
		return nil
	}

	return c.execSQL(ctx, containerID, "UNLOCK TABLES;")
}

// execSQL runs a SQL command inside a container using psql or mysql CLI.
func (c *Client) execSQL(ctx context.Context, containerID, query string) error {
	slog.Debug("executing SQL in container", "container", containerID[:12])

	// Try psql first (Postgres), fall back to mysql
	execConfig := types.ExecConfig{
		Cmd:          []string{"psql", "-c", query},
		AttachStdout: true,
		AttachStderr: true,
		AttachStdin:  false, // ensure no stdin wait
	}

	execID, err := c.cliUnsafe().ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		// Try MySQL
		execConfig.Cmd = []string{"mysql", "-e", query}
		execID2, err2 := c.cliUnsafe().ContainerExecCreate(ctx, containerID, execConfig)
		if err2 != nil {
			slog.Warn("failed to create exec for SQL backup prep",
				"container", containerID[:12], "psql_err", err, "mysql_err", err2)
			return fmt.Errorf("exec SQL in %s: psql: %v, mysql: %v", containerID[:12], err, err2)
		}
		execID = execID2
	}

	if err := c.cliUnsafe().ContainerExecStart(ctx, execID.ID, types.ExecStartCheck{}); err != nil {
		return fmt.Errorf("exec SQL start: %w", err)
	}

	return nil
}

// execSave triggers a Redis BGSAVE.
func (c *Client) execSave(ctx context.Context, containerID string) error {
	slog.Debug("executing Redis BGSAVE in container", "container", containerID[:12])

	execConfig := types.ExecConfig{
		Cmd:          []string{"redis-cli", "BGSAVE"},
		AttachStdout: true,
		AttachStderr: true,
	}

	execID, err := c.cliUnsafe().ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return fmt.Errorf("exec Redis BGSAVE in %s: %w", containerID[:12], err)
	}

	if err := c.cliUnsafe().ContainerExecStart(ctx, execID.ID, types.ExecStartCheck{}); err != nil {
		return fmt.Errorf("exec Redis BGSAVE start: %w", err)
	}

	return nil
}

// findContainerByVolume looks for a running container that has the given
// volume mounted. Returns the container ID or empty string if not found.
func (c *Client) findContainerByVolume(ctx context.Context, volumeName string) (string, error) {
	containers, err := c.cliUnsafe().ContainerList(ctx, types.ContainerListOptions{})
	if err != nil {
		return "", fmt.Errorf("list containers: %w", err)
	}

	for _, container := range containers {
		for _, mount := range container.Mounts {
			if mount.Name == volumeName {
				return container.ID, nil
			}
		}
	}

	return "", nil
}

// ---------------------------------------------------------------------------
// Volume cleanup
// ---------------------------------------------------------------------------

// ListUnmountedVolumes returns all yourplatform volumes that are not currently
// mounted by any container. These are safe candidates for removal.
// The agent never auto-deletes volumes — this is for dashboard display only.
func (c *Client) ListUnmountedVolumes(ctx context.Context) ([]*types.Volume, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, fmt.Errorf("docker unavailable: %w", err)
	}

	all, err := c.cliUnsafe().VolumeList(ctx, labelFilter(labelOwner, labelOwnerValue))
	if err != nil {
		return nil, fmt.Errorf("list volumes: %w", err)
	}

	// Get all containers with their mounts
	containers, err := c.cliUnsafe().ContainerList(ctx, types.ContainerListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}

	// Build set of mounted volume names
	mounted := make(map[string]bool)
	for _, cont := range containers {
		for _, m := range cont.Mounts {
			if m.Name != "" {
				mounted[m.Name] = true
			}
		}
	}

	var unmounted []*types.Volume
	for _, v := range all.Volumes {
		if !mounted[v.Name] {
			unmounted = append(unmounted, v)
		}
	}

	return unmounted, nil
}

// RemoveVolume removes a single Docker volume.
// Verifies no container is currently using the volume before removal.
// Returns an error if the volume is still in use.
func (c *Client) RemoveVolume(ctx context.Context, name string) error {
	if err := c.ensureConnected(ctx); err != nil {
		return fmt.Errorf("docker unavailable: %w", err)
	}

	// Verify no container is currently mounted to this volume
	containerID, err := c.findContainerByVolume(ctx, name)
	if err != nil {
		return fmt.Errorf("check volume usage: %w", err)
	}
	if containerID != "" {
		return fmt.Errorf("volume %s is currently used by container %s — stop the container first",
			name, containerID[:12])
	}

	if err := c.cliUnsafe().VolumeRemove(ctx, name, true); err != nil {
		return fmt.Errorf("remove volume %s: %w", name, err)
	}

	slog.Info("removed volume", "volume", name)
	return nil
}

// CleanupOrphanedVolumes removes yourplatform volumes that are not mounted
// by any container (running or stopped). Volumes still in use are skipped.
// Returns the count of removed volumes.
func (c *Client) CleanupOrphanedVolumes(ctx context.Context) (int, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return 0, fmt.Errorf("docker unavailable: %w", err)
	}

	unmounted, err := c.ListUnmountedVolumes(ctx)
	if err != nil {
		return 0, err
	}

	removed := 0
	for _, v := range unmounted {
		slog.Info("removing orphaned volume", "volume", v.Name)
		if err := c.cliUnsafe().VolumeRemove(ctx, v.Name, true); err != nil {
			slog.Warn("failed to remove orphaned volume", "volume", v.Name, "error", err)
			continue
		}
		removed++
	}

	if removed > 0 {
		slog.Info("cleaned up orphaned volumes", "count", removed)
	} else {
		slog.Debug("no orphaned volumes to remove")
	}

	return removed, nil
}

// ListProjectVolumes returns all volumes for a specific project.
func (c *Client) ListProjectVolumes(ctx context.Context, projectName string) ([]*types.Volume, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, fmt.Errorf("docker unavailable: %w", err)
	}

	projectSafe := SanitizeProjectName(projectName)
	vols, err := c.cliUnsafe().VolumeList(ctx, filterProjectVolumes(projectSafe))
	if err != nil {
		return nil, fmt.Errorf("list volumes for project %s: %w", projectSafe, err)
	}

	return vols.Volumes, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// findVolumeByName looks up a Docker volume by name.
// Returns nil (no error) if the volume doesn't exist.
func (c *Client) findVolumeByName(ctx context.Context, name string) (*types.Volume, error) {
	vols, err := c.cliUnsafe().VolumeList(ctx, filters.NewArgs(filters.Arg("name", name)))
	if err != nil {
		return nil, fmt.Errorf("list volumes by name %s: %w", name, err)
	}

	for _, v := range vols.Volumes {
		if v.Name == name {
			return v, nil
		}
	}

	return nil, nil
}
