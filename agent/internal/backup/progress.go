package backup

import (
	"encoding/json"
	"fmt"
	"strings"
)

// BackupProgress represents the current state of a backup operation.
type BackupProgress struct {
	Phase      string  `json:"phase"`                  // "dumping", "backing_up", "verifying", "pruning", "complete", "error"
	Project    string  `json:"project,omitempty"`       // current project being backed up
	Percent    float64 `json:"percent"`                 // 0.0 - 100.0
	FilesDone  int     `json:"files_done"`
	FilesTotal int     `json:"files_total"`
	BytesDone  int64   `json:"bytes_done"`
	BytesTotal int64   `json:"bytes_total"`
	Speed      float64 `json:"speed_bytes_per_sec"`    // bytes per second
	ETA        int     `json:"eta_seconds"`            // estimated seconds remaining
	Message    string  `json:"message"`                 // human-readable status
}

// ProgressReporter sends backup progress updates to the control plane.
type ProgressReporter interface {
	ReportProgress(progress BackupProgress)
	ReportError(project, errorMsg string)
	ReportComplete(result BackupRunResult)
}

// WsProgressReporter sends backup progress via WebSocket.
type WsProgressReporter struct {
	sendFunc func(v interface{}) error
}

// NewWsProgressReporter creates a new WebSocket-based progress reporter.
func NewWsProgressReporter(sendFunc func(v interface{}) error) *WsProgressReporter {
	return &WsProgressReporter{sendFunc: sendFunc}
}

// ReportProgress sends a progress update to the control plane.
func (r *WsProgressReporter) ReportProgress(p BackupProgress) {
	msg := map[string]interface{}{
		"type": "backup_progress",
		"payload": p,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	r.sendFunc(data)
}

// ReportError sends an error message to the control plane.
func (r *WsProgressReporter) ReportError(project, errorMsg string) {
	msg := map[string]interface{}{
		"type": "backup_error",
		"payload": map[string]interface{}{
			"project": project,
			"error":   errorMsg,
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	r.sendFunc(data)
}

// ReportComplete sends a backup completion message to the control plane.
func (r *WsProgressReporter) ReportComplete(result BackupRunResult) {
	payload := map[string]interface{}{
		"snapshot_id": result.SnapshotID,
		"duration_ms": result.Duration.Milliseconds(),
		"total_bytes": result.TotalBytes,
		"dumps":       len(result.DumpResults),
	}
	if result.Error != "" {
		payload["error"] = result.Error
	}
	msg := map[string]interface{}{
		"type":    "backup_complete",
		"payload": payload,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	r.sendFunc(data)
}

// ResticStatusLine represents a restic JSON status output line.
type ResticStatusLine struct {
	MessageType string `json:"message_type"`
	SecondsElapsed float64 `json:"seconds_elapsed"`
	PercentDone   float64 `json:"percent_done"`
	TotalFiles    int    `json:"total_files"`
	FilesDone     int    `json:"files_done"`
	TotalBytes    int64  `json:"total_bytes"`
	BytesDone     int64  `json:"bytes_done"`
	Speed         float64 `json:"speed"`
}

// ParseResticProgress parses a line of restic JSON output and returns a BackupProgress.
func ParseResticProgress(line string, phase string, project string) *BackupProgress {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}

	var status ResticStatusLine
	if err := json.Unmarshal([]byte(line), &status); err != nil {
		return nil
	}

	if status.MessageType != "status" {
		return nil
	}

	eta := 0
	if status.Speed > 0 {
		remaining := status.TotalBytes - status.BytesDone
		eta = int(float64(remaining) / status.Speed)
	}

	return &BackupProgress{
		Phase:      phase,
		Project:    project,
		Percent:    status.PercentDone * 100,
		FilesDone:  status.FilesDone,
		FilesTotal: status.TotalFiles,
		BytesDone:  status.BytesDone,
		BytesTotal: status.TotalBytes,
		Speed:      status.Speed,
		ETA:        eta,
		Message:    formatProgressMessage(phase, project, status.PercentDone*100, status.BytesDone, status.TotalBytes),
	}
}

// formatProgressMessage creates a human-readable progress message.
func formatProgressMessage(phase, project string, percent float64, bytesDone, totalBytes int64) string {
	switch phase {
	case "dumping":
		if project != "" {
			return fmt.Sprintf("Backing up %s... dumping database", project)
		}
		return "Dumping databases..."
	case "backing_up":
		if project != "" {
			return fmt.Sprintf("Backing up %s... %.0f%%", project, percent)
		}
		return fmt.Sprintf("Backing up... %.0f%%", percent)
	case "verifying":
		return "Verifying backup integrity..."
	case "pruning":
		return "Cleaning up old snapshots..."
	case "complete":
		return "Backup completed successfully"
	case "error":
		return "Backup failed"
	default:
		return fmt.Sprintf("Processing... %.0f%%", percent)
	}
}
