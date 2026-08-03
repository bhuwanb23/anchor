package backup

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	CredentialsFile = "backup-credentials"
	PasswordFile    = "backup-password"
)

// RepositoryConfig holds all restic repository configuration.
type RepositoryConfig struct {
	Destination string `json:"destination"`
	Password    string `json:"password"`
	S3Endpoint  string `json:"s3_endpoint,omitempty"`
	S3AccessKey string `json:"s3_access_key,omitempty"`
	S3SecretKey string `json:"s3_secret_key,omitempty"`
	S3Bucket    string `json:"s3_bucket,omitempty"`
	S3Region    string `json:"s3_region,omitempty"`
	// Retention policy
	RetentionDaily   int `json:"retention_daily,omitempty"`
	RetentionWeekly  int `json:"retention_weekly,omitempty"`
	RetentionMonthly int `json:"retention_monthly,omitempty"`
}

// RepositoryManager handles restic repository operations.
type RepositoryManager struct {
	config    RepositoryConfig
	resticBin string
	dataDir   string
}

// NewRepositoryManager creates a new repository manager.
func NewRepositoryManager(config RepositoryConfig, resticBin, dataDir string) *RepositoryManager {
	return &RepositoryManager{
		config:    config,
		resticBin: resticBin,
		dataDir:   dataDir,
	}
}

// InitRepository creates a new restic repository at the destination.
func (rm *RepositoryManager) InitRepository(ctx context.Context) error {
	slog.Info("initializing restic repository", "dest", rm.config.Destination)

	args := rm.repoArgs()
	args = append(args, "init")

	cmd := exec.CommandContext(ctx, rm.resticBin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restic init failed: %w\n%s", err, string(output))
	}

	slog.Info("restic repository initialized", "dest", rm.config.Destination)
	return nil
}

// VerifyRepository checks if the repository is accessible and valid.
func (rm *RepositoryManager) VerifyRepository(ctx context.Context) error {
	slog.Info("verifying restic repository", "dest", rm.config.Destination)

	args := rm.repoArgs()
	args = append(args, "snapshots", "--json")

	cmd := exec.CommandContext(ctx, rm.resticBin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("repository verification failed: %w\n%s", err, string(output))
	}

	var snapshots []Snapshot
	if err := json.Unmarshal(output, &snapshots); err != nil {
		// Empty repository returns "[]" which is valid
		return nil
	}

	slog.Info("repository verified", "dest", rm.config.Destination, "snapshots", len(snapshots))
	return nil
}

// ListSnapshots returns all snapshots in the repository.
func (rm *RepositoryManager) ListSnapshots(ctx context.Context) ([]Snapshot, error) {
	args := rm.repoArgs()
	args = append(args, "snapshots", "--json")

	cmd := exec.CommandContext(ctx, rm.resticBin, args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("restic snapshots failed: %w", err)
	}

	var raw []struct {
		ID       string   `json:"id"`
		Time     string   `json:"time"`
		Paths    []string `json:"paths"`
		Tags     []string `json:"tags"`
		Hostname string   `json:"hostname"`
	}
	if err := json.Unmarshal(output, &raw); err != nil {
		return nil, fmt.Errorf("parse snapshots json: %w", err)
	}

	var snapshots []Snapshot
	for _, s := range raw {
		snapshots = append(snapshots, Snapshot{
			ID:       s.ID,
			Time:     s.Time,
			Paths:    joinStrings(s.Paths),
			Tags:     joinStrings(s.Tags),
			Hostname: s.Hostname,
		})
	}

	return snapshots, nil
}

