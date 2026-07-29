package caddy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

type Manager struct {
	adminURL string
}

func NewManager(caddyAdminURL string) *Manager {
	return &Manager{
		adminURL: caddyAdminURL,
	}
}

func (m *Manager) SetRoute(domain string, appPort int) error {
	slog.Info("setting caddy route", "domain", domain, "app_port", appPort)

	config := map[string]interface{}{
		"apps": map[string]interface{}{
			"http": map[string]interface{}{
				"servers": map[string]interface{}{
					"srv0": map[string]interface{}{
						"listen": []string{":80"},
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
												"destination": fmt.Sprintf("localhost:%d", appPort),
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

	slog.Info("caddy route set", "domain", domain, "app_port", appPort)
	return nil
}

func (m *Manager) DeleteRoute(domain string) error {
	slog.Info("deleting caddy route", "domain", domain)

	resp, err := http.NewRequest(http.MethodDelete, m.adminURL+"/config/apps/http/servers/srv0/routes", nil)
	if err != nil {
		return fmt.Errorf("create delete request: %w", err)
	}

	client := &http.Client{}
	httpResp, err := client.Do(resp)
	if err != nil {
		return fmt.Errorf("delete caddy route: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode >= 400 {
		body, _ := io.ReadAll(httpResp.Body)
		return fmt.Errorf("caddy delete rejected (%d): %s", httpResp.StatusCode, string(body))
	}

	slog.Info("caddy route deleted", "domain", domain)
	return nil
}

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
