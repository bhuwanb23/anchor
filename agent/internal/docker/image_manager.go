package docker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
)

// ---------------------------------------------------------------------------
// Image reference
// ---------------------------------------------------------------------------

// ImageRef holds a parsed Docker image reference.
type ImageRef struct {
	Registry string // e.g. "ghcr.io", "docker.io" — empty for Docker Hub
	Name     string // e.g. "nginx", "library/nginx", "owner/myapp"
	Tag      string // e.g. "latest", "1.25", "v1.2.3"
	Full     string // original reference string (normalized)
}

// ParseImageRef parses a Docker image reference into its components.
//   ""                → {Name:"", Tag:"latest"}
//   "nginx"           → {Name:"nginx", Tag:"latest"}
//   "nginx:1.25"      → {Name:"nginx", Tag:"1.25"}
//   "library/nginx"   → {Name:"library/nginx", Tag:"latest"}
//   "ghcr.io/owner/myapp:v1.0" → {Registry:"ghcr.io", Name:"owner/myapp", Tag:"v1.0"}
//   "docker.io/library/nginx:latest" → {Registry:"docker.io", Name:"library/nginx", Tag:"latest"}
func ParseImageRef(ref string) ImageRef {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ImageRef{Name: "", Tag: "latest", Full: ""}
	}

	r := ImageRef{Full: ref}

	// Split tag from name (last colon)
	if idx := strings.LastIndex(ref, ":"); idx > 0 && !strings.Contains(ref[idx:], "/") {
		// Check that the colon is not part of a port number in a registry URL
		// e.g. "localhost:5000/myimage:tag" — the last colon is the tag separator
		r.Tag = ref[idx+1:]
		ref = ref[:idx]
	} else {
		r.Tag = "latest"
	}

	// Check for registry (contains a dot or colon with a slash after)
	if strings.Contains(ref, ".") || strings.Contains(ref, ":") {
		parts := strings.SplitN(ref, "/", 2)
		if len(parts) == 2 {
			r.Registry = parts[0]
			r.Name = parts[1]
		} else {
			r.Name = ref
		}
	} else {
		r.Name = ref
	}

	// If no registry and name has single path component on Docker Hub,
	// it's a library image (e.g., "nginx" → "library/nginx")
	// Don't modify the name — Docker SDK handles this.

	return r
}

// IsLatest returns true if the tag is "latest" or empty.
func (r ImageRef) IsLatest() bool {
	return r.Tag == "" || r.Tag == "latest"
}

// IsRegistrySpecific returns true if a registry was explicitly specified.
func (r ImageRef) IsRegistrySpecific() bool {
	return r.Registry != ""
}

// Normalized returns the reference in a standard form: [registry/]name:tag.
func (r ImageRef) Normalized() string {
	if r.Registry != "" {
		return fmt.Sprintf("%s/%s:%s", r.Registry, r.Name, r.Tag)
	}
	return fmt.Sprintf("%s:%s", r.Name, r.Tag)
}

// ---------------------------------------------------------------------------
// Image summary
// ---------------------------------------------------------------------------

// ImageSummary holds information about an image available locally.
type ImageSummary struct {
	ID           string    `json:"id"`
	RepoTag      string    `json:"repo_tag"`
	RepoDigests  []string  `json:"repo_digests,omitempty"`
	SizeBytes    int64     `json:"size_bytes"`
	Created      time.Time `json:"created"`
	OS           string    `json:"os"`
	Arch         string    `json:"arch"`
}

// Digest returns the first available digest from RepoDigests, or empty string.
func (s *ImageSummary) Digest() string {
	for _, d := range s.RepoDigests {
		if idx := strings.Index(d, "@"); idx > 0 {
			return d[idx+1:]
		}
	}
	return ""
}

