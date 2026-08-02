package caddy

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	eventsFileName     = "caddy_events.json"
	maxStoredEvents    = 1000
	eventRollbackBatch = 100
)

// ServerEvent represents a significant Caddy event.
type ServerEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`     // "cert_issued", "cert_renewed", "cert_failed", "cert_renewal_failed", "route_added", "route_removed"
	Domain    string    `json:"domain"`
	Message   string    `json:"message"`
}

// EventRecorder records Caddy events to disk.
type EventRecorder struct {
	dataDir string
	mu      sync.Mutex
	events  []ServerEvent
}

// NewEventRecorder creates a new event recorder.
func NewEventRecorder(dataDir string) *EventRecorder {
	r := &EventRecorder{
		dataDir: dataDir,
		events:  make([]ServerEvent, 0),
	}
	r.load()
	return r
}

// Record appends a server event and persists to disk.
func (r *EventRecorder) Record(event ServerEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	r.events = append(r.events, event)

	// Rollback if over max
	if len(r.events) > maxStoredEvents {
		r.events = r.events[eventRollbackBatch:]
	}

	r.save()
	slog.Info("server event recorded",
		"type", event.Type,
		"domain", event.Domain,
		"message", event.Message)
}

// GetRecent returns the last n events.
func (r *EventRecorder) GetRecent(n int) []ServerEvent {
	r.mu.Lock()
	defer r.mu.Unlock()

	total := len(r.events)
	if n > total {
		n = total
	}

	out := make([]ServerEvent, n)
	copy(out, r.events[total-n:])
	return out
}

// GetAll returns all events.
func (r *EventRecorder) GetAll() []ServerEvent {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]ServerEvent, len(r.events))
	copy(out, r.events)
	return out
}

// Count returns the number of stored events.
func (r *EventRecorder) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

func (r *EventRecorder) filePath() string {
	return filepath.Join(r.dataDir, eventsFileName)
}

func (r *EventRecorder) load() {
	data, err := os.ReadFile(r.filePath())
	if err != nil {
		return
	}

	var events []ServerEvent
	if err := json.Unmarshal(data, &events); err != nil {
		slog.Warn("failed to load server events", "error", err)
		return
	}

	r.events = events
}

func (r *EventRecorder) save() {
	data, err := json.MarshalIndent(r.events, "", "  ")
	if err != nil {
		slog.Warn("failed to marshal server events", "error", err)
		return
	}

	if err := os.MkdirAll(r.dataDir, 0700); err != nil {
		slog.Warn("failed to create events directory", "error", err)
		return
	}

	if err := os.WriteFile(r.filePath(), data, 0600); err != nil {
		slog.Warn("failed to write server events", "error", err)
	}
}
