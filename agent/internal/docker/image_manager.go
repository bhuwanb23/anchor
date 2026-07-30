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
	ID        string    `json:"id"`
	RepoTag   string    `json:"repo_tag"`
	SizeBytes int64     `json:"size_bytes"`
	Created   time.Time `json:"created"`
	OS        string    `json:"os"`
	Arch      string    `json:"arch"`
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
func (c *Client) ImageExistsLocally(ctx context.Context, ref string) (*ImageSummary, bool, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, false, fmt.Errorf("docker unavailable: %w", err)
	}

	parsed := ParseImageRef(ref)
	normalized := parsed.Normalized()

	images, err := c.cliUnsafe().ImageList(ctx, types.ImageListOptions{})
	if err != nil {
		return nil, false, fmt.Errorf("list images: %w", err)
	}

	for _, img := range images {
		for _, tag := range img.RepoTags {
			if tag == normalized || tag == ref {
				return &ImageSummary{
					ID:        img.ID,
					RepoTag:   tag,
					SizeBytes: img.Size,
					Created:   time.Unix(img.Created, 0),
				}, true, nil
			}
		}
	}

	return nil, false, nil
}

// ---------------------------------------------------------------------------
// Smart pull
// ---------------------------------------------------------------------------

// PullImageIfNeeded pulls an image only if it is not already cached locally.
// For "latest" tags, it always pulls (the tag may have been updated upstream).
// For specific version tags (e.g., "1.25", "v1.2.3"), it skips the pull if
// the image is already present locally.
func (c *Client) PullImageIfNeeded(ctx context.Context, ref string, progressFn PullProgressFunc) (*ImageSummary, bool, error) {
	parsed := ParseImageRef(ref)

	// Check local cache for specific versions
	if !parsed.IsLatest() {
		summary, exists, err := c.ImageExistsLocally(ctx, ref)
		if err != nil {
			return nil, false, fmt.Errorf("check local image cache: %w", err)
		}
		if exists {
			slog.Info("image already exists locally, skipping pull",
				"image", ref,
				"size", summary.HumanSize(),
			)
			return summary, false, nil
		}
	}

	// Pull the image
	summary, err := c.PullImage(ctx, ref, progressFn)
	if err != nil {
		return nil, false, err
	}

	return summary, true, nil
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
	Err     error
	Message string // User-facing description
	Cause   error  // Original error
}

func (e *PullError) Error() string {
	return e.Message
}

func (e *PullError) Unwrap() error {
	return e.Cause
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
		ID:        info.ID,
		RepoTag:   repoTag,
		SizeBytes: info.Size,
		Created:   created,
		OS:        info.Os,
		Arch:      info.Architecture,
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
