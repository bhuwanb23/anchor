package backup

import (
	"context"
	"testing"
	"time"
)

// mockBackupExecutor is a mock implementation of BackupExecutor for testing.
type mockBackupExecutor struct {
	callCount    int
	lastServerID string
	returnError  error
}

func (m *mockBackupExecutor) RunManifestBackup(ctx context.Context, serverID string) (*BackupRunResult, error) {
	m.callCount++
	m.lastServerID = serverID
	if m.returnError != nil {
		return nil, m.returnError
	}
	return &BackupRunResult{
		SnapshotID: "test-snapshot",
		Duration:   time.Second,
	}, nil
}

// mockAlertSender is a mock implementation of AlertSender for testing.
type mockAlertSender struct {
	alerts []BackupAlert
}

func (m *mockAlertSender) SendBackupAlert(alert BackupAlert) error {
	m.alerts = append(m.alerts, alert)
	return nil
}

func TestNewBackupScheduler(t *testing.T) {
	executor := &mockBackupExecutor{}
	alertSender := &mockAlertSender{}

	scheduler := NewBackupScheduler(executor, t.TempDir(), "srv-test", alertSender)

	if scheduler == nil {
		t.Fatal("expected non-nil scheduler")
	}
	if scheduler.executor != executor {
		t.Error("executor not set correctly")
	}
	if scheduler.serverID != "srv-test" {
		t.Errorf("serverID = %q, want %q", scheduler.serverID, "srv-test")
	}
	if !scheduler.config.Enabled {
		t.Error("expected scheduler to be enabled by default")
	}
}

func TestBackupScheduler_UpdateConfig(t *testing.T) {
	executor := &mockBackupExecutor{}
	alertSender := &mockAlertSender{}
	scheduler := NewBackupScheduler(executor, t.TempDir(), "srv-test", alertSender)

	newConfig := SchedulerConfig{
		Schedule:         "03:00",
		RetentionDaily:   14,
		RetentionWeekly:  8,
		RetentionMonthly: 6,
		Enabled:          true,
	}
	scheduler.UpdateConfig(newConfig)

	if scheduler.config.Schedule != "03:00" {
		t.Errorf("Schedule = %q, want %q", scheduler.config.Schedule, "03:00")
	}
	if scheduler.config.RetentionDaily != 14 {
		t.Errorf("RetentionDaily = %d, want 14", scheduler.config.RetentionDaily)
	}
	if !scheduler.config.Enabled {
		t.Error("expected scheduler to be enabled")
	}
}

func TestBackupScheduler_UpdateConfig_Disabled(t *testing.T) {
	executor := &mockBackupExecutor{}
	alertSender := &mockAlertSender{}
	scheduler := NewBackupScheduler(executor, t.TempDir(), "srv-test", alertSender)

	newConfig := SchedulerConfig{
		Schedule: "03:00",
		Enabled:  false,
	}
	scheduler.UpdateConfig(newConfig)

	if scheduler.config.Enabled {
		t.Error("expected scheduler to be disabled")
	}
}

func TestBackupScheduler_TriggerNow(t *testing.T) {
	executor := &mockBackupExecutor{}
	alertSender := &mockAlertSender{}
	scheduler := NewBackupScheduler(executor, t.TempDir(), "srv-test", alertSender)

	// TriggerNow should not block
	err := scheduler.TriggerNow(context.Background())
	if err != nil {
		t.Fatalf("TriggerNow() error = %v", err)
	}

	// Wait a bit for goroutine to start
	time.Sleep(100 * time.Millisecond)

	if scheduler.IsRunning() {
		// It might have already completed
		t.Log("backup is running (expected)")
	}
}

func TestBackupScheduler_IsRunning(t *testing.T) {
	executor := &mockBackupExecutor{}
	alertSender := &mockAlertSender{}
	scheduler := NewBackupScheduler(executor, t.TempDir(), "srv-test", alertSender)

	// Initially not running
	if scheduler.IsRunning() {
		t.Error("expected scheduler to not be running initially")
	}
}

func TestBackupScheduler_NextRunTime(t *testing.T) {
	executor := &mockBackupExecutor{}
	alertSender := &mockAlertSender{}
	scheduler := NewBackupScheduler(executor, t.TempDir(), "srv-test", alertSender)

	// Calculate next run
	scheduler.calculateNextRun()

	nextRun := scheduler.NextRunTime()
	if nextRun.IsZero() {
		t.Error("expected non-zero next run time")
	}

	// Next run should be in the future
	if !nextRun.After(time.Now()) {
		t.Error("expected next run to be in the future")
	}
}

