package caddy

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	defaultBinaryPath = "/usr/local/bin/yourplatform-caddy"
	defaultDataDir    = "/var/lib/yourplatform/caddy"
	monitorInterval   = 5 * time.Second
	adminReadyTimeout = 15 * time.Second
	adminPollInterval = 500 * time.Millisecond
	stopTimeout       = 5 * time.Second
)

// Route represents a single Caddy reverse proxy route for restoration.
type Route struct {
	Domain  string   `json:"domain"`            // legacy single domain
	Domains []string `json:"domains,omitempty"` // multiple domains
	Port    int      `json:"port"`
	Project string   `json:"project"` // project name for route ID generation
}

// ProcessConfig holds configuration for the Caddy process manager.
type ProcessConfig struct {
	BinaryPath string
	DataDir    string
	AdminURL   string
	ACMEmail   string // ACME email for Let's Encrypt (default: certs@yourplatform.com)
	UseStaging *bool  // Use Let's Encrypt staging (nil or true = staging; false = production)
	CertDir    string // Certificate storage directory
}

// UseStagingEnabled returns whether staging mode is on. Defaults to true.
func (pc *ProcessConfig) UseStagingEnabled() bool {
	if pc.UseStaging == nil {
		return true
	}
	return *pc.UseStaging
}

func (pc *ProcessConfig) defaults() {
	if pc.BinaryPath == "" {
		pc.BinaryPath = defaultBinaryPath
	}
	if pc.DataDir == "" {
		pc.DataDir = defaultDataDir
	}
	if pc.AdminURL == "" {
		pc.AdminURL = "http://localhost:2019"
	}
	if pc.ACMEmail == "" {
		pc.ACMEmail = "certs@yourplatform.com"
	}
	if pc.CertDir == "" {
		pc.CertDir = filepath.Join(pc.DataDir, "certificates")
	}
	if pc.UseStaging == nil {
		t := true
		pc.UseStaging = &t
	}
}

func (pc *ProcessConfig) pidFile() string {
	return filepath.Join(pc.DataDir, "caddy.pid")
}

func (pc *ProcessConfig) configFile() string {
	return filepath.Join(pc.DataDir, "config.json")
}

// ProcessManager manages a Caddy child process.
type ProcessManager struct {
	cfg        ProcessConfig
	cmd        *exec.Cmd
	manager    *Manager
	authorizer *DomainAuthorizer
	logMonitor *LogMonitor
}

// NewProcessManager creates a new Caddy process manager.
func NewProcessManager(cfg ProcessConfig, manager *Manager) *ProcessManager {
	cfg.defaults()
	return &ProcessManager{
		cfg:        cfg,
		manager:    manager,
		authorizer: NewDomainAuthorizer(),
	}
}

// SetLogMonitor sets the log monitor for Caddy stderr parsing.
func (pm *ProcessManager) SetLogMonitor(monitor *LogMonitor) {
	pm.logMonitor = monitor
}

// Authorizer returns the domain authorizer for on-demand TLS.
func (pm *ProcessManager) Authorizer() *DomainAuthorizer {
	return pm.authorizer
}

// Start starts the Caddy process.
func (pm *ProcessManager) Start(ctx context.Context) error {
	slog.Info("starting caddy", "binary", pm.cfg.BinaryPath)

	if _, err := os.Stat(pm.cfg.BinaryPath); os.IsNotExist(err) {
		return fmt.Errorf("caddy binary not found at %s", pm.cfg.BinaryPath)
	}

	if err := os.MkdirAll(pm.cfg.DataDir, 0755); err != nil {
		return fmt.Errorf("create caddy data dir: %w", err)
	}

	if pid, err := pm.readPIDFile(); err == nil {
		if pm.isProcessAlive(pid) {
			slog.Info("caddy already running", "pid", pid)
			return nil
		}
		slog.Warn("stale caddy PID file", "pid", pid)
		pm.removePIDFile()
	}

	if err := pm.ensureConfig(); err != nil {
		return fmt.Errorf("ensure caddy config: %w", err)
	}

	pm.cmd = exec.Command(pm.cfg.BinaryPath, "run",
		"--config", pm.cfg.configFile(),
	)

	stderr, err := pm.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("create stderr pipe: %w", err)
	}

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			slog.Warn("caddy", "output", line)
			if pm.logMonitor != nil {
				pm.logMonitor.ProcessLine(line)
			}
		}
	}()

	if err := pm.cmd.Start(); err != nil {
		return fmt.Errorf("start caddy: %w", err)
	}

	pid := pm.cmd.Process.Pid
	slog.Info("caddy process started", "pid", pid)

	if err := pm.writePIDFile(pid); err != nil {
		slog.Warn("failed to write caddy PID file", "error", err)
	}

	if err := pm.waitForAdminAPI(ctx); err != nil {
		pm.killProcess()
		pm.removePIDFile()
		return fmt.Errorf("caddy admin API not ready: %w", err)
	}

	slog.Info("caddy started successfully", "pid", pid, "admin_url", pm.cfg.AdminURL)
	return nil
}

