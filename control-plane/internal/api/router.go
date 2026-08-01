package api

import (
	"database/sql"
	"net/http"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/yourname/yourplatform/control-plane/internal/api/handlers"
	appmiddleware "github.com/yourname/yourplatform/control-plane/internal/api/middleware"
	"github.com/yourname/yourplatform/control-plane/internal/auth"
	"github.com/yourname/yourplatform/control-plane/internal/config"
	"github.com/yourname/yourplatform/control-plane/internal/dns"
	"github.com/yourname/yourplatform/control-plane/internal/ws"
)

func NewRouter(database *sql.DB, cfg *config.Config, hub *ws.Hub) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(appmiddleware.CORS(cfg.FrontendURL))

	r.Get("/health", handlers.Health)
	r.Get("/install.sh", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./scripts/install.sh")
	})

	r.Get("/ws/agent", ws.HandleAgentWS(hub, database))
	r.Get("/ws/browser", ws.HandleBrowserWS(hub, database, cfg.JWTSecret))

	releaseDir := filepath.Join(".", "release")
	r.Handle("/releases/*", http.StripPrefix("/releases/", http.FileServer(http.Dir(releaseDir))))

	// Create DNS client if Cloudflare credentials are configured
	var dnsClient *dns.Client
	if cfg.DNSConfigured() {
		dnsClient = dns.NewClient(cfg.CloudflareToken, cfg.CloudflareZoneID)
	}

	authHandler := &handlers.Auth{DB: database, Cfg: cfg}
	server := &handlers.Server{DB: database}
	tokenHandler := &handlers.Token{DB: database}
	agentHandler := &handlers.Agent{DB: database, DNS: dnsClient, Config: cfg}

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/register", authHandler.Register)
		r.Post("/auth/login", authHandler.Login)
		r.Post("/agent/register", agentHandler.Register)

		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.Auth(&auth.Config{
				Secret: cfg.JWTSecret,
			}))
			r.Get("/auth/me", authHandler.Me)
			r.Get("/servers", server.ListServers)
			r.Post("/servers", server.CreateServer)
			r.Get("/servers/{serverID}/events", server.ListEvents)
			r.Post("/servers/registration-token", tokenHandler.CreateRegistrationToken)
			r.Post("/deploy", handlers.DeployApp)
			r.Get("/deployments", handlers.GetDeploymentStatus)
		})
	})

	return r
}