// HumanSize returns the image size in a human-readable format.
func (s *ImageSummary) HumanSize() string {
	switch {
	case s.SizeBytes >= 1<<30:
		return fmt.Sprintf("%.1fGB", float64(s.SizeBytes)/float64(1<<30))
	case s.SizeBytes >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(s.SizeBytes)/float64(1<<20))
	case s.SizeBytes >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(s.SizeBytes)/float64(1<<10))
	default:
		return fmt.Sprintf("%dB", s.SizeBytes)
	}
}

// ---------------------------------------------------------------------------
// Pull progress
// ---------------------------------------------------------------------------

// PullProgress represents a single progress update during an image pull.
type PullProgress struct {
	ID      string `json:"id"`      // Layer ID
	Status  string `json:"status"`  // e.g. "Pulling fs layer", "Downloading", "Extracting", "Pull complete"
	Current int64  `json:"current"` // Bytes downloaded so far
	Total   int64  `json:"total"`   // Total bytes for this layer
	Stream  string `json:"stream"`  // Stream message (for non-layer messages)
}

// PullProgressFunc is a callback that receives pull progress updates.
// If it returns an error, the pull is aborted.
type PullProgressFunc func(progress PullProgress) error

// ---------------------------------------------------------------------------
// Image existence check
// ---------------------------------------------------------------------------

// ImageExistsLocally checks whether an image with the given reference
// already exists in the local Docker image store. Returns a summary
// of the local image if found.
//
// Uses ImageInspectWithRaw for an efficient O(1) lookup by reference,
// rather than listing all images and iterating.
func (c *Client) ImageExistsLocally(ctx context.Context, ref string) (*ImageSummary, bool, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, false, fmt.Errorf("docker unavailable: %w", err)
	}

	// Try direct lookup by reference first (fast path)
	summary, err := c.InspectImage(ctx, ref)
	if err == nil {
		return summary, true, nil
	}

	// If direct lookup failed, try the normalized form
	parsed := ParseImageRef(ref)
	normalized := parsed.Normalized()
	if normalized != ref {
		summary, err := c.InspectImage(ctx, normalized)
		if err == nil {
			return summary, true, nil
		}
	}

	return nil, false, nil
}

// ---------------------------------------------------------------------------
// Smart pull
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Remote digest checking
// ---------------------------------------------------------------------------

// CheckRemoteDigest queries the registry for the current manifest digest of
// the given image reference without pulling layers. Returns the digest string
// (e.g. "sha256:abc...") or an error if the image doesn't exist on the registry.
//
// For private registries, this may fail due to missing auth credentials.
// The caller should fall back to a full pull in that case.
func (c *Client) CheckRemoteDigest(ctx context.Context, ref string) (string, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return "", fmt.Errorf("docker unavailable: %w", err)
	}

	distInfo, err := c.cliUnsafe().DistributionInspect(ctx, ref, "")
	if err != nil {
		slog.Debug("distribution inspect failed (may need registry auth), falling back to pull",
			"image", ref, "error", err)
		return "", fmt.Errorf("check remote digest for %s: %w", ref, err)
	}

	return string(distInfo.Descriptor.Digest), nil
}

// GetLocalDigest returns the digest for an image from its RepoDigests.
// Returns empty string if the image doesn't have a digest or doesn't exist.
func (c *Client) GetLocalDigest(ctx context.Context, ref string) (string, error) {
	summary, err := c.InspectImage(ctx, ref)
	if err != nil {
		return "", err
	}
	return summary.Digest(), nil
}

// ---------------------------------------------------------------------------
// Smart pull with cache
// ---------------------------------------------------------------------------

