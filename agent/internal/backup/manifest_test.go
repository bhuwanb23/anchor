package backup

import (
	"context"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/yourname/yourplatform/agent/internal/state"
)

// MockDockerClient implements DockerClient for testing.
type MockDockerClient struct {
	Containers    []types.Container
	Volumes       []*types.Volume
	InspectResult types.ContainerJSON
	ExecOutput    string
	ExecError     error
}

func (m *MockDockerClient) ListManagedContainers(ctx context.Context) ([]types.Container, error) {
	return m.Containers, nil
}

func (m *MockDockerClient) InspectContainer(ctx context.Context, id string) (types.ContainerJSON, error) {
	return m.InspectResult, nil
}

func (m *MockDockerClient) ListProjectVolumes(ctx context.Context, projectName string) ([]*types.Volume, error) {
	return m.Volumes, nil
}

func (m *MockDockerClient) PrepareVolumeForBackup(ctx context.Context, info DockerBackupInfo) error {
	return nil
}

func (m *MockDockerClient) FinishVolumeForBackup(ctx context.Context, info DockerBackupInfo) error {
	return nil
}

func (m *MockDockerClient) ExecInContainer(ctx context.Context, containerID string, cmd []string) (string, error) {
	return m.ExecOutput, m.ExecError
}

func TestNewBackupManifestBuilder(t *testing.T) {
	stateMgr := state.NewManager(t.TempDir())
	mockDocker := &MockDockerClient{}

	builder := NewBackupManifestBuilder(stateMgr, mockDocker)

	if builder == nil {
		t.Fatal("expected non-nil builder")
	}
	if builder.stateMgr != stateMgr {
		t.Error("stateMgr not set correctly")
	}
	if builder.dockerClient != mockDocker {
		t.Error("dockerClient not set correctly")
	}
}

func TestBuildManifest_EmptyState(t *testing.T) {
	stateMgr := state.NewManager(t.TempDir())
	mockDocker := &MockDockerClient{}

	builder := NewBackupManifestBuilder(stateMgr, mockDocker)

	manifest, err := builder.BuildManifest(context.Background(), "srv-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if manifest.ServerID != "srv-test" {
		t.Errorf("server_id = %q, want srv-test", manifest.ServerID)
	}
	if len(manifest.Projects) != 0 {
		t.Errorf("expected 0 projects, got %d", len(manifest.Projects))
	}
	// Should always have caddy certificates
	if len(manifest.PlatformData) != 1 {
		t.Errorf("expected 1 platform_data entry, got %d", len(manifest.PlatformData))
	}
	if manifest.PlatformData[0].Type != PlatformDataTypeCaddyCerts {
		t.Errorf("platform_data type = %q, want %q", manifest.PlatformData[0].Type, PlatformDataTypeCaddyCerts)
	}
}

func TestBuildManifest_WithProject(t *testing.T) {
	stateMgr := state.NewManager(t.TempDir())

	// Add a project with containers
	err := stateMgr.SetContainer("myshop", "app", &state.ContainerState{
		ContainerID: "abc123def456",
		Image:       "nginx:latest",
		Status:      "running",
	})
	if err != nil {
		t.Fatalf("set container: %v", err)
	}
	err = stateMgr.SetContainer("myshop", "postgres", &state.ContainerState{
		ContainerID: "abc123def457",
		Image:       "postgres:15",
		Status:      "running",
	})
	if err != nil {
		t.Fatalf("set container: %v", err)
	}

	mockDocker := &MockDockerClient{
		Containers: []types.Container{
			{
				ID: "abc123def456",
				Labels: map[string]string{
					labelOwner:   "yourplatform-agent",
					labelProject: "myshop",
					labelRole:    "app",
				},
				Names: []string{"/yourplatform_myshop_app"},
			},
			{
				ID: "abc123def457",
				Labels: map[string]string{
					labelOwner:   "yourplatform-agent",
					labelProject: "myshop",
					labelRole:    "postgres",
				},
				Names: []string{"/yourplatform_myshop_postgres"},
			},
		},
		InspectResult: types.ContainerJSON{
			Config: &types.ContainerConfig{
				Env: []string{"POSTGRES_DB=myshop"},
			},
		},
	}

	builder := NewBackupManifestBuilder(stateMgr, mockDocker)

	manifest, err := builder.BuildManifest(context.Background(), "srv-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(manifest.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(manifest.Projects))
	}

	project := manifest.Projects[0]
	if project.Name != "myshop" {
		t.Errorf("project name = %q, want myshop", project.Name)
	}

	// Should have postgres dump component
	foundPostgres := false
	for _, comp := range project.Components {
		if comp.Type == ComponentTypePostgresDump {
			foundPostgres = true
			if comp.Database != "myshop" {
				t.Errorf("database = %q, want myshop", comp.Database)
			}
		}
	}
	if !foundPostgres {
		t.Error("expected postgres_dump component")
	}
}

