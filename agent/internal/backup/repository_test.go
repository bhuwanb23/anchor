package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGeneratePassword(t *testing.T) {
	pwd, err := GeneratePassword()
	if err != nil {
		t.Fatalf("GeneratePassword: %v", err)
	}

	if pwd == "" {
		t.Error("password should not be empty")
	}

	if len(pwd) < 40 {
		t.Errorf("password too short: len=%d", len(pwd))
	}

	// Generate another and verify they're different
	pwd2, err := GeneratePassword()
	if err != nil {
		t.Fatalf("GeneratePassword second: %v", err)
	}

	if pwd == pwd2 {
		t.Error("passwords should be unique")
	}
}

func TestSaveConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := &RepositoryConfig{
		Destination: "s3:s3.amazonaws.com/backup-bucket",
		Password:    "test-password-123",
		S3Endpoint:  "s3.amazonaws.com",
		S3AccessKey: "AKIAIOSFODNN7EXAMPLE",
		S3SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		S3Bucket:    "backup-bucket",
		S3Region:    "us-east-1",
	}

	if err := SaveConfig(dir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	// Verify file exists
	configPath := filepath.Join(dir, CredentialsFile)
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat config file: %v", err)
	}

	// Verify permissions are 600
	if info.Mode().Perm() != 0600 {
		t.Errorf("config file permissions = %o, want 0600", info.Mode().Perm())
	}
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	original := &RepositoryConfig{
		Destination: "s3:s3.amazonaws.com/backup-bucket",
		Password:    "test-password-123",
		S3Endpoint:  "s3.amazonaws.com",
		S3AccessKey: "AKIAIOSFODNN7EXAMPLE",
		S3SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		S3Bucket:    "backup-bucket",
		S3Region:    "us-east-1",
	}

	if err := SaveConfig(dir, original); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	loaded, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if loaded.Destination != original.Destination {
		t.Errorf("Destination = %q, want %q", loaded.Destination, original.Destination)
	}
	if loaded.Password != original.Password {
		t.Errorf("Password = %q, want %q", loaded.Password, original.Password)
	}
	if loaded.S3Endpoint != original.S3Endpoint {
		t.Errorf("S3Endpoint = %q, want %q", loaded.S3Endpoint, original.S3Endpoint)
	}
	if loaded.S3AccessKey != original.S3AccessKey {
		t.Errorf("S3AccessKey = %q, want %q", loaded.S3AccessKey, original.S3AccessKey)
	}
	if loaded.S3SecretKey != original.S3SecretKey {
		t.Errorf("S3SecretKey = %q, want %q", loaded.S3SecretKey, original.S3SecretKey)
	}
	if loaded.S3Bucket != original.S3Bucket {
		t.Errorf("S3Bucket = %q, want %q", loaded.S3Bucket, original.S3Bucket)
	}
	if loaded.S3Region != original.S3Region {
		t.Errorf("S3Region = %q, want %q", loaded.S3Region, original.S3Region)
	}
}

func TestLoadConfig_NotExists(t *testing.T) {
	dir := t.TempDir()

	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig should return nil for missing file: %v", err)
	}
	if cfg != nil {
		t.Error("config should be nil when file doesn't exist")
	}
}

func TestSavePassword(t *testing.T) {
	dir := t.TempDir()
	pwd := "test-password-123"

	if err := SavePassword(dir, pwd); err != nil {
		t.Fatalf("SavePassword: %v", err)
	}

	// Verify file exists
	passwordPath := filepath.Join(dir, PasswordFile)
	info, err := os.Stat(passwordPath)
	if err != nil {
		t.Fatalf("stat password file: %v", err)
	}

	// Verify permissions are 600
	if info.Mode().Perm() != 0600 {
		t.Errorf("password file permissions = %o, want 0600", info.Mode().Perm())
	}

	// Verify content
	data, err := os.ReadFile(passwordPath)
	if err != nil {
		t.Fatalf("read password file: %v", err)
	}
	if string(data) != pwd {
		t.Errorf("password = %q, want %q", string(data), pwd)
	}
}

func TestLoadPassword(t *testing.T) {
	dir := t.TempDir()
	pwd := "test-password-123"

	if err := SavePassword(dir, pwd); err != nil {
		t.Fatalf("SavePassword: %v", err)
	}

	loaded, err := LoadPassword(dir)
	if err != nil {
		t.Fatalf("LoadPassword: %v", err)
	}

	if loaded != pwd {
		t.Errorf("password = %q, want %q", loaded, pwd)
	}
}

func TestLoadPassword_NotExists(t *testing.T) {
	dir := t.TempDir()

	pwd, err := LoadPassword(dir)
	if err != nil {
		t.Fatalf("LoadPassword should return empty for missing file: %v", err)
	}
	if pwd != "" {
		t.Error("password should be empty when file doesn't exist")
	}
}

func TestRepositoryConfig(t *testing.T) {
	cfg := RepositoryConfig{
		Destination: "s3:s3.amazonaws.com/backup-bucket",
		Password:    "test-password-123",
		S3Endpoint:  "s3.amazonaws.com",
		S3AccessKey: "AKIAIOSFODNN7EXAMPLE",
		S3SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		S3Bucket:    "backup-bucket",
		S3Region:    "us-east-1",
	}

	if cfg.Destination == "" {
		t.Error("Destination should not be empty")
	}
	if cfg.Password == "" {
		t.Error("Password should not be empty")
	}
}

func TestNewRepositoryManager(t *testing.T) {
	cfg := RepositoryConfig{
		Destination: "s3:s3.amazonaws.com/backup-bucket",
		Password:    "test-password-123",
	}

	rm := NewRepositoryManager(cfg, "/usr/local/bin/yourplatform-restic", "/var/lib/yourplatform")

	if rm.config.Destination != cfg.Destination {
		t.Errorf("Destination = %q, want %q", rm.config.Destination, cfg.Destination)
	}
	if rm.resticBin != "/usr/local/bin/yourplatform-restic" {
		t.Errorf("resticBin = %q, want /usr/local/bin/yourplatform-restic", rm.resticBin)
	}
	if rm.dataDir != "/var/lib/yourplatform" {
		t.Errorf("dataDir = %q, want /var/lib/yourplatform", rm.dataDir)
	}
}

func TestParseSnapshotID(t *testing.T) {
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
			name:   "no snapshot",
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
			got := parseSnapshotID(tt.output)
			if got != tt.want {
				t.Errorf("parseSnapshotID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestJoinStrings(t *testing.T) {
	tests := []struct {
		name   string
		input  []string
		want   string
	}{
		{
			name:  "empty",
			input: []string{},
			want:  "",
		},
		{
			name:  "single",
			input: []string{"a"},
			want:  "a",
		},
		{
			name:  "multiple",
			input: []string{"a", "b", "c"},
			want:  "a, b, c",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := joinStrings(tt.input)
			if got != tt.want {
				t.Errorf("joinStrings() = %q, want %q", got, tt.want)
			}
		})
	}
}
