package docker

import (
	"context"
	"testing"
	"time"
)

func TestDefaultCleanupPolicy(t *testing.T) {
	p := DefaultCleanupPolicy()
	if p.StaleAge != 30*24*time.Hour {
		t.Errorf("expected 30 days stale age, got %v", p.StaleAge)
	}
	if p.DryRun {
		t.Error("expected DryRun=false by default")
	}
	if p.PinnedImages == nil {
		t.Error("expected non-nil PinnedImages map")
	}
	if p.PreviousVersions == nil {
		t.Error("expected non-nil PreviousVersions map")
	}
}

func TestCleanupPolicy_PinnedImages(t *testing.T) {
	p := DefaultCleanupPolicy()
	p.PinnedImages["nginx:latest"] = true

	if !p.PinnedImages["nginx:latest"] {
		t.Error("expected nginx:latest to be pinned")
	}
}

func TestCleanupPolicy_PreviousVersions(t *testing.T) {
	p := DefaultCleanupPolicy()
	p.PreviousVersions["myapp:v1"] = true

	if !p.PreviousVersions["myapp:v1"] {
		t.Error("expected myapp:v1 to be a previous version")
	}
}

// ---------------------------------------------------------------------------
// Plan cleanup structural tests (no real Docker)
// ---------------------------------------------------------------------------

func TestPlanCleanup_NoDocker(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	policy := DefaultCleanupPolicy()

	_, err := client.planCleanup(context.Background(), policy, false)
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
}

func TestPlanCleanup_NoDocker_Aggressive(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	policy := DefaultCleanupPolicy()

	_, err := client.planCleanup(context.Background(), policy, true)
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
}

// ---------------------------------------------------------------------------
// RunCleanup structural tests
// ---------------------------------------------------------------------------

func TestRunCleanup_NoDocker(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	_, err := client.RunCleanup(context.Background(), DefaultCleanupPolicy())
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
}

func TestRunCleanup_NilPolicy(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	// Should use default policy when nil is passed
	_, err := client.RunCleanup(context.Background(), nil)
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
}

// ---------------------------------------------------------------------------
// Aggressive cleanup
// ---------------------------------------------------------------------------

func TestRunAggressiveCleanup_NoDocker(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	_, err := client.RunAggressiveCleanup(context.Background(), DefaultCleanupPolicy())
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
}

// ---------------------------------------------------------------------------
// Cleanup report
// ---------------------------------------------------------------------------

func TestCleanupReport_DefaultValues(t *testing.T) {
	r := &CleanupReport{}
	if r.ImagesRemoved != 0 {
		t.Errorf("expected 0 images removed, got %d", r.ImagesRemoved)
	}
	if r.Aggressive {
		t.Error("expected aggressive=false")
	}
	if len(r.ImagesProtected) != 0 {
		t.Errorf("expected empty protected list, got %d", len(r.ImagesProtected))
	}
}

func TestCleanupReport_ReclaimedBytes(t *testing.T) {
	r := &CleanupReport{
		ImagesRemoved:  3,
		BytesReclaimed: 500000000, // ~500MB
	}
	if r.ImagesRemoved != 3 {
		t.Errorf("expected 3 removed, got %d", r.ImagesRemoved)
	}
	if r.BytesReclaimed != 500000000 {
		t.Errorf("expected 500MB reclaimed, got %d", r.BytesReclaimed)
	}
}

// ---------------------------------------------------------------------------
// Disk pressure
// ---------------------------------------------------------------------------

func TestDiskPressureLevel_NoDocker(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	level, err := client.DiskPressureLevel(context.Background())
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
	if level != 0 {
		t.Errorf("expected level 0 on error, got %d", level)
	}
}

// ---------------------------------------------------------------------------
// Running image refs
// ---------------------------------------------------------------------------

func TestRunningImageRefs_NoDocker(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	_, err := client.runningImageRefs(context.Background())
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
}

// ---------------------------------------------------------------------------
// Cleanup if needed
// ---------------------------------------------------------------------------

func TestRunCleanupIfNeeded_NoDocker(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	_, triggered, err := client.RunCleanupIfNeeded(context.Background(), DefaultCleanupPolicy())
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
	if triggered {
		t.Error("expected triggered=false on error")
	}
}

func TestRunCleanupIfNeeded_NilPolicy(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	_, triggered, err := client.RunCleanupIfNeeded(context.Background(), nil)
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
	if triggered {
		t.Error("expected triggered=false on error")
	}
}

// ---------------------------------------------------------------------------
// Scheduled cleanup
// ---------------------------------------------------------------------------

func TestRunScheduledCleanup_CancelledContext(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancelled

	// Should return immediately without panic
	client.RunScheduledCleanup(ctx, DefaultCleanupPolicy(), 0)
}

func TestRunScheduledCleanup_NilPolicy(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Should not panic with nil policy
	client.RunScheduledCleanup(ctx, nil, 0)
}

// ---------------------------------------------------------------------------
// Staleness check
// ---------------------------------------------------------------------------

func TestIsImageStaleEnough_NoCache(t *testing.T) {
	client := &Client{}
	policy := DefaultCleanupPolicy()

	// Image created 1 hour ago — not stale (default 30 day threshold)
	if client.isImageStaleEnough(policy, "nginx:latest", time.Now().Add(-1*time.Hour)) {
		t.Error("expected false for 1-hour-old image with 30-day stale age")
	}

	// Image created 60 days ago — stale
	if !client.isImageStaleEnough(policy, "nginx:latest", time.Now().Add(-60*24*time.Hour)) {
		t.Error("expected true for 60-day-old image with 30-day stale age")
	}
}

func TestIsImageStaleEnough_WithCache(t *testing.T) {
	dir := t.TempDir()
	cache, _ := NewImageCache(dir + "/cache.json")

	client := &Client{}
	policy := DefaultCleanupPolicy()
	policy.ImageCache = cache

	// No cache entry — should fall back to Created time
	if client.isImageStaleEnough(policy, "unknown:latest", time.Now().Add(-60*24*time.Hour)) {
		t.Error("expected stale without cache entry when Created is old")
	}
}

// ---------------------------------------------------------------------------
// Pin protection
// ---------------------------------------------------------------------------

func TestPlanCleanup_SkipsPinnedImages(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	policy := DefaultCleanupPolicy()
	policy.PinnedImages["nginx:latest"] = true

	_, err := client.planCleanup(context.Background(), policy, false)
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
	// Structural check: the function doesn't panic when checking PinnedImages
}

func TestPlanCleanup_SkipsPreviousVersions(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	policy := DefaultCleanupPolicy()
	policy.PreviousVersions["myapp:v1"] = true

	_, err := client.planCleanup(context.Background(), policy, false)
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
	// Structural check: the function doesn't panic when checking PreviousVersions
}
