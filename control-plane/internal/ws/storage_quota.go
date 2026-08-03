package ws

import (
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
)

// EvaluateAndAlertStorageQuota checks usage against plan limits and fires
// alerts at 80% (warning) and 95% (urgent), once per threshold band.
func EvaluateAndAlertStorageQuota(db *sql.DB, serverID string, sizeBytes int64) {
	if sizeBytes <= 0 {
		return
	}

	limitBytes, daily, weekly, monthly, err := queries.GetBackupStorageLimits(db, serverID)
	if err != nil || limitBytes <= 0 {
		return
	}

	percent := int(float64(sizeBytes) / float64(limitBytes) * 100)
	currentLevel, _ := queries.GetStorageAlertLevel(db, serverID)

	history, _ := queries.GetBackupUsageInfo(db, serverID)
	var hist []queries.BackupStorageHistoryEntry
	if history != nil {
		hist = history.History
	}
	daysUntilFull := queries.EstimateDaysUntilFull(hist, sizeBytes, limitBytes)

	switch {
	case percent >= 95:
		if currentLevel == "95" {
			return
		}
		msg := formatCPStorageAlert(percent, sizeBytes, limitBytes, daysUntilFull, daily, weekly, monthly)
		_ = queries.InsertServerEvent(db, uuid.New().String(), serverID, "alert", "backup_storage_urgent", msg, "")
		_ = queries.SetStorageAlertLevel(db, serverID, "95")
	case percent >= 80:
		if currentLevel == "80" || currentLevel == "95" {
			return
		}
		msg := formatCPStorageAlert(percent, sizeBytes, limitBytes, daysUntilFull, daily, weekly, monthly)
		_ = queries.InsertServerEvent(db, uuid.New().String(), serverID, "alert", "backup_storage_warning", msg, "")
		_ = queries.SetStorageAlertLevel(db, serverID, "80")
	default:
		if currentLevel != "" {
			_ = queries.SetStorageAlertLevel(db, serverID, "")
		}
	}
}

func formatCPStorageAlert(percent int, usedBytes, limitBytes int64, daysUntilFull, retentionDaily, retentionWeekly, retentionMonthly int) string {
	msg := fmt.Sprintf(
		"Your backup storage is %d%% full (%s of %s used). ",
		percent, formatCPBytes(usedBytes), formatCPBytes(limitBytes),
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

func formatCPBytes(bytes int64) string {
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
