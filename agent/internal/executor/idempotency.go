package executor

import (
	"sync"
	"time"
)

const idempotencyTTL = 48 * time.Hour

// IdempotencyCache stores command results to prevent re-execution.
type IdempotencyCache struct {
	mu    sync.Mutex
	items map[string]idempotencyEntry
}

type idempotencyEntry struct {
	result    Result
	expiresAt time.Time
}

// NewIdempotencyCache creates an empty cache.
func NewIdempotencyCache() *IdempotencyCache {
	return &IdempotencyCache{items: make(map[string]idempotencyEntry)}
}

// Get returns a cached result if present and not expired.
func (c *IdempotencyCache) Get(commandID string) (Result, bool) {
	if commandID == "" {
		return Result{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.purgeExpiredLocked()
	e, ok := c.items[commandID]
	if !ok {
		return Result{}, false
	}
	return e.result, true
}

// Put stores a result for 48 hours.
func (c *IdempotencyCache) Put(commandID string, result Result) {
	if commandID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[commandID] = idempotencyEntry{
		result:    result,
		expiresAt: time.Now().Add(idempotencyTTL),
	}
}

func (c *IdempotencyCache) purgeExpiredLocked() {
	now := time.Now()
	for id, e := range c.items {
		if now.After(e.expiresAt) {
			delete(c.items, id)
		}
	}
}
