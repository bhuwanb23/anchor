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

// RouteID returns a stable, predictable route ID for a project.
func RouteID(project string) string {
	return "yourplatform-" + project
}

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

// SetRouteByID creates or updates a route by its ID.
// This is idempotent — calling with the same routeID replaces the route.
func (m *Manager) SetRouteByID(routeID string, domains []string, upstream string) error {
	slog.Info("setting caddy route", "id", routeID, "domains", domains, "upstream", upstream)

	if len(domains) == 0 {
		return fmt.Errorf("at least one domain is required")
	}

	route := caddyRoute{
		ID: routeID,
		Match: []caddyMatch{
			{Host: domains},
		},
		Handle: []caddyHandler{
			{
				Handler:   "reverse_proxy",
				Upstreams: []caddyUpstream{{Dial: upstream}},
				Headers: &caddyHeaders{
					Set: map[string][]string{
						"X-Real-IP":        {"{http.request.remote.host}"},
						"X-Forwarded-Proto": {"https"},
						"X-Forwarded-Host":  {"{http.request.host}"},
					},
				},
			},
		},
	}

	data, err := json.Marshal(route)
	if err != nil {
		return fmt.Errorf("marshal route: %w", err)
	}

	url := fmt.Sprintf("%s/config/apps/http/servers/main/routes/%s", m.adminURL, routeID)
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("put route %s: %w", routeID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("caddy put route %s rejected (%d): %s", routeID, resp.StatusCode, string(body))
	}

	// Verify route was accepted
	if err := m.verifyRoute(routeID); err != nil {
		return fmt.Errorf("verify route %s: %w", routeID, err)
	}

	return nil
}

// GetRouteByID returns a single route by its ID.
func (m *Manager) GetRouteByID(routeID string) (*caddyRoute, error) {
	url := fmt.Sprintf("%s/config/apps/http/servers/main/routes/%s", m.adminURL, routeID)
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("get route %s: %w", routeID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, nil
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("caddy get route %s rejected (%d): %s", routeID, resp.StatusCode, string(body))
	}

	var route caddyRoute
	if err := json.NewDecoder(resp.Body).Decode(&route); err != nil {
		return nil, fmt.Errorf("decode route %s: %w", routeID, err)
	}

	return &route, nil
}

// DeleteRouteByID removes a single route by its ID.
// Returns nil if the route doesn't exist (idempotent).
func (m *Manager) DeleteRouteByID(routeID string) error {
	slog.Info("deleting caddy route", "id", routeID)

	url := fmt.Sprintf("%s/config/apps/http/servers/main/routes/%s", m.adminURL, routeID)
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("create delete request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete route %s: %w", routeID, err)
	}
	defer resp.Body.Close()

	// 404 means already gone — idempotent
	if resp.StatusCode == 404 {
		return nil
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("caddy delete route %s rejected (%d): %s", routeID, resp.StatusCode, string(body))
	}

	return nil
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
		routeID := RouteID(r.Project)
		upstream := fmt.Sprintf("127.0.0.1:%d", r.Port)
		domains := r.Domains
		if len(domains) == 0 && r.Domain != "" {
			domains = []string{r.Domain}
		}
		if err := m.SetRouteByID(routeID, domains, upstream); err != nil {
			slog.Warn("failed to restore route", "id", routeID, "error", err)
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

// verifyRoute confirms a route exists after PUT.
func (m *Manager) verifyRoute(routeID string) error {
	route, err := m.GetRouteByID(routeID)
	if err != nil {
		return err
	}
	if route == nil {
		return fmt.Errorf("route %s not found after PUT", routeID)
	}
	return nil
}

// Caddy JSON config types for route management.

type caddyRoute struct {
	ID      string          `json:"@id"`
	Match   []caddyMatch    `json:"match"`
	Handle  []caddyHandler  `json:"handle"`
}

type caddyMatch struct {
	Host []string `json:"host"`
}

type caddyHandler struct {
	Handler   string           `json:"handler"`
	Upstreams []caddyUpstream  `json:"upstreams,omitempty"`
	Headers   *caddyHeaders    `json:"headers,omitempty"`
}

type caddyUpstream struct {
	Dial string `json:"dial"`
}

type caddyHeaders struct {
	Set map[string][]string `json:"set,omitempty"`
}