// Backup runs a restic backup of the source path.
func (rm *RepositoryManager) Backup(ctx context.Context, sourcePath string, tags []string) (string, error) {
	slog.Info("starting backup", "source", sourcePath, "dest", rm.config.Destination)

	args := rm.repoArgs()
	args = append(args, "backup", sourcePath)
	for _, tag := range tags {
		args = append(args, "--tag", tag)
	}

	cmd := exec.CommandContext(ctx, rm.resticBin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("restic backup failed: %w\n%s", err, string(output))
	}

	// Parse snapshot ID from output
	snapshotID := parseSnapshotID(string(output))
	slog.Info("backup completed", "source", sourcePath, "snapshot", snapshotID)
	return snapshotID, nil
}

// Restore restores from a specific snapshot.
func (rm *RepositoryManager) Restore(ctx context.Context, snapshotID, targetPath string) error {
	slog.Info("starting restore", "snapshot", snapshotID, "target", targetPath)

	args := rm.repoArgs()
	args = append(args, "restore", snapshotID, "--target", targetPath)

	cmd := exec.CommandContext(ctx, rm.resticBin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restic restore failed: %w\n%s", err, string(output))
	}

	slog.Info("restore completed", "snapshot", snapshotID)
	return nil
}

// Prune removes old snapshots based on retention policy.
func (rm *RepositoryManager) Prune(ctx context.Context, keepDaily, keepWeekly, keepMonthly int) error {
	slog.Info("pruning old backups", "keep_daily", keepDaily, "keep_weekly", keepWeekly, "keep_monthly", keepMonthly)

	args := rm.repoArgs()
	args = append(args, "forget",
		"--keep-daily", fmt.Sprintf("%d", keepDaily),
		"--keep-weekly", fmt.Sprintf("%d", keepWeekly),
		"--keep-monthly", fmt.Sprintf("%d", keepMonthly),
		"--prune")

	cmd := exec.CommandContext(ctx, rm.resticBin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restic prune failed: %w\n%s", err, string(output))
	}

	slog.Info("backup pruning completed")
	return nil
}

// BackupJSON runs a restic backup with --json flag for progress reporting.
// Calls progressFn for each line of JSON output.
func (rm *RepositoryManager) BackupJSON(ctx context.Context, sourcePath string, tags []string, progressFn func([]byte)) (string, error) {
	slog.Info("starting JSON backup", "source", sourcePath, "dest", rm.config.Destination)

	args := rm.repoArgs()
	args = append(args, "backup", sourcePath, "--json")
	for _, tag := range tags {
		args = append(args, "--tag", tag)
	}

	cmd := exec.CommandContext(ctx, rm.resticBin, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start restic backup: %w", err)
	}

	// Read JSON output line by line
	buf := make([]byte, 4096)
	var output strings.Builder
	for {
		n, readErr := stdout.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			output.Write(chunk)
			// Parse and report each complete line
			lines := splitLines(string(chunk))
			for _, line := range lines {
				if line != "" && progressFn != nil {
					progressFn([]byte(line))
				}
			}
		}
		if readErr != nil {
			break
		}
	}

	if err := cmd.Wait(); err != nil {
		return "", fmt.Errorf("restic backup failed: %w\n%s", err, output.String())
	}

	snapshotID := parseSnapshotID(output.String())
	slog.Info("JSON backup completed", "source", sourcePath, "snapshot", snapshotID)
	return snapshotID, nil
}

// Verify runs a quick integrity check on the repository.
func (rm *RepositoryManager) Verify(ctx context.Context) error {
	slog.Info("verifying repository integrity", "dest", rm.config.Destination)

	args := rm.repoArgs()
	args = append(args, "check", "--read-data-subset=1%")

	cmd := exec.CommandContext(ctx, rm.resticBin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restic check failed: %w\n%s", err, string(output))
	}

	slog.Info("repository verification passed")
	return nil
}

// VerifySubset runs an integrity check with a configurable data subset.
// subset should be a percentage string like "5%", "25%", or "100%".
func (rm *RepositoryManager) VerifySubset(ctx context.Context, subset string) error {
	slog.Info("verifying repository integrity", "dest", rm.config.Destination, "subset", subset)

	args := rm.repoArgs()
	args = append(args, "check", "--read-data-subset="+subset)

	cmd := exec.CommandContext(ctx, rm.resticBin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restic check failed: %w\n%s", err, string(output))
	}

	slog.Info("repository verification passed", "subset", subset)
	return nil
}

