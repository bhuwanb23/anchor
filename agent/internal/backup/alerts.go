package backup

import "fmt"

// BackupAlert represents an actionable backup-related alert.
type BackupAlert struct {
	Level   string `json:"level"`   // "warning" | "critical"
	Type    string `json:"type"`    // backup-specific alert types
	Message string `json:"message"` // plain-English explanation with next steps
}

// AlertBackupFailed creates an alert when a backup fails entirely.
func AlertBackupFailed(reason string) BackupAlert {
	return BackupAlert{
		Level: "critical",
		Type:  "backup_failed",
		Message: fmt.Sprintf(
			"Backup failed: %s. Your apps are running normally. "+
				"We will try again in 24 hours.",
			reason,
		),
	}
}

// AlertBackupPartial creates an alert when some projects couldn't be backed up.
func AlertBackupPartial(failedProjects []string, reason string) BackupAlert {
	return BackupAlert{
		Level: "warning",
		Type:  "backup_partial",
		Message: fmt.Sprintf(
			"Partial backup completed. Projects not backed up: %v. Reason: %s. "+
				"The backed-up projects are safe. We will retry the failed projects next time.",
			failedProjects, reason,
		),
	}
}

// AlertS3Unreachable creates an alert when backup storage is unreachable.
func AlertS3Unreachable(reason string) BackupAlert {
	return BackupAlert{
		Level: "critical",
		Type:  "backup_s3_unreachable",
		Message: fmt.Sprintf(
			"Backup storage unreachable: %s. "+
				"Your apps are running normally but backups are not being saved. "+
				"The agent will retry in 5 minutes. "+
				"Check your S3 credentials and network connection.",
			reason,
		),
	}
}

// AlertDiskFull creates an alert when there's not enough disk space for backups.
func AlertDiskFull(reason string) BackupAlert {
	return BackupAlert{
		Level: "critical",
		Type:  "backup_disk_full",
		Message: fmt.Sprintf(
			"Not enough disk space for database dump: %s. "+
				"Your apps are running normally. "+
				"Free up disk space or expand your server's storage.",
			reason,
		),
	}
}

// AlertStaleLock creates an alert when a stale backup lock is cleared.
func AlertStaleLock(reason string) BackupAlert {
	return BackupAlert{
		Level: "warning",
		Type:  "backup_stale_lock",
		Message: fmt.Sprintf(
			"Cleared stale backup lock from previous run: %s. "+
				"The next backup will run as scheduled.",
			reason,
		),
	}
}

// AlertBackupRetrying creates an alert when a backup is being retried.
func AlertBackupRetrying(attempt, maxAttempts int, reason string) BackupAlert {
	return BackupAlert{
		Level: "warning",
		Type:  "backup_retrying",
		Message: fmt.Sprintf(
			"Backup attempt %d/%d failed: %s. Retrying automatically.",
			attempt, maxAttempts, reason,
		),
	}
}

// AlertVerificationFailed creates an alert when post-backup or weekly verification fails.
func AlertVerificationFailed(snapshotID, reason string) BackupAlert {
	return BackupAlert{
		Level: "warning",
		Type:  "backup_verification_failed",
		Message: fmt.Sprintf(
			"Warning: Your backup storage appears to have data integrity issues. "+
				"Backups are continuing but some snapshots may not be fully restorable. "+
				"Please contact support immediately. "+
				"Snapshot: %s. Error: %s",
			snapshotID, reason,
		),
	}
}

// AlertVerificationCritical creates an alert when monthly full verification fails.
func AlertVerificationCritical(reason string) BackupAlert {
	return BackupAlert{
		Level: "critical",
		Type:  "backup_verification_critical",
		Message: fmt.Sprintf(
			"Critical: Monthly backup verification failed. "+
				"Your backup repository may have data corruption. "+
				"Please contact support immediately. "+
				"Error: %s",
			reason,
		),
	}
}

// AlertStorageWarning creates an alert when backup storage reaches 80% of plan limit.
func AlertStorageWarning(usedBytes, limitBytes int64, percent int, daysUntilFull int, retentionDaily, retentionWeekly, retentionMonthly int) BackupAlert {
	return BackupAlert{
		Level: "warning",
		Type:  "backup_storage_warning",
		Message: formatStorageAlert(percent, usedBytes, limitBytes, daysUntilFull, retentionDaily, retentionWeekly, retentionMonthly),
	}
}

// AlertStorageUrgent creates an alert when backup storage reaches 95% of plan limit.
func AlertStorageUrgent(usedBytes, limitBytes int64, percent int, daysUntilFull int, retentionDaily, retentionWeekly, retentionMonthly int) BackupAlert {
	return BackupAlert{
		Level: "critical",
		Type:  "backup_storage_urgent",
		Message: formatStorageAlert(percent, usedBytes, limitBytes, daysUntilFull, retentionDaily, retentionWeekly, retentionMonthly),
	}
}

func formatStorageAlert(percent int, usedBytes, limitBytes int64, daysUntilFull, retentionDaily, retentionWeekly, retentionMonthly int) string {
	msg := fmt.Sprintf(
		"Your backup storage is %d%% full (%s of %s used). ",
		percent, formatBytesHuman(usedBytes), formatBytesHuman(limitBytes),
	)
	if daysUntilFull > 0 {
		msg += fmt.Sprintf("At your current backup rate, storage will be full in approximately %d days. ", daysUntilFull)
	}
	msg += fmt.Sprintf(
		"Options:\n"+
			"  - Upgrade your plan for more storage\n"+
			"  - Reduce retention period (currently keeping %d daily, %d weekly, %d monthly)\n"+
			"  - Add your own S3 storage in settings",
		retentionDaily, retentionWeekly, retentionMonthly,
	)
	return msg
}

func formatBytesHuman(bytes int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.1fGB", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%dMB", bytes/mb)
	case bytes >= kb:
		return fmt.Sprintf("%dKB", bytes/kb)
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}
