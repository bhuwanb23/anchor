// Package ratelimit provides a small in-memory sliding-window rate limiter
// for the control plane (Layer 5A Step 8A).
//
// Counters live in memory and are lost on restart, which is acceptable for
// the MVP single-instance control plane — the plan explicitly accepts this
// and defers a persistent/shared store (e.g. Redis) until the control plane
// scales horizontally.
package ratelimit

import (
	"sync"
	"time"
)

// Store is a per-key sliding-window counter. Keys are opaque strings such as
// "ip:1.2.3.4" or "email:user@example.com". Each Allow call prunes attempts
// that have fallen out of the window, so the oldest surviving attempt
// determines when the key opens up again.
type Store struct {
	mu       sync.Mutex
	max      int
	window   time.Duration
	attempts map[string][]time.Time
}

// NewStore returns a store that permits up to max attempts per window.
func NewStore(max int, window time.Duration) *Store {
	return &Store{
		max:      max,
		window:   window,
		attempts: make(map[string][]time.Time),
	}
}

// Allow records an attempt for key and reports whether it is within the
// limit. When the limit is exceeded, retryAfter is how long until the oldest
// attempt leaves the window (0 when within the limit).
func (s *Store) Allow(key string) (ok bool, retryAfter time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-s.window)
	times := s.attempts[key]

	// Drop attempts older than the window, reusing the backing array. When
	// nothing survives, release the map entry so keys that stop being used do
	// not pin memory forever (keys are recreated on the next attempt).
	kept := times[:0]
	for _, t := range times {
		if t.After(windowStart) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(s.attempts, key)
	}
	times = append(kept, now)
	s.attempts[key] = times

	if len(times) <= s.max {
		return true, 0
	}

	// The oldest surviving attempt is the one that must age out.
	retryAfter = s.window - now.Sub(times[0])
	if retryAfter < 0 {
		retryAfter = 0
	}
	return false, retryAfter
}