func TestBuildManifest_WithVolume(t *testing.T) {
	stateMgr := state.NewManager(t.TempDir())

	err := stateMgr.SetContainer("myshop", "app", &state.ContainerState{
		ContainerID: "abc123def456",
		Image:       "nginx:latest",
		Status:      "running",
	})
	if err != nil {
		t.Fatalf("set container: %v", err)
	}

	mockDocker := &MockDockerClient{
		Containers: []types.Container{
			{
				ID: "abc123def456",
				Labels: map[string]string{
					labelOwner:   "yourplatform-agent",
					labelProject: "myshop",
					labelRole:    "app",
				},
				Names: []string{"/yourplatform_myshop_app"},
			},
		},
		Volumes: []*types.Volume{
			{
				Name: "yourplatform_myshop_uploads",
				Labels: map[string]string{
					labelOwner:   "yourplatform-agent",
					labelProject: "myshop",
					labelPurpose: "uploads",
				},
			},
			{
				Name: "yourplatform_myshop_postgres-data",
				Labels: map[string]string{
					labelOwner:   "yourplatform-agent",
					labelProject: "myshop",
					labelPurpose: "postgres-data",
				},
			},
		},
	}

	builder := NewBackupManifestBuilder(stateMgr, mockDocker)

	manifest, err := builder.BuildManifest(context.Background(), "srv-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(manifest.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(manifest.Projects))
	}

	project := manifest.Projects[0]

	// Should have uploads volume but NOT postgres-data volume
	foundUploads := false
	foundPostgresData := false
	for _, comp := range project.Components {
		if comp.Type == ComponentTypeVolume && comp.VolumeName == "yourplatform_myshop_uploads" {
			foundUploads = true
		}
		if comp.Type == ComponentTypeVolume && comp.VolumeName == "yourplatform_myshop_postgres-data" {
			foundPostgresData = true
		}
	}

	if !foundUploads {
		t.Error("expected uploads volume component")
	}
	if foundPostgresData {
		t.Error("postgres-data volume should be excluded (backed up via dump)")
	}
}

func TestCollectBackupPaths(t *testing.T) {
	manifest := &BackupManifest{
		ServerID:  "srv-test",
		Timestamp: time.Now(),
		Projects: []ProjectBackup{
			{
				Name: "myshop",
				Components: []BackupComponent{
					{
						Type:     ComponentTypePostgresDump,
						DumpPath: "/tmp/backups/myshop/postgres.dump",
					},
					{
						Type:       ComponentTypeVolume,
						VolumeName: "yourplatform_myshop_uploads",
						MountPath:  "/var/lib/docker/volumes/yourplatform_myshop_uploads/_data",
					},
					{
						Type: ComponentTypeEnvFile,
						Path: "/etc/yourplatform/envs/myshop.env",
					},
				},
			},
		},
		PlatformData: []PlatformBackup{
			{
				Type: PlatformDataTypeCaddyCerts,
				Path: "/var/lib/yourplatform/caddy/certificates",
			},
		},
	}

	builder := &BackupManifestBuilder{}

	paths := builder.CollectBackupPaths(manifest, "")

	// Should have 4 unique paths
	if len(paths) != 4 {
		t.Errorf("expected 4 paths, got %d: %v", len(paths), paths)
	}

	// Check specific paths exist
	expectedPaths := []string{
		"/tmp/backups/myshop/postgres.dump",
		"/var/lib/docker/volumes/yourplatform_myshop_uploads/_data",
		"/etc/yourplatform/envs/myshop.env",
		"/var/lib/yourplatform/caddy/certificates",
	}

	for _, expected := range expectedPaths {
		found := false
		for _, p := range paths {
			if p == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected path %q not found in %v", expected, paths)
		}
	}
}

