package env

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
)

// ---------------------------------------------------------------------------
// ReadEnvFile
// ---------------------------------------------------------------------------

func TestReadEnvFile_NotExists(t *testing.T) {
	m := NewManager(t.TempDir())
	vars, err := m.ReadEnvFile("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vars) != 0 {
		t.Errorf("expected empty map, got %d vars", len(vars))
	}
}

func TestReadEnvFile_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.env")
	os.WriteFile(path, []byte(""), 0600)

	m := NewManager(dir)
	vars, err := m.ReadEnvFile("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vars) != 0 {
		t.Errorf("expected empty map, got %d vars", len(vars))
	}
}

func TestReadEnvFile_InvalidFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.env")
	os.WriteFile(path, []byte("this is not valid env format {{{"), 0600)

	m := NewManager(dir)
	_, err := m.ReadEnvFile("bad")
	if err == nil {
		t.Error("expected error for invalid env format")
	}
}

// ---------------------------------------------------------------------------
// WriteEnvFile
// ---------------------------------------------------------------------------

func TestWriteAndReadEnvFile(t *testing.T) {
	m := NewManager(t.TempDir())
	vars := map[string]string{
		"DATABASE_URL": "postgres://localhost/mydb",
		"API_KEY":      "secret123",
	}

	if err := m.WriteEnvFile("myapp", vars); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	read, err := m.ReadEnvFile("myapp")
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}

	if read["DATABASE_URL"] != vars["DATABASE_URL"] {
		t.Errorf("DATABASE_URL mismatch: got %q", read["DATABASE_URL"])
	}
	if read["API_KEY"] != vars["API_KEY"] {
		t.Errorf("API_KEY mismatch: got %q", read["API_KEY"])
	}
}

func TestWriteEnvFile_Permissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file permissions not enforced on Windows")
	}

	dir := t.TempDir()
	m := NewManager(dir)

	if err := m.WriteEnvFile("myapp", map[string]string{"K": "V"}); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	path := filepath.Join(dir, "myapp.env")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("expected permissions 0600, got %04o", perm)
	}
}

func TestWriteEnvFile_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "envs")
	m := NewManager(dir)

	if err := m.WriteEnvFile("myapp", map[string]string{"K": "V"}); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "myapp.env")); os.IsNotExist(err) {
		t.Error("env file was not created")
	}
}

func TestWriteEnvFile_OverwritesExisting(t *testing.T) {
	m := NewManager(t.TempDir())

	m.WriteEnvFile("myapp", map[string]string{"OLD": "value"})
	m.WriteEnvFile("myapp", map[string]string{"NEW": "value"})

	vars, _ := m.ReadEnvFile("myapp")
	if _, exists := vars["OLD"]; exists {
		t.Error("old key should have been removed")
	}
	if vars["NEW"] != "value" {
		t.Errorf("new key not found, got %v", vars)
	}
}

// ---------------------------------------------------------------------------
// UpdateEnvVar
// ---------------------------------------------------------------------------

func TestUpdateEnvVar_NewKey(t *testing.T) {
	m := NewManager(t.TempDir())
	m.WriteEnvFile("myapp", map[string]string{"A": "1"})

	if err := m.UpdateEnvVar("myapp", "B", "2"); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	vars, _ := m.ReadEnvFile("myapp")
	if vars["A"] != "1" || vars["B"] != "2" {
		t.Errorf("unexpected vars: %v", vars)
	}
}

func TestUpdateEnvVar_Overwrite(t *testing.T) {
	m := NewManager(t.TempDir())
	m.WriteEnvFile("myapp", map[string]string{"A": "old"})

	if err := m.UpdateEnvVar("myapp", "A", "new"); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	vars, _ := m.ReadEnvFile("myapp")
	if vars["A"] != "new" {
		t.Errorf("expected 'new', got %q", vars["A"])
	}
}

// ---------------------------------------------------------------------------
// RemoveEnvVar
// ---------------------------------------------------------------------------

func TestRemoveEnvVar_Exists(t *testing.T) {
	m := NewManager(t.TempDir())
	m.WriteEnvFile("myapp", map[string]string{"A": "1", "B": "2"})

	if err := m.RemoveEnvVar("myapp", "A"); err != nil {
		t.Fatalf("remove failed: %v", err)
	}

	vars, _ := m.ReadEnvFile("myapp")
	if _, exists := vars["A"]; exists {
		t.Error("key A should have been removed")
	}
	if vars["B"] != "2" {
		t.Error("key B should still exist")
	}
}

