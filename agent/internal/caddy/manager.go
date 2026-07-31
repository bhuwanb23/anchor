package caddy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// Manager manages Caddy routes via the admin API.
type Manager struct {
	adminURL string
}

// NewManager creates a new Caddy route manager.
func NewManager(caddyAdminURL string) *Manager {
	return &Manager{
		adminURL: caddyAdminURL,
	}
}

// SetRoute adds or updates a route for a domain. It preserves existing routes.
func (m *Manager) SetRoute(domain string, appPort int) error {
	slog.Info("setting caddy route", "domain", domain, "app_port", appPort)

	existing, err := m.GetRoutes()
	if err != nil {
		slog.Warn("failed to get existing routes, starting fresh", "error", err)
		existing = []caddyRoute{}
	}

	// Remove existing route for this domain (replace or add)
	var merged []caddyRoute
	for _, r := range existing {
		hasDomain := false
		for _, h := range r.Match {
			for _, d := range h.Host {
				if d == domain {
					hasDomain = true
					break
				}
			}
		}
		if !hasDomain {
			merged = append(merged, r)
		}
	}

	// Add new route
	merged = append(merged, caddyRoute{
		Match: []caddyMatch{{Host: []string{domain}}},
		Handle: []caddyHandler{{
			Handler:    "reverse_proxy",
			Upstreams:  []caddyUpstream{{Dial: fmt.Sprintf("localhost:%d", appPort)}},
		}},
	})

	return m.putRoutes(merged)
}

// DeleteRoute removes a route for a specific domain.
func (m *Manager) DeleteRoute(domain string) error {
	slog.Info("deleting caddy route", "domain", domain)

	existing, err := m.GetRoutes()
	if err != nil {
		return fmt.Errorf("get routes: %w", err)
	}

	var filtered []caddyRoute
	removed := false
	for _, r := range existing {
		hasDomain := false
		for _, h := range r.Match {
			for _, d := range h.Host {
				if d == domain {
					hasDomain = true
					break
				}
			}
		}
		if hasDomain {
			removed = true
			continue
		}
		filtered = append(filtered, r)
	}

	if !removed {
		slog.Debug("route not found for domain", "domain", domain)
		return nil
	}

	return m.putRoutes(filtered)
}

// GetRoutes returns all current routes from the "main" HTTPS server.
func (m *Manager) GetRoutes() ([]caddyRoute, error) {
	resp, err := http.Get(m.adminURL + "/config/apps/http/servers/main/routes")
	if err != nil {
		return nil, fmt.Errorf("get routes: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return []caddyRoute{}, nil
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("caddy get routes rejected (%d): %s", resp.StatusCode, string(body))
	}

	var routes []caddyRoute
	if err := json.NewDecoder(resp.Body).Decode(&routes); err != nil {
		return nil, fmt.Errorf("decode routes: %w", err)
	}

	return routes, nil
}

// RestoreRoutes re-applies all routes after a Caddy restart.
func (m *Manager) RestoreRoutes(routes []Route) error {
	if len(routes) == 0 {
		return nil
	}
	slog.Info("restoring caddy routes", "count", len(routes))

	for _, r := range routes {
		if err := m.SetRoute(r.Domain, r.Port); err != nil {
			slog.Warn("failed to restore route", "domain", r.Domain, "error", err)
			continue
		}
	}
	return nil
}

// IsAlive checks if the Caddy admin API is responsive.
func (m *Manager) IsAlive() bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(m.adminURL + "/")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 500
}

// Reload triggers a Caddy config reload.
func (m *Manager) Reload() error {
	slog.Info("reloading caddy configuration")

	resp, err := http.Post(m.adminURL+"/load", "application/json", nil)
	if err != nil {
		return fmt.Errorf("reload caddy: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("caddy reload rejected (%d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// putRoutes replaces routes on the "main" server while preserving
// the "redirect" server (HTTP→HTTPS on :80).
func (m *Manager) putRoutes(routes []caddyRoute) error {
	config := map[string]interface{}{
		"apps": map[string]interface{}{
			"http": map[string]interface{}{
				"servers": map[string]interface{}{
					"main": map[string]interface{}{
						"listen": []string{":443"},
						"routes": routes,
						"tls_connection_policies": []interface{}{
							map[string]interface{}{},
						},
					},
				},
			},
		},
	}

	data, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("marshal caddy config: %w", err)
	}

	resp, err := http.Post(
		m.adminURL+"/config",
		"application/json",
		bytes.NewReader(data),
	)
	if err != nil {
		return fmt.Errorf("post caddy config: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("caddy config rejected (%d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// Caddy JSON config types for route management.

type caddyRoute struct {
	Match  []caddyMatch  `json:"match"`
	Handle []caddyHandler `json:"handle"`
}

type caddyMatch struct {
	Host []string `json:"host"`
}

type caddyHandler struct {
	Handler   string           `json:"handler"`
	Upstreams []caddyUpstream `json:"upstreams,omitempty"`
}

type caddyUpstream struct {
	Dial string `json:"dial"`
}
