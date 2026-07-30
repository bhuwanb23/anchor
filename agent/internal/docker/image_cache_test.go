package docker

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseImageRef_Normalized(t *testing.T) {
	tests := []struct {
		ref  string
		want string
	}{
		{"nginx", "nginx:latest"},
		{"nginx:latest", "nginx:latest"},
		{"nginx:1.25", "nginx:1.25"},
		{"postgres:16", "postgres:16"},
		{"library/nginx", "library/nginx:latest"},
		{"ghcr.io/owner/app:v1", "ghcr.io/owner/app:v1"},
	}
	for _, tt := range tests {
		r := ParseImageRef(tt.ref)
		got := r.Normalized()
		if got != tt.want {
			t.Errorf("ParseImageRef(%q).Normalized() = %q, want %q", tt.ref, got, tt.want)
		}
	}
}

func TestNormalizeCacheKey(t *testing.T) {
	tests := []struct {
		ref  string
		want string
	}{
		{"nginx", "nginx:latest"},
		{"nginx:latest", "nginx:latest"},
		{"nginx:1.25", "nginx:1.25"},
	}
	for _, tt := range tests {
		got := normalizeCacheKey(tt.ref)
		if got != tt.want {
			t.Errorf("normalizeCacheKey(%q) = %q, want %q", tt.ref, got, tt.want)
		}
	}
}

func TestImageCacheNew(t *testing.T) {
	dir, err := os.MkdirTemp("", "image-cache-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, "cache.json")
	cache, err := NewImageCache(path)
	if err != nil {
		t.Fatalf("NewImageCache: %v", err)
	}

	if cache.Len() != 0 {
		t.Errorf("expected empty cache, got %d entries", cache.Len())
	}
}

func TestImageCacheSetAndGet(t *testing.T) {
	dir := t.TempDir()
	cache, _ := NewImageCache(filepath.Join(dir, "cache.json"))

	entry := &CacheEntry{
		Ref:     "nginx:latest",
		Tag:     "latest",
		Digest:  "sha256:abcdef1234567890",
		ImageID: "sha256:deadbeef",
	}
	cache.Set(entry)

	got := cache.Get("nginx:latest")
	if got == nil {
		t.Fatal("expected entry, got nil")
	}
	if got.Digest != "sha256:abcdef1234567890" {
		t.Errorf("expected digest %q, got %q", "sha256:abcdef1234567890", got.Digest)
	}
}

func TestImageCacheUpdateLastUsed(t *testing.T) {
	dir := t.TempDir()
	cache, _ := NewImageCache(filepath.Join(dir, "cache.json"))

	entry := &CacheEntry{
		Ref:     "nginx:latest",
		PulledAt: time.Now().UTC().Add(-1 * time.Hour),
		LastUsedAt: time.Now().UTC().Add(-1 * time.Hour),
	}
	cache.Set(entry)

	// Update last used
	if !cache.UpdateLastUsed("nginx:latest") {
		t.Fatal("expected UpdateLastUsed to return true")
	}

	got := cache.Get("nginx:latest")
	if got.LastUsedAt.Before(time.Now().UTC().Add(-1 * time.Minute)) {
		t.Error("LastUsedAt should have been updated to near now")
	}

	// Update non-existent
	if cache.UpdateLastUsed("nonexistent") {
		t.Error("expected UpdateLastUsed to return false for non-existent key")
	}
}

func TestImageCacheRemove(t *testing.T) {
	dir := t.TempDir()
	cache, _ := NewImageCache(filepath.Join(dir, "cache.json"))

	cache.Set(&CacheEntry{Ref: "nginx:latest"})
	cache.Remove("nginx:latest")

	if got := cache.Get("nginx:latest"); got != nil {
		t.Error("expected nil after remove")
	}
}

func TestImageCachePersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")

	// Create and save
	cache, _ := NewImageCache(path)
	cache.Set(&CacheEntry{
		Ref:     "nginx:1.25",
		Tag:     "1.25",
		Digest:  "sha256:abc",
		ImageID: "sha256:def",
	})
	if err := cache.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Reload from same file
	cache2, err := NewImageCache(path)
	if err != nil {
		t.Fatalf("NewImageCache reload: %v", err)
	}
	if cache2.Len() != 1 {
		t.Errorf("expected 1 entry after reload, got %d", cache2.Len())
	}
	entry := cache2.Get("nginx:1.25")
	if entry == nil {
		t.Fatal("expected entry after reload")
	}
	if entry.Digest != "sha256:abc" {
		t.Errorf("expected digest %q, got %q", "sha256:abc", entry.Digest)
	}
}