func TestCollectBackupPaths_WithDumpDirOverride(t *testing.T) {
	manifest := &BackupManifest{
		Projects: []ProjectBackup{
			{
				Name: "myshop",
				Components: []BackupComponent{
					{
						Type:     ComponentTypePostgresDump,
						DumpPath: "/tmp/backups/myshop/postgres.dump",
					},
				},
			},
		},
	}

	builder := &BackupManifestBuilder{}

	paths := builder.CollectBackupPaths(manifest, "/custom/dump/dir")

	// The dump path should be overridden
	if len(paths) != 1 {
		t.Fatalf("expected 1 path, got %d", len(paths))
	}

	// Path should use the override dump dir
	expected := "/custom/dump/dir/myshop/postgres.dump"
	if paths[0] != expected {
		t.Errorf("path = %q, want %q", paths[0], expected)
	}
}

func TestInferDatabaseName_Postgres(t *testing.T) {
	mockDocker := &MockDockerClient{
		InspectResult: types.ContainerJSON{
			Config: &types.ContainerConfig{
				Env: []string{
					"POSTGRES_USER=yourplatform",
					"POSTGRES_DB=myshop",
					"POSTGRES_PASSWORD=secret",
				},
			},
		},
	}

	builder := &BackupManifestBuilder{
		dockerClient: mockDocker,
	}

	dbName := builder.inferDatabaseName(context.Background(), "test-container", "postgres")
	if dbName != "myshop" {
		t.Errorf("database name = %q, want myshop", dbName)
	}
}

func TestInferDatabaseName_Mysql(t *testing.T) {
	mockDocker := &MockDockerClient{
		InspectResult: types.ContainerJSON{
			Config: &types.ContainerConfig{
				Env: []string{
					"MYSQL_ROOT_PASSWORD=secret",
					"MYSQL_DATABASE=mydb",
				},
			},
		},
	}

	builder := &BackupManifestBuilder{
		dockerClient: mockDocker,
	}

	dbName := builder.inferDatabaseName(context.Background(), "test-container", "mysql")
	if dbName != "mydb" {
		t.Errorf("database name = %q, want mydb", dbName)
	}
}

func TestInferDatabaseName_Default(t *testing.T) {
	mockDocker := &MockDockerClient{
		InspectResult: types.ContainerJSON{
			Config: &types.ContainerConfig{
				Env: []string{"OTHER=value"},
			},
		},
	}

	builder := &BackupManifestBuilder{
		dockerClient: mockDocker,
	}

	dbName := builder.inferDatabaseName(context.Background(), "test-container", "postgres")
	if dbName != "yourplatform" {
		t.Errorf("database name = %q, want yourplatform (default)", dbName)
	}
}

func TestAppendIfMissing(t *testing.T) {
	tests := []struct {
		name     string
		slice    []string
		s        string
		expected int
	}{
		{
			name:     "add to empty",
			slice:    []string{},
			s:        "new",
			expected: 1,
		},
		{
			name:     "add new",
			slice:    []string{"a", "b"},
			s:        "c",
			expected: 3,
		},
		{
			name:     "skip existing",
			slice:    []string{"a", "b", "c"},
			s:        "b",
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := appendIfMissing(tt.slice, tt.s)
			if len(result) != tt.expected {
				t.Errorf("len = %d, want %d", len(result), tt.expected)
			}
		})
	}
}

func TestMarshalJSON(t *testing.T) {
	manifest := &BackupManifest{
		ServerID:  "srv-test",
		Timestamp: time.Date(2024, 1, 15, 2, 0, 0, 0, time.UTC),
		Projects: []ProjectBackup{
			{
				Name: "myshop",
				Components: []BackupComponent{
					{Type: ComponentTypePostgresDump, Database: "myshop"},
				},
			},
		},
		PlatformData: []PlatformBackup{
			{Type: PlatformDataTypeCaddyCerts, Path: "/certs"},
		},
	}

	data, err := manifest.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	// Should contain RFC3339 timestamp
	if !contains(string(data), "2024-01-15T02:00:00Z") {
		t.Errorf("expected RFC3339 timestamp in JSON, got: %s", string(data))
	}

	// Should contain server_id
	if !contains(string(data), "srv-test") {
		t.Errorf("expected server_id in JSON")
	}
}
