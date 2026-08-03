package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yourname/yourplatform/agent/internal/backup"
	"github.com/yourname/yourplatform/agent/internal/caddy"
	"github.com/yourname/yourplatform/agent/internal/config"
	"github.com/yourname/yourplatform/agent/internal/docker"
	"github.com/yourname/yourplatform/agent/internal/executor"
	"github.com/yourname/yourplatform/agent/internal/lifecycle"
	"github.com/yourname/yourplatform/agent/internal/preflight"
	"github.com/yourname/yourplatform/agent/internal/state"
	"github.com/yourname/yourplatform/agent/internal/update"
	"github.com/yourname/yourplatform/agent/internal/version"
	"github.com/yourname/yourplatform/agent/internal/ws"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "version", "--version", "-v":
		fmt.Println(version.Version)
		return
	case "preflight":
		os.Exit(runPreflight())
	case "run":
		os.Exit(runAgent(os.Args[2:]))
	default:
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "Usage: yourplatform-agent <run|preflight|version> [flags]\n")
	fmt.Fprintf(os.Stderr, "  run --config <path>   Start the agent\n")
	fmt.Fprintf(os.Stderr, "  preflight             Run environment checks\n")
	fmt.Fprintf(os.Stderr, "  version               Print agent version\n")
}

func runPreflight() int {
	result := preflight.RunAll()
	preflight.PreflightLog(result)
	out, _ := result.ToJSON()
	fmt.Println(out)
	if !result.Passed {
		return 1
	}
	return 0
}

