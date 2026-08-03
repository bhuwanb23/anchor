package caddy

import (
	"testing"
)

// mockRouteManager records route set calls.
type mockRouteManager struct {
	routes map[string]mockRouteSet
}

type mockRouteSet struct {
	domains  []string
	upstream string
}

func newMockRouteManager() *mockRouteManager {
	return &mockRouteManager{routes: make(map[string]mockRouteSet)}
}

func (m *mockRouteManager) SetRouteByID(routeID string, domains []string, upstream string) error {
	m.routes[routeID] = mockRouteSet{domains: domains, upstream: upstream}
	return nil
}

func TestRouteQueue_Enqueue(t *testing.T) {
	dir := t.TempDir()
	mgr := newMockRouteManager()
	queue := NewRouteQueue(dir, mgr)

	queue.Enqueue("yourplatform-app1", []string{"app1.example.com"}, "127.0.0.1:8080")

	if queue.PendingCount() != 1 {
		t.Errorf("expected 1 pending, got %d", queue.PendingCount())
	}
}

func TestRouteQueue_ApplyPending(t *testing.T) {
	dir := t.TempDir()
	mgr := newMockRouteManager()
	queue := NewRouteQueue(dir, mgr)

	queue.Enqueue("yourplatform-app1", []string{"app1.example.com"}, "127.0.0.1:8080")
	queue.Enqueue("yourplatform-app2", []string{"app2.example.com"}, "127.0.0.1:9090")

	applied := queue.ApplyPending(nil)

	if applied != 2 {
		t.Errorf("expected 2 applied, got %d", applied)
	}
	if queue.PendingCount() != 0 {
		t.Errorf("expected 0 pending after apply, got %d", queue.PendingCount())
	}

	// Verify routes were set
	if _, ok := mgr.routes["yourplatform-app1"]; !ok {
		t.Error("expected yourplatform-app1 to be set")
	}
	if _, ok := mgr.routes["yourplatform-app2"]; !ok {
		t.Error("expected yourplatform-app2 to be set")
	}
}

func TestRouteQueue_Persistence(t *testing.T) {
	dir := t.TempDir()
	mgr := newMockRouteManager()

	// Enqueue routes
	queue1 := NewRouteQueue(dir, mgr)
	queue1.Enqueue("yourplatform-persist", []string{"p.example.com"}, "127.0.0.1:3000")

	// Create new queue from same directory
	queue2 := NewRouteQueue(dir, mgr)

	if queue2.PendingCount() != 1 {
		t.Errorf("expected 1 pending after reload, got %d", queue2.PendingCount())
	}

	// Apply should work
	applied := queue2.ApplyPending(nil)
	if applied != 1 {
		t.Errorf("expected 1 applied after reload, got %d", applied)
	}
}

func TestRouteQueue_Clear(t *testing.T) {
	dir := t.TempDir()
	mgr := newMockRouteManager()
	queue := NewRouteQueue(dir, mgr)

	queue.Enqueue("yourplatform-clear", []string{"c.example.com"}, "127.0.0.1:4000")
	queue.Clear()

	if queue.PendingCount() != 0 {
		t.Errorf("expected 0 pending after clear, got %d", queue.PendingCount())
	}
}

func TestRouteQueue_GetPending(t *testing.T) {
	dir := t.TempDir()
	mgr := newMockRouteManager()
	queue := NewRouteQueue(dir, mgr)

	queue.Enqueue("yourplatform-gp1", []string{"a.example.com"}, "127.0.0.1:1000")
	queue.Enqueue("yourplatform-gp2", []string{"b.example.com"}, "127.0.0.1:2000")

	pending := queue.GetPending()
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending, got %d", len(pending))
	}
	if pending[0].RouteID != "yourplatform-gp1" {
		t.Errorf("expected first route to be yourplatform-gp1, got %s", pending[0].RouteID)
	}
}

func TestRouteQueue_NilManager(t *testing.T) {
	dir := t.TempDir()
	queue := NewRouteQueue(dir, nil)

	queue.Enqueue("yourplatform-nil", []string{"n.example.com"}, "127.0.0.1:5000")

	applied := queue.ApplyPending(nil)
	if applied != 0 {
		t.Errorf("expected 0 applied with nil manager, got %d", applied)
	}
	// Queue should still have the route
	if queue.PendingCount() != 1 {
		t.Errorf("expected 1 pending with nil manager, got %d", queue.PendingCount())
	}
}
