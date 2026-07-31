package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewState(t *testing.T) {
	s := NewState()
	if s.Version != StateVersion {
		t.Errorf("expected version %d, got %d", StateVersion, s.Version)
	}
	if len(s.Projects) != 0 {
		t.Errorf("expected empty projects, got %d", len(s.Projects))
	}
}

func TestLoadState_NotExists(t *testing.T) {
	s := LoadState(filepath.Join(t.TempDir(), "nonexistent.json"))
	if s == nil {
		t.Fatal("expected non-nil state")
	}
	if len(s.Projects) != 0 {
		t.Errorf("expected empty projects, got %d", len(s.Projects))
	}
}

func TestLoadState_Corrupted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	os.WriteFile(path, []byte("not valid json {{{"), 0600)

	s := LoadState(path)
	if s == nil {
		t.Fatal("expected non-nil state")
	}
	if len(s.Projects) != 0 {
		t.Errorf("expected empty projects, got %d", len(s.Projects))
	}

	// Corrupted file should be backed up
	if _, err := os.Stat(path + ".bak"); os.IsNotExist(err) {
		t.Error("expected backup of corrupted file")
	}
}

func TestLoadState_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	os.WriteFile(path, []byte(""), 0600)

	s := LoadState(path)
	if s == nil {
		t.Fatal("expected non-nil state")
	}
}

func TestLoadState_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	// Save a state, then load it
	original := NewState()
	original.Projects["myapp"] = &ProjectState{
		Containers: map[string]*ContainerState{
			"app": {
				ContainerID: "abc123def456",
				Image:       "nginx:latest",
				Status:      "running",
				HostPort:    8080,
			},
		},
	}
	SaveState(path, original)

	loaded := LoadState(path)
	if loaded == nil {
		t.Fatal("expected non-nil state")
	}
	if loaded.Version != StateVersion {
		t.Errorf("expected version %d, got %d", StateVersion, loaded.Version)
	}
	if _, ok := loaded.Projects["myapp"]; !ok {
		t.Error("expected myapp project")
	}
}

func TestSaveState_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s := NewState()
	if err := SaveState(path, s); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	// Temp file should not exist after atomic rename
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("temp file should not exist after save")
	}

	// Main file should exist
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("state file should exist after save")
	}
}

func TestSaveState_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "state")
	path := filepath.Join(dir, "state.json")

	s := NewState()
	if err := SaveState(path, s); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("state file should exist after save")
	}
}

// Manager tests

func TestManager_SetAndGetContainer(t *testing.T) {
	mgr := NewManager(t.TempDir())

	container := &ContainerState{
		ContainerID: "abc123def456",
		Image:       "nginx:latest",
		Status:      "running",
		HostPort:    8080,
	}

	if err := mgr.SetContainer("myapp", "app", container); err != nil {
		t.Fatalf("set container failed: %v", err)
	}

	state := mgr.GetState()
	if _, ok := state.Projects["myapp"]; !ok {
		t.Fatal("expected myapp project")
	}
	if c, ok := state.Projects["myapp"].Containers["app"]; !ok {
		t.Fatal("expected app container")
	} else {
		if c.ContainerID != "abc123def456" {
			t.Errorf("expected container ID abc123def456, got %q", c.ContainerID)
		}
		if c.HostPort != 8080 {
			t.Errorf("expected port 8080, got %d", c.HostPort)
		}
	}
}

func TestManager_UpdateStatus(t *testing.T) {
	mgr := NewManager(t.TempDir())

	mgr.SetContainer("myapp", "app", &ContainerState{
		ContainerID: "abc123",
		Image:       "nginx:latest",
		Status:      "running",
	})

	if err := mgr.UpdateStatus("myapp", "app", "stopped"); err != nil {
		t.Fatalf("update status failed: %v", err)
	}

	c := mgr.GetState().Projects["myapp"].Containers["app"]
	if c.Status != "stopped" {
		t.Errorf("expected status 'stopped', got %q", c.Status)
	}
}

func TestManager_RemoveContainer(t *testing.T) {
	mgr := NewManager(t.TempDir())

	mgr.SetContainer("myapp", "app", &ContainerState{ContainerID: "abc123"})
	mgr.SetContainer("myapp", "postgres", &ContainerState{ContainerID: "def456"})

	if err := mgr.RemoveContainer("myapp", "app"); err != nil {
		t.Fatalf("remove container failed: %v", err)
	}

	state := mgr.GetState()
	if _, ok := state.Projects["myapp"].Containers["app"]; ok {
		t.Error("app container should have been removed")
	}
	// postgres should still exist
	if _, ok := state.Projects["myapp"].Containers["postgres"]; !ok {
		t.Error("postgres container should still exist")
	}
}

func TestManager_RemoveContainer_LastOneDeletesProject(t *testing.T) {
	mgr := NewManager(t.TempDir())

	mgr.SetContainer("myapp", "app", &ContainerState{ContainerID: "abc123"})
	mgr.RemoveContainer("myapp", "app")

	if _, ok := mgr.GetState().Projects["myapp"]; ok {
		t.Error("empty project should have been removed")
	}
}

func TestManager_RemoveProject(t *testing.T) {
	mgr := NewManager(t.TempDir())

	mgr.SetContainer("myapp", "app", &ContainerState{ContainerID: "abc123"})
	mgr.SetContainer("myapp", "postgres", &ContainerState{ContainerID: "def456"})

	if err := mgr.RemoveProject("myapp"); err != nil {
		t.Fatalf("remove project failed: %v", err)
	}

	if _, ok := mgr.GetState().Projects["myapp"]; ok {
		t.Error("project should have been removed")
	}
}

func TestManager_PersistsAcrossReloads(t *testing.T) {
	dir := t.TempDir()

	// First manager saves
	mgr1 := NewManager(dir)
	mgr1.SetContainer("myapp", "app", &ContainerState{
		ContainerID: "abc123",
		Image:       "nginx:latest",
		Status:      "running",
	})

	// Second manager loads from disk
	mgr2 := NewManager(dir)
	state := mgr2.GetState()

	c, ok := state.Projects["myapp"].Containers["app"]
	if !ok {
		t.Fatal("expected app container after reload")
	}
	if c.ContainerID != "abc123" {
		t.Errorf("expected container ID abc123, got %q", c.ContainerID)
	}
}

func TestManager_UpdateStatus_Nonexistent(t *testing.T) {
	mgr := NewManager(t.TempDir())

	// Should not error — just no-op
	if err := mgr.UpdateStatus("nonexistent", "app", "running"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestManager_RemoveContainer_Nonexistent(t *testing.T) {
	mgr := NewManager(t.TempDir())

	// Should not error
	if err := mgr.RemoveContainer("nonexistent", "app"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
