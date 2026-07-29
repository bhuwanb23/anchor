package api

import (
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/yourname/yourplatform/control-plane/internal/api/handlers"
	appmiddleware "github.com/yourname/yourplatform/control-plane/internal/api/middleware"
	"github.com/yourname/yourplatform/control-plane/internal/auth"
	"github.com/yourname/yourplatform/control-plane/internal/config"
)

func NewRouter(database *sql.DB, cfg *config.Config) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(appmiddleware.CORS(cfg.FrontendURL))

	r.Get("/health", handlers.Health)

	authHandler := &handlers.Auth{DB: database, Cfg: cfg}
	server := &handlers.Server{DB: database}

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/register", authHandler.Register)
		r.Post("/auth/login", authHandler.Login)

		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.Auth(&auth.Config{
				Secret: cfg.JWTSecret,
			}))
			r.Get("/auth/me", authHandler.Me)
			r.Get("/servers", server.ListServers)
			r.Post("/servers", server.CreateServer)
			r.Post("/deploy", handlers.DeployApp)
			r.Get("/deployments", handlers.GetDeploymentStatus)
		})
	})

	return r
}
