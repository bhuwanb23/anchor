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
	routes []caddyRoute
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

	// POST /config — full config replacement (putRoutes)
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
							var newRoutes []caddyRoute
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

func (m *mockCaddy) getRoutes() []caddyRoute {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]caddyRoute, len(m.routes))
	copy(out, m.routes)
	return out
}

func (m *mockCaddy) setRoutes(routes []caddyRoute) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.routes = routes
}

func TestManager_SetRoute_SingleDomain(t *testing.T) {
	mock := newMockCaddy()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	m := NewManager(srv.URL)

	if err := m.SetRoute("app.example.com", 3000); err != nil {
		t.Fatalf("SetRoute: %v", err)
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
}

func TestManager_SetRoute_MultipleDomains(t *testing.T) {
	mock := newMockCaddy()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	m := NewManager(srv.URL)

	m.SetRoute("app1.example.com", 3000)
	m.SetRoute("app2.example.com", 4000)

	routes := mock.getRoutes()
	if len(routes) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(routes))
	}

	// Check both routes exist
	hosts := map[string]int{}
	for _, r := range routes {
		hosts[r.Match[0].Host[0]] = r.Handle[0].Upstreams[0].dialPort()
	}
	if hosts["app1.example.com"] != 3000 {
		t.Errorf("app1 port = %d, want 3000", hosts["app1.example.com"])
	}
	if hosts["app2.example.com"] != 4000 {
		t.Errorf("app2 port = %d, want 4000", hosts["app2.example.com"])
	}
}

func TestManager_SetRoute_UpdateExisting(t *testing.T) {
	mock := newMockCaddy()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	m := NewManager(srv.URL)

	m.SetRoute("app.example.com", 3000)
	m.SetRoute("app.example.com", 4000) // Update port

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

	m.SetRoute("app1.example.com", 3000)
	m.SetRoute("app2.example.com", 4000)

	if err := m.DeleteRoute("app1.example.com"); err != nil {
		t.Fatalf("DeleteRoute: %v", err)
	}

	routes := mock.getRoutes()
	if len(routes) != 1 {
		t.Fatalf("expected 1 route after delete, got %d", len(routes))
	}
	if routes[0].Match[0].Host[0] != "app2.example.com" {
		t.Errorf("remaining route = %q, want app2.example.com", routes[0].Match[0].Host[0])
	}
}

func TestManager_DeleteRoute_NotFound(t *testing.T) {
	mock := newMockCaddy()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	m := NewManager(srv.URL)

	m.SetRoute("app.example.com", 3000)

	// Delete non-existent domain — should not error
	if err := m.DeleteRoute("other.example.com"); err != nil {
		t.Fatalf("DeleteRoute non-existent: %v", err)
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
		{Domain: "app1.example.com", Port: 3000},
		{Domain: "app2.example.com", Port: 4000},
		{Domain: "app3.example.com", Port: 5000},
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

	// Add 3 routes
	m.SetRoute("a.example.com", 1000)
	m.SetRoute("b.example.com", 2000)
	m.SetRoute("c.example.com", 3000)

	// Delete middle one
	m.DeleteRoute("b.example.com")

	routes := mock.getRoutes()
	if len(routes) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(routes))
	}

	hosts := []string{}
	for _, r := range routes {
		hosts = append(hosts, r.Match[0].Host[0])
	}
	if hosts[0] != "a.example.com" || hosts[1] != "c.example.com" {
		t.Errorf("remaining routes = %v, want [a.example.com c.example.com]", hosts)
	}
}

// dialPort extracts the port from a "host:port" dial string.
func (h *caddyUpstream) dialPort() int {
	// Parse "localhost:3000" → 3000
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
