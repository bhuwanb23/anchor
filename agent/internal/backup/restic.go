package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

const (
	ResticVersion = "0.16.4"
	ResticBinary  = "/usr/local/bin/yourplatform-restic"
)

// ResticManager handles restic binary lifecycle.
type ResticManager struct {
	binaryPath string
}

// NewResticManager creates a new restic manager with default binary path.
func NewResticManager() *ResticManager {
	return &ResticManager{binaryPath: ResticBinary}
}

// NewResticManagerWithPath creates a restic manager with custom binary path.
func NewResticManagerWithPath(path string) *ResticManager {
	return &ResticManager{binaryPath: path}
}

// EnsureRestic verifies restic exists and is correct version.
func (r *ResticManager) EnsureRestic(ctx context.Context) error {
	if _, err := os.Stat(r.binaryPath); os.IsNotExist(err) {
		return fmt.Errorf("restic binary not found at %s", r.binaryPath)
	}

	installed, err := r.Version(ctx)
	if err != nil {
		return fmt.Errorf("check restic version: %w", err)
	}

	if installed != ResticVersion {
		return fmt.Errorf("restic version mismatch: installed %s, expected %s", installed, ResticVersion)
	}

	slog.Info("restic binary verified", "path", r.binaryPath, "version", installed)
	return nil
}

// Version returns the installed restic version string.
func (r *ResticManager) Version(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, r.binaryPath, "version")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("restic version command failed: %w", err)
	}

	// Output format: "restic 0.16.4 compiled with go1.21.0 on linux/amd64"
	parts := strings.Fields(string(output))
	if len(parts) < 2 {
		return "", fmt.Errorf("unexpected restic version output: %s", string(output))
	}

	return parts[1], nil
}

// DownloadRestic downloads restic binary from control plane and verifies checksum.
func (r *ResticManager) DownloadRestic(ctx context.Context, baseURL, expectedChecksum string) error {
	arch := runtime.GOARCH
	osName := runtime.GOOS

	downloadURL := fmt.Sprintf("%s/releases/v%s/yourplatform-restic-%s-%s", baseURL, ResticVersion, osName, arch)
	slog.Info("downloading restic", "url", downloadURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download restic: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download restic: HTTP %d", resp.StatusCode)
	}

	// Read and verify checksum
	hasher := sha256.New()
	tmpPath := r.binaryPath + ".tmp"
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer func() {
		tmpFile.Close()
		os.Remove(tmpPath)
	}()

	if _, err := io.Copy(io.MultiWriter(tmpFile, hasher), resp.Body); err != nil {
		return fmt.Errorf("write restic binary: %w", err)
	}
	tmpFile.Close()

	actualChecksum := hex.EncodeToString(hasher.Sum(nil))
	if expectedChecksum != "" && actualChecksum != expectedChecksum {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedChecksum, actualChecksum)
	}

	// Install binary
	if err := os.Rename(tmpPath, r.binaryPath); err != nil {
		return fmt.Errorf("install restic binary: %w", err)
	}

	if err := os.Chmod(r.binaryPath, 0755); err != nil {
		return fmt.Errorf("chmod restic binary: %w", err)
	}

	slog.Info("restic binary installed", "path", r.binaryPath, "version", ResticVersion)
	return nil
}

// BinaryPath returns the path to the restic binary.
func (r *ResticManager) BinaryPath() string {
	return r.binaryPath
}