// ListSnapshotFiles lists files in a snapshot and returns the count.
// Used to verify snapshot index is readable and metadata is intact.
func (rm *RepositoryManager) ListSnapshotFiles(ctx context.Context, snapshotID string) (int, error) {
	slog.Info("listing snapshot files", "snapshot", snapshotID)

	args := rm.repoArgs()
	args = append(args, "ls", "--json", snapshotID)

	cmd := exec.CommandContext(ctx, rm.resticBin, args...)
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("restic ls failed: %w", err)
	}

	// restic ls --json outputs one JSON object per line
	var filesCount int
	lines := splitLines(string(output))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err == nil && entry.Name != "" {
			filesCount++
		}
	}

	slog.Info("snapshot files listed", "snapshot", snapshotID, "count", filesCount)
	return filesCount, nil
}

// PruneWithConfig removes old snapshots based on configurable retention policy.
func (rm *RepositoryManager) PruneWithConfig(ctx context.Context, keepDaily, keepWeekly, keepMonthly int) error {
	slog.Info("pruning old backups", "keep_daily", keepDaily, "keep_weekly", keepWeekly, "keep_monthly", keepMonthly)

	args := rm.repoArgs()
	args = append(args, "forget",
		"--keep-daily", fmt.Sprintf("%d", keepDaily),
		"--keep-weekly", fmt.Sprintf("%d", keepWeekly),
		"--keep-monthly", fmt.Sprintf("%d", keepMonthly),
		"--prune")

	cmd := exec.CommandContext(ctx, rm.resticBin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restic prune failed: %w\n%s", err, string(output))
	}

	slog.Info("backup pruning completed")
	return nil
}

// StatsResult holds restic repository size from `restic stats --mode raw-data`.
type StatsResult struct {
	TotalSize      int64 `json:"total_size"`
	TotalFileCount int64 `json:"total_file_count"`
	SnapshotsCount int64 `json:"snapshots_count"`
}

const ResticCacheDir = "/var/lib/yourplatform/restic-cache"

// StatsRawData runs `restic stats --mode raw-data --json` and returns repository size.
func (rm *RepositoryManager) StatsRawData(ctx context.Context) (*StatsResult, error) {
	slog.Info("collecting restic repository stats", "dest", rm.config.Destination)

	args := rm.repoArgs()
	args = append(args, "stats", "--mode", "raw-data", "--json")

	cmd := exec.CommandContext(ctx, rm.resticBin, args...)
	rm.setCacheEnv(cmd)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("restic stats failed: %w", err)
	}

	stats, err := ParseStatsJSON(output)
	if err != nil {
		return nil, err
	}

	slog.Info("repository stats collected",
		"total_size", stats.TotalSize,
		"files", stats.TotalFileCount,
		"snapshots", stats.SnapshotsCount)
	return stats, nil
}

// ParseStatsJSON parses restic stats --json output.
func ParseStatsJSON(data []byte) (*StatsResult, error) {
	var stats StatsResult
	if err := json.Unmarshal(data, &stats); err != nil {
		return nil, fmt.Errorf("parse restic stats json: %w", err)
	}
	return &stats, nil
}

// RebuildIndex runs `restic rebuild-index` to rebuild the repository index.
func (rm *RepositoryManager) RebuildIndex(ctx context.Context) error {
	slog.Info("rebuilding restic index", "dest", rm.config.Destination)

	args := rm.repoArgs()
	args = append(args, "rebuild-index")

	cmd := exec.CommandContext(ctx, rm.resticBin, args...)
	rm.setCacheEnv(cmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restic rebuild-index failed: %w\n%s", err, string(output))
	}

	slog.Info("restic index rebuilt")
	return nil
}

