package backup

import (
	"log/slog"
	"time"
)

// WSClient is the interface for sending WebSocket messages.
type WSClient interface {
	SendJSON(v interface{}) error
}

// BackupReporter sends backup status reports to the control plane via WebSocket.
type BackupReporter struct {
	wsClient WSClient
}

// NewBackupReporter creates a new backup reporter.
func NewBackupReporter(wsClient WSClient) *BackupReporter {
	return &BackupReporter{wsClient: wsClient}
}

// BackupStatusPayload is the full backup status message sent to the control plane.
type BackupStatusPayload struct {
	ServerID         string          `json:"server_id"`
	BackupID         string          `json:"backup_id"`
	ResticSnapshotID string          `json:"restic_snapshot_id"`
	Status           string          `json:"status"` // "running" | "success" | "partial" | "failed"
	StartedAt        time.Time       `json:"started_at"`
	CompletedAt      time.Time       `json:"completed_at,omitempty"`
	DurationSeconds  int64           `json:"duration_seconds"`
	SizeNewBytes     int64           `json:"size_new_bytes"`
	SizeTotalBytes   int64           `json:"size_total_bytes"`
	Projects         []ProjectResult `json:"projects,omitempty"`
	RetentionApplied bool            `json:"retention_applied"`
	SnapshotsPruned  int             `json:"snapshots_pruned"`
	Error            string          `json:"error,omitempty"`
}

// ReportRunning sends a "running" status at backup start.
func (r *BackupReporter) ReportRunning(serverID, backupID string) {
	if r.wsClient == nil {
		return
	}

	payload := BackupStatusPayload{
		ServerID: serverID,
		BackupID: backupID,
		Status:   "running",
		StartedAt: time.Now(),
	}

	msg := map[string]interface{}{
		"type":    "backup_status",
		"payload": payload,
	}

	if err := r.wsClient.SendJSON(msg); err != nil {
		slog.Warn("failed to send backup running status", "error", err)
	}
}

// ReportResult sends the full backup result to the control plane.
func (r *BackupReporter) ReportResult(serverID, backupID string, result *BackupRunResult, retentionApplied bool, snapshotsPruned int) {
	if r.wsClient == nil {
		return
	}

	status := "success"
	if result.Error != "" {
		status = "failed"
	} else if r.hasPartialFailures(result) {
		status = "partial"
	}

	payload := BackupStatusPayload{
		ServerID:         serverID,
		BackupID:         backupID,
		ResticSnapshotID: result.SnapshotID,
		Status:           status,
		StartedAt:        result.StartedAt,
		CompletedAt:      time.Now(),
		DurationSeconds:  int64(result.Duration.Seconds()),
		SizeNewBytes:     result.TotalBytes,
		Projects:         result.ProjectResults,
		RetentionApplied: retentionApplied,
		SnapshotsPruned:  snapshotsPruned,
	}

	if result.Error != "" {
		payload.Error = result.Error
	}

	msg := map[string]interface{}{
		"type":    "backup_status",
		"payload": payload,
	}

	if err := r.wsClient.SendJSON(msg); err != nil {
		slog.Warn("failed to send backup result", "error", err)
	}

	slog.Info("backup status reported to control plane",
		"server_id", serverID,
		"backup_id", backupID,
		"status", status,
		"snapshot", result.SnapshotID)
}

// ReportFailed sends a "failed" status when the backup could not complete.
func (r *BackupReporter) ReportFailed(serverID, backupID string, startedAt time.Time, err error) {
	if r.wsClient == nil {
		return
	}

	payload := BackupStatusPayload{
		ServerID:  serverID,
		BackupID:  backupID,
		Status:    "failed",
		StartedAt: startedAt,
		CompletedAt: time.Now(),
		Error:     err.Error(),
	}

	msg := map[string]interface{}{
		"type":    "backup_status",
		"payload": payload,
	}

	if sendErr := r.wsClient.SendJSON(msg); sendErr != nil {
		slog.Warn("failed to send backup failed status", "error", sendErr)
	}
}

// hasPartialFailures checks if any project had a partial failure.
func (r *BackupReporter) hasPartialFailures(result *BackupRunResult) bool {
	for _, p := range result.ProjectResults {
		if p.Status == "partial" || p.Status == "failed" {
			return true
		}
	}
	return false
}

