package backup

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
)

// VerifySnapshot runs a quick verification check on a restic snapshot.
// Uses --read-data-subset=1% to verify 1% of data checksums (seconds, not minutes).
func (r *BackupRunner) VerifySnapshot(ctx context.Context) error {
	if r.manager.repository == nil {
		return fmt.Errorf("repository not initialized")
	}

	slog.Info("verifying backup snapshot integrity")

	args := r.manager.repository.repoArgs()
	args = append(args, "check", "--read-data-subset=1%")

	cmd := exec.CommandContext(ctx, r.manager.repository.resticBin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restic check failed: %w\n%s", err, string(output))
	}

	slog.Info("backup snapshot verification passed")
	return nil
}
