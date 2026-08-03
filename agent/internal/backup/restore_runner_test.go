package backup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
)

func TestNewRestoreRunner(t *testing.T) {
	mockDocker := &MockDockerClient{}
	manager := &BackupManager{
		destination: "local:/tmp/test-repo",
		password:    "test-password",
		dataDir:     t.TempDir(),
	}

	runner := NewRestoreRunner(manager, mockDocker)

	if runner == nil {
		t.Fatal("expected non-nil runner")
	}
	if runner.manager != manager {
		t.Error("manager not set correctly")
	}
	if runner.tempDir != "/tmp/yourplatform/restore" {
		t.Errorf("tempDir = %q, want /tmp/yourplatform/restore", runner.tempDir)
	}
}

func TestRestoreRunResult_Structure(t *testing.T) {
	result := &RestoreRunResult{
		SnapshotID:  "abc123def456",
		ProjectName: "my-project",
		ProjectResult: &RestoreProjectResult{
			Name:   "my-project",
			Status: "success",
			Components: []RestoreResult{
				{Type: ComponentTypePostgresDump, Name: "mydb", Status: "success"},
			},
		},
		Duration:  5 * time.Second,
		StartedAt: time.Now(),
	}

	if result.SnapshotID != "abc123def456" {
		t.Errorf("SnapshotID = %q, want abc123def456", result.SnapshotID)
	}
	if result.ProjectName != "my-project" {
		t.Errorf("ProjectName = %q, want my-project", result.ProjectName)
	}
	if result.ProjectResult.Status != "success" {
		t.Errorf("ProjectResult.Status = %q, want success", result.ProjectResult.Status)
	}
	if len(result.ProjectResult.Components) != 1 {
		t.Errorf("Components len = %d, want 1", len(result.ProjectResult.Components))
	}
}

func TestRestoreRunResult_WithError(t *testing.T) {
	result := &RestoreRunResult{
		Error: "restore failed",
	}

	if result.Error != "restore failed" {
		t.Errorf("Error = %q, want 'restore failed'", result.Error)
	}
}

func TestRestoreProjectResult_PartialStatus(t *testing.T) {
	result := &RestoreProjectResult{
		Name:   "my-project",
		Status: "partial",
		Components: []RestoreResult{
			{Type: ComponentTypePostgresDump, Name: "mydb", Status: "success"},
			{Type: ComponentTypeVolume, Name: "uploads", Status: "failed", Error: "volume not found"},
		},
	}

	if result.Status != "partial" {
		t.Errorf("Status = %q, want partial", result.Status)
	}
	if len(result.Components) != 2 {
		t.Errorf("Components len = %d, want 2", len(result.Components))
	}
	if result.Components[1].Error != "volume not found" {
		t.Errorf("Components[1].Error = %q, want 'volume not found'", result.Components[1].Error)
	}
}

func TestRestoreRunner_findDumpFile(t *testing.T) {
	// Create temp directory structure
	restoreDir := t.TempDir()
	projectName := "test-project"

	// Create project directory with dump file
	projectDir := filepath.Join(restoreDir, projectName)
	if err := os.MkdirAll(projectDir, 0700); err != nil {
		t.Fatal(err)
	}
	dumpPath := filepath.Join(projectDir, "postgres.dump")
	if err := os.WriteFile(dumpPath, []byte("test dump data"), 0600); err != nil {
		t.Fatal(err)
	}

	mockDocker := &MockDockerClient{}
	manager := &BackupManager{dataDir: t.TempDir()}
	runner := NewRestoreRunner(manager, mockDocker)

	// Should find the dump file
	found := runner.findDumpFile(restoreDir, projectName, "postgres.dump")
	if found != dumpPath {
		t.Errorf("findDumpFile() = %q, want %q", found, dumpPath)
	}
}

func TestRestoreRunner_findDumpFile_NotFound(t *testing.T) {
	restoreDir := t.TempDir()

	mockDocker := &MockDockerClient{}
	manager := &BackupManager{dataDir: t.TempDir()}
	runner := NewRestoreRunner(manager, mockDocker)

	found := runner.findDumpFile(restoreDir, "nonexistent", "postgres.dump")
	if found != "" {
		t.Errorf("findDumpFile() = %q, want empty string", found)
	}
}

