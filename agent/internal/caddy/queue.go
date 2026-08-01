package caddy

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const routeQueueFileName = "route_queue.json"

// QueuedRoute represents a route addition that was deferred.
type QueuedRoute struct {
	RouteID  string    `json:"route_id"`
	Domains  []string  `json:"domains"`
	Upstream string    `json:"upstream"`
	QueuedAt time.Time `json:"queued_at"`
}

// RouteManager is the interface for applying routes.
type RouteManager interface {
	SetRouteByID(routeID string, domains []string, upstream string) error
}

// RouteQueue queues route additions when the admin API is unavailable.
type RouteQueue struct {
	dataDir string
	manager RouteManager
	mu      sync.Mutex
	pending []QueuedRoute
}

// NewRouteQueue creates a new route queue.
func NewRouteQueue(dataDir string, manager RouteManager) *RouteQueue {
	q := &RouteQueue{
		dataDir: dataDir,
		manager: manager,
	}
	q.load()
	return q
}

// Enqueue adds a route to the pending queue.
func (q *RouteQueue) Enqueue(routeID string, domains []string, upstream string) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.pending = append(q.pending, QueuedRoute{
		RouteID:  routeID,
		Domains:  domains,
		Upstream: upstream,
		QueuedAt: time.Now(),
	})

	q.save()
	slog.Info("route queued for deferred application",
		"route_id", routeID,
		"domains", domains,
		"upstream", upstream,
		"queue_size", len(q.pending))
}

// ApplyPending attempts to apply all queued routes.
// Returns the number of successfully applied routes.
func (q *RouteQueue) ApplyPending(ctx interface{}) int {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.manager == nil {
		return 0
	}

	applied := 0
	remaining := q.pending[:0]

	for _, route := range q.pending {
		if err := q.manager.SetRouteByID(route.RouteID, route.Domains, route.Upstream); err != nil {
			slog.Warn("failed to apply queued route",
				"route_id", route.RouteID,
				"error", err)
			remaining = append(remaining, route)
			continue
		}

		slog.Info("applied queued route",
			"route_id", route.RouteID,
			"domains", route.Domains)
		applied++
	}

	q.pending = remaining
	q.save()

	return applied
}

// PendingCount returns the number of queued routes.
func (q *RouteQueue) PendingCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.pending)
}

// GetPending returns a copy of all pending routes.
func (q *RouteQueue) GetPending() []QueuedRoute {
	q.mu.Lock()
	defer q.mu.Unlock()

	out := make([]QueuedRoute, len(q.pending))
	copy(out, q.pending)
	return out
}

// Clear empties the queue.
func (q *RouteQueue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.pending = nil
	q.save()
}

func (q *RouteQueue) filePath() string {
	return filepath.Join(q.dataDir, routeQueueFileName)
}

func (q *RouteQueue) load() {
	data, err := os.ReadFile(q.filePath())
	if err != nil {
		return
	}

	var routes []QueuedRoute
	if err := json.Unmarshal(data, &routes); err != nil {
		slog.Warn("failed to load route queue", "error", err)
		return
	}

	q.pending = routes
}

func (q *RouteQueue) save() {
	data, err := json.MarshalIndent(q.pending, "", "  ")
	if err != nil {
		slog.Warn("failed to marshal route queue", "error", err)
		return
	}

	if err := os.MkdirAll(q.dataDir, 0700); err != nil {
		slog.Warn("failed to create route queue directory", "error", err)
		return
	}

	if err := os.WriteFile(q.filePath(), data, 0600); err != nil {
		slog.Warn("failed to write route queue", "error", err)
	}
}
