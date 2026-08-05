package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/joho/godotenv"
	"github.com/yourname/yourplatform/control-plane/internal/alerts"
	"github.com/yourname/yourplatform/control-plane/internal/api"
	"github.com/yourname/yourplatform/control-plane/internal/config"
	"github.com/yourname/yourplatform/control-plane/internal/db"
	"github.com/yourname/yourplatform/control-plane/internal/mailer"
	"github.com/yourname/yourplatform/control-plane/internal/ws"
)

func main() {
	if err := godotenv.Load(); err != nil {
		slog.Warn("no .env file found, using environment variables")
	}

	cfg := config.Load()

	database, err := db.Open(cfg.DatabasePath)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	if err := db.Migrate(database); err != nil {
		slog.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	hub := ws.NewHub()
	go hub.StartHeartbeat(database)
	go hub.StartMetricsLogger()

	// Layer 5C Step 4 — data lifecycle: metrics rollup (raw → hourly → daily),
	// retention tiers (7 days / 30 days / 12 months), expired-token and
	// command cleanup, event/email retention, VACUUM, and DB-size monitoring.
	// One goroutine wakes every hour and runs the daily jobs at UTC midnight.
	go db.StartCleanup(database, cfg.DatabasePath)

	// Layer 4C Step 6 — alert email delivery. Runs in the background and
	// never blocks the agent/metrics paths.
	sender := mailer.NewFromConfig(cfg)
	delivery := alerts.NewDelivery(database, sender, cfg)
	go delivery.Run(context.Background())

	router := api.NewRouter(database, cfg, hub, delivery, sender)

	addr := fmt.Sprintf(":%s", cfg.Port)
	slog.Info("control plane starting", "addr", addr, "env", cfg.Env)

	if err := http.ListenAndServe(addr, router); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}