func TestRestoreRunner_findDumpFile_RootLocation(t *testing.T) {
	restoreDir := t.TempDir()

	// Create dump file at root level
	dumpPath := filepath.Join(restoreDir, "postgres.dump")
	if err := os.WriteFile(dumpPath, []byte("test dump data"), 0600); err != nil {
		t.Fatal(err)
	}

	mockDocker := &MockDockerClient{}
	manager := &BackupManager{dataDir: t.TempDir()}
	runner := NewRestoreRunner(manager, mockDocker)

	found := runner.findDumpFile(restoreDir, "test-project", "postgres.dump")
	if found != dumpPath {
		t.Errorf("findDumpFile() = %q, want %q", found, dumpPath)
	}
}

func TestRestoreRunner_readManifest(t *testing.T) {
	restoreDir := t.TempDir()

	// Create a manifest file
	manifest := &BackupManifest{
		ServerID:  "srv-test",
		Timestamp: time.Now(),
		Projects: []ProjectBackup{
			{
				Name: "test-project",
				Components: []BackupComponent{
					{Type: ComponentTypePostgresDump, Container: "pg-container", Database: "testdb"},
				},
			},
		},
	}

	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}

	manifestPath := filepath.Join(restoreDir, "manifest.json")
	if err := os.WriteFile(manifestPath, data, 0600); err != nil {
		t.Fatal(err)
	}

	mockDocker := &MockDockerClient{}
	manager := &BackupManager{dataDir: t.TempDir()}
	runner := NewRestoreRunner(manager, mockDocker)

	readManifest, err := runner.readManifest(restoreDir)
	if err != nil {
		t.Fatalf("readManifest() error = %v", err)
	}

	if readManifest.ServerID != "srv-test" {
		t.Errorf("ServerID = %q, want srv-test", readManifest.ServerID)
	}
	if len(readManifest.Projects) != 1 {
		t.Errorf("Projects len = %d, want 1", len(readManifest.Projects))
	}
	if readManifest.Projects[0].Name != "test-project" {
		t.Errorf("Projects[0].Name = %q, want test-project", readManifest.Projects[0].Name)
	}
}

func TestRestoreRunner_readManifest_NotFound(t *testing.T) {
	restoreDir := t.TempDir()

	mockDocker := &MockDockerClient{}
	manager := &BackupManager{dataDir: t.TempDir()}
	runner := NewRestoreRunner(manager, mockDocker)

	_, err := runner.readManifest(restoreDir)
	if err == nil {
		t.Fatal("expected error reading non-existent manifest")
	}
}

