package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/docker/docker/api/types"
)

func TestNewDumper(t *testing.T) {
	mockDocker := &MockDockerClient{}
	dumper := NewDumper(mockDocker, "/tmp/test-dumps")

	if dumper == nil {
		t.Fatal("expected non-nil dumper")
	}
	if dumper.dumpDir != "/tmp/test-dumps" {
		t.Errorf("dumpDir = %q, want /tmp/test-dumps", dumper.dumpDir)
	}
}

func TestNewDumper_DefaultDumpDir(t *testing.T) {
	mockDocker := &MockDockerClient{}
	dumper := NewDumper(mockDocker, "")

	if dumper.dumpDir != defaultDumpDir {
		t.Errorf("dumpDir = %q, want %q", dumper.dumpDir, defaultDumpDir)
	}
}

func TestDumper_DumpPostgres_NoContainer(t *testing.T) {
	mockDocker := &MockDockerClient{
		Containers: []types.Container{}, // Empty - no containers
	}

	dumper := NewDumper(mockDocker, t.TempDir())

	_, err := dumper.DumpPostgres(context.Background(), "nonexistent_postgres", "testproject", "testdb")
	if err == nil {
		t.Error("expected error for nonexistent container")
	}
}

func TestDumper_DumpMySQL_NoContainer(t *testing.T) {
	mockDocker := &MockDockerClient{
		Containers: []types.Container{},
	}

	dumper := NewDumper(mockDocker, t.TempDir())

	_, err := dumper.DumpMySQL(context.Background(), "nonexistent_mysql", "testproject", "testdb")
	if err == nil {
		t.Error("expected error for nonexistent container")
	}
}

func TestDumper_DumpRedis_NoContainer(t *testing.T) {
	mockDocker := &MockDockerClient{
		Containers: []types.Container{},
	}

	dumper := NewDumper(mockDocker, t.TempDir())

	_, err := dumper.DumpRedis(context.Background(), "nonexistent_redis", "testproject")
	if err == nil {
		t.Error("expected error for nonexistent container")
	}
}

func TestDumper_DumpPostgres_ExecFailure(t *testing.T) {
	mockDocker := &MockDockerClient{
		Containers: []types.Container{
			{
				ID:    "abc123def456",
				Names: []string{"/yourplatform_testproject_postgres"},
			},
		},
		ExecError: &execError{msg: "exec failed"},
	}

	dumper := NewDumper(mockDocker, t.TempDir())

	_, err := dumper.DumpPostgres(context.Background(), "yourplatform_testproject_postgres", "testproject", "testdb")
	if err == nil {
		t.Error("expected error for exec failure")
	}
}

type execError struct {
	msg string
}

func (e *execError) Error() string {
	return e.msg
}

func TestDumper_CleanupDumps(t *testing.T) {
	dumpDir := t.TempDir()

	// Create some dump files
	projectDir := filepath.Join(dumpDir, "testproject")
	if err := os.MkdirAll(projectDir, 0700); err != nil {
		t.Fatal(err)
	}
	dumpFile := filepath.Join(projectDir, "postgres.dump")
	if err := os.WriteFile(dumpFile, []byte("test data"), 0600); err != nil {
		t.Fatal(err)
	}

	mockDocker := &MockDockerClient{}
	dumper := NewDumper(mockDocker, dumpDir)

	dumper.CleanupDumps("testproject")

	// Verify directory was removed
	if _, err := os.Stat(projectDir); !os.IsNotExist(err) {
		t.Errorf("expected directory to be removed, got: %v", err)
	}
}

