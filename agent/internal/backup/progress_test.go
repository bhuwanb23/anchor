package backup

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseResticProgress_ValidStatusLine(t *testing.T) {
	line := `{"message_type":"status","seconds_elapsed":1.5,"percent_done":0.452,"total_files":500,"files_done":230,"total_bytes":536870912,"bytes_done":241172480,"speed":10485760}`
	progress := ParseResticProgress(line, "backing_up", "myshop")

	if progress == nil {
		t.Fatal("expected non-nil progress")
	}
	if progress.Phase != "backing_up" {
		t.Errorf("Phase = %q, want %q", progress.Phase, "backing_up")
	}
	if progress.Project != "myshop" {
		t.Errorf("Project = %q, want %q", progress.Project, "myshop")
	}
	if progress.Percent != 45.2 {
		t.Errorf("Percent = %f, want 45.2", progress.Percent)
	}
	if progress.FilesDone != 230 {
		t.Errorf("FilesDone = %d, want 230", progress.FilesDone)
	}
	if progress.FilesTotal != 500 {
		t.Errorf("FilesTotal = %d, want 500", progress.FilesTotal)
	}
	if progress.BytesDone != 241172480 {
		t.Errorf("BytesDone = %d, want 241172480", progress.BytesDone)
	}
	if progress.BytesTotal != 536870912 {
		t.Errorf("BytesTotal = %d, want 536870912", progress.BytesTotal)
	}
	if progress.Speed != 10485760 {
		t.Errorf("Speed = %f, want 10485760", progress.Speed)
	}
}

func TestParseResticProgress_NonStatusMessage(t *testing.T) {
	line := `{"message_type":"summary","files_new":10,"total_bytes_processed":1024}`
	progress := ParseResticProgress(line, "backing_up", "myshop")

	if progress != nil {
		t.Error("expected nil progress for non-status message")
	}
}

func TestParseResticProgress_EmptyLine(t *testing.T) {
	progress := ParseResticProgress("", "backing_up", "myshop")
	if progress != nil {
		t.Error("expected nil progress for empty line")
	}
}

func TestParseResticProgress_InvalidJSON(t *testing.T) {
	progress := ParseResticProgress("not json", "backing_up", "myshop")
	if progress != nil {
		t.Error("expected nil progress for invalid JSON")
	}
}

func TestParseResticProgress_ZeroSpeed(t *testing.T) {
	line := `{"message_type":"status","percent_done":0.5,"total_bytes":1000,"bytes_done":500,"speed":0}`
	progress := ParseResticProgress(line, "backing_up", "")

	if progress == nil {
		t.Fatal("expected non-nil progress")
	}
	if progress.ETA != 0 {
		t.Errorf("ETA = %d, want 0 (zero speed)", progress.ETA)
	}
}

func TestFormatProgressMessage(t *testing.T) {
	tests := []struct {
		name     string
		phase    string
		project  string
		percent  float64
		want     string
	}{
		{"dumping with project", "dumping", "myshop", 0, "Backing up myshop... dumping database"},
		{"dumping without project", "dumping", "", 0, "Dumping databases..."},
		{"backing_up with project", "backing_up", "myshop", 45.2, "Backing up myshop... 45%"},
		{"backing_up without project", "backing_up", "", 75.0, "Backing up... 75%"},
		{"verifying", "verifying", "", 0, "Verifying backup integrity..."},
		{"pruning", "pruning", "", 0, "Cleaning up old snapshots..."},
		{"complete", "complete", "", 0, "Backup completed successfully"},
		{"error", "error", "", 0, "Backup failed"},
		{"unknown", "unknown", "", 50.0, "Processing... 50%"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatProgressMessage(tt.phase, tt.project, tt.percent, 0, 0)
			if got != tt.want {
				t.Errorf("formatProgressMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWsProgressReporter_ReportProgress(t *testing.T) {
	var sentMsg interface{}
	sendFunc := func(v interface{}) error {
		sentMsg = v
		return nil
	}

	reporter := NewWsProgressReporter(sendFunc)
	reporter.ReportProgress(BackupProgress{
		Phase:   "backing_up",
		Project: "myshop",
		Percent: 50.0,
		Message: "Backing up myshop... 50%",
	})

	if sentMsg == nil {
		t.Fatal("expected message to be sent")
	}

	// Marshal to JSON and back to map for assertion
	data, _ := json.Marshal(sentMsg)
	var msg map[string]interface{}
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("failed to parse sent message: %v", err)
	}

	if msg["type"] != "backup_progress" {
		t.Errorf("type = %q, want %q", msg["type"], "backup_progress")
	}
}

func TestWsProgressReporter_ReportError(t *testing.T) {
	var sentMsg interface{}
	sendFunc := func(v interface{}) error {
		sentMsg = v
		return nil
	}

	reporter := NewWsProgressReporter(sendFunc)
	reporter.ReportError("myshop", "dump failed")

	if sentMsg == nil {
		t.Fatal("expected message to be sent")
	}

	data, _ := json.Marshal(sentMsg)
	var msg map[string]interface{}
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("failed to parse sent message: %v", err)
	}

	if msg["type"] != "backup_error" {
		t.Errorf("type = %q, want %q", msg["type"], "backup_error")
	}
}

func TestWsProgressReporter_ReportComplete(t *testing.T) {
	var sent []byte
	sendFunc := func(v interface{}) error {
		data, _ := json.Marshal(v)
		sent = data
		return nil
	}

	reporter := NewWsProgressReporter(sendFunc)
	reporter.ReportComplete(BackupRunResult{
		SnapshotID: "abc123",
		Duration:   5 * time.Second,
		TotalBytes: 1024,
	})

	if sent == nil {
		t.Fatal("expected message to be sent")
	}

	var msg map[string]interface{}
	if err := json.Unmarshal(sent, &msg); err != nil {
		t.Fatalf("failed to parse sent message: %v", err)
	}

	if msg["type"] != "backup_complete" {
		t.Errorf("type = %q, want %q", msg["type"], "backup_complete")
	}
}

func TestBackupProgress_Structure(t *testing.T) {
	progress := BackupProgress{
		Phase:      "backing_up",
		Project:    "myshop",
		Percent:    45.2,
		FilesDone:  230,
		FilesTotal: 500,
		BytesDone:  241172480,
		BytesTotal: 536870912,
		Speed:      10485760,
		ETA:        28,
		Message:    "Backing up myshop... 45%",
	}

	data, err := json.Marshal(progress)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var parsed BackupProgress
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if parsed.Phase != progress.Phase {
		t.Errorf("Phase = %q, want %q", parsed.Phase, progress.Phase)
	}
	if parsed.Percent != progress.Percent {
		t.Errorf("Percent = %f, want %f", parsed.Percent, progress.Percent)
	}
}