// CacheCleanup runs `restic cache --cleanup` to remove stale local cache entries.
func (rm *RepositoryManager) CacheCleanup(ctx context.Context) error {
	slog.Info("cleaning restic cache", "cache_dir", ResticCacheDir)

	if err := os.MkdirAll(ResticCacheDir, 0700); err != nil {
		return fmt.Errorf("create restic cache dir: %w", err)
	}

	args := []string{"cache", "--cleanup"}
	cmd := exec.CommandContext(ctx, rm.resticBin, args...)
	rm.setCacheEnv(cmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restic cache --cleanup failed: %w\n%s", err, string(output))
	}

	slog.Info("restic cache cleaned")
	return nil
}

// setCacheEnv sets RESTIC_CACHE_DIR on the command.
func (rm *RepositoryManager) setCacheEnv(cmd *exec.Cmd) {
	cmd.Env = append(os.Environ(), "RESTIC_CACHE_DIR="+ResticCacheDir)
}

// GeneratePassword creates a cryptographically secure 32-byte password.
func GeneratePassword() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate password: %w", err)
	}
	return base64.StdEncoding.EncodeToString(bytes), nil
}

// LoadConfig reads repository config from secure storage.
func LoadConfig(dataDir string) (*RepositoryConfig, error) {
	configPath := filepath.Join(dataDir, CredentialsFile)
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read backup config: %w", err)
	}

	var cfg RepositoryConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse backup config: %w", err)
	}

	return &cfg, nil
}

// SaveConfig writes repository config to secure storage with chmod 600.
func SaveConfig(dataDir string, cfg *RepositoryConfig) error {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	configPath := filepath.Join(dataDir, CredentialsFile)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal backup config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return fmt.Errorf("write backup config: %w", err)
	}

	slog.Info("backup config saved", "path", configPath)
	return nil
}

// SavePassword saves the restic password to a separate secure file.
func SavePassword(dataDir, password string) error {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	passwordPath := filepath.Join(dataDir, PasswordFile)
	if err := os.WriteFile(passwordPath, []byte(password), 0600); err != nil {
		return fmt.Errorf("write backup password: %w", err)
	}

	slog.Info("backup password saved", "path", passwordPath)
	return nil
}

// LoadPassword reads the restic password from secure storage.
func LoadPassword(dataDir string) (string, error) {
	passwordPath := filepath.Join(dataDir, PasswordFile)
	data, err := os.ReadFile(passwordPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read backup password: %w", err)
	}
	return string(data), nil
}

// repoArgs builds the restic command arguments with repo and password.
func (rm *RepositoryManager) repoArgs() []string {
	args := []string{"--repo", rm.config.Destination}
	if rm.config.Password != "" {
		args = append(args, "--password", rm.config.Password)
	}
	return args
}

// parseSnapshotID extracts the snapshot ID from restic backup output.
func parseSnapshotID(output string) string {
	// Restic output: "snapshot abc123 saved!"
	for _, line := range splitLines(output) {
		if contains(line, "snapshot") && contains(line, "saved") {
			words := splitWords(line)
			for i, word := range words {
				if word == "snapshot" && i+1 < len(words) {
					return words[i+1]
				}
			}
		}
	}
	return ""
}

// joinStrings joins a slice of strings with ", ".
func joinStrings(ss []string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += ", "
		}
		result += s
	}
	return result
}

// splitLines splits a string by newlines.
func splitLines(s string) []string {
	var lines []string
	current := ""
	for _, c := range s {
		if c == '\n' {
			lines = append(lines, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

// splitWords splits a string by whitespace.
func splitWords(s string) []string {
	var words []string
	current := ""
	for _, c := range s {
		if c == ' ' || c == '\t' {
			if current != "" {
				words = append(words, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		words = append(words, current)
	}
	return words
}

// contains checks if a string contains a substring.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Now returns the current time (for testing).
var Now = time.Now