// Stop gracefully stops the Caddy process.
func (pm *ProcessManager) Stop() error {
	pid, err := pm.readPIDFile()
	if err != nil {
		return nil
	}

	if !pm.isProcessAlive(pid) {
		pm.removePIDFile()
		return nil
	}

	slog.Info("stopping caddy", "pid", pid)

	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find caddy process: %w", err)
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		slog.Warn("failed to send SIGTERM, force killing", "error", err)
		pm.killProcess()
		pm.removePIDFile()
		return nil
	}

	done := make(chan struct{})
	go func() {
		pm.cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
		slog.Info("caddy stopped gracefully")
	case <-time.After(stopTimeout):
		slog.Warn("caddy did not stop gracefully, force killing")
		pm.killProcess()
	}

	pm.removePIDFile()
	return nil
}

// Restart stops and starts Caddy, then restores all routes.
func (pm *ProcessManager) Restart(ctx context.Context, routes []Route) error {
	slog.Info("restarting caddy")

	if err := pm.Stop(); err != nil {
		slog.Warn("error stopping caddy during restart", "error", err)
	}

	if err := pm.Start(ctx); err != nil {
		return fmt.Errorf("start caddy after restart: %w", err)
	}

	if len(routes) > 0 {
		if err := pm.manager.RestoreRoutes(routes); err != nil {
			return fmt.Errorf("restore routes after restart: %w", err)
		}
		slog.Info("caddy routes restored after restart", "count", len(routes))
	}

	return nil
}

// IsAlive checks if the Caddy process is running.
func (pm *ProcessManager) IsAlive() bool {
	pid, err := pm.readPIDFile()
	if err != nil {
		return false
	}
	return pm.isProcessAlive(pid)
}

// Monitor starts a background goroutine that checks Caddy health.
func (pm *ProcessManager) Monitor(ctx context.Context, onCrash func()) {
	go func() {
		ticker := time.NewTicker(monitorInterval)
		defer ticker.Stop()

		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !pm.IsAlive() {
					slog.Error("caddy process died, restarting")
					if onCrash != nil {
						onCrash()
					}
					if err := pm.Start(ctx); err != nil {
						slog.Error("failed to restart caddy", "error", err)
					}
				}
			}
		}
	}()
}

func (pm *ProcessManager) killProcess() {
	if pm.cmd != nil && pm.cmd.Process != nil {
		_ = pm.cmd.Process.Kill()
	}
}

func (pm *ProcessManager) isProcessAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

func (pm *ProcessManager) readPIDFile() (int, error) {
	data, err := os.ReadFile(pm.cfg.pidFile())
	if err != nil {
		return 0, err
	}
	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return 0, fmt.Errorf("invalid PID: %q", pidStr)
	}
	return pid, nil
}

func (pm *ProcessManager) writePIDFile(pid int) error {
	return os.WriteFile(pm.cfg.pidFile(), []byte(strconv.Itoa(pid)), 0644)
}

func (pm *ProcessManager) removePIDFile() {
	os.Remove(pm.cfg.pidFile())
}

func (pm *ProcessManager) waitForAdminAPI(ctx context.Context) error {
	deadline := time.Now().Add(adminReadyTimeout)
	client := &http.Client{Timeout: 2 * time.Second}

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := client.Get(pm.cfg.AdminURL + "/")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
		}
		time.Sleep(adminPollInterval)
	}
	return fmt.Errorf("caddy admin API not ready after %v", adminReadyTimeout)
}

func (pm *ProcessManager) ensureConfig() error {
	configPath := pm.cfg.configFile()
	if _, err := os.Stat(configPath); err == nil {
		return nil
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(pm.cfg.DataDir, 0755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	// Ensure certificate directory exists
	if err := os.MkdirAll(pm.cfg.CertDir, 0755); err != nil {
		return fmt.Errorf("create cert dir: %w", err)
	}

	slog.Info("generating initial caddy config", "path", configPath)

	acmeCA := "https://acme-staging-v02.api.letsencrypt.org/directory"
	if !pm.cfg.UseStagingEnabled() {
		acmeCA = "https://acme-v02.api.letsencrypt.org/directory"
	}

	initialConfig := map[string]interface{}{
		"admin": map[string]interface{}{
			"listen": "localhost:2019",
		},
		"storage": map[string]interface{}{
			"module": "file_system",
			"root":   pm.cfg.CertDir,
		},
		"apps": map[string]interface{}{
			"tls": map[string]interface{}{
				"automation": map[string]interface{}{
					"policies": []interface{}{
						map[string]interface{}{
							"issuers": []interface{}{
								map[string]interface{}{
									"module": "acme",
									"email":  pm.cfg.ACMEmail,
									"ca":     acmeCA,
								},
							},
						},
					},
				},
			},
			"http": map[string]interface{}{
				"servers": map[string]interface{}{
					"main": map[string]interface{}{
						"listen": []string{":443"},
						"routes": []interface{}{},
						"tls_connection_policies": []interface{}{
							map[string]interface{}{},
						},
						"on_demand": map[string]interface{}{
							"ask": "http://localhost:2020/__yourplatform_ask",
						},
					},
					"redirect": map[string]interface{}{
						"listen": []string{":80"},
						"routes": []interface{}{
							map[string]interface{}{
								"match": []interface{}{
									map[string]interface{}{
										"path": []string{"/*"},
									},
								},
								"handle": []interface{}{
									map[string]interface{}{
										"handler": "static_response",
										"status_code": 308,
										"headers": map[string]interface{}{
											"Location": []interface{}{
												"https://{http.request.host}{http.request.uri}",
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

	data, err := json.MarshalIndent(initialConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal initial config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("write initial config: %w", err)
	}

	return nil
}
