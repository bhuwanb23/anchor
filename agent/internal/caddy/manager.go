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
	adminURL    string
	retryConfig RetryConfig
}

// NewManager creates a new Caddy route manager.
func NewManager(caddyAdminURL string) *Manager {
	return &Manager{
		adminURL: caddyAdminURL,
		retryConfig: RetryConfig{
			MaxAttempts: defaultMaxAttempts,
			BaseDelay:   defaultBaseDelay,
		},
	}
}

// SetRetryConfig sets the retry configuration for admin API calls.
func (m *Manager) SetRetryConfig(cfg RetryConfig) {
	m.retryConfig = cfg
}

// SetRouteByID creates or updates a route by its ID.
// This is idempotent — calling with the same routeID replaces the route.
func (m *Manager) SetRouteByID(routeID string, domains []string, upstream string) error {
	slog.Info("setting caddy route", "id", routeID, "domains", domains, "upstream", upstream)

	if len(domains) == 0 {
		return fmt.Errorf("at least one domain is required")
	}

	route := CaddyRoute{
		ID: routeID,
		Match: []CaddyMatch{
			{Host: domains},
		},
		Handle: []CaddyHandler{
			{
				Handler:   "reverse_proxy",
				Upstreams: []CaddyUpstream{{Dial: upstream}},
				Headers: &CaddyHeaders{
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
func (m *Manager) GetRouteByID(routeID string) (*CaddyRoute, error) {
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

	var route CaddyRoute
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
func (m *Manager) GetRoutes() ([]CaddyRoute, error) {
	resp, err := http.Get(m.adminURL + "/config/apps/http/servers/main/routes")
	if err != nil {
		return nil, fmt.Errorf("get routes: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return []CaddyRoute{}, nil
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("caddy get routes rejected (%d): %s", resp.StatusCode, string(body))
	}

	var routes []CaddyRoute
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

const (
	letsEncryptProd   = "https://acme-v02.api.letsencrypt.org/directory"
	letsEncryptStaging = "https://acme-staging-v02.api.letsencrypt.org/directory"
)

// SwitchCA changes the ACME CA between staging and production and reloads Caddy.
func (m *Manager) SwitchCA(useProduction bool) error {
	targetCA := letsEncryptStaging
	if useProduction {
		targetCA = letsEncryptProd
	}

	slog.Info("switching ACME CA", "target", targetCA, "production", useProduction)

	// Read current config
	resp, err := http.Get(m.adminURL + "/config/")
	if err != nil {
		return fmt.Errorf("read caddy config: %w", err)
	}
	defer resp.Body.Close()

	var config map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&config); err != nil {
		return fmt.Errorf("decode caddy config: %w", err)
	}

	// Navigate to apps.tls.automation.policies[0].issuers[0].ca
	apps, ok := config["apps"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("missing apps in caddy config")
	}

	tls, ok := apps["tls"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("missing tls in caddy config")
	}

	automation, ok := tls["automation"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("missing automation in caddy config")
	}

	policies, ok := automation["policies"].([]interface{})
	if !ok || len(policies) == 0 {
		return fmt.Errorf("missing or empty policies in caddy config")
	}

	policy, ok := policies[0].(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid policy format")
	}

	issuers, ok := policy["issuers"].([]interface{})
	if !ok || len(issuers) == 0 {
		return fmt.Errorf("missing or empty issuers in caddy config")
	}

	issuer, ok := issuers[0].(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid issuer format")
	}

	issuer["ca"] = targetCA

	// Update config via PUT
	data, _ := json.Marshal(config)
	updateReq, err := http.NewRequest(http.MethodPut, m.adminURL+"/load", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create update request: %w", err)
	}
	updateReq.Header.Set("Content-Type", "application/json")

	updateResp, err := http.DefaultClient.Do(updateReq)
	if err != nil {
		return fmt.Errorf("update caddy config: %w", err)
	}
	defer updateResp.Body.Close()

	if updateResp.StatusCode >= 400 {
		body, _ := io.ReadAll(updateResp.Body)
		return fmt.Errorf("caddy config update rejected (%d): %s", updateResp.StatusCode, string(body))
	}

	slog.Info("ACME CA switched successfully", "ca", targetCA)
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

// CaddyRoute is a Caddy route configuration for the admin API.
type CaddyRoute struct {
	ID      string          `json:"@id"`
	Match   []CaddyMatch    `json:"match"`
	Handle  []CaddyHandler  `json:"handle"`
}

// CaddyMatch defines the match criteria for a route.
type CaddyMatch struct {
	Host []string `json:"host"`
}

// CaddyHandler defines what happens to matched requests.
type CaddyHandler struct {
	Handler   string           `json:"handler"`
	Upstreams []CaddyUpstream  `json:"upstreams,omitempty"`
	Headers   *CaddyHeaders    `json:"headers,omitempty"`
}

// CaddyUpstream is a backend address to proxy to.
type CaddyUpstream struct {
	Dial string `json:"dial"`
}

// CaddyHeaders defines header manipulation rules.
type CaddyHeaders struct {
	Set map[string][]string `json:"set,omitempty"`
}

// SetAskRoute creates the on-demand TLS ask endpoint.
// This starts a local HTTP server that Caddy calls to verify domain authorization.
func (m *Manager) SetAskRoute(authorizer *DomainAuthorizer) error {
	const askAddr = "localhost:2020"

	// Update the Caddy config to point ask to the correct port
	askURL := "http://" + askAddr + "/__yourplatform_ask"
	updateReq := map[string]interface{}{
		"apps": map[string]interface{}{
			"http": map[string]interface{}{
				"servers": map[string]interface{}{
					"main": map[string]interface{}{
						"on_demand": map[string]interface{}{
							"ask": askURL,
						},
					},
				},
			},
		},
	}
	data, _ := json.Marshal(updateReq)
	req, err := http.NewRequest(http.MethodPatch, m.adminURL+"/config/", bytes.NewReader(data))
	if err == nil {
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			slog.Warn("could not update on_demand.ask in caddy config, using initial config", "error", err)
		} else {
			resp.Body.Close()
		}
	}

	// Start the ask endpoint server
	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/__yourplatform_ask", authorizer.HandleAsk)
		slog.Info("starting ask endpoint server", "addr", askAddr)
		if err := http.ListenAndServe(askAddr, mux); err != nil {
			slog.Error("ask endpoint server failed", "error", err)
		}
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)
	slog.Info("on-demand TLS ask endpoint configured", "url", askURL)
	return nil
}
