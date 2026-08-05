package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/yourname/yourplatform/control-plane/internal/alerts"
	"github.com/yourname/yourplatform/control-plane/internal/api/handlers"
	appmiddleware "github.com/yourname/yourplatform/control-plane/internal/api/middleware"
	"github.com/yourname/yourplatform/control-plane/internal/config"
	"github.com/yourname/yourplatform/control-plane/internal/dns"
	"github.com/yourname/yourplatform/control-plane/internal/mailer"
	"github.com/yourname/yourplatform/control-plane/internal/ratelimit"
	"github.com/yourname/yourplatform/control-plane/internal/ws"
	"log/slog"
)

func NewRouter(database *sql.DB, cfg *config.Config, hub *ws.Hub, delivery *alerts.Delivery, sender mailer.Sender) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	// Security headers first so even CORS-short-circuited preflight (OPTIONS)
	// responses carry them — "every response" (Layer 5A Step 8B).
	r.Use(appmiddleware.SecurityHeaders)
	r.Use(appmiddleware.CORS(cfg.FrontendURL))

	r.Get("/health", handlers.Health)
	r.Get("/install.sh", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./scripts/install.sh")
	})

	r.Get("/ws/agent", ws.HandleAgentWS(hub, database, cfg.BaseDomain, delivery))
	r.Get("/ws/browser", ws.HandleBrowserWS(hub, database, cfg.JWTSecret))

	// Internal hub stats endpoint (no auth required, internal use only).
	r.Get("/internal/hub/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		stats := hub.Stats()
		json.NewEncoder(w).Encode(stats)
	})

	releaseDir := filepath.Join(".", "release")
	r.Get("/releases/latest.json", handlers.LatestRelease)
	r.Handle("/releases/*", http.StripPrefix("/releases/", http.FileServer(http.Dir(releaseDir))))

	// Create DNS client if Cloudflare credentials are configured
	var dnsClient *dns.Client
	if cfg.DNSConfigured() {
		dnsClient = dns.NewClient(cfg.CloudflareToken, cfg.CloudflareZoneID)
	}

	authHandler := &handlers.Auth{DB: database, Cfg: cfg, Mailer: sender, Limiter: ratelimit.New()}
	server := &handlers.Server{DB: database}
	tokenHandler := &handlers.Token{DB: database}
	agentHandler := &handlers.Agent{DB: database, DNS: dnsClient, Config: cfg}
	customDomainHandler := &handlers.CustomDomain{DB: database, Hub: hub}
	backupHandler := &handlers.Backup{DB: database, Hub: hub}
	teamHandler := &handlers.Teams{DB: database, Mailer: sender, Logger: slog.Default()}

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/register", authHandler.Register)
		r.Post("/auth/login", authHandler.Login)
		r.Post("/auth/refresh", authHandler.Refresh)
		r.Post("/auth/forgot-password", authHandler.ForgotPassword)
		r.Post("/auth/reset-password", authHandler.ResetPassword)
		r.Post("/agent/register", agentHandler.Register)

		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.Auth(database, cfg.JWTSecret))
			r.Get("/auth/me", authHandler.Me)
			r.Post("/auth/logout", authHandler.Logout)
			r.Post("/auth/logout-all", authHandler.LogoutAll)
			r.Get("/auth/sessions", authHandler.Sessions)
			r.Delete("/auth/sessions/{sessionID}", authHandler.DeleteSession)
			r.Get("/servers", server.ListServers)
			r.Post("/servers", server.CreateServer)
			r.Delete("/servers/{serverID}", server.DeleteServer)
			r.Get("/servers/{serverID}/events", server.ListEvents)
			r.Get("/servers/{serverID}/alerts", server.ListAlerts)
			r.Post("/servers/{serverID}/alerts/{alertID}/ack", server.AcknowledgeAlert)
			r.Get("/alerts", server.ListAllAlerts)
			r.Post("/alerts/read", server.MarkAllAlertsRead)
			r.Post("/servers/registration-token", tokenHandler.CreateRegistrationToken)
			r.Post("/deploy", handlers.MakeDeployApp(database, cfg, hub))
			r.Get("/deployments", handlers.GetDeploymentStatus)
			r.Post("/servers/{serverID}/deployments/{deploymentID}/domains", customDomainHandler.AddDomain)
			r.Post("/servers/{serverID}/deployments/{deploymentID}/domains/{domainID}/verify", customDomainHandler.VerifyDomain)
			r.Delete("/servers/{serverID}/deployments/{deploymentID}/domains/{domainID}", customDomainHandler.RemoveDomain)

			// Backup management routes
			r.Get("/servers/{serverID}/backup/config", backupHandler.GetBackupConfig)
			r.Put("/servers/{serverID}/backup/config", backupHandler.UpdateBackupConfig)
			r.Get("/servers/{serverID}/backup/snapshots", backupHandler.GetBackupSnapshots)
			r.Get("/servers/{serverID}/backup/jobs", backupHandler.GetBackupJobs)
			r.Post("/servers/{serverID}/backup/trigger", backupHandler.TriggerBackup)
			r.Get("/servers/{serverID}/backup/history", backupHandler.GetBackupHistory)
			r.Get("/servers/{serverID}/backup/schedule", backupHandler.GetBackupSchedule)
			r.Put("/servers/{serverID}/backup/schedule", backupHandler.UpdateBackupSchedule)
			r.Get("/servers/{serverID}/backup/usage", backupHandler.GetBackupUsage)
			r.Post("/servers/{serverID}/backup/restore", backupHandler.TriggerRestore)
			r.Get("/servers/{serverID}/backup/restores", backupHandler.GetRestoreHistory)
			r.Get("/servers/{serverID}/backup/verification", backupHandler.GetBackupVerificationStatus)
			r.Post("/servers/{serverID}/backup/verification/trigger", backupHandler.TriggerBackupVerification)
			r.Get("/servers/{serverID}/backup/storage/stats", backupHandler.GetStorageStats)
			r.Post("/servers/{serverID}/backup/storage/maintenance", backupHandler.TriggerMaintenance)

			// Team management routes
			r.Get("/teams", teamHandler.ListTeams)
			r.Post("/teams", teamHandler.CreateTeam)
			r.Get("/teams/{teamID}", teamHandler.GetTeam)
			r.Put("/teams/{teamID}", teamHandler.UpdateTeam)
			r.Delete("/teams/{teamID}", teamHandler.DeleteTeam)
			r.Post("/teams/{teamID}/transfer-ownership", teamHandler.TransferOwnership)
			r.Get("/teams/{teamID}/members", teamHandler.ListMembers)
			r.Put("/teams/{teamID}/members/{memberID}/role", teamHandler.UpdateMemberRole)
			r.Delete("/teams/{teamID}/members/{memberID}", teamHandler.RemoveMember)
			r.Post("/teams/{teamID}/invite", teamHandler.SendInvitation)
			r.Post("/invitations/{token}/accept", teamHandler.AcceptInvitation)
		})
	})

	return r
}
