package backup

import (
	"context"
	"testing"
	"time"
)

func TestNewBackupRunner(t *testing.T) {
	stateMgr := &mockStateManager{}
	mockDocker := &MockDockerClient{}

	manager := &BackupManager{
		destination: "local:/tmp/test-repo",
		password:    "test-password",
		dataDir:     t.TempDir(),
		stateMgr:    stateMgr,
	}

	runner := NewBackupRunner(manager, mockDocker)

	if runner == nil {
		t.Fatal("expected non-nil runner")
	}
	if runner.manager != manager {
		t.Error("manager not set correctly")
	}
	if runner.dumpDir != defaultDumpDir {
		t.Errorf("dumpDir = %q, want %q", runner.dumpDir, defaultDumpDir)
	}
}

func TestBackupRunResult_Structure(t *testing.T) {
	result := &BackupRunResult{
		Manifest: &BackupManifest{
			ServerID:  "srv-test",
			Timestamp: time.Now(),
		},
		SnapshotID: "abc123",
		DumpResults: []*DumpResult{
			{
				ComponentType: ComponentTypePostgresDump,
				SizeBytes:     1024,
			},
		},
		TotalBytes: 1024,
		Duration:   5 * time.Second,
	}

	if result.SnapshotID != "abc123" {
		t.Errorf("SnapshotID = %q, want abc123", result.SnapshotID)
	}
	if result.TotalBytes != 1024 {
		t.Errorf("TotalBytes = %d, want 1024", result.TotalBytes)
	}
	if len(result.DumpResults) != 1 {
		t.Errorf("DumpResults len = %d, want 1", len(result.DumpResults))
	}
}

func TestBackupRunResult_WithError(t *testing.T) {
	result := &BackupRunResult{
		Error: "backup failed",
	}

	if result.Error != "backup failed" {
		t.Errorf("Error = %q, want 'backup failed'", result.Error)
	}
}

func TestBackupRunner_GetDumpDir(t *testing.T) {
	mockDocker := &MockDockerClient{}
	manager := &BackupManager{
		destination: "local:/tmp/test-repo",
		dataDir:     t.TempDir(),
	}

	runner := NewBackupRunner(manager, mockDocker)

	if runner.GetDumpDir() != defaultDumpDir {
		t.Errorf("GetDumpDir() = %q, want %q", runner.GetDumpDir(), defaultDumpDir)
	}
}

func TestBackupRunner_GetManifestBuilder(t *testing.T) {
	mockDocker := &MockDockerClient{}
	manager := &BackupManager{
		destination: "local:/tmp/test-repo",
		dataDir:     t.TempDir(),
	}

	runner := NewBackupRunner(manager, mockDocker)

	builder := runner.GetManifestBuilder()
	if builder == nil {
		t.Fatal("expected non-nil manifest builder")
	}
}

func TestBackupRunner_GetDumper(t *testing.T) {
	mockDocker := &MockDockerClient{}
	manager := &BackupManager{
		destination: "local:/tmp/test-repo",
		dataDir:     t.TempDir(),
	}

	runner := NewBackupRunner(manager, mockDocker)

	dumper := runner.GetDumper()
	if dumper == nil {
		t.Fatal("expected non-nil dumper")
	}
}

func TestParseSnapshotIDFromBackupOutput(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "standard output",
			output: "snapshot abc123def456 saved!",
			want:   "abc123def456",
		},
		{
			name:   "with progress lines",
			output: "[0:00] 100%  1.234 MiB  1.234 MiB/s  0:00\nsnapshot abc123 saved!",
			want:   "abc123",
		},
		{
			name:   "no snapshot line",
			output: "error: something went wrong",
			want:   "",
		},
		{
			name:   "empty output",
			output: "",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSnapshotIDFromOutput(tt.output)
			if got != tt.want {
				t.Errorf("parseSnapshotIDFromOutput() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{
			name:  "single line",
			input: "hello",
			want:  1,
		},
		{
			name:  "multiple lines",
			input: "line1\nline2\nline3",
			want:  3,
		},
		{
			name:  "empty",
			input: "",
			want:  0, // Empty string returns no elements
		},
		{
			name:  "trailing newline",
			input: "line1\nline2\n",
			want:  2, // Trailing newline doesn't add extra element
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := splitLines(tt.input)
			if len(lines) != tt.want {
				t.Errorf("splitLines() returned %d lines, want %d", len(lines), tt.want)
			}
		})
	}
}

func TestSplitWords(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{
			name:  "single word",
			input: "hello",
			want:  1,
		},
		{
			name:  "multiple words",
			input: "word1 word2 word3",
			want:  3,
		},
		{
			name:  "tabs",
			input: "word1\tword2\tword3",
			want:  3,
		},
		{
			name:  "empty",
			input: "",
			want:  0,
		},
		{
			name:  "leading/trailing spaces",
			input: "  hello  world  ",
			want:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			words := splitWords(tt.input)
			if len(words) != tt.want {
				t.Errorf("splitWords() returned %d words, want %d: %v", len(words), tt.want, words)
			}
		})
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		substr   string
		expected bool
	}{
		{"found", "hello world", "world", true},
		{"not found", "hello", "world", false},
		{"empty substring", "hello", "", true},
		{"equal strings", "hello", "hello", true},
		{"substring at start", "hello", "hel", true},
		{"substring at end", "hello", "llo", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contains(tt.s, tt.substr)
			if got != tt.expected {
				t.Errorf("contains(%q, %q) = %v, want %v", tt.s, tt.substr, got, tt.expected)
			}
		})
	}
}

func TestPrepareVolumesForBackup(t *testing.T) {
	mockDocker := &MockDockerClient{}
	manager := &BackupManager{
		destination:  "local:/tmp/test-repo",
		dataDir:      t.TempDir(),
		dockerClient: mockDocker,
	}

	runner := NewBackupRunner(manager, mockDocker)

	manifest := &BackupManifest{
		Projects: []ProjectBackup{
			{
				Name: "myshop",
				Components: []BackupComponent{
					{
						Type:       ComponentTypeVolume,
						VolumeName: "yourplatform_myshop_uploads",
						MountPath:  "/var/lib/docker/volumes/yourplatform_myshop_uploads/_data",
					},
					{
						Type:     ComponentTypePostgresDump,
						DumpPath: "/tmp/test.dump",
					},
				},
			},
		},
	}

	err := runner.PrepareVolumesForBackup(context.Background(), manifest)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFinishVolumesAfterBackup(t *testing.T) {
	mockDocker := &MockDockerClient{}
	manager := &BackupManager{
		destination:  "local:/tmp/test-repo",
		dataDir:      t.TempDir(),
		dockerClient: mockDocker,
	}

	runner := NewBackupRunner(manager, mockDocker)

	manifest := &BackupManifest{
		Projects: []ProjectBackup{
			{
				Name: "myshop",
				Components: []BackupComponent{
					{
						Type:       ComponentTypeVolume,
						VolumeName: "yourplatform_myshop_uploads",
						MountPath:  "/var/lib/docker/volumes/yourplatform_myshop_uploads/_data",
					},
				},
			},
		},
	}

	err := runner.FinishVolumesAfterBackup(context.Background(), manifest)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