func TestDumper_CleanupAllDumps(t *testing.T) {
	dumpDir := t.TempDir()

	// Create multiple project directories
	for _, project := range []string{"project1", "project2"} {
		projectDir := filepath.Join(dumpDir, project)
		if err := os.MkdirAll(projectDir, 0700); err != nil {
			t.Fatal(err)
		}
		dumpFile := filepath.Join(projectDir, "dump.sql")
		if err := os.WriteFile(dumpFile, []byte("test data"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	mockDocker := &MockDockerClient{}
	dumper := NewDumper(mockDocker, dumpDir)

	dumper.CleanupAllDumps()

	// Verify entire dump directory was removed
	if _, err := os.Stat(dumpDir); !os.IsNotExist(err) {
		t.Errorf("expected dump directory to be removed, got: %v", err)
	}
}

func TestDumper_verifyDumpFile(t *testing.T) {
	dumpDir := t.TempDir()
	dumper := &Dumper{dumpDir: dumpDir}

	t.Run("file exists and not empty", func(t *testing.T) {
		dumpFile := filepath.Join(dumpDir, "valid.dump")
		if err := os.WriteFile(dumpFile, []byte("valid data"), 0600); err != nil {
			t.Fatal(err)
		}

		if err := dumper.verifyDumpFile(dumpFile); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("file does not exist", func(t *testing.T) {
		err := dumper.verifyDumpFile(filepath.Join(dumpDir, "nonexistent.dump"))
		if err == nil {
			t.Error("expected error for nonexistent file")
		}
	})

	t.Run("file is empty", func(t *testing.T) {
		dumpFile := filepath.Join(dumpDir, "empty.dump")
		if err := os.WriteFile(dumpFile, []byte{}, 0600); err != nil {
			t.Fatal(err)
		}

		err := dumper.verifyDumpFile(dumpFile)
		if err == nil {
			t.Error("expected error for empty file")
		}
	})
}

func TestDumper_findContainerByName(t *testing.T) {
	mockDocker := &MockDockerClient{
		Containers: []types.Container{
			{
				ID:    "abc123def456",
				Names: []string{"/yourplatform_myshop_postgres"},
			},
			{
				ID:    "abc123def457",
				Names: []string{"/yourplatform_myshop_app"},
			},
		},
	}

	dumper := NewDumper(mockDocker, t.TempDir())

	t.Run("found", func(t *testing.T) {
		id, err := dumper.findContainerByName(context.Background(), "yourplatform_myshop_postgres")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != "abc123def456" {
			t.Errorf("container ID = %q, want abc123def456", id)
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, err := dumper.findContainerByName(context.Background(), "nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent container")
		}
	})

	t.Run("without slash prefix", func(t *testing.T) {
		id, err := dumper.findContainerByName(context.Background(), "yourplatform_myshop_app")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != "abc123def457" {
			t.Errorf("container ID = %q, want abc123def457", id)
		}
	})
}

func TestDumpResult_Structure(t *testing.T) {
	result := &DumpResult{
		ComponentType: ComponentTypePostgresDump,
		ContainerName: "test_container",
		Database:      "testdb",
		DumpPath:      "/tmp/test.dump",
		SizeBytes:     1024,
	}

	if result.ComponentType != ComponentTypePostgresDump {
		t.Errorf("ComponentType = %q, want %q", result.ComponentType, ComponentTypePostgresDump)
	}
	if result.SizeBytes != 1024 {
		t.Errorf("SizeBytes = %d, want 1024", result.SizeBytes)
	}
}

func TestDumpResult_WithError(t *testing.T) {
	result := &DumpResult{
		ComponentType: ComponentTypeMysqlDump,
		ContainerName: "test_container",
		Error:         "dump failed",
	}

	if result.Error != "dump failed" {
		t.Errorf("Error = %q, want 'dump failed'", result.Error)
	}
}

// ---------------------------------------------------------------------------
// Restore tests
// ---------------------------------------------------------------------------

func TestDumper_RestorePostgres_NoContainer(t *testing.T) {
	mockDocker := &MockDockerClient{
		Containers: []types.Container{},
	}

	dumper := NewDumper(mockDocker, t.TempDir())

	_, err := dumper.RestorePostgres(context.Background(), "nonexistent_postgres", "testdb", "/tmp/test.dump")
	if err == nil {
		t.Error("expected error for nonexistent container")
	}
}

func TestDumper_RestoreMySQL_NoContainer(t *testing.T) {
	mockDocker := &MockDockerClient{
		Containers: []types.Container{},
	}

	dumper := NewDumper(mockDocker, t.TempDir())

	_, err := dumper.RestoreMySQL(context.Background(), "nonexistent_mysql", "testdb", "/tmp/test.dump")
	if err == nil {
		t.Error("expected error for nonexistent container")
	}
}

func TestDumper_RestoreRedis_NoContainer(t *testing.T) {
	mockDocker := &MockDockerClient{
		Containers: []types.Container{},
	}

	dumper := NewDumper(mockDocker, t.TempDir())

	_, err := dumper.RestoreRedis(context.Background(), "nonexistent_redis", "/tmp/test.rdb")
	if err == nil {
		t.Error("expected error for nonexistent container")
	}
}

func TestDumper_RestorePostgres_DumpFileNotFound(t *testing.T) {
	mockDocker := &MockDockerClient{
		Containers: []types.Container{
			{
				ID:    "abc123def456",
				Names: []string{"/yourplatform_testproject_postgres"},
			},
		},
	}

	dumper := NewDumper(mockDocker, t.TempDir())

	_, err := dumper.RestorePostgres(context.Background(), "yourplatform_testproject_postgres", "testdb", "/nonexistent/dump.sql")
	if err == nil {
		t.Error("expected error for nonexistent dump file")
	}
}

func TestDumper_RestoreMySQL_DumpFileNotFound(t *testing.T) {
	mockDocker := &MockDockerClient{
		Containers: []types.Container{
			{
				ID:    "abc123def456",
				Names: []string{"/yourplatform_testproject_mysql"},
			},
		},
	}

	dumper := NewDumper(mockDocker, t.TempDir())

	_, err := dumper.RestoreMySQL(context.Background(), "yourplatform_testproject_mysql", "testdb", "/nonexistent/dump.sql")
	if err == nil {
		t.Error("expected error for nonexistent dump file")
	}
}

func TestDumper_RestoreRedis_DumpFileNotFound(t *testing.T) {
	mockDocker := &MockDockerClient{
		Containers: []types.Container{
			{
				ID:    "abc123def456",
				Names: []string{"/yourplatform_testproject_redis"},
			},
		},
	}

	dumper := NewDumper(mockDocker, t.TempDir())

	_, err := dumper.RestoreRedis(context.Background(), "yourplatform_testproject_redis", "/nonexistent/dump.rdb")
	if err == nil {
		t.Error("expected error for nonexistent dump file")
	}
}

func TestRestoreResult_Structure(t *testing.T) {
	result := &RestoreResult{
		ComponentType: ComponentTypePostgresDump,
		ContainerName: "test_container",
		Database:      "testdb",
		Status:        "success",
	}

	if result.ComponentType != ComponentTypePostgresDump {
		t.Errorf("ComponentType = %q, want %q", result.ComponentType, ComponentTypePostgresDump)
	}
	if result.Status != "success" {
		t.Errorf("Status = %q, want success", result.Status)
	}
	if result.Database != "testdb" {
		t.Errorf("Database = %q, want testdb", result.Database)
	}
}

func TestRestoreResult_WithError(t *testing.T) {
	result := &RestoreResult{
		ComponentType: ComponentTypeMysqlDump,
		ContainerName: "test_container",
		Status:        "failed",
		Error:         "restore failed",
	}

	if result.Error != "restore failed" {
		t.Errorf("Error = %q, want 'restore failed'", result.Error)
	}
	if result.Status != "failed" {
		t.Errorf("Status = %q, want failed", result.Status)
	}
}
