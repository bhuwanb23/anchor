package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

type Config struct {
	ControlPlaneURL string `yaml:"control_plane_url"`
	// Registration mode (first run)
	RegistrationToken string `yaml:"registration_token,omitempty"`
	// Authenticated mode (after registration)
	AgentID     string `yaml:"agent_id,omitempty"`
	AgentSecret string `yaml:"agent_secret,omitempty"`
	// Shared fields
	ServerID       string `yaml:"server_id,omitempty"`
	DockerSocket   string `yaml:"docker_socket"`
	CaddyConfigDir string `yaml:"caddy_config_dir"`
	BackupDest     string `yaml:"backup_dest"`
	WSReconnectSec int    `yaml:"ws_reconnect_sec"`
	LogLevel       string `yaml:"log_level"`
}

func Load(path string) (*Config, error) {
	_ = godotenv.Load()

	if path == "" {
		path = resolveConfigPath()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if v := os.Getenv("CONTROL_PLANE_URL"); v != "" {
		cfg.ControlPlaneURL = v
	}
	if v := os.Getenv("AGENT_TOKEN"); v != "" {
		cfg.RegistrationToken = v
	}
	if v := os.Getenv("SERVER_ID"); v != "" {
		cfg.ServerID = v
	}

	if cfg.DockerSocket == "" {
		cfg.DockerSocket = "unix:///var/run/docker.sock"
	}
	if cfg.CaddyConfigDir == "" {
		cfg.CaddyConfigDir = "/etc/caddy"
	}
	if cfg.WSReconnectSec == 0 {
		cfg.WSReconnectSec = 5
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}

	return &cfg, nil
}

func (c *Config) Validate() error {
	if c.ControlPlaneURL == "" {
		return fmt.Errorf("control_plane_url is required")
	}
	hasToken := c.RegistrationToken != ""
	hasCredentials := c.AgentID != "" && c.AgentSecret != ""
	if !hasToken && !hasCredentials {
		return fmt.Errorf("either registration_token or agent_id+agent_secret is required")
	}
	return nil
}

func (c *Config) NeedsRegistration() bool {
	return c.RegistrationToken != "" && c.AgentID == ""
}

func resolveConfigPath() string {
	if v := os.Getenv("AGENT_CONFIG_PATH"); v != "" {
		return v
	}
	if _, err := os.Stat("/etc/yourplatform/config.yaml"); err == nil {
		return "/etc/yourplatform/config.yaml"
	}
	return "./config.yaml"
}
