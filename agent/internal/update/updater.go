package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// WSSender sends JSON messages to the control plane.
type WSSender interface {
	SendJSON(v interface{}) error
}

// Config holds self-update settings.
type Config struct {
	ControlPlaneURL string
	CurrentVersion  string
	BinaryPath      string
	WSClient        WSSender
	Interval        time.Duration
	HTTPClient      *http.Client
}

// LatestManifest is served at /releases/latest.json.
type LatestManifest struct {
	Version   string            `json:"version"`
	Released  string            `json:"released,omitempty"`
	Checksums map[string]string `json:"checksums"` // key: "linux-amd64", value: sha256 hex
}

// Updater polls for and applies agent self-updates.
type Updater struct {
	cfg Config
}

// New creates an updater.
func New(cfg Config) *Updater {
	if cfg.Interval <= 0 {
		cfg.Interval = time.Hour
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 5 * time.Minute}
	}
	if cfg.BinaryPath == "" {
		cfg.BinaryPath = "/usr/local/bin/yourplatform-agent"
	}
	return &Updater{cfg: cfg}
}

// Start polls for updates until ctx is cancelled.
func (u *Updater) Start(ctx context.Context) {
	ticker := time.NewTicker(u.cfg.Interval)
	defer ticker.Stop()

	// Initial check after short delay
	select {
	case <-ctx.Done():
		return
	case <-time.After(30 * time.Second):
	}
	u.checkAndApply(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			u.checkAndApply(ctx)
		}
	}
}

func (u *Updater) checkAndApply(ctx context.Context) {
	manifest, err := u.FetchLatest(ctx)
	if err != nil {
		slog.Debug("update check failed", "error", err)
		return
	}
	if manifest.Version == "" || manifest.Version == u.cfg.CurrentVersion {
		return
	}
	slog.Info("update available", "current", u.cfg.CurrentVersion, "latest", manifest.Version)
	if err := u.ApplyVersion(ctx, manifest.Version); err != nil {
		slog.Warn("self-update failed", "error", err)
		u.alertFailure(err.Error())
	}
}

// ApplyVersion downloads, verifies, smoke-tests, and swaps the agent binary.
func (u *Updater) ApplyVersion(ctx context.Context, version string) error {
	manifest, err := u.FetchLatest(ctx)
	if err != nil {
		return err
	}
	if version != "" {
		manifest.Version = version
	}
	if manifest.Version == "" {
		return fmt.Errorf("no version to apply")
	}
	if manifest.Version == u.cfg.CurrentVersion {
		return nil
	}

	key := runtime.GOOS + "-" + runtime.GOARCH
	checksum := ""
	if manifest.Checksums != nil {
		checksum = manifest.Checksums[key]
	}

	downloadURL := u.binaryURL(manifest.Version)
	tmpPath := u.cfg.BinaryPath + ".new"

	if err := u.download(ctx, downloadURL, tmpPath, checksum); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("download: %w", err)
	}

	if err := os.Chmod(tmpPath, 0755); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod: %w", err)
	}

	if err := SmokeTest(ctx, tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("smoke test: %w", err)
	}

	if err := AtomicSwap(tmpPath, u.cfg.BinaryPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("atomic swap: %w", err)
	}

	slog.Info("agent binary updated, exiting for systemd restart", "version", manifest.Version)
	os.Exit(0)
	return nil
}

// FetchLatest loads /releases/latest.json from the control plane.
func (u *Updater) FetchLatest(ctx context.Context) (*LatestManifest, error) {
	endpoint := strings.TrimRight(httpBase(u.cfg.ControlPlaneURL), "/") + "/releases/latest.json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := u.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var m LatestManifest
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (u *Updater) binaryURL(version string) string {
	key := runtime.GOOS + "-" + runtime.GOARCH
	base := strings.TrimRight(httpBase(u.cfg.ControlPlaneURL), "/")
	return fmt.Sprintf("%s/releases/v%s/yourplatform-agent-%s", base, version, key)
}

func (u *Updater) download(ctx context.Context, url, dest, expectedSHA string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := u.cfg.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, hasher), resp.Body); err != nil {
		return err
	}
	actual := hex.EncodeToString(hasher.Sum(nil))
	if expectedSHA != "" && !strings.EqualFold(actual, expectedSHA) {
		return fmt.Errorf("checksum mismatch: expected %s got %s", expectedSHA, actual)
	}
	return nil
}

func (u *Updater) alertFailure(reason string) {
	if u.cfg.WSClient == nil {
		return
	}
	_ = u.cfg.WSClient.SendJSON(map[string]interface{}{
		"type": "error_alert",
		"payload": map[string]string{
			"type":    "agent_update_failed",
			"message": "Agent self-update failed: " + reason + ". Current version continues running.",
		},
	})
}

// SmokeTest runs `<binary> version` with a timeout.
func SmokeTest(ctx context.Context, binaryPath string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binaryPath, "version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, string(out))
	}
	if strings.TrimSpace(string(out)) == "" {
		return fmt.Errorf("empty version output")
	}
	return nil
}

// AtomicSwap renames newPath over targetPath.
func AtomicSwap(newPath, targetPath string) error {
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	backup := targetPath + ".bak"
	_ = os.Remove(backup)
	if _, err := os.Stat(targetPath); err == nil {
		if err := os.Rename(targetPath, backup); err != nil {
			return fmt.Errorf("backup current binary: %w", err)
		}
	}
	if err := os.Rename(newPath, targetPath); err != nil {
		// Restore
		_ = os.Rename(backup, targetPath)
		return err
	}
	_ = os.Remove(backup)
	return nil
}

// VerifyChecksum compares file SHA256 to expected hex digest.
func VerifyChecksum(path, expected string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("checksum mismatch: expected %s got %s", expected, actual)
	}
	return nil
}

func httpBase(controlPlaneURL string) string {
	u := strings.TrimSpace(controlPlaneURL)
	u = strings.Replace(u, "wss://", "https://", 1)
	u = strings.Replace(u, "ws://", "http://", 1)
	parsed, err := url.Parse(u)
	if err != nil {
		return u
	}
	parsed.Path = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}
