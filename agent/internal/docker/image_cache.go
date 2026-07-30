package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Cache entry
// ---------------------------------------------------------------------------

// CacheEntry holds metadata about a pulled image for smart caching decisions.
type CacheEntry struct {
	Ref        string    `json:"ref"`         // e.g. "nginx:latest"
	Tag        string    `json:"tag"`         // e.g. "latest", "1.25"
	Digest     string    `json:"digest"`      // remote manifest digest, e.g. "sha256:abc..."
	ImageID    string    `json:"image_id"`    // Docker content hash
	SizeBytes  int64     `json:"size_bytes"`  // image size on disk
	PulledAt   time.Time `json:"pulled_at"`   // when this image was pulled
	LastUsedAt time.Time `json:"last_used_at"`// when this image was last referenced
}

// IsStale returns true if the entry's LastUsedAt is older than maxAge.
func (e *CacheEntry) IsStale(maxAge time.Duration) bool {
	return time.Since(e.LastUsedAt) > maxAge
}

// ---------------------------------------------------------------------------
// Image cache
// ---------------------------------------------------------------------------

// ImageCache tracks metadata about pulled images to avoid unnecessary pulls.
// It is persisted as JSON to a file and is thread-safe.
type ImageCache struct {
	path    string
	entries map[string]*CacheEntry // keyed by image reference (normalized)
	mu      sync.RWMutex
	dirty   bool // tracks unsaved changes
}

// NewImageCache loads an existing cache from path, or creates an empty one.
// The cache directory is created if it does not exist.
func NewImageCache(path string) (*ImageCache, error) {
	ic := &ImageCache{
		path:    path,
		entries: make(map[string]*CacheEntry),
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create image cache directory %s: %w", dir, err)
	}

	if err := ic.load(); err != nil {
		// If the file doesn't exist yet, start with an empty cache
		if !os.IsNotExist(err) {
			// File exists but is corrupted — rename it so we don't lose data
			backup := path + ".bak"
			slog.Warn("image cache file is corrupted, backing up", "path", path, "backup", backup, "error", err)
			os.Rename(path, backup)
		}
	}

	return ic, nil
}

// ---------------------------------------------------------------------------
// Persistence
// ---------------------------------------------------------------------------

type cacheFile struct {
	Version int                    `json:"version"`
	Entries map[string]*CacheEntry `json:"entries"`
}

const cacheVersion = 1

func (ic *ImageCache) load() error {
	data, err := os.ReadFile(ic.path)
	if err != nil {
		return err
	}

	var cf cacheFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return fmt.Errorf("parse image cache: %w", err)
	}

	if cf.Version != cacheVersion {
		slog.Warn("image cache version mismatch, resetting", "expected", cacheVersion, "got", cf.Version)
		return nil
	}

	ic.mu.Lock()
	ic.entries = cf.Entries
	if ic.entries == nil {
		ic.entries = make(map[string]*CacheEntry)
	}
	ic.mu.Unlock()

	slog.Debug("loaded image cache", "path", ic.path, "entries", len(ic.entries))
	return nil
}

// Save persists the cache to disk if there are unsaved changes.
func (ic *ImageCache) Save() error {
	ic.mu.RLock()
	dirty := ic.dirty
	ic.mu.RUnlock()

	if !dirty {
		return nil
	}

	ic.mu.Lock()
	defer ic.mu.Unlock()

	cf := cacheFile{
		Version: cacheVersion,
		Entries: ic.entries,
	}

	data, err := json.MarshalIndent(cf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal image cache: %w", err)
	}

	// Write atomically via temp file + rename
	tmpPath := ic.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write image cache tmp: %w", err)
	}
	if err := os.Rename(tmpPath, ic.path); err != nil {
		return fmt.Errorf("rename image cache: %w", err)
	}

	ic.dirty = false
	return nil
}

// ---------------------------------------------------------------------------
// Accessors
// ---------------------------------------------------------------------------

// Get returns the cache entry for the given image reference, or nil.
func (ic *ImageCache) Get(ref string) *CacheEntry {
	ic.mu.RLock()
	defer ic.mu.RUnlock()
	return ic.entries[normalizeCacheKey(ref)]
}

// Set stores or updates an entry in the cache.
func (ic *ImageCache) Set(entry *CacheEntry) {
	ic.mu.Lock()
	defer ic.mu.Unlock()
	ic.entries[normalizeCacheKey(entry.Ref)] = entry
	ic.dirty = true
}

// UpdateLastUsed refreshes the LastUsedAt timestamp for an image.
// Returns true if the entry existed.
func (ic *ImageCache) UpdateLastUsed(ref string) bool {
	ic.mu.Lock()
	defer ic.mu.Unlock()

	key := normalizeCacheKey(ref)
	entry, ok := ic.entries[key]
	if !ok {
		return false
	}
	entry.LastUsedAt = time.Now().UTC()
	ic.dirty = true
	return true
}

// Remove deletes an entry from the cache.
func (ic *ImageCache) Remove(ref string) {
	ic.mu.Lock()
	defer ic.mu.Unlock()
	delete(ic.entries, normalizeCacheKey(ref))
	ic.dirty = true
}

// Len returns the number of entries in the cache.
func (ic *ImageCache) Len() int {
	ic.mu.RLock()
	defer ic.mu.RUnlock()
	return len(ic.entries)
}

// Entries returns all cache entries (for inspection/display).
func (ic *ImageCache) Entries() []CacheEntry {
	ic.mu.RLock()
	defer ic.mu.RUnlock()

	result := make([]CacheEntry, 0, len(ic.entries))
	for _, e := range ic.entries {
		result = append(result, *e)
	}
	return result
}

// ---------------------------------------------------------------------------
// Stale cleanup
// ---------------------------------------------------------------------------

// StaleEntries returns entries that haven't been used in longer than maxAge.
func (ic *ImageCache) StaleEntries(maxAge time.Duration) []CacheEntry {
	ic.mu.RLock()
	defer ic.mu.RUnlock()

	var stale []CacheEntry
	for _, e := range ic.entries {
		if e.IsStale(maxAge) {
			stale = append(stale, *e)
		}
	}
	return stale
}

// ClearStale removes entries older than maxAge from both the cache and
// Docker. It returns the number of images cleaned up.
func (ic *ImageCache) ClearStale(ctx context.Context, dockerClient *Client, maxAge time.Duration) (int, error) {
	stale := ic.StaleEntries(maxAge)
	if len(stale) == 0 {
		return 0, nil
	}

	removed := 0
	for _, entry := range stale {
		slog.Info("removing stale cached image", "image", entry.Ref, "last_used", entry.LastUsedAt)

		// Remove from Docker
		if err := dockerClient.RemoveImage(ctx, entry.Ref); err != nil {
			slog.Warn("failed to remove stale image from Docker", "image", entry.Ref, "error", err)
			// Don't fail the whole batch — just skip this one
			continue
		}

		// Remove from cache
		ic.Remove(entry.Ref)
		removed++
	}

	if removed > 0 {
		slog.Info("cleaned up stale cached images", "count", removed)
	}

	return removed, nil
}

// ---------------------------------------------------------------------------
// Key normalization
// ---------------------------------------------------------------------------

// normalizeCacheKey normalizes an image reference for use as a cache key.
// This ensures that "nginx" and "nginx:latest" map to the same entry.
func normalizeCacheKey(ref string) string {
	parsed := ParseImageRef(ref)
	return parsed.Normalized()
}
