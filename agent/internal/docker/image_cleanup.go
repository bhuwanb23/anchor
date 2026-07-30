package docker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/docker/docker/api/types"
	"golang.org/x/sys/unix"
)

// ---------------------------------------------------------------------------
// Cleanup policy
// ---------------------------------------------------------------------------

// CleanupPolicy defines the rules for automatic image cleanup.
type CleanupPolicy struct {
	// StaleAge is the age after which an unused image is eligible for removal.
	// Default: 30 days.
	StaleAge time.Duration

	// DryRun if true, logs what would be removed without actually removing.
	DryRun bool

	// PinnedImages is a set of image refs that must never be removed.
	PinnedImages map[string]bool

	// PreviousVersions is a set of image refs that are the previous version
	// of each deployed app. These are kept for one-click rollback and must
	// not be removed by cleanup.
	PreviousVersions map[string]bool

	// ImageCache is an optional cache that tracks LastUsedAt timestamps.
	// When set, staleness is determined by LastUsedAt instead of Created time.
	ImageCache *ImageCache
}

// DefaultCleanupPolicy returns a sensible default policy.
func DefaultCleanupPolicy() *CleanupPolicy {
	return &CleanupPolicy{
		StaleAge:         30 * 24 * time.Hour, // 30 days
		DryRun:           false,
		PinnedImages:     make(map[string]bool),
		PreviousVersions: make(map[string]bool),
	}
}

// ---------------------------------------------------------------------------
// Cleanup report
// ---------------------------------------------------------------------------

// CleanupReport summarizes what was cleaned up.
type CleanupReport struct {
	ImagesRemoved     int      `json:"images_removed"`
	BytesReclaimed    int64    `json:"bytes_reclaimed"`
	ImagesSkipped     int      `json:"images_skipped"`    // protected images
	Aggressive        bool     `json:"aggressive"`
	ImagesProtected   []string `json:"images_protected"`  // why each was kept
}

// ---------------------------------------------------------------------------
// Running container image detection
// ---------------------------------------------------------------------------

// runningImageRefs returns a set of image IDs currently depended on by
// any container (running, paused, or stopped). Every app's current version
// image is protected so cleanup never breaks rollback or restarts.
func (c *Client) runningImageRefs(ctx context.Context) (map[string]bool, error) {
	containers, err := c.cliUnsafe().ContainerList(ctx, types.ContainerListOptions{
		All: true, // every container — running, stopped, paused
	})
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}

	refs := make(map[string]bool, len(containers))
	for _, ct := range containers {
		// Track both ImageID (sha256) and Image (tag reference)
		if ct.ImageID != "" {
			refs[ct.ImageID] = true
		}
		if ct.Image != "" {
			refs[ct.Image] = true
		}
	}

	return refs, nil
}

// ---------------------------------------------------------------------------
// Disk pressure check
// ---------------------------------------------------------------------------

// DiskPressureLevel checks disk usage on Docker's data directory and returns
// a pressure level:
//   - 0: normal (below 70%)
//   - 1: elevated (70%–85%) — run standard cleanup
//   - 2: critical (85%+) — run aggressive cleanup
func (c *Client) DiskPressureLevel(ctx context.Context) (int, error) {
	info, err := c.cliUnsafe().Info(ctx)
	if err != nil {
		slog.Warn("failed to get docker info for disk pressure check", "error", err)
		return 0, nil // assume normal if we can't check
	}

	dir := info.DockerRootDir
	if dir == "" {
		dir = "/var/lib/docker"
	}

	return checkDiskPressure(dir)
}

// checkDiskPressure performs a real disk usage check on the given directory
// using unix.Statfs. Returns pressure level (0, 1, or 2) and a human message.
func checkDiskPressure(dir string) (int, string) {
	var stat unix.Statfs_t
	if err := unix.Statfs(dir, &stat); err != nil {
		slog.Warn("failed to statfs for disk pressure check", "dir", dir, "error", err)
		return 0, ""
	}

	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bavail * uint64(stat.Bsize)

	if total == 0 {
		return 0, ""
	}

	usedPercent := 100.0 - (float64(free)/float64(total))*100.0

	slog.Debug("disk pressure check",
		"dir", dir,
		"total_gb", total/(1024*1024*1024),
		"free_gb", free/(1024*1024*1024),
		"used_percent", fmt.Sprintf("%.1f%%", usedPercent),
	)

	if usedPercent >= 85.0 {
		return 2, fmt.Sprintf("Disk is %.0f%% full on %s — critical", usedPercent, dir)
	}
	if usedPercent >= 70.0 {
		return 1, fmt.Sprintf("Disk is %.0f%% full on %s — elevated", usedPercent, dir)
	}

	return 0, ""
}

