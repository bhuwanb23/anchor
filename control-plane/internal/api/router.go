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

	// Layer 6 Step 2 middleware stack, in plan order:
	// RequestID → RealIP → Logging → Recoverer → SecurityHeaders → CORS → Auth.
	// RealIP runs before Logging so the request log records the caller's real
	// IP (from X-Forwarded-For), not the proxy's.
	r.Use(appmiddleware.RequestID) // req-{12hex}, sets X-Request-ID response header
	r.Use(middleware.RealIP)
	r.Use(appmiddleware.Logging) // structured request log (Layer 6 Step 2B)
	r.Use(middleware.Recoverer)
	// Security headers first so even CORS-short-circuited preflight (OPTIONS)
	// responses carry them — "every response" (Layer 5A Step 8B / 6 Step 2F).
	r.Use(appmiddleware.SecurityHeaders)
	r.Use(appmiddleware.CORS(cfg.FrontendURL))

	// Unknown routes return 404 JSON and unknown methods on known paths
	// return 405 JSON (Layer 6 Step 1 done conditions) — not chi's default
	// plain-text page. Registered after middleware so they inherit the same
	// RequestID/CORS/security treatment as real routes.
	//
	// Tradeoff: chi's custom MethodNotAllowed API does not hand the handler
	// the list of allowed methods (only its default text handler receives
	// them), so this 405 response omits the RFC 7231 Allow header. Buffering
	// responses to recover it is not worth it — /releases/* streams large
	// binaries. Accepted and documented; revisit only if a client needs Allow.
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusNotFound, "not_found", "The requested resource was not found")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "The requested method is not allowed for this resource")
	})

	r.Get("/health", (&handlers.Health{DB: database, Hub: hub}).ServeHTTP)
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

	// /api/v1 — versioned API (Layer 6 Step 1 route layout): a public group
	// (no auth) for registration/login/refresh, then the protected group
	// behind the JWT middleware for everything else.

	// Create DNS client if Cloudflare credentials are configured
	var dnsClient *dns.Client
	if cfg.DNSConfigured() {
		dnsClient = dns.NewClient(cfg.CloudflareToken, cfg.CloudflareZoneID)
	}

	authHandler := &handlers.Auth{DB: database, Cfg: cfg, Mailer: sender, Limiter: ratelimit.New()}
	server := &handlers.Server{DB: database}
	tokenHandler := &handlers.Token{DB: database, Cfg: cfg}
	agentHandler := &handlers.Agent{DB: database, DNS: dnsClient, Config: cfg}
	customDomainHandler := &handlers.CustomDomain{DB: database, Hub: hub}
	backupHandler := &handlers.Backup{DB: database, Hub: hub}
	teamHandler := &handlers.Teams{DB: database, Mailer: sender, Logger: slog.Default()}
	step5 := &handlers.Step5{DB: database, Cfg: cfg}

	r.Route("/api/v1", func(r chi.Router) {
		// Public routes (no auth): registration, login, token refresh,
		// password recovery, and agent registration (registration-token auth).
		r.Post("/auth/register", authHandler.Register)
		r.Post("/auth/login", authHandler.Login)
		r.Post("/auth/refresh", authHandler.Refresh)
		r.Post("/auth/forgot-password", authHandler.ForgotPassword)
		r.Post("/auth/reset-password", authHandler.ResetPassword)
		r.Post("/agent/register", agentHandler.Register)

		// Protected routes — every route below runs through the JWT Auth
		// middleware (Layer 6 Step 1 route layout). Routes whose handlers are
		// implemented in later Layer 6 steps are registered against
		// handlers.NotImplemented (501) so the URL contract exists from day
		// one and the done condition "all routes are registered" holds.
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.Auth(database, cfg.JWTSecret))

			// --- Auth management ---
			r.Get("/auth/me", authHandler.Me) // legacy alias; plan URL is GET /user
			r.Post("/auth/logout", authHandler.Logout)
			r.Post("/auth/logout-all", authHandler.LogoutAll)
			r.Get("/auth/sessions", authHandler.Sessions)
			r.Delete("/auth/sessions/{sessionID}", authHandler.DeleteSession)

			// --- User ---
			r.Get("/user", authHandler.Me)
			r.Put("/user", handlers.NotImplemented)    // Layer 6 Step 5
			r.Delete("/user", handlers.NotImplemented) // Layer 6 Step 5

			// --- Teams ---
			r.Get("/teams", teamHandler.ListTeams)
			r.Post("/teams", teamHandler.CreateTeam)
			r.Get("/teams/{teamID}", teamHandler.GetTeam)
			r.Put("/teams/{teamID}", teamHandler.UpdateTeam)
			r.Delete("/teams/{teamID}", teamHandler.DeleteTeam)
			r.Post("/teams/{teamID}/transfer-ownership", teamHandler.TransferOwnership)
			r.Get("/teams/{teamID}/members", teamHandler.ListMembers)
			r.Put("/teams/{teamID}/members/{memberID}/role", teamHandler.UpdateMemberRole)
			r.Delete("/teams/{teamID}/members/{memberID}", teamHandler.RemoveMember)
			r.Post("/teams/{teamID}/invite", teamHandler.SendInvitation)        // legacy alias
			r.Post("/teams/{teamID}/invitations", teamHandler.SendInvitation) // plan URL
			r.Delete("/teams/{teamID}/invitations/{invitationID}", handlers.NotImplemented) // Layer 6 Step 5
			r.Post("/invitations/{token}/accept", teamHandler.AcceptInvitation) // legacy (token in URL)
			r.Post("/invitations/accept", handlers.NotImplemented)              // plan URL; token-in-body contract is Layer 6 Step 5

			// --- Servers ---
			r.Get("/servers", server.ListServers)
			r.Post("/servers", server.CreateServer)
			r.Get("/servers/{serverID}", step5.GetServer)
			r.Delete("/servers/{serverID}", server.DeleteServer)
			r.Post("/servers/registration-token", tokenHandler.CreateRegistrationToken)   // legacy alias
			r.Post("/servers/{serverID}/registration-token", step5.CreateServerRegistrationToken)
			r.Get("/servers/{serverID}/events", server.ListEvents)

			// --- Apps (Layer 6 Step 5 handlers) ---
			r.Get("/servers/{serverID}/apps", step5.ListApps)
			r.Post("/servers/{serverID}/apps", step5.CreateApp)
			r.Get("/servers/{serverID}/apps/{appID}", step5.GetApp)
			r.Patch("/servers/{serverID}/apps/{appID}", step5.UpdateAppSettings)
			r.Delete("/servers/{serverID}/apps/{appID}", step5.DeleteApp)

			// --- Deployments ---
			r.Post("/deploy", handlers.MakeDeployApp(database, cfg, hub)) // legacy alias
			r.Get("/deployments", handlers.GetDeploymentStatus)            // legacy alias
			r.Post("/servers/{serverID}/apps/{appID}/deploy", step5.DeployApp)
			r.Post("/servers/{serverID}/apps/{appID}/rollback", step5.RollbackApp)
			r.Get("/servers/{serverID}/apps/{appID}/deployments", step5.ListDeployments)

			// --- App lifecycle (Layer 6 Step 5 handlers) ---
			r.Post("/servers/{serverID}/apps/{appID}/start", step5.StartApp)
			r.Post("/servers/{serverID}/apps/{appID}/stop", step5.StopApp)
			r.Post("/servers/{serverID}/apps/{appID}/restart", step5.RestartApp)
			r.Get("/servers/{serverID}/apps/{appID}/logs", step5.GetAppLogs)

			// --- Environment variables (Layer 6 Step 5 handlers) ---
			r.Get("/servers/{serverID}/apps/{appID}/env", step5.ListEnvVars)
			r.Put("/servers/{serverID}/apps/{appID}/env/{key}", step5.SetEnvVar)
			r.Delete("/servers/{serverID}/apps/{appID}/env/{key}", step5.DeleteEnvVar)

			// --- Databases (Layer 6 Step 5 handlers) ---
			r.Get("/servers/{serverID}/apps/{appID}/databases", handlers.NotImplemented)
			r.Post("/servers/{serverID}/apps/{appID}/databases", handlers.NotImplemented)
			r.Delete("/servers/{serverID}/apps/{appID}/databases/{dbID}", handlers.NotImplemented)

			// --- Domains ---
			// Legacy routes (domain owned by a deployment).
			r.Post("/servers/{serverID}/deployments/{deploymentID}/domains", customDomainHandler.AddDomain)
			r.Post("/servers/{serverID}/deployments/{deploymentID}/domains/{domainID}/verify", customDomainHandler.VerifyDomain)
			r.Delete("/servers/{serverID}/deployments/{deploymentID}/domains/{domainID}", customDomainHandler.RemoveDomain)
			// Plan routes (domain owned by an app), Layer 6 Step 5 / Layer 7 Step 10.
			r.Get("/servers/{serverID}/apps/{appID}/domains", customDomainHandler.ListAppDomains)
			r.Post("/servers/{serverID}/apps/{appID}/domains", customDomainHandler.AddAppDomain)
			r.Delete("/servers/{serverID}/apps/{appID}/domains/{domain}", customDomainHandler.RemoveAppDomain)
			r.Post("/servers/{serverID}/apps/{appID}/domains/{domain}/verify", customDomainHandler.VerifyAppDomain)

			// --- Metrics (Layer 6 Step 5 handlers) ---
			r.Get("/servers/{serverID}/metrics", step5.GetServerMetrics)
			r.Get("/servers/{serverID}/metrics/history", step5.GetServerMetricsHistory)

			// --- Alerts ---
			r.Get("/servers/{serverID}/alerts", server.ListAlerts)
			r.Post("/servers/{serverID}/alerts/{alertID}/ack", server.AcknowledgeAlert)             // legacy alias
			r.Post("/servers/{serverID}/alerts/{alertID}/acknowledge", server.AcknowledgeAlert)     // plan URL
			r.Get("/alerts", server.ListAllAlerts)
			r.Post("/alerts/read", server.MarkAllAlertsRead)

			// --- Backups ---
			// Legacy rich routes (config/snapshots/jobs/schedule/usage/restore/
			// verification/storage) — the dashboard depends on these.
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
			// Plan routes (Layer 6 Step 5 handlers).
			r.Get("/servers/{serverID}/backups", step5.ListBackupsPlan)
			r.Post("/servers/{serverID}/backups", step5.TriggerBackupPlan)
			r.Post("/servers/{serverID}/backups/{backupID}/restore", step5.RestoreBackupPlan)

			// --- Commands (Layer 6 Step 5 handlers) ---
			r.Get("/servers/{serverID}/commands", handlers.NotImplemented)
			r.Get("/servers/{serverID}/commands/{commandID}", handlers.NotImplemented)
		})
	})

	return r
}
