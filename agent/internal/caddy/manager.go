package caddy

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
)

type Manager struct {
	adminURL string
}

type RouteConfig struct {
	Domain string
	Port   int
}

func NewManager(controlPlaneURL string) *Manager {
	return &Manager{
		adminURL: fmt.Sprintf("%s/admin", controlPlaneURL),
	}
}

func (m *Manager) SetRoute(domain string, port int) error {
	slog.Info("setting caddy route", "domain", domain, "port", port)

	config := map[string]interface{}{
		"apps": map[string]interface{}{
			"http": map[string]interface{}{
				"servers": map[string]interface{}{
					"srv0": map[string]interface{}{
						"listen": []string{fmt.Sprintf(":%d", port)},
						"routes": []interface{}{
							map[string]interface{}{
								"match": []map[string]interface{}{
									{
										"host": []string{domain},
									},
								},
								"handle": []map[string]interface{}{
									{
										"handler": "reverse_proxy",
										"upstreams": []map[string]interface{}{
											{
												"destination": fmt.Sprintf("http://localhost:%d", port),
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	data, err := json.Marshal(config)
	if err != nil {
		return err
	}

	resp, err := http.Post(m.adminURL+"/config", "application/json", nil)
	if err != nil {
		return fmt.Errorf("failed to update caddy config: %w", err)
	}
	defer resp.Body.Close()

	_ = data
	return nil
}

func (m *Manager) DeleteRoute(domain string) error {
	slog.Info("deleting caddy route", "domain", domain)
	// TODO: implement route deletion via Caddy admin API
	return nil
}

func (m *Manager) Reload() error {
	slog.Info("reloading caddy configuration")
	// TODO: trigger Caddy reload via admin API
	return nil
}

func generateRouteID() string {
	return uuid.New().String()
}