// ---------------------------------------------------------------------------
// Image cleanup planner
// ---------------------------------------------------------------------------

// planCleanup determines which images should be removed based on the policy.
// It never removes:
//   - Images referenced by any container (running, stopped, or paused)
//   - Images marked as pinned
//   - Previous versions of deployed apps (for rollback)
//   - Images that have been used recently (normal mode only)
func (c *Client) planCleanup(ctx context.Context, policy *CleanupPolicy, aggressive bool) (*CleanupReport, error) {
	report := &CleanupReport{
		ImagesProtected: make([]string, 0),
	}

	// 1. Get images depended on by ALL containers (running, stopped, paused)
	containerRefs, err := c.runningImageRefs(ctx)
	if err != nil {
		return nil, fmt.Errorf("detect container images: %w", err)
	}

	// 2. List all local images
	images, err := c.ListLocalImages(ctx)
	if err != nil {
		return nil, fmt.Errorf("list images: %w", err)
	}

	// 3. Evaluate each image
	for _, img := range images {
		ref := img.RepoTag

		// Skip untagged images (let PruneUnusedImages handle these)
		if ref == "<none>:<none>" {
			continue
		}

		// Never remove images used by any container
		if containerRefs[img.ID] || containerRefs[ref] {
			report.ImagesSkipped++
			report.ImagesProtected = append(report.ImagesProtected,
				fmt.Sprintf("%s (in use by container)", ref))
			continue
		}

		// Never remove pinned images
		if policy.PinnedImages[ref] {
			report.ImagesSkipped++
			report.ImagesProtected = append(report.ImagesProtected,
				fmt.Sprintf("%s (pinned)", ref))
			continue
		}

		// Never remove previous versions (needed for one-click rollback)
		if policy.PreviousVersions[ref] {
			report.ImagesSkipped++
			report.ImagesProtected = append(report.ImagesProtected,
				fmt.Sprintf("%s (previous version — kept for rollback)", ref))
			continue
		}

		// Age check using cache's LastUsedAt if available, else Created time
		if !aggressive {
			if !c.isImageStaleEnough(policy, ref, img.Created) {
				report.ImagesSkipped++
				continue
			}
		}

		// Mark for removal
		report.ImagesRemoved++
		report.BytesReclaimed += img.SizeBytes

		if !policy.DryRun {
			if err := c.RemoveImage(ctx, ref); err != nil {
				slog.Warn("failed to remove image during cleanup", "image", ref, "error", err)
				report.ImagesRemoved-- // don't count failed removals
				report.BytesReclaimed -= img.SizeBytes
			}
		}
	}

	// 4. Always prune dangling images
	if !policy.DryRun {
		if err := c.PruneUnusedImages(ctx); err != nil {
			slog.Warn("failed to prune unused images during cleanup", "error", err)
		}
	}

	return report, nil
}

// isImageStaleEnough checks whether an image is old enough to be removed.
// Uses ImageCache.LastUsedAt when available (more accurate), falls back
// to the image's Created timestamp.
func (c *Client) isImageStaleEnough(policy *CleanupPolicy, ref string, created time.Time) bool {
	// Check cache for LastUsedAt
	if policy.ImageCache != nil {
		if entry := policy.ImageCache.Get(ref); entry != nil && !entry.LastUsedAt.IsZero() {
			return time.Since(entry.LastUsedAt) > policy.StaleAge
		}
	}
	// Fall back to image Created time
	return time.Since(created) > policy.StaleAge
}

// ---------------------------------------------------------------------------
// Public cleanup entry points
// ---------------------------------------------------------------------------

