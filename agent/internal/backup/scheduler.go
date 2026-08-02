package backup

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// BackupExecutor is the interface for running backups.
type BackupExecutor interface {
	RunManifestBackup(ctx context.Context, serverID string) (*BackupRunResult, error)
}

// AlertSender sends backup alerts to the control plane.
type AlertSender interface {
	SendBackupAlert(alert BackupAlert) error
}

// SchedulerConfig holds backup scheduling configuration.
type SchedulerConfig struct {
	Schedule         string // cron expression (for MVP: "HH:MM" format)
	RetentionDaily   int
	RetentionWeekly  int
	RetentionMonthly int
	Enabled          bool
}

// BackupScheduler manages scheduled backup execution.
type BackupScheduler struct {
	executor    BackupExecutor
	lock        *BackupLock
	config      SchedulerConfig
	serverID    string
	dataDir     string
	alertSender AlertSender
	stopCh      chan struct{}
	mu          sync.Mutex
	nextRun     time.Time
	running     bool
}

// NewBackupScheduler creates a new backup scheduler.
func NewBackupScheduler(
	executor BackupExecutor,
	dataDir string,
	serverID string,
	alertSender AlertSender,
) *BackupScheduler {
	return &BackupScheduler{
		executor:    executor,
		lock:        NewBackupLock(dataDir),
		dataDir:     dataDir,
		serverID:    serverID,
		alertSender: alertSender,
		stopCh:      make(chan struct{}),
		config: SchedulerConfig{
			Schedule:         "02:00", // Default: 2am daily
			RetentionDaily:   7,
			RetentionWeekly:  4,
			RetentionMonthly: 12,
			Enabled:          true,
		},
	}
}

// Start begins the scheduler loop. It runs in the calling goroutine.
func (s *BackupScheduler) Start(ctx context.Context) {
	slog.Info("backup scheduler started",
		"schedule", s.config.Schedule,
		"enabled", s.config.Enabled)

	if !s.config.Enabled {
		slog.Info("backup scheduler is disabled")
		return
	}

	s.calculateNextRun()

	for {
		select {
		case <-ctx.Done():
			slog.Info("backup scheduler stopping")
			return
		case <-s.stopCh:
			slog.Info("backup scheduler stopped")
			return
		default:
		}

		now := time.Now()
		if !s.nextRun.IsZero() && now.After(s.nextRun) {
			s.runBackup(ctx)
			s.calculateNextRun()
		}

		// Sleep for 30 seconds before checking again
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-time.After(30 * time.Second):
		}
	}
}

// Stop signals the scheduler to stop.
func (s *BackupScheduler) Stop() {
	close(s.stopCh)
}

// UpdateConfig updates the scheduler configuration at runtime.
func (s *BackupScheduler) UpdateConfig(cfg SchedulerConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = cfg
	if cfg.Enabled {
		s.calculateNextRunLocked()
	}
	slog.Info("backup scheduler config updated",
		"schedule", cfg.Schedule,
		"enabled", cfg.Enabled)
}

// TriggerNow triggers an immediate backup (manual trigger).
func (s *BackupScheduler) TriggerNow(ctx context.Context) error {
	slog.Info("manual backup triggered")
	go s.runBackup(ctx)
	return nil
}

// IsRunning returns whether a backup is currently in progress.
func (s *BackupScheduler) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// NextRunTime returns the next scheduled backup time.
func (s *BackupScheduler) NextRunTime() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nextRun
}

// runBackup executes a backup with locking and alerting.
func (s *BackupScheduler) runBackup(ctx context.Context) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		slog.Warn("backup already in progress, skipping")
		return
	}
	s.running = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	slog.Info("scheduled backup starting")

	// Acquire lock
	acquired, err := s.lock.Acquire()
	if err != nil {
		slog.Error("failed to acquire backup lock", "error", err)
		s.sendAlert(AlertBackupFailed(fmt.Sprintf("lock acquisition: %v", err)))
		return
	}
	if !acquired {
		slog.Warn("backup lock held by another process, skipping")
		return
	}
	defer s.lock.Release()

	// Run backup with retry
	var result *BackupRunResult
	maxRetries := 2
	for attempt := 1; attempt <= maxRetries; attempt++ {
		var backupErr error
		result, backupErr = s.executor.RunManifestBackup(ctx, s.serverID)
		if backupErr == nil {
			break
		}

		slog.Warn("backup attempt failed",
			"attempt", attempt,
			"max_retries", maxRetries,
			"error", backupErr)

		if attempt < maxRetries {
			s.sendAlert(AlertBackupRetrying(attempt, maxRetries, backupErr.Error()))
			// Wait before retry (5 minutes for S3 unreachable, 30 seconds for other errors)
			retryDelay := 30 * time.Second
			select {
			case <-ctx.Done():
				return
			case <-time.After(retryDelay):
			}
		} else {
			s.sendAlert(AlertBackupFailed(backupErr.Error()))
			return
		}
	}

	if result != nil {
		slog.Info("scheduled backup completed",
			"snapshot", result.SnapshotID,
			"duration", result.Duration)
	}
}

// calculateNextRun computes the next backup time from the schedule.
// Caller must NOT hold s.mu.
func (s *BackupScheduler) calculateNextRun() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calculateNextRunLocked()
}

// calculateNextRunLocked computes the next backup time. Caller must hold s.mu.
func (s *BackupScheduler) calculateNextRunLocked() {
	// Parse simple HH:MM schedule format
	var hour, minute int
	n, _ := fmt.Sscanf(s.config.Schedule, "%d:%d", &hour, &minute)
	if n < 2 {
		// Default to 2am
		hour, minute = 2, 0
	}

	now := time.Now()
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())

	// If today's time has passed, schedule for tomorrow
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}

	s.nextRun = next
	slog.Debug("next backup scheduled", "at", next)
}

// sendAlert sends a backup alert if alert sender is configured.
func (s *BackupScheduler) sendAlert(alert BackupAlert) {
	if s.alertSender != nil {
		if err := s.alertSender.SendBackupAlert(alert); err != nil {
			slog.Warn("failed to send backup alert", "error", err)
		}
	}
}