// GenerateBackupID creates a unique backup ID.
func GenerateBackupID() string {
	return "bkp-" + time.Now().Format("20060102150405") + "-" + randomHex(6)
}

// ---------------------------------------------------------------------------
// Restore reporting
// ---------------------------------------------------------------------------

// RestoreStatusPayload is the restore status message sent to the control plane.
type RestoreStatusPayload struct {
	ServerID        string                `json:"server_id"`
	RestoreID       string                `json:"restore_id"`
	SnapshotID      string                `json:"snapshot_id"`
	ProjectName     string                `json:"project_name"`
	Status          string                `json:"status"` // "running" | "success" | "partial" | "failed"
	StartedAt       time.Time             `json:"started_at"`
	CompletedAt     time.Time             `json:"completed_at,omitempty"`
	DurationSeconds int64                 `json:"duration_seconds"`
	Components      []RestoreResult       `json:"components,omitempty"`
	Error           string                `json:"error,omitempty"`
}

// ReportRestoreRunning sends a "running" status at restore start.
func (r *BackupReporter) ReportRestoreRunning(serverID, restoreID, snapshotID, projectName string) {
	if r.wsClient == nil {
		return
	}

	payload := RestoreStatusPayload{
		ServerID:    serverID,
		RestoreID:   restoreID,
		SnapshotID:  snapshotID,
		ProjectName: projectName,
		Status:      "running",
		StartedAt:   time.Now(),
	}

	msg := map[string]interface{}{
		"type":    "restore_status",
		"payload": payload,
	}

	if err := r.wsClient.SendJSON(msg); err != nil {
		slog.Warn("failed to send restore running status", "error", err)
	}
}

// ReportRestoreResult sends the full restore result to the control plane.
func (r *BackupReporter) ReportRestoreResult(serverID, restoreID string, result *RestoreRunResult) {
	if r.wsClient == nil {
		return
	}

	status := "success"
	if result.Error != "" {
		status = "failed"
	} else if result.ProjectResult != nil && result.ProjectResult.Status == "partial" {
		status = "partial"
	}

	payload := RestoreStatusPayload{
		ServerID:        serverID,
		RestoreID:       restoreID,
		SnapshotID:      result.SnapshotID,
		ProjectName:     result.ProjectName,
		Status:          status,
		StartedAt:       result.StartedAt,
		CompletedAt:     time.Now(),
		DurationSeconds: int64(result.Duration.Seconds()),
	}

	if result.ProjectResult != nil {
		payload.Components = result.ProjectResult.Components
	}

	if result.Error != "" {
		payload.Error = result.Error
	} else if result.ProjectResult != nil && result.ProjectResult.Error != "" {
		payload.Error = result.ProjectResult.Error
	}

	msg := map[string]interface{}{
		"type":    "restore_status",
		"payload": payload,
	}

	if err := r.wsClient.SendJSON(msg); err != nil {
		slog.Warn("failed to send restore result", "error", err)
	}

	slog.Info("restore status reported to control plane",
		"server_id", serverID,
		"restore_id", restoreID,
		"status", status,
		"snapshot", result.SnapshotID)
}

// ReportRestoreFailed sends a "failed" status when the restore could not complete.
func (r *BackupReporter) ReportRestoreFailed(serverID, restoreID, snapshotID, projectName string, startedAt time.Time, err error) {
	if r.wsClient == nil {
		return
	}

	payload := RestoreStatusPayload{
		ServerID:    serverID,
		RestoreID:   restoreID,
		SnapshotID:  snapshotID,
		ProjectName: projectName,
		Status:      "failed",
		StartedAt:   startedAt,
		CompletedAt: time.Now(),
		Error:       err.Error(),
	}

	msg := map[string]interface{}{
		"type":    "restore_status",
		"payload": payload,
	}

	if sendErr := r.wsClient.SendJSON(msg); sendErr != nil {
		slog.Warn("failed to send restore failed status", "error", sendErr)
	}
}

// GenerateRestoreID creates a unique restore ID.
func GenerateRestoreID() string {
	return "rst-" + time.Now().Format("20060102150405") + "-" + randomHex(6)
}

// randomHex returns a random hex string of the given length.
func randomHex(n int) string {
	const hexChars = "0123456789abcdef"
	b := make([]byte, n)
	for i := range b {
		b[i] = hexChars[time.Now().UnixNano()%16]
		time.Sleep(1)
	}
	return string(b)
}