// PullImageIfNeeded pulls an image using the following strategy:
//
//  1. For "latest" tags:
//     a. Check the digest cache for the last known remote digest
//     b. Query the registry for the current digest (without pulling)
//     c. If digests match AND image still exists locally → skip pull
//     d. If digests differ or image missing → pull
//
//  2. For specific version tags (e.g., "1.25", "v1.2.3"):
//     a. Check if image exists locally → skip pull
//     b. Otherwise → pull
//
//  3. For images from custom registries:
//     Always pull (user may have pushed a new version with the same tag)
//
// After a successful pull, the cache is updated with the new digest and metadata.
// Returns (summary, wasPulled, error).
func (c *Client) PullImageIfNeeded(ctx context.Context, ref string, cache *ImageCache, progressFn PullProgressFunc) (*ImageSummary, bool, error) {
	parsed := ParseImageRef(ref)
	normalized := parsed.Normalized()

	// Strategy for "latest" tags: use digest comparison
	if parsed.IsLatest() {
		return c.pullLatestWithDigestCheck(ctx, ref, normalized, cache, progressFn)
	}

	// Strategy for custom registries: always pull (user images)
	if parsed.IsRegistrySpecific() {
		slog.Debug("custom registry detected, always pulling", "image", ref)
		return c.pullAndCache(ctx, ref, cache, progressFn)
	}

	// Strategy for specific version tags: check local, skip if present
	summary, exists, err := c.ImageExistsLocally(ctx, ref)
	if err != nil {
		return nil, false, fmt.Errorf("check local image cache: %w", err)
	}
	if exists {
		slog.Info("image already exists locally, skipping pull",
			"image", ref,
			"size", summary.HumanSize(),
		)
		// Update last-used timestamp in cache
		if cache != nil {
			cache.UpdateLastUsed(normalized)
		}
		return summary, false, nil
	}

	return c.pullAndCache(ctx, ref, cache, progressFn)
}

// pullLatestWithDigestCheck handles the "latest" tag strategy with digest comparison.
func (c *Client) pullLatestWithDigestCheck(ctx context.Context, ref, normalized string, cache *ImageCache, progressFn PullProgressFunc) (*ImageSummary, bool, error) {
	if cache == nil {
		// No cache available, just pull
		return c.pullAndCache(ctx, ref, nil, progressFn)
	}

	// Check cache for last known digest
	cached := cache.Get(normalized)

	// Query remote digest
	remoteDigest, err := c.CheckRemoteDigest(ctx, ref)
	if err != nil {
		// If we can't check the remote digest, fall back to pulling
		slog.Warn("failed to check remote digest, pulling anyway", "image", ref, "error", err)
		return c.pullAndCache(ctx, ref, cache, progressFn)
	}

	// Compare with cached digest
	if cached != nil && cached.Digest == remoteDigest {
		// Digests match — check if the image still exists locally
		summary, exists, err := c.ImageExistsLocally(ctx, ref)
		if err == nil && exists {
			slog.Info("image digest unchanged, skipping pull",
				"image", ref,
				"digest", remoteDigest[:19],
			)
			cache.UpdateLastUsed(normalized)
			return summary, false, nil
		}
	}

	slog.Info("image digest changed or not cached, pulling",
		"image", ref,
		"cached_digest", digestOrNone(cached),
		"remote_digest", shortDigest(remoteDigest),
	)

	return c.pullAndCache(ctx, ref, cache, progressFn)
}

// pullAndCache pulls the image and records metadata in the cache.
func (c *Client) pullAndCache(ctx context.Context, ref string, cache *ImageCache, progressFn PullProgressFunc) (*ImageSummary, bool, error) {
	summary, err := c.PullImage(ctx, ref, progressFn)
	if err != nil {
		return nil, false, err
	}

	// Record in cache
	if cache != nil {
		entry := &CacheEntry{
			Ref:        ref,
			Tag:        ParseImageRef(ref).Tag,
			Digest:     summary.Digest(),
			ImageID:    summary.ID,
			SizeBytes:  summary.SizeBytes,
			PulledAt:   time.Now().UTC(),
			LastUsedAt: time.Now().UTC(),
		}
		cache.Set(entry)
		if err := cache.Save(); err != nil {
			slog.Warn("failed to save image cache", "error", err)
		}
	}

	return summary, true, nil
}

// ---------------------------------------------------------------------------
// Digest helpers
// ---------------------------------------------------------------------------