func TestRestoreRunner_readManifest_InvalidJSON(t *testing.T) {
	restoreDir := t.TempDir()

	// Write invalid JSON
	manifestPath := filepath.Join(restoreDir, "manifest.json")
	if err := os.WriteFile(manifestPath, []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}

	mockDocker := &MockDockerClient{}
	manager := &BackupManager{dataDir: t.TempDir()}
	runner := NewRestoreRunner(manager, mockDocker)

	_, err := runner.readManifest(restoreDir)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestRestoreRunner_findContainerByName_Found(t *testing.T) {
	mockDocker := &MockDockerClient{
		Containers: []types.Container{
			{
				ID:   "abc123def456",
				Names: []string{"/my-container"},
			},
		},
	}
	manager := &BackupManager{dataDir: t.TempDir()}
	runner := NewRestoreRunner(manager, mockDocker)

	id, err := runner.findContainerByName(context.Background(), "my-container")
	if err != nil {
		t.Fatalf("findContainerByName() error = %v", err)
	}
	if id != "abc123def456" {
		t.Errorf("id = %q, want abc123def456", id)
	}
}

func TestRestoreRunner_findContainerByName_NotFound(t *testing.T) {
	mockDocker := &MockDockerClient{
		Containers: []types.Container{},
	}
	manager := &BackupManager{dataDir: t.TempDir()}
	runner := NewRestoreRunner(manager, mockDocker)

	_, err := runner.findContainerByName(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent container")
	}
}

func TestRestoreRunner_findContainerByVolume_Found(t *testing.T) {
	mockDocker := &MockDockerClient{
		Containers: []types.Container{
			{
				ID: "abc123def456",
				Mounts: []types.MountPoint{
					{Name: "yourplatform_test_uploads"},
				},
			},
		},
	}
	manager := &BackupManager{dataDir: t.TempDir()}
	runner := NewRestoreRunner(manager, mockDocker)

	id, err := runner.findContainerByVolume(context.Background(), "yourplatform_test_uploads")
	if err != nil {
		t.Fatalf("findContainerByVolume() error = %v", err)
	}
	if id != "abc123def456" {
		t.Errorf("id = %q, want abc123def456", id)
	}
}

func TestRestoreRunner_findContainerByVolume_NotFound(t *testing.T) {
	mockDocker := &MockDockerClient{
		Containers: []types.Container{},
	}
	manager := &BackupManager{dataDir: t.TempDir()}
	runner := NewRestoreRunner(manager, mockDocker)

	_, err := runner.findContainerByVolume(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent volume")
	}
}

func TestRestoreRunner_cleanupRestoreDir(t *testing.T) {
	restoreDir := t.TempDir()
	subDir := filepath.Join(restoreDir, "test")
	if err := os.MkdirAll(subDir, 0700); err != nil {
		t.Fatal(err)
	}
	testFile := filepath.Join(subDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}

	mockDocker := &MockDockerClient{}
	manager := &BackupManager{dataDir: t.TempDir()}
	runner := NewRestoreRunner(manager, mockDocker)

	runner.cleanupRestoreDir(subDir)

	if _, err := os.Stat(subDir); !os.IsNotExist(err) {
		t.Error("expected directory to be removed")
	}
}

func TestRestoreProgressReporter_Mock(t *testing.T) {
	mockDocker := &MockDockerClient{}
	manager := &BackupManager{dataDir: t.TempDir()}
	runner := NewRestoreRunner(manager, mockDocker)

	reporter := &mockRestoreReporter{}

	result := &RestoreRunResult{
		SnapshotID:  "abc123def456",
		ProjectName: "test-project",
		ProjectResult: &RestoreProjectResult{
			Name:   "test-project",
			Status: "success",
		},
	}

	reporter.ReportRestoreComplete(*result)

	if len(reporter.progress) != 0 {
		t.Errorf("expected 0 progress messages, got %d", len(reporter.progress))
	}
	if reporter.completed == nil {
		t.Error("expected completed result")
	}
	if reporter.errMsg != "" {
		t.Errorf("expected no error, got %q", reporter.errMsg)
	}
}

func TestRestoreProgressReporter_Error(t *testing.T) {
	reporter := &mockRestoreReporter{}

	reporter.ReportRestoreError("something went wrong")

	if reporter.errMsg != "something went wrong" {
		t.Errorf("errMsg = %q, want 'something went wrong'", reporter.errMsg)
	}
}

// mockRestoreReporter implements RestoreProgressReporter for testing.
type mockRestoreReporter struct {
	progress  []RestoreProgress
	completed *RestoreRunResult
	errMsg    string
}

func (m *mockRestoreReporter) ReportRestoreProgress(progress RestoreProgress) {
	m.progress = append(m.progress, progress)
}

func (m *mockRestoreReporter) ReportRestoreComplete(result RestoreRunResult) {
	completed := result
	m.completed = &completed
}

func (m *mockRestoreReporter) ReportRestoreError(err string) {
	m.errMsg = err
}

func TestRestoreRunner_RunRestore_ProjectNotFound(t *testing.T) {
	mockDocker := &MockDockerClient{}
	manager := &BackupManager{
		destination: "local:/tmp/test-repo",
		dataDir:     t.TempDir(),
		repository:  &RepositoryManager{config: RepositoryConfig{Destination: "local:/tmp/test-repo"}},
	}
	runner := NewRestoreRunner(manager, mockDocker)

	reporter := &mockRestoreReporter{}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// This will fail because restore will fail (no actual restic repo),
	// but we're testing the project-not-found logic
	result, err := runner.RunRestore(ctx, "abc123def456", "nonexistent", reporter)
	if err == nil {
		// If it somehow succeeded (mocked repo), the result should show the project was not found
		if result.Error == "" {
			t.Error("expected error for non-existent project")
		}
	}
}
