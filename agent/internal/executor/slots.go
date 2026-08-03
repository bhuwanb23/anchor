package executor

import (
	"sync"
)

// SlotManager provides one execution slot per project (plus a global slot).
// Commands for the same project serialize; different projects run concurrently.
type SlotManager struct {
	mu    sync.Mutex
	slots map[string]*slot
}

type slot struct {
	busy  bool
	queue []queuedJob
}

type queuedJob struct {
	run func()
}

// NewSlotManager creates a slot manager.
func NewSlotManager() *SlotManager {
	return &SlotManager{slots: make(map[string]*slot)}
}

// Submit runs fn when the project's slot is free. Returns immediately;
// fn may run asynchronously. Same-project jobs queue FIFO.
func (m *SlotManager) Submit(projectKey string, fn func()) {
	if projectKey == "" {
		projectKey = "global"
	}
	m.mu.Lock()
	s, ok := m.slots[projectKey]
	if !ok {
		s = &slot{}
		m.slots[projectKey] = s
	}
	if s.busy {
		s.queue = append(s.queue, queuedJob{run: fn})
		m.mu.Unlock()
		return
	}
	s.busy = true
	m.mu.Unlock()
	go m.runSlot(projectKey, fn)
}

func (m *SlotManager) runSlot(projectKey string, fn func()) {
	defer m.releaseNext(projectKey)
	fn()
}

func (m *SlotManager) releaseNext(projectKey string) {
	m.mu.Lock()
	s := m.slots[projectKey]
	if s == nil {
		m.mu.Unlock()
		return
	}
	if len(s.queue) == 0 {
		s.busy = false
		m.mu.Unlock()
		return
	}
	next := s.queue[0]
	s.queue = s.queue[1:]
	// stay busy
	m.mu.Unlock()
	go m.runSlot(projectKey, next.run)
}

// IsBusy reports whether a project slot is currently executing.
func (m *SlotManager) IsBusy(projectKey string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if projectKey == "" {
		projectKey = "global"
	}
	s, ok := m.slots[projectKey]
	return ok && s.busy
}

// QueueLen returns queued jobs behind the current execution for a project.
func (m *SlotManager) QueueLen(projectKey string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if projectKey == "" {
		projectKey = "global"
	}
	s, ok := m.slots[projectKey]
	if !ok {
		return 0
	}
	return len(s.queue)
}