func digestOrNone(entry *CacheEntry) string {
	if entry == nil || entry.Digest == "" {
		return "<none>"
	}
	return shortDigest(entry.Digest)
}

func shortDigest(digest string) string {
	if len(digest) > 19 {
		return digest[:19]
	}
	return digest
}

// ---------------------------------------------------------------------------
// Pull errors
// ---------------------------------------------------------------------------

var (
	ErrImageNotFound      = errors.New("image not found on registry")
	ErrRegistryAuth       = errors.New("registry authentication required")
	ErrNetwork            = errors.New("network error during pull")
	ErrDiskFull           = errors.New("not enough disk space")
	ErrPullTimeout        = errors.New("image pull timed out")
	ErrUnknownPullFailure = errors.New("image pull failed")
)

// PullError wraps a pull failure with a classified error and a user-facing message.
type PullError struct {
	Err     error  // Classified sentinel error (ErrImageNotFound, etc.)
	Message string // User-facing description
	Cause   error  // Original error from Docker (for debugging)
}

func (e *PullError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s (underlying: %v)", e.Message, e.Cause)
	}
	return e.Message
}

func (e *PullError) Unwrap() error {
	return e.Err
}

// classifyPullError takes a raw Docker pull error and returns a structured PullError
// with a user-facing message suitable for showing in the deployment log.
func classifyPullError(ref string, originalErr error) *PullError {
	if originalErr == nil {
		return nil
	}

	errStr := originalErr.Error()

	// Image not found
	if strings.Contains(errStr, "manifest for") && strings.Contains(errStr, "not found") {
		return &PullError{
			Err:   ErrImageNotFound,
			Cause: originalErr,
			Message: fmt.Sprintf(
				"Image '%s' was not found on the registry.\n"+
					"Check the image name and tag in your deployment settings.", ref),
		}
	}
	if strings.Contains(errStr, "repository does not exist") || strings.Contains(errStr, "not found") {
		return &PullError{
			Err:   ErrImageNotFound,
			Cause: originalErr,
			Message: fmt.Sprintf(
				"Image '%s' was not found on the registry.\n"+
					"Check the image name and tag in your deployment settings.", ref),
		}
	}

	// Registry authentication
	if strings.Contains(errStr, "authentication required") ||
		strings.Contains(errStr, "unauthorized") ||
		strings.Contains(errStr, "denied") ||
		strings.Contains(errStr, "no basic auth credentials") {
		return &PullError{
			Err:   ErrRegistryAuth,
			Cause: originalErr,
			Message: fmt.Sprintf(
				"Image '%s' requires registry authentication.\n"+
					"Add your registry credentials in the deployment settings.", ref),
		}
	}

	// Disk full
	if strings.Contains(errStr, "no space left on device") ||
		strings.Contains(errStr, "insufficient storage") ||
		strings.Contains(errStr, "disk full") {
		return &PullError{
			Err:   ErrDiskFull,
			Cause: originalErr,
			Message: fmt.Sprintf(
				"Not enough disk space to download '%s'.\n"+
					"Free up space or expand your disk to continue.", ref),
		}
	}

	// Network errors
	if isConnectionError(originalErr) ||
		strings.Contains(errStr, "i/o timeout") ||
		strings.Contains(errStr, "TLS handshake") ||
		strings.Contains(errStr, "net/http: request canceled") {
		return &PullError{
			Err:   ErrNetwork,
			Cause: originalErr,
			Message: fmt.Sprintf(
				"Download interrupted while pulling '%s'.\n"+
					"This is usually a temporary network issue. Retrying...", ref),
		}
	}

	// Timeout
	if strings.Contains(errStr, "deadline exceeded") ||
		strings.Contains(errStr, "context deadline exceeded") ||
		strings.Contains(errStr, "Client.Timeout") {
		return &PullError{
			Err:   ErrPullTimeout,
			Cause: originalErr,
			Message: fmt.Sprintf(
				"Pulling '%s' is taking longer than expected.\n"+
					"Large images on slow connections can take several minutes. Still trying...", ref),
		}
	}

	return &PullError{
		Err:   ErrUnknownPullFailure,
		Cause: originalErr,
		Message: fmt.Sprintf(
			"Failed to pull image '%s'.\nError: %s", ref, errStr),
	}
}