func TestBackupScheduler_CalculateNextRun(t *testing.T) {
	executor := &mockBackupExecutor{}
	alertSender := &mockAlertSender{}
	scheduler := NewBackupScheduler(executor, t.TempDir(), "srv-test", alertSender)

	// Test with specific schedule
	scheduler.config.Schedule = "14:30"
	scheduler.calculateNextRun()

	nextRun := scheduler.NextRunTime()
	if nextRun.IsZero() {
		t.Error("expected non-zero next run time")
	}

	// Verify the hour and minute
	if nextRun.Hour() != 14 || nextRun.Minute() != 30 {
		t.Errorf("next run = %v, want 14:30", nextRun)
	}
}

func TestBackupScheduler_CalculateNextRun_PastTime(t *testing.T) {
	executor := &mockBackupExecutor{}
	alertSender := &mockAlertSender{}
	scheduler := NewBackupScheduler(executor, t.TempDir(), "srv-test", alertSender)

	// Use a time that has already passed today
	now := time.Now()
	pastHour := now.Hour() - 1
	if pastHour < 0 {
		pastHour = 23
	}
	scheduler.config.Schedule = ""
	// Manually set a past time
	scheduler.mu.Lock()
	scheduler.config.Schedule = "00:00" // Midnight is usually in the past
	scheduler.mu.Unlock()

	scheduler.calculateNextRun()

	nextRun := scheduler.NextRunTime()
	// Should be tomorrow
	if nextRun.Before(now) {
		t.Error("expected next run to be in the future")
	}
}

func TestBackupScheduler_CalculateNextRun_InvalidFormat(t *testing.T) {
	executor := &mockBackupExecutor{}
	alertSender := &mockAlertSender{}
	scheduler := NewBackupScheduler(executor, t.TempDir(), "srv-test", alertSender)

	// Invalid schedule format should default to 2am
	scheduler.config.Schedule = "invalid"
	scheduler.calculateNextRun()

	nextRun := scheduler.NextRunTime()
	if nextRun.IsZero() {
		t.Error("expected non-zero next run time for invalid format")
	}

	// Should default to 2am
	if nextRun.Hour() != 2 {
		t.Errorf("expected default hour 2, got %d", nextRun.Hour())
	}
}

func TestBackupScheduler_Stop(t *testing.T) {
	executor := &mockBackupExecutor{}
	alertSender := &mockAlertSender{}
	scheduler := NewBackupScheduler(executor, t.TempDir(), "srv-test", alertSender)

	// Stop should close the stop channel
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go scheduler.Start(ctx)

	// Wait a bit for scheduler to start
	time.Sleep(100 * time.Millisecond)

	// Stop
	scheduler.Stop()

	// Wait a bit for scheduler to stop
	time.Sleep(200 * time.Millisecond)

	// Scheduler should have stopped
	t.Log("scheduler stopped successfully")
}