func TestImageCachePersistenceNoSave(t *testing.T) {
	// If Save() is not called, changes should not persist
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")

	cache, _ := NewImageCache(path)
	cache.Set(&CacheEntry{Ref: "nginx:latest"})
	// No Save() call

	cache2, err := NewImageCache(path)
	if err != nil {
		t.Fatalf("NewImageCache reload: %v", err)
	}
	if cache2.Len() != 0 {
		t.Errorf("expected 0 entries (no save called), got %d", cache2.Len())
	}
}

func TestCacheEntryIsStale(t *testing.T) {
	now := time.Now().UTC()

	entry := &CacheEntry{
		Ref:        "old:image",
		LastUsedAt: now.Add(-48 * time.Hour),
	}
	if !entry.IsStale(24 * time.Hour) {
		t.Error("expected entry to be stale (>24h)")
	}

	entry.LastUsedAt = now.Add(-1 * time.Hour)
	if entry.IsStale(24 * time.Hour) {
		t.Error("expected entry to NOT be stale (<24h)")
	}
}

func TestImageCacheStaleEntries(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	cache, _ := NewImageCache(filepath.Join(dir, "cache.json"))

	cache.Set(&CacheEntry{Ref: "fresh:image", LastUsedAt: now.Add(-1 * time.Hour)})
	cache.Set(&CacheEntry{Ref: "stale:image", LastUsedAt: now.Add(-48 * time.Hour)})

	stale := cache.StaleEntries(24 * time.Hour)
	if len(stale) != 1 {
		t.Fatalf("expected 1 stale entry, got %d", len(stale))
	}
	if stale[0].Ref != "stale:image" {
		t.Errorf("expected stale entry 'stale:image', got %q", stale[0].Ref)
	}
}

func TestImageCacheLenAndEntries(t *testing.T) {
	dir := t.TempDir()
	cache, _ := NewImageCache(filepath.Join(dir, "cache.json"))

	if cache.Len() != 0 {
		t.Errorf("expected 0, got %d", cache.Len())
	}

	cache.Set(&CacheEntry{Ref: "a"})
	cache.Set(&CacheEntry{Ref: "b"})

	if cache.Len() != 2 {
		t.Errorf("expected 2, got %d", cache.Len())
	}

	entries := cache.Entries()
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
}

func TestNormalizeCacheKey_Empty(t *testing.T) {
	got := normalizeCacheKey("")
	if got != ":latest" {
		t.Errorf("normalizeCacheKey('') = %q, want ':latest'", got)
	}
}

// ---------------------------------------------------------------------------
// ImageSummary digest extraction
// ---------------------------------------------------------------------------

func TestImageSummary_Digest(t *testing.T) {
	s := &ImageSummary{
		RepoDigests: []string{
			"nginx@sha256:abcdef1234567890",
		},
	}
	if s.Digest() != "sha256:abcdef1234567890" {
		t.Errorf("expected digest %q, got %q", "sha256:abcdef1234567890", s.Digest())
	}
}

func TestImageSummary_Digest_Empty(t *testing.T) {
	s := &ImageSummary{}
	if s.Digest() != "" {
		t.Errorf("expected empty digest, got %q", s.Digest())
	}
}

func TestImageSummary_Digest_NoMatch(t *testing.T) {
	s := &ImageSummary{
		RepoDigests: []string{"no-at-sign-here"},
	}
	if s.Digest() != "" {
		t.Errorf("expected empty digest for malformed repo digest, got %q", s.Digest())
	}
}

// ---------------------------------------------------------------------------
// Digest helpers
// ---------------------------------------------------------------------------

func TestShortDigest(t *testing.T) {
	d := "sha256:abcdef1234567890abcdef1234567890abcdef12"
	short := shortDigest(d)
	if len(short) != 19 {
		t.Errorf("expected 19 chars, got %d: %q", len(short), short)
	}
	if short != "sha256:abcdef12345" {
		t.Errorf("expected %q, got %q", "sha256:abcdef12345", short)
	}
}

func TestShortDigest_Short(t *testing.T) {
	d := "sha256:abc"
	short := shortDigest(d)
	if short != "sha256:abc" {
		t.Errorf("expected unchanged, got %q", short)
	}
}

func TestDigestOrNone(t *testing.T) {
	if s := digestOrNone(nil); s != "<none>" {
		t.Errorf("expected '<none>', got %q", s)
	}
	if s := digestOrNone(&CacheEntry{Digest: ""}); s != "<none>" {
		t.Errorf("expected '<none>', got %q", s)
	}
	if s := digestOrNone(&CacheEntry{Digest: "sha256:abcdef1234567890"}); s != "sha256:abcdef12345" {
		t.Errorf("expected short digest, got %q", s)
	}
}