// ---------------------------------------------------------------------------
// Image inspection
// ---------------------------------------------------------------------------

// InspectImage retrieves detailed information about a locally available image.
func (c *Client) InspectImage(ctx context.Context, ref string) (*ImageSummary, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, fmt.Errorf("docker unavailable: %w", err)
	}

	info, _, err := c.cliUnsafe().ImageInspectWithRaw(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("inspect image %s: %w", ref, err)
	}

	created, _ := time.Parse(time.RFC3339Nano, info.Created)

	repoTag := ref
	if len(info.RepoTags) > 0 {
		repoTag = info.RepoTags[0]
	}

	return &ImageSummary{
		ID:          info.ID,
		RepoTag:     repoTag,
		RepoDigests: info.RepoDigests,
		SizeBytes:   info.Size,
		Created:     created,
		OS:          info.Os,
		Arch:        info.Architecture,
	}, nil
}

// VerifyImage checks that an image was pulled successfully by inspecting it.
// Returns a summary of the image including its ID, size, and creation date.
func (c *Client) VerifyImage(ctx context.Context, ref string) (*ImageSummary, error) {
	summary, err := c.InspectImage(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("verification failed for image '%s': %w", ref, err)
	}

	slog.Info("image verified",
		"image", ref,
		"id", summary.ID[:19], // short ID like "sha256:abc123..."
		"size", summary.HumanSize(),
	)

	return summary, nil
}

// ---------------------------------------------------------------------------
// Image listing and cleanup
// ---------------------------------------------------------------------------

// ListLocalImages returns a list of all images available locally.
func (c *Client) ListLocalImages(ctx context.Context) ([]ImageSummary, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, fmt.Errorf("docker unavailable: %w", err)
	}

	images, err := c.cliUnsafe().ImageList(ctx, types.ImageListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list images: %w", err)
	}

	summaries := make([]ImageSummary, 0, len(images))
	for _, img := range images {
		repoTag := "<none>:<none>"
		if len(img.RepoTags) > 0 {
			repoTag = img.RepoTags[0]
		}
		summaries = append(summaries, ImageSummary{
			ID:        img.ID,
			RepoTag:   repoTag,
			SizeBytes: img.Size,
			Created:   time.Unix(img.Created, 0),
		})
	}

	return summaries, nil
}

// RemoveImage removes an image from the local store.
func (c *Client) RemoveImage(ctx context.Context, ref string) error {
	if err := c.ensureConnected(ctx); err != nil {
		return fmt.Errorf("docker unavailable: %w", err)
	}

	_, err := c.cliUnsafe().ImageRemove(ctx, ref, types.ImageRemoveOptions{
		Force:         false,
		PruneChildren: true,
	})
	if err != nil {
		return fmt.Errorf("remove image %s: %w", ref, err)
	}

	slog.Info("removed image", "image", ref)
	return nil
}

// PruneUnusedImages removes all dangling (untagged) images.
func (c *Client) PruneUnusedImages(ctx context.Context) error {
	if err := c.ensureConnected(ctx); err != nil {
		return fmt.Errorf("docker unavailable: %w", err)
	}

	report, err := c.cliUnsafe().ImagesPrune(ctx, types.ImagesPruneConfig{})
	if err != nil {
		return fmt.Errorf("prune images: %w", err)
	}

	if len(report.ImagesDeleted) > 0 {
		slog.Info("pruned unused images", "count", len(report.ImagesDeleted), "reclaimed_bytes", report.SpaceReclaimed)
	} else {
		slog.Debug("no unused images to prune")
	}

	return nil
}
