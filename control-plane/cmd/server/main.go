package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/yourname/yourplatform/control-plane/internal/alerts"
	"github.com/yourname/yourplatform/control-plane/internal/api"
	"github.com/yourname/yourplatform/control-plane/internal/config"
	"github.com/yourname/yourplatform/control-plane/internal/db"
	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
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
	go pruneMetrics(database)
	go rollupMetrics(database)

	// Layer 5A Step 4A Level 3 — delete expired refresh tokens weekly.
	go pruneExpiredRefreshTokens(database)

	// Layer 5A Step 7A — expired password-reset tokens weekly.
	go pruneExpiredPasswordResets(database)

	// Layer 5C Step 2 — expired registration tokens daily.
	go pruneExpiredRegistrationTokens(database)

	// Layer 5C Step 2 — expired pending commands daily.
	go pruneExpiredPendingCommands(database)

	// Layer 5C Step 2 — old server events (90 days) weekly.
	go pruneOldEvents(database)

	// Layer 5C Step 2 — VACUUM weekly.
	go vacuumDatabase(database)

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

// pruneExpiredRefreshTokens deletes refresh tokens whose expiry has passed,
// running weekly. It uses the stored RFC3339 expires_at for comparison.
func pruneExpiredRefreshTokens(db *sql.DB) {
	cutoff := time.Now().UTC().Format(time.RFC3339)
	if n, err := queries.DeleteExpiredRefreshTokens(db, cutoff); err != nil {
		slog.Warn("failed to prune expired refresh tokens", "error", err)
	} else if n > 0 {
		slog.Info("pruned expired refresh tokens", "count", n)
	}

	ticker := time.NewTicker(7 * 24 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().UTC().Format(time.RFC3339)
		if n, err := queries.DeleteExpiredRefreshTokens(db, cutoff); err != nil {
			slog.Warn("failed to prune expired refresh tokens", "error", err)
		} else if n > 0 {
			slog.Info("pruned expired refresh tokens", "count", n)
		}
	}
}

// pruneExpiredPasswordResets deletes password-reset tokens whose 1-hour
// expiry has passed, running weekly (Layer 5A Step 7A).
func pruneExpiredPasswordResets(db *sql.DB) {
	cutoff := time.Now().UTC().Format(time.RFC3339)
	if n, err := queries.DeleteExpiredPasswordResets(db, cutoff); err != nil {
		slog.Warn("failed to prune expired password resets", "error", err)
	} else if n > 0 {
		slog.Info("pruned expired password resets", "count", n)
	}

	ticker := time.NewTicker(7 * 24 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().UTC().Format(time.RFC3339)
		if n, err := queries.DeleteExpiredPasswordResets(db, cutoff); err != nil {
			slog.Warn("failed to prune expired password resets", "error", err)
		} else if n > 0 {
			slog.Info("pruned expired password resets", "count", n)
		}
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

// rollupMetrics runs hourly and aggregates raw metrics into hourly averages,
// then deletes raw metrics older than 7 days.
func rollupMetrics(db *sql.DB) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		if n, err := queries.RollupHourlyMetrics(db); err != nil {
			slog.Warn("failed to rollup hourly metrics", "error", err)
		} else if n > 0 {
			slog.Info("rolled up hourly metrics", "rows", n)
		}
		if n, err := queries.DeleteOldRawMetrics(db); err != nil {
			slog.Warn("failed to delete old raw metrics", "error", err)
		} else if n > 0 {
			slog.Info("deleted old raw metrics", "rows", n)
		}
	}
}

// pruneExpiredRegistrationTokens runs daily and deletes unused tokens past expiry.
func pruneExpiredRegistrationTokens(db *sql.DB) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		if err := queries.DeleteExpiredRegistrationTokens(db); err != nil {
			slog.Warn("failed to prune expired registration tokens", "error", err)
		} else {
			slog.Info("pruned expired registration tokens")
		}
	}
}

// pruneExpiredPendingCommands runs daily and deletes commands past their expiry.
func pruneExpiredPendingCommands(db *sql.DB) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		if n, err := queries.DeleteExpiredPendingCommands(db); err != nil {
			slog.Warn("failed to prune expired pending commands", "error", err)
		} else if n > 0 {
			slog.Info("pruned expired pending commands", "count", n)
		}
	}
}

// pruneOldEvents runs weekly and deletes server events older than 90 days.
func pruneOldEvents(db *sql.DB) {
	ticker := time.NewTicker(7 * 24 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		if n, err := queries.DeleteOldEvents(db, 90); err != nil {
			slog.Warn("failed to prune old server events", "error", err)
		} else if n > 0 {
			slog.Info("pruned old server events", "count", n)
		}
	}
}

// vacuumDatabase runs weekly and reclaims space from deleted rows.
func vacuumDatabase(db *sql.DB) {
	ticker := time.NewTicker(7 * 24 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		if _, err := db.Exec("VACUUM"); err != nil {
			slog.Warn("failed to vacuum database", "error", err)
		} else {
			slog.Info("vacuumed database")
		}
	}
}
