package lifecycle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/yourname/yourplatform/agent/internal/preflight"
)

// RegisterResponse is returned by POST /agent/register.
type RegisterResponse struct {
	AgentID     string `json:"agent_id"`
	AgentSecret string `json:"agent_secret"`
	ServerID    string `json:"server_id"`
	WSURL       string `json:"ws_url"`
}

// Register exchanges a registration token for agent credentials.
func Register(controlPlaneURL, token string, result *preflight.Result) (*RegisterResponse, error) {
	base := httpBaseURL(controlPlaneURL)
	endpoint := strings.TrimRight(base, "/") + "/agent/register"

	body := map[string]interface{}{
		"token": token,
	}
	if result != nil {
		body["system_info"] = result.SystemInfo
		body["warnings"] = result.Checks
		body["auto_fixed"] = result.AutoFixed
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(endpoint, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("register request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("register failed: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var out RegisterResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("parse register response: %w", err)
	}
	if out.AgentID == "" || out.AgentSecret == "" {
		return nil, fmt.Errorf("register response missing credentials")
	}
	return &out, nil
}

// WSURLFromControlPlane converts an HTTP/WS control plane URL into the agent WS endpoint.
func WSURLFromControlPlane(controlPlaneURL string) string {
	u := strings.TrimSpace(controlPlaneURL)
	if strings.Contains(u, "/ws/agent") {
		return u
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return u
	}
	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	case "ws", "wss":
		// ok
	default:
		parsed.Scheme = "ws"
	}
	parsed.Path = "/ws/agent"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func httpBaseURL(controlPlaneURL string) string {
	u := strings.TrimSpace(controlPlaneURL)
	u = strings.Replace(u, "wss://", "https://", 1)
	u = strings.Replace(u, "ws://", "http://", 1)
	parsed, err := url.Parse(u)
	if err != nil {
		return u
	}
	parsed.Path = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}
