package docker

import (
	"context"
	"testing"
)

// ---------------------------------------------------------------------------
// Volume naming
// ---------------------------------------------------------------------------

func TestVolumeName_Simple(t *testing.T) {
	got := VolumeName("myshop", "postgres-data")
	if got != "yourplatform_myshop_postgres-data" {
		t.Errorf("expected 'yourplatform_myshop_postgres-data', got '%s'", got)
	}
}

func TestVolumeName_WithSpecialChars(t *testing.T) {
	got := VolumeName("My Shop!", "postgres-data")
	if got != "yourplatform_my-shop_postgres-data" {
		t.Errorf("expected 'yourplatform_my-shop_postgres-data', got '%s'", got)
	}
}

func TestVolumeName_WithUnderscores(t *testing.T) {
	got := VolumeName("my_project", "uploads")
	if got != "yourplatform_my-project_uploads" {
		t.Errorf("expected 'yourplatform_my-project_uploads', got '%s'", got)
	}
}

func TestVolumeName_EmptyProject(t *testing.T) {
	got := VolumeName("", "data")
	if got != "yourplatform_project_data" {
		t.Errorf("expected 'yourplatform_project_data', got '%s'", got)
	}
}

func TestVolumeName_PurposeWithHyphens(t *testing.T) {
	got := VolumeName("blog", "wp-uploads")
	if got != "yourplatform_blog_wp-uploads" {
		t.Errorf("expected 'yourplatform_blog_wp-uploads', got '%s'", got)
	}
}

// ---------------------------------------------------------------------------
// Volume labels
// ---------------------------------------------------------------------------

func TestVolumeLabels_HasOwner(t *testing.T) {
	labels := VolumeLabels("test-project", "data")
	if labels[labelOwner] != labelOwnerValue {
		t.Errorf("expected label %s=%s, got '%s'", labelOwner, labelOwnerValue, labels[labelOwner])
	}
}

func TestVolumeLabels_HasProject(t *testing.T) {
	labels := VolumeLabels("My Shop!", "data")
	if labels["yourplatform.project"] != "my-shop" {
		t.Errorf("expected project 'my-shop', got '%s'", labels["yourplatform.project"])
	}
}

func TestVolumeLabels_HasPurpose(t *testing.T) {
	labels := VolumeLabels("test", "uploads")
	if labels["yourplatform.purpose"] != "uploads" {
		t.Errorf("expected purpose 'uploads', got '%s'", labels["yourplatform.purpose"])
	}
}

func TestVolumeLabels_HasCreated(t *testing.T) {
	labels := VolumeLabels("test", "data")
	if labels["yourplatform.created"] == "" {
		t.Error("expected created timestamp in labels")
	}
}

// ---------------------------------------------------------------------------
// DB volume purpose and mount path
// ---------------------------------------------------------------------------

func TestDBVolumePurpose_Postgres(t *testing.T) {
	if got := DBVolumePurpose(ContainerTypePostgres); got != "postgres-data" {
		t.Errorf("expected 'postgres-data', got '%s'", got)
	}
}

func TestDBVolumePurpose_MySQL(t *testing.T) {
	if got := DBVolumePurpose(ContainerTypeMySQL); got != "mysql-data" {
		t.Errorf("expected 'mysql-data', got '%s'", got)
	}
}

func TestDBVolumePurpose_Redis(t *testing.T) {
	if got := DBVolumePurpose(ContainerTypeRedis); got != "redis-data" {
		t.Errorf("expected 'redis-data', got '%s'", got)
	}
}

func TestDBVolumePurpose_App(t *testing.T) {
	if got := DBVolumePurpose(ContainerTypeApp); got != "app-data" {
		t.Errorf("expected 'app-data', got '%s'", got)
	}
}

func TestDBVolumeMountPath_Postgres(t *testing.T) {
	if got := DBVolumeMountPath(ContainerTypePostgres); got != "/var/lib/postgresql/data" {
		t.Errorf("expected '/var/lib/postgresql/data', got '%s'", got)
	}
}

func TestDBVolumeMountPath_MySQL(t *testing.T) {
	if got := DBVolumeMountPath(ContainerTypeMySQL); got != "/var/lib/mysql" {
		t.Errorf("expected '/var/lib/mysql', got '%s'", got)
	}
}

func TestDBVolumeMountPath_Redis(t *testing.T) {
	if got := DBVolumeMountPath(ContainerTypeRedis); got != "/data" {
		t.Errorf("expected '/data', got '%s'", got)
	}
}

func TestDBVolumeMountPath_App(t *testing.T) {
	if got := DBVolumeMountPath(ContainerTypeApp); got != "/data" {
		t.Errorf("expected '/data' (default), got '%s'", got)
	}
}

// ---------------------------------------------------------------------------
// EnsureVolume (structural — no real Docker)
// ---------------------------------------------------------------------------

func TestEnsureVolume_NoDocker(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	_, err := client.EnsureVolume(context.Background(), "test-project", "data")
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
}

func TestEnsureDBVolume_NoDocker(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	_, err := client.EnsureDBVolume(context.Background(), "test-project", ContainerTypePostgres)
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
}

func TestListUnmountedVolumes_NoDocker(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	_, err := client.ListUnmountedVolumes(context.Background())
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
}

func TestRemoveVolume_NoDocker(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	err := client.RemoveVolume(context.Background(), "test-volume")
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
}

func TestPrepareVolumeForBackup_NoDocker(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	err := client.PrepareVolumeForBackup(context.Background(), BackupInfo{
		VolumeName: "test-vol",
		Project:    "test",
		DBType:     ContainerTypePostgres,
	})
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
}

// ---------------------------------------------------------------------------
// BackupInfo structure
// ---------------------------------------------------------------------------

func TestBackupInfo_Fields(t *testing.T) {
	info := BackupInfo{
		VolumeName: "yourplatform_myshop_postgres-data",
		MountPath:  "/var/lib/postgresql/data",
		Project:    "myshop",
		DBType:     ContainerTypePostgres,
	}

	if info.VolumeName != "yourplatform_myshop_postgres-data" {
		t.Errorf("unexpected VolumeName: %s", info.VolumeName)
	}
	if info.MountPath != "/var/lib/postgresql/data" {
		t.Errorf("unexpected MountPath: %s", info.MountPath)
	}
	if info.Project != "myshop" {
		t.Errorf("unexpected Project: %s", info.Project)
	}
	if info.DBType != ContainerTypePostgres {
		t.Errorf("unexpected DBType: %s", info.DBType)
	}
}

func TestPrepareVolumeForBackup_AppTypeNoOp(t *testing.T) {
	// App container volumes don't need backup prep — should not error
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	err := client.PrepareVolumeForBackup(context.Background(), BackupInfo{
		VolumeName: "test-vol",
		MountPath:  "/data",
		Project:    "test",
		DBType:     ContainerTypeApp,
	})
	if err == nil {
		t.Skip("no Docker socket, but app type should not require prep")
	}
}

// ---------------------------------------------------------------------------
// FindVolumeByName — helper unit tests (no real Docker)
// ---------------------------------------------------------------------------

func TestFindVolumeByName_NoDocker(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	vol, err := client.findVolumeByName(context.Background(), "test-volume")
	if err == nil {
		t.Error("expected error without real Docker socket")
	}
	if vol != nil {
		t.Error("expected nil volume on error")
	}
}
