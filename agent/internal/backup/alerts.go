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
