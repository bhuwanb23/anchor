package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/yourname/yourplatform/agent/internal/backup"
	"github.com/yourname/yourplatform/agent/internal/caddy"
	"github.com/yourname/yourplatform/agent/internal/config"
	"github.com/yourname/yourplatform/agent/internal/docker"
	"github.com/yourname/yourplatform/agent/internal/executor"
	"github.com/yourname/yourplatform/agent/internal/preflight"
	"github.com/yourname/yourplatform/agent/internal/ws"
)

const Version = "0.1.0-dev"

const connectedFile = "/var/lib/yourplatform/agent.connected"

var agentVersion = Version // overridden at runtime

func main() {
	args := os.Args[1:]
	configPath := ""

	for i := 0; i < len(args); i++ {
		switch {
	case args[i] == "preflight":
		useJSON := false
		if i+1 < len(args) && args[i+1] == "--json" {
			useJSON = true
			i++
		}
		runPreflight(useJSON)
		return
		case args[i] == "--version" || args[i] == "-v":
			fmt.Printf("yourplatform-agent %s\n", Version)
			return
		case args[i] == "--config" && i+1 < len(args):
			configPath = args[i+1]
			i++
		case args[i] == "run":
			// continue to run loop
		}
	}

	run(configPath)
}

func run(configPath string) {
	cfg, err := config.Load(configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	if err := cfg.Validate(); err != nil {
		slog.Error("invalid config", "error", err)
		os.Exit(1)
	}

	if cfg.NeedsRegistration() {
		slog.Info("registration token detected, registering with control plane")
		if err := registerAgent(cfg, configPath); err != nil {
			slog.Error("registration failed", "error", err)
			os.Exit(1)
		}
		cfg, err = config.Load(configPath)
		if err != nil {
			slog.Error("failed to reload config after registration", "error", err)
			os.Exit(1)
		}
		slog.Info("registration successful", "agent_id", cfg.AgentID, "server_id", cfg.ServerID)
	}

	slog.Info("agent starting",
		"version", Version,
		"control_plane", cfg.ControlPlaneURL,
		"agent_id", cfg.AgentID,
		"server_id", cfg.ServerID,
	)

	dockerClient, err := docker.NewClient(cfg.DockerSocket)
	if err != nil {
		slog.Error("failed to create docker client", "error", err)
		os.Exit(1)
	}

	// Create image cache for smart pull decisions (digest-based caching)
	imageCache, err := docker.NewImageCache("/var/lib/yourplatform/image_cache.json")
	if err != nil {
		slog.Warn("failed to create image cache, continuing without cache", "error", err)
		imageCache = nil
	}

	caddyManager := caddy.NewManager("http://localhost:2019")
	backupManager := backup.NewManager(cfg.BackupDest)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	wsURL := cfg.ControlPlaneURL + "/ws/agent"
	wsClient := ws.NewClient(wsURL, cfg.AgentID, cfg.AgentSecret, cfg.WSReconnectSec)

	exec := executor.New(dockerClient, caddyManager, backupManager).
		WithImageCache(imageCache).
		WithProgressReporter(&wsProgressReporter{client: wsClient})

	// Start background Docker health monitor
	// This ensures the agent survives Docker daemon restarts
	// and reconnects automatically when Docker comes back.
	go monitorDockerHealth(ctx, dockerClient, wsClient)

	// Start background image cleanup (weekly schedule + disk-pressure triggers)
	go dockerClient.RunScheduledCleanup(ctx, nil, 0)

	// Run orphan network cleanup on startup
	// Removes any leftover networks from deleted projects
	go func() {
		if err := dockerClient.CleanupOrphanedNetworks(ctx); err != nil {
			slog.Warn("orphan network cleanup failed", "error", err)
		} else {
			slog.Info("orphan network cleanup completed")
		}
	}()

	go wsClient.Run(ctx)

	connected := false

	for {
		select {
		case <-ctx.Done():
			slog.Info("shutting down agent")
			if connected {
				os.Remove(connectedFile)
				slog.Info("removed agent.connected file")
			}
			wsClient.Close()
			return
		case msg, ok := <-wsClient.Recv():
			if !ok {
				continue
			}

			switch msg.Type {
			case "command":
				var cmd executor.Command
				if err := json.Unmarshal(msg.Payload, &cmd); err != nil {
					slog.Error("failed to parse command", "error", err)
					continue
				}

				result := exec.Execute(ctx, cmd)

				if err := wsClient.SendJSON(result); err != nil {
					slog.Error("failed to send result", "error", err)
				}

			case "register_ack":
				slog.Info("registered with control plane", "server_id", cfg.ServerID)
				if !connected {
					if err := os.WriteFile(connectedFile, []byte(cfg.ServerID), 0644); err != nil {
						slog.Warn("failed to write connected file", "error", err)
					} else {
						slog.Info("wrote agent.connected file")
					}
					connected = true

					// Run pre-flight on startup and send results to control plane
					go func() {
						preflightResult := preflight.RunAll()
						if err := wsClient.SendJSON(map[string]interface{}{
							"type":          "preflight_result",
							"system_info":   preflightResult.SystemInfo,
							"passed":        preflightResult.Passed,
							"warnings":      preflightResult.Warnings(),
							"auto_fixed":    preflightResult.AutoFixed,
							"checks":        preflightResult.Checks,
						}); err != nil {
							slog.Warn("failed to send preflight result", "error", err)
						} else {
							slog.Info("sent preflight results to control plane")
						}
					}()
				}

			case "heartbeat":
				slog.Debug("heartbeat received")

			default:
				slog.Warn("unknown message type", "type", msg.Type)
			}
		}
	}
}

func registerAgent(cfg *config.Config, configPath string) error {
	// Run pre-flight checks and include results in registration
	preflightResult := preflight.RunAll()

	ip := detectIP()

	body := map[string]interface{}{
		"token":        cfg.RegistrationToken,
		"system_info":  preflightResult.SystemInfo,
		"ip_address":   ip,
		"warnings":     preflightResult.Warnings(),
		"auto_fixed":   preflightResult.AutoFixed,
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	url := cfg.ControlPlaneURL + "/api/v1/agent/register"
	resp, err := http.Post(url, "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("POST %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		var errResp struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&errResp)
		return fmt.Errorf("registration failed (%d): %s", resp.StatusCode, errResp.Error)
	}

	var result struct {
		AgentID     string `json:"agent_id"`
		AgentSecret string `json:"agent_secret"`
		ServerID    string `json:"server_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	if err := config.SaveCredentials(configPath, result.AgentID, result.AgentSecret, result.ServerID); err != nil {
		return fmt.Errorf("save credentials: %w", err)
	}

	return nil
}

// monitorDockerHealth periodically checks Docker connectivity and
// attempts reconnection with backoff when the daemon is unreachable.
// This ensures the agent survives Docker daemon restarts without crashing
// and reports availability changes to the control plane.
func monitorDockerHealth(ctx context.Context, dockerClient *docker.Client, wsClient *ws.Client) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	wasConnected := dockerClient.IsConnected()

	// If initial connection test failed, attempt reconnect in background
	if !wasConnected {
		slog.Warn("docker daemon not connected on startup, will retry in background")
		reportDockerStatus(wsClient, "unavailable", "Docker daemon was unreachable on agent startup. Retrying...")
		go func() {
			if err := dockerClient.Reconnect(ctx); err != nil {
				slog.Error("initial docker reconnect failed", "error", err)
			} else {
				slog.Info("docker reconnected after initial failure")
				reportDockerStatus(wsClient, "connected",
					fmt.Sprintf("Docker reconnected (version %s)", dockerClient.DockerInfo().Version))
			}
		}()
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			isConnected := dockerClient.IsConnected()

			// State transition: connected -> disconnected
			if wasConnected && !isConnected {
				slog.Warn("docker daemon connection lost")
				reportDockerStatus(wsClient, "unavailable", "Docker daemon connection lost. Reconnecting...")

				// Attempt reconnect in background
				go func() {
					if err := dockerClient.Reconnect(ctx); err != nil {
						slog.Error("docker reconnect failed", "error", err)
					} else {
						slog.Info("docker reconnected")
						reportDockerStatus(wsClient, "connected",
							fmt.Sprintf("Docker reconnected (version %s)", dockerClient.DockerInfo().Version))
					}
				}()
			}

			// State transition: disconnected -> connected
			if !wasConnected && isConnected {
				slog.Info("docker daemon connection restored")
				reportDockerStatus(wsClient, "connected",
					fmt.Sprintf("Docker connection restored (version %s)", dockerClient.DockerInfo().Version))
			}

			wasConnected = isConnected
		}
	}
}

// reportDockerStatus sends a Docker availability event to the control plane.
func reportDockerStatus(wsClient *ws.Client, status, message string) {
	payload := map[string]interface{}{
		"type":    "docker_status",
		"status":  status,
		"message": message,
	}
	if err := wsClient.SendJSON(payload); err != nil {
		slog.Warn("failed to send docker status", "error", err)
	}
}

func detectIP() string {
	out, err := exec.Command("hostname", "-I").Output()
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(out))
	if len(fields) > 0 {
		return fields[0]
	}
	return ""
}

func runPreflight(useJSON bool) {
	result := preflight.RunAll()

	if useJSON {
		// Compact JSON for machine parsing by install.sh
		jsonStr, err := result.ToJSONCompact()
		if err != nil {
			slog.Error("failed to serialize preflight result", "error", err)
			os.Exit(1)
		}
		fmt.Print(jsonStr)
	} else {
		// Human-readable text output
		fmt.Print(result.Text())
	}

	if result.HasBlockingFailures() {
		os.Exit(1)
	}
}

// wsProgressReporter sends image pull progress updates to the control plane
// via the agent's WebSocket connection.
type wsProgressReporter struct {
	client *ws.Client
}

func (r *wsProgressReporter) ReportProgress(p docker.PullProgress) {
	payload := map[string]interface{}{
		"type":     "pull_progress",
		"image_id": p.ID,
		"status":   p.Status,
		"stream":   p.Stream,
		"current":  p.Current,
		"total":    p.Total,
	}
	if err := r.client.SendJSON(payload); err != nil {
		slog.Debug("failed to send pull progress", "error", err)
	}
}
