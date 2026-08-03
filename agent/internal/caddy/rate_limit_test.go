package caddy

import (
	"testing"
	"time"
)

func TestRateLimitTracker_MarkAndCheck(t *testing.T) {
	dir := t.TempDir()
	tracker := NewRateLimitTracker(dir)

	if tracker.IsRateLimited("example.com") {
		t.Error("domain should not be rate-limited before marking")
	}

	tracker.MarkRateLimited("example.com")

	if !tracker.IsRateLimited("example.com") {
		t.Error("domain should be rate-limited after marking")
	}
}

func TestRateLimitTracker_ClearRateLimit(t *testing.T) {
	dir := t.TempDir()
	tracker := NewRateLimitTracker(dir)

	tracker.MarkRateLimited("example.com")
	tracker.ClearRateLimit("example.com")

	if tracker.IsRateLimited("example.com") {
		t.Error("domain should not be rate-limited after clearing")
	}
}

func TestRateLimitTracker_Persistence(t *testing.T) {
	dir := t.TempDir()

	// Mark a domain as rate-limited
	tracker1 := NewRateLimitTracker(dir)
	tracker1.MarkRateLimited("persist.com")

	// Create new tracker from same directory
	tracker2 := NewRateLimitTracker(dir)

	if !tracker2.IsRateLimited("persist.com") {
		t.Error("rate limit should persist across tracker instances")
	}
}

func TestRateLimitTracker_GetRetryAfter(t *testing.T) {
	dir := t.TempDir()
	tracker := NewRateLimitTracker(dir)

	retryAfter := tracker.GetRetryAfter("unknown.com")
	if !retryAfter.IsZero() {
		t.Error("expected zero time for unknown domain")
	}

	tracker.MarkRateLimited("test.com")
	retryAfter = tracker.GetRetryAfter("test.com")
	if retryAfter.IsZero() {
		t.Error("expected non-zero retry time for rate-limited domain")
	}
	if retryAfter.Before(time.Now()) {
		t.Error("retry time should be in the future")
	}
}

func TestRateLimitTracker_GetAll(t *testing.T) {
	dir := t.TempDir()
	tracker := NewRateLimitTracker(dir)

	tracker.MarkRateLimited("a.com")
	tracker.MarkRateLimited("b.com")

	all := tracker.GetAll()
	if len(all) != 2 {
		t.Errorf("expected 2 entries, got %d", len(all))
	}
	if _, ok := all["a.com"]; !ok {
		t.Error("expected a.com in GetAll")
	}
	if _, ok := all["b.com"]; !ok {
		t.Error("expected b.com in GetAll")
	}
}

func TestRateLimitTracker_UpdateExisting(t *testing.T) {
	dir := t.TempDir()
	tracker := NewRateLimitTracker(dir)

	tracker.MarkRateLimited("update.com")
	firstHit := tracker.GetRetryAfter("update.com")

	// Mark again — should update LastHit but keep same cooldown
	time.Sleep(10 * time.Millisecond)
	tracker.MarkRateLimited("update.com")
	secondHit := tracker.GetRetryAfter("update.com")

	// RetryAfter should be the same (based on first hit)
	if firstHit != secondHit {
		t.Errorf("retry_after should be same, got %v and %v", firstHit, secondHit)
	}
}

func TestRateLimitTracker_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	tracker := NewRateLimitTracker(dir)

	if tracker.IsRateLimited("anything.com") {
		t.Error("empty tracker should not rate-limit anything")
	}

	all := tracker.GetAll()
	if len(all) != 0 {
		t.Errorf("expected 0 entries in empty tracker, got %d", len(all))
	}
}