func runAgent(args []string) int {
	configPath := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--config" && i+1 < len(args) {
			configPath = args[i+1]
			i++
		}
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		return 1
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "invalid config: %v\n", err)
		return 1
	}

	setupLogging(cfg.LogLevel)
	slog.Info("startup step 1 complete: config loaded")

	dataDir := state.DefaultStateDir
	stateMgr := state.NewManager(dataDir)
	st := stateMgr.GetState()
	unclean := stateMgr.WasUncleanShutdown()
	if unclean {
		slog.Warn("detected unclean shutdown, will reconcile", "shutdown_clean", st.ShutdownClean)
	}
	_ = stateMgr.MarkStartup(version.Version)
	slog.Info("startup step 2 complete: logging and state loaded")

	// Step 3: preflight
	pfResult := preflight.RunAll()
	preflight.PreflightLog(pfResult)
	if !pfResult.Passed {
		slog.Error("preflight failed with blocking errors")
		return 1
	}
	slog.Info("startup step 3 complete: preflight passed")

	resolvedConfigPath := configPath
	if resolvedConfigPath == "" {
		resolvedConfigPath = "/etc/yourplatform/config.yaml"
	}

	if cfg.NeedsRegistration() {
		reg, err := lifecycle.Register(cfg.ControlPlaneURL, cfg.RegistrationToken, pfResult)
		if err != nil {
			slog.Error("registration failed", "error", err)
			return 1
		}
		cfg.AgentID = reg.AgentID
		cfg.AgentSecret = reg.AgentSecret
		cfg.ServerID = reg.ServerID
		if err := config.SaveCredentials(resolvedConfigPath, reg.AgentID, reg.AgentSecret, reg.ServerID); err != nil {
			slog.Warn("failed to persist credentials", "error", err)
		}
		slog.Info("registered with control plane", "server_id", reg.ServerID)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Step 4: Caddy
	adminURL := fmt.Sprintf("http://localhost:%d", cfg.CaddyAdminPort)
	caddyMgr := caddy.NewManager(adminURL)
	caddyProc := caddy.NewProcessManager(caddy.ProcessConfig{
		BinaryPath: cfg.CaddyBinaryPath,
		DataDir:    cfg.CaddyDataDir,
		AdminURL:   adminURL,
		ACMEmail:   cfg.CaddyACMEmail,
		UseStaging: cfg.CaddyUseStaging,
		CertDir:    cfg.CaddyCertDir,
	}, caddyMgr)
	if err := caddyProc.Start(ctx); err != nil {
		slog.Error("caddy start failed", "error", err)
		return 1
	}
	slog.Info("startup step 4 complete: caddy started")

	if _, _, err := state.ReconcileCaddy(ctx, stateMgr, caddyMgr); err != nil {
		slog.Warn("caddy route restore failed", "error", err)
	} else {
		slog.Info("caddy routes restored from state")
	}

	// Step 5: Docker
	dockerClient, err := docker.NewClient(cfg.DockerSocket)
	if err != nil {
		slog.Error("docker connect failed", "error", err)
		return 1
	}
	if _, err := state.Reconcile(ctx, stateMgr, dockerClient); err != nil {
		slog.Warn("docker reconcile failed", "error", err)
	}
	slog.Info("startup step 5 complete: docker reconciled")

	// Backup manager
	var backupMgr *backup.BackupManager
	if cfg.BackupDest != "" {
		backupMgr = backup.NewManagerWithConfig(backup.BackupConfig{
			Destination: cfg.BackupDest,
			DataDir:     dataDir,
			ServerID:    cfg.ServerID,
		})
		backupMgr.WithStateManager(backupStateAdapter{stateMgr})
		if err := backupMgr.Initialize(ctx); err != nil {
			slog.Warn("backup init failed", "error", err)
		}
	} else {
		backupMgr = backup.NewManager("")
	}

	exec := executor.New(dockerClient, caddyMgr, backupMgr).
		WithStateManager(stateMgr).
		WithServerID(cfg.ServerID)

	wsURL := lifecycle.WSURLFromControlPlane(cfg.ControlPlaneURL)
	wsClient := ws.NewClient(wsURL, cfg.AgentID, cfg.AgentSecret, cfg.WSReconnectSec)
	backupReporter := backup.NewBackupReporter(wsClient)
	exec.WithBackupReporter(backupReporter)

	var scheduler *backup.BackupScheduler
	if cfg.BackupDest != "" && backupMgr != nil {
		scheduler = backup.NewBackupScheduler(backupMgr, dataDir, cfg.ServerID, nil).
			WithStateManager(backupStateAdapter{stateMgr}).
			WithReporter(backupReporter)
		if repo := backupMgr.GetRepository(); repo != nil {
			scheduler.WithMaintenance(repo)
		}
		exec.WithScheduler(scheduler)
		go scheduler.Start(ctx)
		slog.Info("startup step 7 complete: backup scheduler started")
	}

	connMgr := lifecycle.NewManager(wsClient, exec, stateMgr, dataDir, cfg.ServerID)
	connMgr.SetPreflightResult(pfResult)

	// Self-update
	updater := update.New(update.Config{
		ControlPlaneURL: cfg.ControlPlaneURL,
		CurrentVersion:  version.Version,
		BinaryPath:      agentBinaryPath(),
		WSClient:        wsClient,
		Interval:        time.Hour,
	})
	connMgr.OnUpdateAvailable(func(ver string) {
		go func() {
			if err := updater.ApplyVersion(ctx, ver); err != nil {
				slog.Warn("update from push failed", "error", err)
			}
		}()
	})
	go updater.Start(ctx)

	slog.Info("startup step 6: starting connection manager")
	go connMgr.Run(ctx)

	slog.Info("startup step 8-10 complete: agent operational", "version", version.Version)

	<-ctx.Done()
	slog.Info("shutdown signal received")

	connMgr.StopAccepting()
	connMgr.WaitInFlight(60 * time.Second)
	connMgr.NotifyShutdown()
	lifecycle.ClearConnected(dataDir)
	_ = stateMgr.MarkCleanShutdown()
	wsClient.Close()
	if scheduler != nil {
		scheduler.Stop()
	}
	// Do NOT stop Caddy or Docker containers
	slog.Info("agent exited cleanly")
	return 0
}

func setupLogging(level string) {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	slog.SetDefault(slog.New(h))
}

func agentBinaryPath() string {
	p, err := os.Executable()
	if err != nil {
		return "/usr/local/bin/yourplatform-agent"
	}
	return p
}

// backupStateAdapter adapts state.Manager to backup.StateManager.
type backupStateAdapter struct {
	m *state.Manager
}

func (a backupStateAdapter) GetState() *backup.StateData {
	st := a.m.GetState()
	projects := make(map[string]interface{}, len(st.Projects))
	for k, v := range st.Projects {
		projects[k] = v
	}
	return &backup.StateData{Projects: projects}
}

func (a backupStateAdapter) GetLastBackupTime() time.Time {
	st := a.m.GetState()
	if st.Backup == nil || st.Backup.LastBackupAt == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339, st.Backup.LastBackupAt)
	return t
}

func (a backupStateAdapter) RecordBackupCompletion(snapshotID string, duration time.Duration, totalBytes int64) error {
	return a.m.RecordBackupCompletion(snapshotID, duration, totalBytes)
}
