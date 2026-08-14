package config

import (
	"fmt"
	"os"
	"runtime"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

type Config struct {
	ControlPlaneURL string `yaml:"control_plane_url"`
	// Registration mode (first run)
	RegistrationToken string `yaml:"registration_token,omitempty"`
	// AgentToken is an accepted alias for registration_token (install docs / local configs).
	AgentToken string `yaml:"agent_token,omitempty"`
	// Authenticated mode (after registration)
	AgentID     string `yaml:"agent_id,omitempty"`
	AgentSecret string `yaml:"agent_secret,omitempty"`
	// Shared fields
	ServerID        string `yaml:"server_id,omitempty"`
	DockerSocket    string `yaml:"docker_socket"`
	CaddyBinaryPath string `yaml:"caddy_binary_path,omitempty"`
	CaddyDataDir    string `yaml:"caddy_data_dir,omitempty"`
	CaddyConfigDir  string `yaml:"caddy_config_dir"`
	CaddyAdminPort  int    `yaml:"caddy_admin_port,omitempty"`
	CaddyACMEmail   string `yaml:"caddy_acme_email,omitempty"`
	CaddyUseStaging *bool  `yaml:"caddy_use_staging,omitempty"`
	CaddyCertDir    string `yaml:"caddy_cert_dir,omitempty"`
	BackupDest      string `yaml:"backup_dest"`
	WSReconnectSec  int    `yaml:"ws_reconnect_sec"`
	LogLevel        string `yaml:"log_level"`

	// Backup configuration
	BackupSchedule       string `yaml:"backup_schedule,omitempty"`
	BackupRetentionDaily int    `yaml:"backup_retention_daily,omitempty"`
	BackupRetentionWeekly int   `yaml:"backup_retention_weekly,omitempty"`
	BackupRetentionMonthly int  `yaml:"backup_retention_monthly,omitempty"`
	BackupS3Endpoint     string `yaml:"backup_s3_endpoint,omitempty"`
	BackupS3AccessKey    string `yaml:"backup_s3_access_key,omitempty"`
	BackupS3SecretKey    string `yaml:"backup_s3_secret_key,omitempty"`
	BackupS3Bucket       string   `yaml:"backup_s3_bucket,omitempty"`
	BackupS3Region       string   `yaml:"backup_s3_region,omitempty"`
	BackupIncludePaths   []string `yaml:"backup_include_paths,omitempty"`
	BackupExcludePaths   []string `yaml:"backup_exclude_paths,omitempty"`
	BackupDumpDir        string   `yaml:"backup_dump_dir,omitempty"`
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

	if cfg.RegistrationToken == "" && cfg.AgentToken != "" {
		cfg.RegistrationToken = cfg.AgentToken
	}

	if v := os.Getenv("CONTROL_PLANE_URL"); v != "" {
		cfg.ControlPlaneURL = v
	}
	if v := os.Getenv("AGENT_TOKEN"); v != "" {
		cfg.RegistrationToken = v
	}
	if v := os.Getenv("AGENT_ID"); v != "" {
		cfg.AgentID = v
	}
	if v := os.Getenv("AGENT_SECRET"); v != "" {
		cfg.AgentSecret = v
	}
	if v := os.Getenv("SERVER_ID"); v != "" {
		cfg.ServerID = v
	}
	if v := os.Getenv("DOCKER_SOCKET"); v != "" {
		cfg.DockerSocket = v
	}

	if cfg.DockerSocket == "" {
		if runtime.GOOS == "windows" {
			cfg.DockerSocket = "npipe:////./pipe/dockerDesktopLinuxEngine"
		} else {
			cfg.DockerSocket = "unix:///var/run/docker.sock"
		}
	}
	if cfg.CaddyConfigDir == "" {
		cfg.CaddyConfigDir = "/etc/caddy"
	}
	if cfg.CaddyBinaryPath == "" {
		cfg.CaddyBinaryPath = "/usr/local/bin/yourplatform-caddy"
	}
	if cfg.CaddyDataDir == "" {
		cfg.CaddyDataDir = "/var/lib/yourplatform/caddy"
	}
	if cfg.CaddyAdminPort == 0 {
		cfg.CaddyAdminPort = 2019
	}
	if cfg.CaddyACMEmail == "" {
		cfg.CaddyACMEmail = "certs@yourplatform.com"
	}
	if cfg.CaddyCertDir == "" {
		cfg.CaddyCertDir = "/var/lib/yourplatform/caddy/certificates"
	}
	if cfg.CaddyUseStaging == nil {
		t := true
		cfg.CaddyUseStaging = &t
	}
	if cfg.WSReconnectSec == 0 {
		cfg.WSReconnectSec = 5
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	if cfg.BackupSchedule == "" {
		cfg.BackupSchedule = "0 2 * * *" // Default: 2am daily
	}
	if cfg.BackupRetentionDaily == 0 {
		cfg.BackupRetentionDaily = 7
	}
	if cfg.BackupRetentionWeekly == 0 {
		cfg.BackupRetentionWeekly = 4
	}
	if cfg.BackupRetentionMonthly == 0 {
		cfg.BackupRetentionMonthly = 12
	}
	if cfg.BackupDumpDir == "" {
		cfg.BackupDumpDir = "/tmp/yourplatform/backups"
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
		return fmt.Errorf("either registration_token (or agent_token / AGENT_TOKEN) or agent_id+agent_secret is required — create a server in the dashboard and paste the token into agent/config.yaml")
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
