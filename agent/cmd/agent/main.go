package main

import (
	"log/slog"
	"os"

	"github.com/yourname/yourplatform/agent/internal/config"
)

func main() {
	args := os.Args[1:]

	if len(args) > 0 {
		switch args[0] {
		case "preflight":
			runPreflight(args[1:])
			return
		case "--version":
			printVersion()
			return
		case "run":
			// fall through to main run loop
		}
	}

	cfg, err := config.Load("/etc/yourplatform/config.yaml")
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	slog.Info("agent starting",
		"version", Version,
		"control_plane", cfg.ControlPlaneURL,
	)

	slog.Info("agent base skeleton running — layers to be added")
	select {}
}

func runPreflight(args []string) {
	slog.Info("preflight check placeholder")
}

func printVersion() {
	slog.Info("version", "version", Version)
}

const Version = "0.1.0-dev"