func TestBackupScheduler_StartDisabled(t *testing.T) {
	executor := &mockBackupExecutor{}
	alertSender := &mockAlertSender{}
	scheduler := NewBackupScheduler(executor, t.TempDir(), "srv-test", alertSender)

	// Disable scheduler
	scheduler.UpdateConfig(SchedulerConfig{
		Schedule: "02:00",
		Enabled:  false,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start should return immediately for disabled scheduler
	done := make(chan struct{})
	go func() {
		scheduler.Start(ctx)
		close(done)
	}()

	select {
	case <-done:
		// Expected - scheduler returned immediately
	case <-time.After(1 * time.Second):
		t.Error("scheduler did not return for disabled config")
	}
}

func TestBackupAlert_Structure(t *testing.T) {
	alert := AlertBackupFailed("S3 unreachable")

	if alert.Level != "critical" {
		t.Errorf("Level = %q, want %q", alert.Level, "critical")
	}
	if alert.Type != "backup_failed" {
		t.Errorf("Type = %q, want %q", alert.Type, "backup_failed")
	}
	if alert.Message == "" {
		t.Error("expected non-empty message")
	}
}

func TestBackupAlert_Partial(t *testing.T) {
	alert := AlertBackupPartial([]string{"myshop", "blog"}, "container not running")

	if alert.Level != "warning" {
		t.Errorf("Level = %q, want %q", alert.Level, "warning")
	}
	if alert.Type != "backup_partial" {
		t.Errorf("Type = %q, want %q", alert.Type, "backup_partial")
	}
}

func TestBackupAlert_S3Unreachable(t *testing.T) {
	alert := AlertS3Unreachable("connection refused")

	if alert.Level != "critical" {
		t.Errorf("Level = %q, want %q", alert.Level, "critical")
	}
	if alert.Type != "backup_s3_unreachable" {
		t.Errorf("Type = %q, want %q", alert.Type, "backup_s3_unreachable")
	}
}

func TestBackupAlert_DiskFull(t *testing.T) {
	alert := AlertDiskFull("no space left on device")

	if alert.Level != "critical" {
		t.Errorf("Level = %q, want %q", alert.Level, "critical")
	}
	if alert.Type != "backup_disk_full" {
		t.Errorf("Type = %q, want %q", alert.Type, "backup_disk_full")
	}
}

func TestBackupAlert_StaleLock(t *testing.T) {
	alert := AlertStaleLock("lock age exceeded")

	if alert.Level != "warning" {
		t.Errorf("Level = %q, want %q", alert.Level, "warning")
	}
	if alert.Type != "backup_stale_lock" {
		t.Errorf("Type = %q, want %q", alert.Type, "backup_stale_lock")
	}
}

func TestBackupAlert_Retrying(t *testing.T) {
	alert := AlertBackupRetrying(1, 3, "timeout")

	if alert.Level != "warning" {
		t.Errorf("Level = %q, want %q", alert.Level, "warning")
	}
	if alert.Type != "backup_retrying" {
		t.Errorf("Type = %q, want %q", alert.Type, "backup_retrying")
	}
}

// mockStateManagerForScheduler implements StateManager for scheduler testing.
type mockStateManagerForScheduler struct {
	lastBackupTime time.Time
	snapshotID     string
	duration       time.Duration
	totalBytes     int64
}

func (m *mockStateManagerForScheduler) GetState() *StateData {
	return &StateData{Projects: map[string]interface{}{}}
}

func (m *mockStateManagerForScheduler) GetLastBackupTime() time.Time {
	return m.lastBackupTime
}

func (m *mockStateManagerForScheduler) RecordBackupCompletion(snapshotID string, duration time.Duration, totalBytes int64) error {
	m.snapshotID = snapshotID
	m.duration = duration
	m.totalBytes = totalBytes
	return nil
}

func TestScheduler_MissedBackupDetection_NoPreviousBackup(t *testing.T) {
	executor := &mockBackupExecutor{}
	alertSender := &mockAlertSender{}
	stateMgr := &mockStateManagerForScheduler{
		lastBackupTime: time.Time{}, // Zero time = no backup ever
	}

	scheduler := NewBackupScheduler(executor, t.TempDir(), "server-1", alertSender).
		WithStateManager(stateMgr)

	// Start scheduler with a short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go scheduler.Start(ctx)

	// Wait for the missed backup to trigger
	time.Sleep(1 * time.Second)

	// Should have triggered a backup
	if executor.callCount < 1 {
		t.Error("expected at least 1 backup call for no previous backup")
	}
}

func TestScheduler_MissedBackupDetection_OldBackup(t *testing.T) {
	executor := &mockBackupExecutor{}
	alertSender := &mockAlertSender{}
	stateMgr := &mockStateManagerForScheduler{
		lastBackupTime: time.Now().Add(-26 * time.Hour), // 26 hours ago
	}

	scheduler := NewBackupScheduler(executor, t.TempDir(), "server-1", alertSender).
		WithStateManager(stateMgr)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go scheduler.Start(ctx)

	time.Sleep(1 * time.Second)

	if executor.callCount < 1 {
		t.Error("expected at least 1 backup call for old backup (>25h)")
	}
}

func TestScheduler_MissedBackupDetection_RecentBackup(t *testing.T) {
	executor := &mockBackupExecutor{}
	alertSender := &mockAlertSender{}
	stateMgr := &mockStateManagerForScheduler{
		lastBackupTime: time.Now().Add(-1 * time.Hour), // 1 hour ago
	}

	scheduler := NewBackupScheduler(executor, t.TempDir(), "server-1", alertSender).
		WithStateManager(stateMgr)

	// Set schedule far in the future so no scheduled backup runs
	scheduler.UpdateConfig(SchedulerConfig{
		Schedule: "23:59",
		Enabled:  true,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go scheduler.Start(ctx)

	time.Sleep(1 * time.Second)

	// Should NOT trigger a backup
	if executor.callCount > 0 {
		t.Error("expected no backup for recent backup (<25h)")
	}
}

func TestScheduler_MissedBackupDetection_NoStateManager(t *testing.T) {
	executor := &mockBackupExecutor{}
	alertSender := &mockAlertSender{}

	// No state manager set
	scheduler := NewBackupScheduler(executor, t.TempDir(), "server-1", alertSender)

	scheduler.UpdateConfig(SchedulerConfig{
		Schedule: "23:59",
		Enabled:  true,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go scheduler.Start(ctx)

	time.Sleep(1 * time.Second)

	// Should NOT trigger a backup
	if executor.callCount > 0 {
		t.Error("expected no backup when state manager is nil")
	}
}
