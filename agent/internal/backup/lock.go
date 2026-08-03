package backup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	lockFileName  = "backup.lock"
	staleDuration = 2 * time.Hour
)

// lockData holds information about the current lock holder.
type lockData struct {
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
	ServerID  string    `json:"server_id"`
}

// BackupLock manages a file-based lock to prevent overlapping backups.
type BackupLock struct {
	lockPath string
	mu       sync.Mutex
}

// NewBackupLock creates a new backup lock at the given data directory.
func NewBackupLock(dataDir string) *BackupLock {
	return &BackupLock{
		lockPath: filepath.Join(dataDir, lockFileName),
	}
}

// Acquire attempts to acquire the backup lock.
// Returns true if the lock was acquired, false if another backup is running.
// Automatically detects and clears stale locks.
func (l *BackupLock) Acquire() (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Check if lock file exists
	data, err := os.ReadFile(l.lockPath)
	if err != nil {
		if os.IsNotExist(err) {
			// No lock file, create one
			return l.writeLock()
		}
		return false, fmt.Errorf("read lock file: %w", err)
	}

	// Parse existing lock
	var existing lockData
	if err := json.Unmarshal(data, &existing); err != nil {
		// Corrupt lock file, treat as stale and overwrite
		return l.writeLock()
	}

	// Check if lock is stale (older than 2 hours)
	if time.Since(existing.StartedAt) > staleDuration {
		return l.writeLock()
	}

	// Lock is held by a live process (within stale duration)
	return false, nil
}

// Release removes the backup lock file.
func (l *BackupLock) Release() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := os.Remove(l.lockPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove lock file: %w", err)
	}
	return nil
}

// IsLocked returns whether a backup lock file exists.
func (l *BackupLock) IsLocked() bool {
	_, err := os.Stat(l.lockPath)
	return err == nil
}

// IsStale returns whether the lock file is older than the stale duration.
func (l *BackupLock) IsStale() bool {
	data, err := os.ReadFile(l.lockPath)
	if err != nil {
		return false
	}

	var existing lockData
	if err := json.Unmarshal(data, &existing); err != nil {
		return true // corrupt lock is stale
	}

	return time.Since(existing.StartedAt) > staleDuration
}

// ForceRelease removes a stale lock file regardless of age.
func (l *BackupLock) ForceRelease() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := os.Remove(l.lockPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("force release lock: %w", err)
	}
	return nil
}

// writeLock creates the lock file with current PID and timestamp.
func (l *BackupLock) writeLock() (bool, error) {
	lock := lockData{
		PID:       os.Getpid(),
		StartedAt: time.Now().UTC(),
	}

	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return false, fmt.Errorf("marshal lock data: %w", err)
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(l.lockPath), 0700); err != nil {
		return false, fmt.Errorf("create lock dir: %w", err)
	}

	if err := os.WriteFile(l.lockPath, data, 0600); err != nil {
		return false, fmt.Errorf("write lock file: %w", err)
	}

	return true, nil
}
