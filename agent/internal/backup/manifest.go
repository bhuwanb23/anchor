package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
)

const (
	ComponentTypePostgresDump = "postgres_dump"
	ComponentTypeMysqlDump    = "mysql_dump"
	ComponentTypeRedisDump    = "redis_dump"
	ComponentTypeVolume       = "volume"
	ComponentTypeEnvFile      = "env_file"

	PlatformDataTypeCaddyCerts = "caddy_certificates"

	labelOwner   = "yourplatform.owner"
	labelProject = "yourplatform.project"
	labelRole    = "yourplatform.role"
	labelPurpose = "yourplatform.purpose"

	roleApp      = "app"
	rolePostgres = "postgres"
	roleMySQL    = "mysql"
	roleRedis    = "redis"

	purposePostgresData = "postgres-data"
	purposeMysqlData    = "mysql-data"
	purposeRedisData    = "redis-data"
	purposeUploads      = "uploads"
	purposeAppData      = "app-data"

	defaultDumpDir       = "/tmp/yourplatform/backups"
	defaultEnvDir        = "/etc/yourplatform/envs"
	defaultCaddyCertDir  = "/var/lib/yourplatform/caddy/certificates"
	defaultVolumeDataDir = "/var/lib/docker/volumes"
)

// BackupManifest represents a complete snapshot of what should be backed up.
type BackupManifest struct {
	ServerID     string            `json:"server_id"`
	Timestamp    time.Time         `json:"timestamp"`
	Projects     []ProjectBackup   `json:"projects"`
	PlatformData []PlatformBackup  `json:"platform_data"`
}

// ProjectBackup holds backup info for a single project.
type ProjectBackup struct {
	Name       string           `json:"name"`
	Components []BackupComponent `json:"components"`
}

// BackupComponent describes a single item to back up.
type BackupComponent struct {
	Type        string `json:"type"`
	Container   string `json:"container,omitempty"`
	Database    string `json:"database,omitempty"`
	DumpPath    string `json:"dump_path,omitempty"`
	VolumeName  string `json:"volume_name,omitempty"`
	MountPath   string `json:"mount_path,omitempty"`
	Path        string `json:"path,omitempty"`
}

// PlatformBackup holds platform-level data to back up.
type PlatformBackup struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

// DockerClient interface for container and volume operations.
type DockerClient interface {
	ListManagedContainers(ctx context.Context) ([]types.Container, error)
	InspectContainer(ctx context.Context, id string) (types.ContainerJSON, error)
	ListProjectVolumes(ctx context.Context, projectName string) ([]*types.Volume, error)
	PrepareVolumeForBackup(ctx context.Context, info DockerBackupInfo) error
	FinishVolumeForBackup(ctx context.Context, info DockerBackupInfo) error
	ExecInContainer(ctx context.Context, containerID string, cmd []string) (string, error)
}

// DockerBackupInfo mirrors docker.BackupInfo for the backup package.
type DockerBackupInfo struct {
	VolumeName string
	MountPath  string
	Project    string
	DBType     string
}

// BackupManifestBuilder discovers projects and builds backup manifests.
type BackupManifestBuilder struct {
	stateMgr    StateManager
	dockerClient DockerClient
	dataDir     string
	envDir      string
	certDir     string
	dumpDir     string
	volumeDir   string
}

// NewBackupManifestBuilder creates a new manifest builder.
func NewBackupManifestBuilder(stateMgr StateManager, dockerClient DockerClient) *BackupManifestBuilder {
	return &BackupManifestBuilder{
		stateMgr:    stateMgr,
		dockerClient: dockerClient,
		dataDir:     "/var/lib/yourplatform",
		envDir:      defaultEnvDir,
		certDir:     defaultCaddyCertDir,
		dumpDir:     defaultDumpDir,
		volumeDir:   defaultVolumeDataDir,
	}
}

// BuildManifest discovers all projects and constructs a backup manifest.
func (b *BackupManifestBuilder) BuildManifest(ctx context.Context, serverID string) (*BackupManifest, error) {
	manifest := &BackupManifest{
		ServerID:  serverID,
		Timestamp: time.Now().UTC(),
	}

	// Get all projects from state
	st := b.stateMgr.GetState()
	if st == nil || len(st.Projects) == 0 {
		slog.Info("no projects found in state for backup")
		return manifest, nil
	}

	// Build project backups
	for projectName := range st.Projects {
		projectBackup, err := b.buildProjectBackup(ctx, projectName)
		if err != nil {
			slog.Warn("failed to build project backup",
				"project", projectName, "error", err)
			continue
		}
		if projectBackup != nil && len(projectBackup.Components) > 0 {
			manifest.Projects = append(manifest.Projects, *projectBackup)
		}
	}

	// Add platform data (caddy certificates)
	manifest.PlatformData = append(manifest.PlatformData, PlatformBackup{
		Type: PlatformDataTypeCaddyCerts,
		Path: b.certDir,
	})

	slog.Info("backup manifest built",
		"projects", len(manifest.Projects),
		"platform_data", len(manifest.PlatformData))

	return manifest, nil
}