// ---------------------------------------------------------------------------
// Corrupted cache recovery
// ---------------------------------------------------------------------------

func TestImageCache_CorruptedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")

	// Write garbage to the cache file
	if err := os.WriteFile(path, []byte("{invalid json garbage"), 0644); err != nil {
		t.Fatal(err)
	}

	// NewImageCache should recover by renaming the corrupted file
	cache, err := NewImageCache(path)
	if err != nil {
		t.Fatalf("NewImageCache should recover from corruption, got: %v", err)
	}

	if cache.Len() != 0 {
		t.Errorf("expected empty cache after corruption recovery, got %d entries", cache.Len())
	}

	// Verify the corrupted file was renamed to .bak
	if _, err := os.Stat(path + ".bak"); os.IsNotExist(err) {
		t.Error("expected corrupted file to be renamed to .bak")
	}
}

// ---------------------------------------------------------------------------
// ClearStale (structural test — no Docker)
// ---------------------------------------------------------------------------

func TestImageCache_ClearStale(t *testing.T) {
	dir := t.TempDir()
	cache, _ := NewImageCache(filepath.Join(dir, "cache.json"))

	// Add an entry with old LastUsedAt
	oldEntry := &CacheEntry{
		Ref:        "nginx:1.20",
		Tag:        "1.20",
		LastUsedAt: time.Now().Add(-60 * 24 * time.Hour), // 60 days ago
	}
	cache.Set(oldEntry)

	// Add a recent entry
	recentEntry := &CacheEntry{
		Ref:        "nginx:latest",
		Tag:        "latest",
		LastUsedAt: time.Now().Add(-1 * time.Hour), // 1 hour ago
	}
	cache.Set(recentEntry)

	// Verify both entries exist
	if cache.Len() != 2 {
		t.Fatalf("expected 2 entries, got %d", cache.Len())
	}

	// StaleEntries should find only the old one
	stale := cache.StaleEntries(30 * 24 * time.Hour)
	if len(stale) != 1 {
		t.Fatalf("expected 1 stale entry, got %d", len(stale))
	}
	if stale[0].Ref != "nginx:1.20" {
		t.Errorf("expected stale entry nginx:1.20, got %s", stale[0].Ref)
	}

	// ClearStale without Docker client should still remove from cache
	// (RemoveImage will fail but the cache entry should still be removed)
	// We can't test ClearStale fully without Docker, but we can verify
	// that StaleEntries works correctly for cleanup planning.
}

// ---------------------------------------------------------------------------
// Thread safety (concurrent access)
// ---------------------------------------------------------------------------

func TestImageCache_ConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	cache, _ := NewImageCache(filepath.Join(dir, "cache.json"))

	done := make(chan struct{})
	// Concurrent writers
	for i := 0; i < 10; i++ {
		go func(n int) {
			for j := 0; j < 100; j++ {
				entry := &CacheEntry{
					Ref:        "nginx:" + string(rune('a'+j%26)),
					Tag:        "latest",
					LastUsedAt: time.Now(),
				}
				cache.Set(entry)
			}
			done <- struct{}{}
		}(i)
	}

	// Concurrent readers
	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = cache.Get("nginx:latest")
				_ = cache.Len()
				_ = cache.Entries()
			}
			done <- struct{}{}
		}()
	}

	// Wait for all goroutines (with timeout)
	timeout := time.After(5 * time.Second)
	for i := 0; i < 15; i++ {
		select {
		case <-done:
		case <-timeout:
			t.Fatal("timeout waiting for concurrent access test")
		}
	}

	// Cache should be in a consistent state (no panic or data race)
	t.Logf("cache has %d entries after concurrent access", cache.Len())
}

// ---------------------------------------------------------------------------
// Cache key normalization edge cases
// ---------------------------------------------------------------------------

func TestNormalizeCacheKey_WithTag(t *testing.T) {
	got := normalizeCacheKey("postgres:16")
	if got != "postgres:16" {
		t.Errorf("expected 'postgres:16', got %q", got)
	}
}

func TestNormalizeCacheKey_Registry(t *testing.T) {
	got := normalizeCacheKey("ghcr.io/owner/app:v1")
	if got != "ghcr.io/owner/app:v1" {
		t.Errorf("expected 'ghcr.io/owner/app:v1', got %q", got)
	}
}
