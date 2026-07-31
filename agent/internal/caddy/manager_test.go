package caddy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// mockCaddy simulates Caddy's admin API for testing.
type mockCaddy struct {
	mu     sync.Mutex
	routes []CaddyRoute
}

func newMockCaddy() *mockCaddy {
	return &mockCaddy{}
}

func (m *mockCaddy) handler() http.Handler {
	mux := http.NewServeMux()

	// GET /config/apps/http/servers/main/routes — list routes
	mux.HandleFunc("/config/apps/http/servers/main/routes", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			m.mu.Lock()
			defer m.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(m.routes)
		}
	})

	// PUT /config/apps/http/servers/main/routes/{id} — upsert route
	mux.HandleFunc("/config/apps/http/servers/main/routes/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			var route CaddyRoute
			if err := json.Unmarshal(body, &route); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			m.mu.Lock()
			defer m.mu.Unlock()
			// Replace existing route with same ID
			found := false
			for i, existing := range m.routes {
				if existing.ID == route.ID {
					m.routes[i] = route
					found = true
					break
				}
			}
			if !found {
				m.routes = append(m.routes, route)
			}
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			// Extract route ID from path
			id := r.URL.Path[len("/config/apps/http/servers/main/routes/"):]
			m.mu.Lock()
			defer m.mu.Unlock()
			for _, route := range m.routes {
				if route.ID == id {
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(route)
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
		case http.MethodDelete:
			id := r.URL.Path[len("/config/apps/http/servers/main/routes/"):]
			m.mu.Lock()
			defer m.mu.Unlock()
			for i, route := range m.routes {
				if route.ID == id {
					m.routes = append(m.routes[:i], m.routes[i+1:]...)
					w.WriteHeader(http.StatusOK)
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
		}
	})

	// POST /config — full config replacement (legacy)
	mux.HandleFunc("/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var config map[string]interface{}
		json.Unmarshal(body, &config)
		m.mu.Lock()
		defer m.mu.Unlock()
		if apps, ok := config["apps"].(map[string]interface{}); ok {
			if httpApp, ok := apps["http"].(map[string]interface{}); ok {
				if servers, ok := httpApp["servers"].(map[string]interface{}); ok {
					if srv, ok := servers["main"].(map[string]interface{}); ok {
						if routes, ok := srv["routes"].([]interface{}); ok {
							var newRoutes []CaddyRoute
							data, _ := json.Marshal(routes)
							json.Unmarshal(data, &newRoutes)
							m.routes = newRoutes
						}
					}
				}
			}
		}
		w.WriteHeader(http.StatusOK)
	})

	// GET / — health check
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	return mux
}

func (m *mockCaddy) getRoutes() []CaddyRoute {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]CaddyRoute, len(m.routes))
	copy(out, m.routes)
	return out
}

func (m *mockCaddy) setRoutes(routes []CaddyRoute) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.routes = routes
}

func TestManager_SetRoute_SingleDomain(t *testing.T) {
	mock := newMockCaddy()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	m := NewManager(srv.URL)

	if err := m.SetRouteByID("test-single", []string{"app.example.com"}, "localhost:3000"); err != nil {
		t.Fatalf("SetRouteByID: %v", err)
	}

	routes := mock.getRoutes()
	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(routes))
	}
	if routes[0].Match[0].Host[0] != "app.example.com" {
		t.Errorf("route host = %q, want app.example.com", routes[0].Match[0].Host[0])
	}
	if routes[0].Handle[0].Upstreams[0].Dial != "localhost:3000" {
		t.Errorf("route upstream = %q, want localhost:3000", routes[0].Handle[0].Upstreams[0].Dial)
	}
	if routes[0].ID != "test-single" {
		t.Errorf("route ID = %q, want test-single", routes[0].ID)
	}
}

func TestManager_SetRoute_MultipleDomains(t *testing.T) {
	mock := newMockCaddy()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	m := NewManager(srv.URL)

	m.SetRouteByID("test-multi", []string{"app1.example.com", "shop.client.com"}, "localhost:3000")

	routes := mock.getRoutes()
	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(routes))
	}
	if len(routes[0].Match[0].Host) != 2 {
		t.Fatalf("expected 2 domains, got %d", len(routes[0].Match[0].Host))
	}
	if routes[0].Match[0].Host[0] != "app1.example.com" {
		t.Errorf("first domain = %q, want app1.example.com", routes[0].Match[0].Host[0])
	}
	if routes[0].Match[0].Host[1] != "shop.client.com" {
		t.Errorf("second domain = %q, want shop.client.com", routes[0].Match[0].Host[1])
	}
}

func TestManager_SetRoute_UpdateExisting(t *testing.T) {
	mock := newMockCaddy()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	m := NewManager(srv.URL)

	m.SetRouteByID("test-update", []string{"app.example.com"}, "localhost:3000")
	m.SetRouteByID("test-update", []string{"app.example.com"}, "localhost:4000")

	routes := mock.getRoutes()
	if len(routes) != 1 {
		t.Fatalf("expected 1 route after update, got %d", len(routes))
	}
	if routes[0].Handle[0].Upstreams[0].Dial != "localhost:4000" {
		t.Errorf("route upstream = %q, want localhost:4000 after update", routes[0].Handle[0].Upstreams[0].Dial)
	}
}

func TestManager_DeleteRoute(t *testing.T) {
	mock := newMockCaddy()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	m := NewManager(srv.URL)

	m.SetRouteByID("test-del1", []string{"app1.example.com"}, "localhost:3000")
	m.SetRouteByID("test-del2", []string{"app2.example.com"}, "localhost:4000")

	if err := m.DeleteRouteByID("test-del1"); err != nil {
		t.Fatalf("DeleteRouteByID: %v", err)
	}

	routes := mock.getRoutes()
	if len(routes) != 1 {
		t.Fatalf("expected 1 route after delete, got %d", len(routes))
	}
	if routes[0].ID != "test-del2" {
		t.Errorf("remaining route ID = %q, want test-del2", routes[0].ID)
	}
}