// buildProjectBackup constructs backup info for a single project.
func (b *BackupManifestBuilder) buildProjectBackup(ctx context.Context, projectName string) (*ProjectBackup, error) {
	project := &ProjectBackup{
		Name: projectName,
	}

	// Find all containers for this project
	containers, err := b.dockerClient.ListManagedContainers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}

	for _, container := range containers {
		// Check if this container belongs to this project
		projectLabel := container.Labels[labelProject]
		if projectLabel != projectName {
			continue
		}

		role := container.Labels[labelRole]
		containerName := ""
		if len(container.Names) > 0 {
			containerName = container.Names[0]
		}

		// Determine database type and add appropriate dump component
		switch role {
		case rolePostgres:
			dbName := b.inferDatabaseName(ctx, container.ID, "postgres")
			project.Components = append(project.Components, BackupComponent{
				Type:      ComponentTypePostgresDump,
				Container: containerName,
				Database:  dbName,
				DumpPath:  filepath.Join(b.dumpDir, projectName, "postgres.dump"),
			})
		case roleMySQL:
			dbName := b.inferDatabaseName(ctx, container.ID, "mysql")
			project.Components = append(project.Components, BackupComponent{
				Type:      ComponentTypeMysqlDump,
				Container: containerName,
				Database:  dbName,
				DumpPath:  filepath.Join(b.dumpDir, projectName, "mysql.dump"),
			})
		case roleRedis:
			project.Components = append(project.Components, BackupComponent{
				Type:      ComponentTypeRedisDump,
				Container: containerName,
				DumpPath:  filepath.Join(b.dumpDir, projectName, "redis.rdb"),
			})
		}
	}

	// Find volumes for this project
	volumes, err := b.dockerClient.ListProjectVolumes(ctx, projectName)
	if err != nil {
		slog.Warn("failed to list volumes for project",
			"project", projectName, "error", err)
	} else {
		for _, vol := range volumes {
			purpose := vol.Labels[labelPurpose]
			// Skip database data volumes (handled by dumps)
			if purpose == purposePostgresData || purpose == purposeMysqlData || purpose == purposeRedisData {
				continue
			}
			// Include user data volumes (uploads, app-data)
			if purpose == purposeUploads || purpose == purposeAppData {
				project.Components = append(project.Components, BackupComponent{
					Type:       ComponentTypeVolume,
					VolumeName: vol.Name,
					MountPath:  filepath.Join(b.volumeDir, vol.Name, "_data"),
				})
			}
		}
	}

	// Check for env file
	envPath := filepath.Join(b.envDir, projectName+".env")
	if _, err := os.Stat(envPath); err == nil {
		project.Components = append(project.Components, BackupComponent{
			Type: ComponentTypeEnvFile,
			Path: envPath,
		})
	}

	return project, nil
}

// inferDatabaseName attempts to determine the database name from container env vars.
func (b *BackupManifestBuilder) inferDatabaseName(ctx context.Context, containerID, dbType string) string {
	info, err := b.dockerClient.InspectContainer(ctx, containerID)
	if err != nil {
		// Default database names
		if dbType == "postgres" {
			return "yourplatform"
		}
		return "yourplatform"
	}

	// Check environment variables for database name
	for _, env := range info.Config.Env {
		switch {
		case dbType == "postgres" && len(env) > 12 && env[:12] == "POSTGRES_DB=":
			return env[12:]
		case dbType == "mysql" && len(env) > 9 && env[:9] == "MYSQL_DB=":
			return env[9:]
		case dbType == "mysql" && len(env) > 15 && env[:15] == "MYSQL_DATABASE=":
			return env[15:]
		}
	}

	// Default database name
	return "yourplatform"
}

// CollectBackupPaths maps manifest components to filesystem paths for restic.
func (b *BackupManifestBuilder) CollectBackupPaths(manifest *BackupManifest, dumpDir string) []string {
	var paths []string

	// Collect dump files
	for _, project := range manifest.Projects {
		for _, comp := range project.Components {
			if comp.DumpPath != "" {
				// Use dumpDir override if provided
				path := comp.DumpPath
				if dumpDir != "" {
					path = filepath.Join(dumpDir, project.Name, filepath.Base(comp.DumpPath))
				}
				paths = appendIfMissing(paths, path)
			}
			if comp.Type == ComponentTypeVolume && comp.MountPath != "" {
				paths = appendIfMissing(paths, comp.MountPath)
			}
			if comp.Type == ComponentTypeEnvFile && comp.Path != "" {
				paths = appendIfMissing(paths, comp.Path)
			}
		}
	}

	// Collect platform data
	for _, pd := range manifest.PlatformData {
		if pd.Path != "" {
			paths = appendIfMissing(paths, pd.Path)
		}
	}

	return paths
}

// MarshalJSON implements custom JSON marshaling for the manifest.
func (m *BackupManifest) MarshalJSON() ([]byte, error) {
	type Alias BackupManifest
	return json.Marshal(&struct {
		*Alias
		Timestamp string `json:"timestamp"`
	}{
		Alias:     (*Alias)(m),
		Timestamp: m.Timestamp.Format(time.RFC3339),
	})
}

// appendIfMissing adds a string to a slice only if it's not already present.
func appendIfMissing(slice []string, s string) []string {
	for _, existing := range slice {
		if existing == s {
			return slice
		}
	}
	return append(slice, s)
}

// filterByProject returns a Docker API filter for a specific project.
func filterByProject(projectName string) filters.Args {
	return filters.NewArgs(
		filters.Arg("label", labelOwner+"=yourplatform-agent"),
		filters.Arg("label", labelProject+"="+projectName),
	)
}
