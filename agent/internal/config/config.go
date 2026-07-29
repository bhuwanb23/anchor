package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	ControlPlaneURL string `yaml:"control_plane_url"`
	AgentToken      string `yaml:"agent_token"`
	ServerID        string `yaml:"server_id"`
	DockerSocket    string `yaml:"docker_socket"`
	CaddyConfigDir  string `yaml:"caddy_config_dir"`
	BackupDest      string `yaml:"backup_dest"`
	WSReconnectSec  int    `yaml:"ws_reconnect_sec"`
	LogLevel        string `yaml:"log_level"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
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