func TestRemoveEnvVar_NotExists(t *testing.T) {
	m := NewManager(t.TempDir())
	m.WriteEnvFile("myapp", map[string]string{"A": "1"})

	// Should not error
	if err := m.RemoveEnvVar("myapp", "NONEXISTENT"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ListEnvKeys
// ---------------------------------------------------------------------------

func TestListEnvKeys(t *testing.T) {
	m := NewManager(t.TempDir())
	m.WriteEnvFile("myapp", map[string]string{
		"CHARLIE": "3",
		"ALPHA":   "1",
		"BRAVO":   "2",
	})

	keys, err := m.ListEnvKeys("myapp")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}

	expected := []string{"ALPHA", "BRAVO", "CHARLIE"}
	if len(keys) != len(expected) {
		t.Fatalf("expected %d keys, got %d", len(expected), len(keys))
	}
	for i, k := range keys {
		if k != expected[i] {
			t.Errorf("key[%d] = %q, want %q", i, k, expected[i])
		}
	}
}

func TestListEnvKeys_NotExists(t *testing.T) {
	m := NewManager(t.TempDir())
	keys, err := m.ListEnvKeys("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("expected empty list, got %d keys", len(keys))
	}
}

// ---------------------------------------------------------------------------
// FormatForDocker
// ---------------------------------------------------------------------------

func TestFormatForDocker(t *testing.T) {
	vars := map[string]string{"A": "1", "B": "2"}
	env := FormatForDocker(vars)

	if len(env) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(env))
	}

	// Sort for deterministic comparison
	sort.Strings(env)
	if env[0] != "A=1" || env[1] != "B=2" {
		t.Errorf("unexpected format: %v", env)
	}
}

func TestFormatForDocker_Empty(t *testing.T) {
	env := FormatForDocker(nil)
	if len(env) != 0 {
		t.Errorf("expected empty slice, got %v", env)
	}
}

// ---------------------------------------------------------------------------
// MergeWithDefaults
// ---------------------------------------------------------------------------

func TestMergeWithDefaults(t *testing.T) {
	vars := map[string]string{"CUSTOM": "value"}
	merged := MergeWithDefaults(vars, 8080)

	if merged["CUSTOM"] != "value" {
		t.Error("custom var lost")
	}
	if merged["YOURPLATFORM"] != "true" {
		t.Error("YOURPLATFORM not set")
	}
	if merged["PORT"] != "8080" {
		t.Errorf("PORT = %q, want '8080'", merged["PORT"])
	}
}

func TestMergeWithDefaults_NilInput(t *testing.T) {
	merged := MergeWithDefaults(nil, 3000)
	if merged["YOURPLATFORM"] != "true" {
		t.Error("YOURPLATFORM not set")
	}
	if merged["PORT"] != "3000" {
		t.Errorf("PORT = %q, want '3000'", merged["PORT"])
	}
}

// ---------------------------------------------------------------------------
// MaskEnvVars
// ---------------------------------------------------------------------------

func TestMaskEnvVars(t *testing.T) {
	vars := map[string]string{
		"SECRET":    "sk_live_abc123",
		"DB_URL":    "postgres://user:pass@host/db",
		"API_KEY":   "key123",
	}

	masked := MaskEnvVars(vars)

	for k := range vars {
		if masked[k] != "••••••" {
			t.Errorf("key %s not masked, got %q", k, masked[k])
		}
	}
}

func TestMaskEnvVars_Empty(t *testing.T) {
	masked := MaskEnvVars(nil)
	if len(masked) != 0 {
		t.Errorf("expected empty map, got %v", masked)
	}
}

// ---------------------------------------------------------------------------
// GenerateDatabaseURL
// ---------------------------------------------------------------------------

func TestGenerateDatabaseURL(t *testing.T) {
	url := GenerateDatabaseURL("secretpass", "mydb")
	expected := "postgres://yourplatform:secretpass@postgres:5432/mydb?sslmode=disable"
	if url != expected {
		t.Errorf("got %q", url)
	}
}

// ---------------------------------------------------------------------------
// Default env dir
// ---------------------------------------------------------------------------

func TestNewManager_DefaultDir(t *testing.T) {
	m := NewManager("")
	if m.envDir != DefaultEnvDir {
		t.Errorf("expected default dir %q, got %q", DefaultEnvDir, m.envDir)
	}
}
