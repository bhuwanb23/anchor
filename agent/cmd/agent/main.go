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

func main() {
	args := os.Args[1:]

	if len(args) > 0 {
		switch args[0] {
		case "preflight":
			runPreflight()
			return
		case "--version", "-v":
			fmt.Printf("yourplatform-agent %s\n", Version)
			return
		case "run":
			// fall through to main run loop
		}
	}

	run()
}

func run() {
	cfg, err := config.Load("")
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

	for {
		select {
		case <-ctx.Done():
			slog.Info("shutting down agent")
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
}
