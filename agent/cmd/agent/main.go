package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
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
			runPreflight()
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

	slog.Info("agent starting",
		"version", Version,
		"control_plane", cfg.ControlPlaneURL,
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

	wsClient := ws.NewClient(cfg.ControlPlaneURL, cfg.AgentToken, cfg.WSReconnectSec)

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

func runPreflight() {
	results := preflight.RunAll()
	preflight.PreflightLog(results)
	if preflight.HasErrors(results) {
		os.Exit(1)
	}
}
