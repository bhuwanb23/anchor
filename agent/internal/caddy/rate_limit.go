package caddy

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const rateLimitFileName = "rate_limits.json"

// RateLimitEntry records when a domain was first rate-limited.
type RateLimitEntry struct {
	Domain    string    `json:"domain"`
	FirstHit  time.Time `json:"first_hit"`
	LastHit   time.Time `json:"last_hit"`
	RetryAfter time.Time `json:"retry_after"`
}

// RateLimitTracker prevents retries for rate-limited domains.
type RateLimitTracker struct {
	dataDir  string
	cooldown time.Duration
	mu       sync.Mutex
	limits   map[string]*RateLimitEntry
}

// NewRateLimitTracker creates a new rate limit tracker.
func NewRateLimitTracker(dataDir string) *RateLimitTracker {
	t := &RateLimitTracker{
		dataDir:  dataDir,
		cooldown: rateLimitCooldown,
		limits:   make(map[string]*RateLimitEntry),
	}
	t.load()
	return t
}

// IsRateLimited returns true if the domain is still within the cooldown period.
func (t *RateLimitTracker) IsRateLimited(domain string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	entry, ok := t.limits[domain]
	if !ok {
		return false
	}

	return time.Now().Before(entry.RetryAfter)
}

// MarkRateLimited records a rate limit hit for a domain.
func (t *RateLimitTracker) MarkRateLimited(domain string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	if existing, ok := t.limits[domain]; ok {
		existing.LastHit = now
	} else {
		t.limits[domain] = &RateLimitEntry{
			Domain:     domain,
			FirstHit:   now,
			LastHit:    now,
			RetryAfter: now.Add(t.cooldown),
		}
	}

	t.save()
	slog.Info("domain marked as rate-limited", "domain", domain, "retry_after", t.limits[domain].RetryAfter)
}

// ClearRateLimit removes the rate limit for a domain (e.g., after successful issuance).
func (t *RateLimitTracker) ClearRateLimit(domain string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	delete(t.limits, domain)
	t.save()
}

// GetRetryAfter returns when the domain can be retried, or zero time if not limited.
func (t *RateLimitTracker) GetRetryAfter(domain string) time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()

	entry, ok := t.limits[domain]
	if !ok {
		return time.Time{}
	}
	return entry.RetryAfter
}

// GetAll returns all rate-limited domains (for monitoring).
func (t *RateLimitTracker) GetAll() map[string]RateLimitEntry {
	t.mu.Lock()
	defer t.mu.Unlock()

	out := make(map[string]RateLimitEntry, len(t.limits))
	for k, v := range t.limits {
		out[k] = *v
	}
	return out
}

func (t *RateLimitTracker) filePath() string {
	return filepath.Join(t.dataDir, rateLimitFileName)
}

func (t *RateLimitTracker) load() {
	data, err := os.ReadFile(t.filePath())
	if err != nil {
		return // file doesn't exist yet, that's fine
	}

	var entries []RateLimitEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		slog.Warn("failed to load rate limit data", "error", err)
		return
	}

	for i := range entries {
		t.limits[entries[i].Domain] = &entries[i]
	}
}

func (t *RateLimitTracker) save() {
	entries := make([]RateLimitEntry, 0, len(t.limits))
	for _, v := range t.limits {
		entries = append(entries, *v)
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		slog.Warn("failed to marshal rate limit data", "error", err)
		return
	}

	if err := os.MkdirAll(t.dataDir, 0700); err != nil {
		slog.Warn("failed to create rate limit data directory", "error", err)
		return
	}

	if err := os.WriteFile(t.filePath(), data, 0600); err != nil {
		slog.Warn("failed to write rate limit data", "error", err)
	}
}
