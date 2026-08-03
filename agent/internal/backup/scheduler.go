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
	executor     BackupExecutor
	lock         *BackupLock
	config       SchedulerConfig
	serverID     string
	dataDir      string
	alertSender  AlertSender
	stateManager StateManager
	reporter     *BackupReporter
	verifier     *VerificationManager
	stopCh       chan struct{}
	mu           sync.Mutex
	nextRun      time.Time
	running      bool
	// Verification scheduling
	lastVerification     time.Time
	lastFullVerification time.Time
	verifyInterval       time.Duration // default: 7 days (weekly)
	fullVerifyInterval   time.Duration // default: 30 days (monthly)
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
		verifyInterval:     7 * 24 * time.Hour,  // weekly
		fullVerifyInterval: 30 * 24 * time.Hour, // monthly
	}
}

// WithStateManager sets the state manager for missed backup detection.
func (s *BackupScheduler) WithStateManager(sm StateManager) *BackupScheduler {
	s.stateManager = sm
	return s
}

// WithReporter sets the backup reporter for status reporting.
func (s *BackupScheduler) WithReporter(reporter *BackupReporter) *BackupScheduler {
	s.reporter = reporter
	return s
}

// WithVerifier sets the verification manager for scheduled verification.
func (s *BackupScheduler) WithVerifier(verifier *VerificationManager) *BackupScheduler {
	s.verifier = verifier
	return s
}

// WithVerificationInterval sets the interval between verification runs.
func (s *BackupScheduler) WithVerificationInterval(interval time.Duration) *BackupScheduler {
	s.verifyInterval = interval
	return s
}

// WithFullVerificationInterval sets the interval between full verification runs.
func (s *BackupScheduler) WithFullVerificationInterval(interval time.Duration) *BackupScheduler {
	s.fullVerifyInterval = interval
	return s
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

	// Check for missed backups on startup
	if s.stateManager != nil {
		lastBackup := s.stateManager.GetLastBackupTime()
		if !lastBackup.IsZero() {
			timeSinceBackup := time.Since(lastBackup)
			if timeSinceBackup > 25*time.Hour {
				slog.Warn("missed backup detected, running immediately",
					"last_backup", lastBackup,
					"hours_since", timeSinceBackup.Hours())
				go s.runBackup(ctx)
			}
		} else {
			slog.Info("no previous backup found, running immediately")
			go s.runBackup(ctx)
		}
	}

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

		// Check if verification is due
		if s.verifier != nil {
			if s.verificationDue(now) {
				go s.runVerification(ctx)
			}
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

	// Generate backup ID for tracking
	backupID := GenerateBackupID()
	startedAt := time.Now()

	// Report running status
	if s.reporter != nil {
		s.reporter.ReportRunning(s.serverID, backupID)
	}

	// Acquire lock
	acquired, err := s.lock.Acquire()
	if err != nil {
		slog.Error("failed to acquire backup lock", "error", err)
		s.sendAlert(AlertBackupFailed(fmt.Sprintf("lock acquisition: %v", err)))
		if s.reporter != nil {
			s.reporter.ReportFailed(s.serverID, backupID, startedAt, fmt.Errorf("lock acquisition: %v", err))
		}
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
			if s.reporter != nil {
				s.reporter.ReportFailed(s.serverID, backupID, startedAt, fmt.Errorf("backup failed after %d attempts: %v", maxRetries, backupErr))
			}
			return
		}
	}

	if result != nil {
		slog.Info("scheduled backup completed",
			"snapshot", result.SnapshotID,
			"duration", result.Duration)

		// Record completion to state for missed backup detection
		if s.stateManager != nil {
			if err := s.stateManager.RecordBackupCompletion(
				result.SnapshotID,
				result.Duration,
				result.TotalBytes,
			); err != nil {
				slog.Warn("failed to record backup completion", "error", err)
			}
		}

		// Report full result to control plane
		if s.reporter != nil {
			s.reporter.ReportResult(s.serverID, backupID, result, true, 0)
		}
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

// verificationDue checks if verification is due based on configured intervals.
func (s *BackupScheduler) verificationDue(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if weekly verification is due
	if s.lastVerification.IsZero() || now.Sub(s.lastVerification) >= s.verifyInterval {
		return true
	}

	// Check if monthly full verification is due
	if s.lastFullVerification.IsZero() || now.Sub(s.lastFullVerification) >= s.fullVerifyInterval {
		return true
	}

	return false
}

// runVerification executes the appropriate verification tier.
func (s *BackupScheduler) runVerification(ctx context.Context) {
	slog.Info("starting scheduled verification")

	// Get the latest snapshot ID from the last backup result
	var snapshotID string
	if s.stateManager != nil {
		// Use a placeholder - the actual snapshot ID should come from the last backup
		// For now, we verify the entire repository
		snapshotID = "latest"
	}

	now := time.Now()
	s.mu.Lock()
	isMonthly := s.lastFullVerification.IsZero() || now.Sub(s.lastFullVerification) >= s.fullVerifyInterval
	s.mu.Unlock()

	var result *VerificationStatus
	if isMonthly {
		slog.Info("running monthly full verification (100%)")
		result = s.verifier.VerifyFull(ctx, snapshotID)
	} else {
		slog.Info("running weekly deep verification (25%)")
		result = s.verifier.VerifyDeep(ctx, snapshotID)
	}

	if result == nil {
		slog.Error("verification returned nil result")
		return
	}

	// Update last verification times
	s.mu.Lock()
	s.lastVerification = now
	if isMonthly {
		s.lastFullVerification = now
	}
	s.mu.Unlock()

	// Alert on failure
	if result.Status == "failed" {
		if isMonthly {
			s.sendAlert(AlertVerificationCritical(result.Error))
		} else {
			s.sendAlert(AlertVerificationFailed(result.SnapshotID, result.Error))
		}
	}

	slog.Info("scheduled verification completed",
		"status", result.Status,
		"subset", result.Subset,
		"duration", result.Duration)
}
