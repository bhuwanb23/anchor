package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewBackupLock(t *testing.T) {
	dataDir := t.TempDir()
	lock := NewBackupLock(dataDir)

	if lock == nil {
		t.Fatal("expected non-nil lock")
	}

	expectedPath := filepath.Join(dataDir, lockFileName)
	if lock.lockPath != expectedPath {
		t.Errorf("lockPath = %q, want %q", lock.lockPath, expectedPath)
	}
}

func TestBackupLock_AcquireAndRelease(t *testing.T) {
	dataDir := t.TempDir()
	lock := NewBackupLock(dataDir)

	// Acquire lock
	acquired, err := lock.Acquire()
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if !acquired {
		t.Fatal("expected to acquire lock")
	}

	// Verify lock file exists
	if !lock.IsLocked() {
		t.Error("expected lock to be locked")
	}

	// Release lock
	if err := lock.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}

	// Verify lock is released
	if lock.IsLocked() {
		t.Error("expected lock to be unlocked")
	}
}

func TestBackupLock_SecondAcquireFails(t *testing.T) {
	dataDir := t.TempDir()
	lock1 := NewBackupLock(dataDir)
	lock2 := NewBackupLock(dataDir)

	// First acquire
	acquired, err := lock1.Acquire()
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if !acquired {
		t.Fatal("expected to acquire lock")
	}

	// Second acquire should fail
	acquired, err = lock2.Acquire()
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if acquired {
		t.Error("expected second acquire to fail")
	}
}

func TestBackupLock_StaleDetection(t *testing.T) {
	dataDir := t.TempDir()
	lock := NewBackupLock(dataDir)

	// Create a stale lock file (3 hours old)
	staleLock := lockData{
		PID:       99999,
		StartedAt: time.Now().Add(-3 * time.Hour),
		ServerID:  "srv-test",
	}
	data, _ := json.MarshalIndent(staleLock, "", "  ")
	os.WriteFile(lock.lockPath, data, 0600)

	// Should be stale
	if !lock.IsStale() {
		t.Error("expected lock to be stale")
	}

	// Should be able to acquire (stale lock should be cleared)
	acquired, err := lock.Acquire()
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if !acquired {
		t.Error("expected to acquire lock after stale detection")
	}
}

func TestBackupLock_CorruptLockFile(t *testing.T) {
	dataDir := t.TempDir()
	lock := NewBackupLock(dataDir)

	// Create corrupt lock file
	os.WriteFile(lock.lockPath, []byte("not valid json"), 0600)

	// Should be able to acquire (corrupt lock should be overwritten)
	acquired, err := lock.Acquire()
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if !acquired {
		t.Error("expected to acquire lock after corrupt lock")
	}
}

func TestBackupLock_ForceRelease(t *testing.T) {
	dataDir := t.TempDir()
	lock := NewBackupLock(dataDir)

	// Create a lock
	lock.Acquire()
	if !lock.IsLocked() {
		t.Fatal("expected lock to be locked")
	}

	// Force release
	if err := lock.ForceRelease(); err != nil {
		t.Fatalf("ForceRelease() error = %v", err)
	}

	// Should be unlocked
	if lock.IsLocked() {
		t.Error("expected lock to be unlocked after force release")
	}
}

func TestBackupLock_LockDataFormat(t *testing.T) {
	dataDir := t.TempDir()
	lock := NewBackupLock(dataDir)

	// Acquire lock
	lock.Acquire()

	// Read and parse lock file
	data, err := os.ReadFile(lock.lockPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	var ld lockData
	if err := json.Unmarshal(data, &ld); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	// Verify lock data
	if ld.PID != os.Getpid() {
		t.Errorf("PID = %d, want %d", ld.PID, os.Getpid())
	}
	if time.Since(ld.StartedAt) > time.Second {
		t.Errorf("StartedAt is too old: %v", ld.StartedAt)
	}

	// Clean up
	lock.Release()
}

func TestBackupLock_NoLockFile(t *testing.T) {
	dataDir := t.TempDir()
	lock := NewBackupLock(dataDir)

	// Should not be locked
	if lock.IsLocked() {
		t.Error("expected lock to not be locked")
	}

	// Should not be stale
	if lock.IsStale() {
		t.Error("expected lock to not be stale")
	}
}

func TestBackupLock_CreateDirIfNeeded(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "nested", "dir")
	lock := NewBackupLock(dataDir)

	// Acquire should create the directory
	acquired, err := lock.Acquire()
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if !acquired {
		t.Fatal("expected to acquire lock")
	}

	// Verify directory was created
	if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		t.Error("expected directory to be created")
	}

	lock.Release()
}