// RunCleanup performs a standard cleanup: removes images not used in 30 days
// and not referenced by any running container or pinned list.
func (c *Client) RunCleanup(ctx context.Context, policy *CleanupPolicy) (*CleanupReport, error) {
	if policy == nil {
		policy = DefaultCleanupPolicy()
	}

	slog.Info("running standard image cleanup",
		"stale_age_hours", policy.StaleAge.Hours(),
		"dry_run", policy.DryRun,
	)

	report, err := c.planCleanup(ctx, policy, false)
	if err != nil {
		return nil, err
	}

	slog.Info("cleanup complete",
		"removed", report.ImagesRemoved,
		"reclaimed_bytes", report.BytesReclaimed,
		"skipped", report.ImagesSkipped,
	)

	return report, nil
}

// RunAggressiveCleanup performs an aggressive cleanup: removes every image
// not currently in use by a running container or pinned, regardless of age.
func (c *Client) RunAggressiveCleanup(ctx context.Context, policy *CleanupPolicy) (*CleanupReport, error) {
	if policy == nil {
		policy = DefaultCleanupPolicy()
	}

	slog.Warn("running aggressive image cleanup — will remove all unused images",
		"dry_run", policy.DryRun,
	)

	report, err := c.planCleanup(ctx, policy, true)
	if err != nil {
		return nil, err
	}

	report.Aggressive = true

	slog.Info("aggressive cleanup complete",
		"removed", report.ImagesRemoved,
		"reclaimed_bytes", report.BytesReclaimed,
		"skipped", report.ImagesSkipped,
	)

	return report, nil
}

// RunCleanupIfNeeded checks disk pressure and runs standard or aggressive
// cleanup accordingly. Returns the report and whether cleanup was triggered.
func (c *Client) RunCleanupIfNeeded(ctx context.Context, policy *CleanupPolicy) (*CleanupReport, bool, error) {
	if policy == nil {
		policy = DefaultCleanupPolicy()
	}

	// Check disk pressure via Docker system info
	pressure, err := c.DiskPressureLevel(ctx)
	if err != nil {
		// If we can't check disk, still run a gentle cleanup
		slog.Warn("failed to check disk pressure, running standard cleanup anyway", "error", err)
		report, runErr := c.RunCleanup(ctx, policy)
		return report, true, runErr
	}

	switch {
	case pressure >= 2:
		slog.Warn("CRITICAL disk pressure detected, running aggressive cleanup")
		report, err := c.RunAggressiveCleanup(ctx, policy)
		return report, true, err

	case pressure >= 1:
		slog.Info("elevated disk pressure detected, running standard cleanup")
		report, err := c.RunCleanup(ctx, policy)
		return report, true, err

	default:
		slog.Debug("disk pressure normal, no cleanup needed")
		return &CleanupReport{}, false, nil
	}
}

// ---------------------------------------------------------------------------
// Scheduled cleanup loop
// ---------------------------------------------------------------------------

// RunScheduledCleanup runs cleanup on a fixed interval (default: weekly),
// and also checks disk pressure on every tick to trigger early cleanup if needed.
//
// This is designed to run in a background goroutine.
func (c *Client) RunScheduledCleanup(ctx context.Context, policy *CleanupPolicy, interval time.Duration) {
	if policy == nil {
		policy = DefaultCleanupPolicy()
	}
	if interval <= 0 {
		interval = 7 * 24 * time.Hour // default: weekly
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	slog.Info("scheduled image cleanup started",
		"interval_hours", interval.Hours(),
		"stale_age_hours", policy.StaleAge.Hours(),
	)

	// Run once at startup
	c.runCleanupCycle(ctx, policy, "startup")

	for {
		select {
		case <-ctx.Done():
			slog.Info("scheduled cleanup stopped")
			return
		case <-ticker.C:
			c.runCleanupCycle(ctx, policy, "scheduled")
		}
	}
}

func (c *Client) runCleanupCycle(ctx context.Context, policy *CleanupPolicy, reason string) {
	slog.Info("image cleanup cycle triggered", "reason", reason)

	report, triggered, err := c.RunCleanupIfNeeded(ctx, policy)
	if err != nil {
		slog.Error("image cleanup failed", "reason", reason, "error", err)
		return
	}

	if !triggered {
		slog.Debug("image cleanup not needed", "reason", reason)
		return
	}

	if report.ImagesRemoved > 0 || report.Aggressive {
		slog.Info("image cleanup completed",
			"reason", reason,
			"removed", report.ImagesRemoved,
			"reclaimed_mb", report.BytesReclaimed/(1024*1024),
			"aggressive", report.Aggressive,
		)
	}
}
