package main

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/yourname/yourplatform/control-plane/internal/api"
	"github.com/yourname/yourplatform/control-plane/internal/config"
	"github.com/yourname/yourplatform/control-plane/internal/db"
	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
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
	go pruneMetrics(database)

	router := api.NewRouter(database, cfg, hub)

	addr := fmt.Sprintf(":%s", cfg.Port)
	slog.Info("control plane starting", "addr", addr, "env", cfg.Env)

	if err := http.ListenAndServe(addr, router); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}

// pruneMetrics runs every 6 hours and deletes raw metrics older than 7 days.
func pruneMetrics(db *sql.DB) {
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().Add(-7 * 24 * time.Hour).UTC().Format(time.RFC3339)
		if err := queries.DeleteMetricsBefore(db, "", cutoff); err != nil {
			slog.Warn("failed to prune metrics_history", "error", err)
		} else {
			slog.Info("pruned metrics_history before", "cutoff", cutoff)
		}
	}
}
