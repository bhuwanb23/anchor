package backup

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseStatsJSON(t *testing.T) {
	data := []byte(`{"total_size":859832320,"total_file_count":42,"snapshots_count":7}`)
	stats, err := ParseStatsJSON(data)
	if err != nil {
		t.Fatalf("ParseStatsJSON: %v", err)
	}
	if stats.TotalSize != 859832320 {
		t.Errorf("TotalSize = %d, want 859832320", stats.TotalSize)
	}
	if stats.TotalFileCount != 42 {
		t.Errorf("TotalFileCount = %d, want 42", stats.TotalFileCount)
	}
	if stats.SnapshotsCount != 7 {
		t.Errorf("SnapshotsCount = %d, want 7", stats.SnapshotsCount)
	}
}

func TestParseStatsJSON_Invalid(t *testing.T) {
	_, err := ParseStatsJSON([]byte(`not-json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestAlertStorageWarning(t *testing.T) {
	alert := AlertStorageWarning(820*1024*1024, 1024*1024*1024, 82, 12, 7, 4, 3)
	if alert.Level != "warning" {
		t.Errorf("Level = %q, want warning", alert.Level)
	}
	if alert.Type != "backup_storage_warning" {
		t.Errorf("Type = %q, want backup_storage_warning", alert.Type)
	}
	if !contains(alert.Message, "82%") {
		t.Errorf("message missing percent: %s", alert.Message)
	}
	if !contains(alert.Message, "12 days") {
		t.Errorf("message missing ETA: %s", alert.Message)
	}
	if !contains(alert.Message, "7 daily") {
		t.Errorf("message missing retention: %s", alert.Message)
	}
}

func TestAlertStorageUrgent(t *testing.T) {
	alert := AlertStorageUrgent(980*1024*1024, 1024*1024*1024, 95, 2, 7, 4, 3)
	if alert.Level != "critical" {
		t.Errorf("Level = %q, want critical", alert.Level)
	}
	if alert.Type != "backup_storage_urgent" {
		t.Errorf("Type = %q, want backup_storage_urgent", alert.Type)
	}
}

type mockMaintenance struct {
	cacheCalls  atomic.Int32
	indexCalls  atomic.Int32
	cacheErr    error
	indexErr    error
}

func (m *mockMaintenance) RebuildIndex(ctx context.Context) error {
	m.indexCalls.Add(1)
	return m.indexErr
}

func (m *mockMaintenance) CacheCleanup(ctx context.Context) error {
	m.cacheCalls.Add(1)
	return m.cacheErr
}

func TestScheduler_CacheCleanupSkipsWhenLocked(t *testing.T) {
	executor := &mockBackupExecutor{}
	alertSender := &mockAlertSender{}
	maint := &mockMaintenance{}
	dir := t.TempDir()

	scheduler := NewBackupScheduler(executor, dir, "srv-test", alertSender).
		WithMaintenance(maint)

	acquired, err := scheduler.lock.Acquire()
	if err != nil || !acquired {
		t.Fatalf("acquire lock: acquired=%v err=%v", acquired, err)
	}
	defer scheduler.lock.Release()

	scheduler.runCacheCleanup(context.Background())
	if maint.cacheCalls.Load() != 0 {
		t.Errorf("cache cleanup should skip when lock held, got %d calls", maint.cacheCalls.Load())
	}
}

func TestScheduler_IndexRebuildSkipsWhenLocked(t *testing.T) {
	executor := &mockBackupExecutor{}
	alertSender := &mockAlertSender{}
	maint := &mockMaintenance{}
	dir := t.TempDir()

	scheduler := NewBackupScheduler(executor, dir, "srv-test", alertSender).
		WithMaintenance(maint)

	acquired, err := scheduler.lock.Acquire()
	if err != nil || !acquired {
		t.Fatalf("acquire lock: acquired=%v err=%v", acquired, err)
	}
	defer scheduler.lock.Release()

	scheduler.runIndexRebuild(context.Background())
	if maint.indexCalls.Load() != 0 {
		t.Errorf("index rebuild should skip when lock held, got %d calls", maint.indexCalls.Load())
	}
}

func TestScheduler_CacheCleanupRunsWhenUnlocked(t *testing.T) {
	executor := &mockBackupExecutor{}
	alertSender := &mockAlertSender{}
	maint := &mockMaintenance{}

	scheduler := NewBackupScheduler(executor, t.TempDir(), "srv-test", alertSender).
		WithMaintenance(maint)

	scheduler.runCacheCleanup(context.Background())
	if maint.cacheCalls.Load() != 1 {
		t.Errorf("cache cleanup calls = %d, want 1", maint.cacheCalls.Load())
	}
	if scheduler.lastCacheCleanup.IsZero() {
		t.Error("lastCacheCleanup should be set")
	}
}

func TestScheduler_IndexRebuildRunsWhenUnlocked(t *testing.T) {
	executor := &mockBackupExecutor{}
	alertSender := &mockAlertSender{}
	maint := &mockMaintenance{}

	scheduler := NewBackupScheduler(executor, t.TempDir(), "srv-test", alertSender).
		WithMaintenance(maint)

	scheduler.runIndexRebuild(context.Background())
	if maint.indexCalls.Load() != 1 {
		t.Errorf("index rebuild calls = %d, want 1", maint.indexCalls.Load())
	}
	if scheduler.lastIndexRebuild.IsZero() {
		t.Error("lastIndexRebuild should be set")
	}
}

func TestScheduler_MaintenanceDue(t *testing.T) {
	scheduler := NewBackupScheduler(&mockBackupExecutor{}, t.TempDir(), "srv-test", &mockAlertSender{}).
		WithCacheCleanupInterval(time.Hour).
		WithIndexRebuildInterval(2 * time.Hour)

	now := time.Now()
	if !scheduler.cacheCleanupDue(now) {
		t.Error("cache cleanup should be due when never run")
	}
	if !scheduler.indexRebuildDue(now) {
		t.Error("index rebuild should be due when never run")
	}

	scheduler.lastCacheCleanup = now
	scheduler.lastIndexRebuild = now
	if scheduler.cacheCleanupDue(now.Add(30 * time.Minute)) {
		t.Error("cache cleanup should not be due within interval")
	}
	if scheduler.indexRebuildDue(now.Add(30 * time.Minute)) {
		t.Error("index rebuild should not be due within interval")
	}
	if !scheduler.cacheCleanupDue(now.Add(2 * time.Hour)) {
		t.Error("cache cleanup should be due after interval")
	}
}
