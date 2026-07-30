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
	"runtime"
	"strconv"
	"strings"
	"syscall"

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

	caddyManager := caddy.NewManager("http://localhost:2019")
	backupManager := backup.NewManager(cfg.BackupDest)
	exec := executor.New(dockerClient, caddyManager, backupManager)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	wsURL := cfg.ControlPlaneURL + "/ws/agent"
	wsClient := ws.NewClient(wsURL, cfg.AgentID, cfg.AgentSecret, cfg.WSReconnectSec)

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
	info := collectServerInfo()

	body := map[string]interface{}{
		"token": cfg.RegistrationToken,
		"server_info": info,
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

type serverInfo struct {
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	RAMMB     int    `json:"ram_mb"`
	DiskGB    int    `json:"disk_gb"`
	IPAddress string `json:"ip_address"`
}

func collectServerInfo() serverInfo {
	info := serverInfo{
		OS:   detectOS(),
		Arch: runtime.GOARCH,
	}

	info.RAMMB = detectRAM()
	info.DiskGB = detectDisk()
	info.IPAddress = detectIP()

	return info
}

func detectOS() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return runtime.GOOS
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "ID=") {
			return strings.Trim(strings.TrimPrefix(line, "ID="), "\"")
		}
	}
	return runtime.GOOS
}

func detectRAM() int {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, _ := strconv.Atoi(fields[1])
				return kb / 1024
			}
		}
	}
	return 0
}

func detectDisk() int {
	out, err := exec.Command("df", "-BG", "/").Output()
	if err != nil {
		return 0
	}
	lines := strings.Split(string(out), "\n")
	if len(lines) < 2 {
		return 0
	}
	fields := strings.Fields(lines[1])
	if len(fields) >= 2 {
		gb, _ := strconv.Atoi(strings.TrimSuffix(fields[1], "G"))
		return gb
	}
	return 0
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
		jsonStr, err := result.ToJSON()
		if err != nil {
			slog.Error("failed to serialize preflight result", "error", err)
			os.Exit(1)
		}
		fmt.Println(jsonStr)
	} else {
		// Human-readable text output (used by install.sh and debugging)
		fmt.Print(result.Text())
	}

	if result.HasBlockingFailures() {
		os.Exit(1)
	}
}