func TestManager_DeleteRoute_NotFound(t *testing.T) {
	mock := newMockCaddy()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	m := NewManager(srv.URL)

	m.SetRouteByID("test-dnf", []string{"app.example.com"}, "localhost:3000")

	// Delete non-existent — should not error (idempotent)
	if err := m.DeleteRouteByID("nonexistent"); err != nil {
		t.Fatalf("DeleteRouteByID nonexistent: %v", err)
	}

	routes := mock.getRoutes()
	if len(routes) != 1 {
		t.Errorf("original route should still exist, got %d", len(routes))
	}
}

func TestManager_GetRoutes_Empty(t *testing.T) {
	mock := newMockCaddy()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	m := NewManager(srv.URL)

	routes, err := m.GetRoutes()
	if err != nil {
		t.Fatalf("GetRoutes: %v", err)
	}
	if len(routes) != 0 {
		t.Errorf("expected 0 routes, got %d", len(routes))
	}
}

func TestManager_RestoreRoutes(t *testing.T) {
	mock := newMockCaddy()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	m := NewManager(srv.URL)

	routes := []Route{
		{Project: "app1", Domain: "app1.example.com", Domains: []string{"app1.example.com"}, Port: 3000},
		{Project: "app2", Domain: "app2.example.com", Domains: []string{"app2.example.com"}, Port: 4000},
		{Project: "app3", Domain: "app3.example.com", Domains: []string{"app3.example.com"}, Port: 5000},
	}

	if err := m.RestoreRoutes(routes); err != nil {
		t.Fatalf("RestoreRoutes: %v", err)
	}

	got := mock.getRoutes()
	if len(got) != 3 {
		t.Fatalf("expected 3 routes after restore, got %d", len(got))
	}
}

func TestManager_IsAlive(t *testing.T) {
	mock := newMockCaddy()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	m := NewManager(srv.URL)

	if !m.IsAlive() {
		t.Error("IsAlive should return true for running server")
	}
}

func TestManager_IsAlive_Down(t *testing.T) {
	m := NewManager("http://localhost:19999")

	if m.IsAlive() {
		t.Error("IsAlive should return false for unreachable server")
	}
}

func TestManager_DeleteRoute_PreservesOtherRoutes(t *testing.T) {
	mock := newMockCaddy()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	m := NewManager(srv.URL)

	m.SetRouteByID("test-a", []string{"a.example.com"}, "localhost:1000")
	m.SetRouteByID("test-b", []string{"b.example.com"}, "localhost:2000")
	m.SetRouteByID("test-c", []string{"c.example.com"}, "localhost:3000")

	m.DeleteRouteByID("test-b")

	routes := mock.getRoutes()
	if len(routes) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(routes))
	}

	ids := []string{}
	for _, r := range routes {
		ids = append(ids, r.ID)
	}
	if ids[0] != "test-a" || ids[1] != "test-c" {
		t.Errorf("remaining routes = %v, want [test-a test-c]", ids)
	}
}

func TestRouteID(t *testing.T) {
	tests := []struct {
		project string
		want    string
	}{
		{"myshop", "yourplatform-myshop"},
		{"app", "yourplatform-app"},
		{"my-cool-app", "yourplatform-my-cool-app"},
	}
	for _, tt := range tests {
		got := RouteID(tt.project)
		if got != tt.want {
			t.Errorf("RouteID(%q) = %q, want %q", tt.project, got, tt.want)
		}
	}
}

func TestManager_SetRouteByID_Idempotent(t *testing.T) {
	mock := newMockCaddy()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	m := NewManager(srv.URL)

	m.SetRouteByID("test-idem", []string{"app.example.com"}, "localhost:3000")
	m.SetRouteByID("test-idem", []string{"app.example.com"}, "localhost:3000")
	m.SetRouteByID("test-idem", []string{"app.example.com"}, "localhost:3000")

	routes := mock.getRoutes()
	if len(routes) != 1 {
		t.Fatalf("expected 1 route after idempotent puts, got %d", len(routes))
	}
}

func TestManager_SetRouteByID_VerifyHeaders(t *testing.T) {
	mock := newMockCaddy()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	m := NewManager(srv.URL)

	m.SetRouteByID("test-headers", []string{"app.example.com"}, "localhost:3000")

	routes := mock.getRoutes()
	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(routes))
	}

	h := routes[0].Handle[0]
	if h.Headers == nil {
		t.Fatal("expected headers to be set")
	}
	if h.Headers.Set["X-Real-IP"] == nil || len(h.Headers.Set["X-Real-IP"]) == 0 {
		t.Error("X-Real-IP header not set")
	}
	if h.Headers.Set["X-Forwarded-Proto"] == nil || h.Headers.Set["X-Forwarded-Proto"][0] != "https" {
		t.Error("X-Forwarded-Proto header not set correctly")
	}
	if h.Headers.Set["X-Forwarded-Host"] == nil || len(h.Headers.Set["X-Forwarded-Host"]) == 0 {
		t.Error("X-Forwarded-Host header not set")
	}
}

// dialPort extracts the port from a "host:port" dial string.
func (h *CaddyUpstream) dialPort() int {
	for i := len(h.Dial) - 1; i >= 0; i-- {
		if h.Dial[i] == ':' {
			port := 0
			for j := i + 1; j < len(h.Dial); j++ {
				port = port*10 + int(h.Dial[j]-'0')
			}
			return port
		}
	}
	return 0
}
