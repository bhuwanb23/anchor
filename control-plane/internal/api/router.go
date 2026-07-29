package api

import (
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/yourname/yourplatform/control-plane/internal/api/handlers"
	"github.com/yourname/yourplatform/control-plane/internal/api/middleware"
	"github.com/yourname/yourplatform/control-plane/internal/auth"
	"github.com/yourname/yourplatform/control-plane/internal/config"
	"github.com/yourname/yourplatform/control-plane/internal/db"
)

func NewRouter(database *sql.DB, cfg *config.Config) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.CORS(cfg.FrontendURL))

	r.Get("/health", handlers.Health)

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/register", handlers.Register)
		r.Post("/auth/login", handlers.Login)

		r.Group(func(r chi.Router) {
			r.Use(middleware.Auth(&auth.Config{
				Secret: cfg.JWTSecret,
			}))
			r.Get("/servers", handlers.ListServers)
			r.Post("/servers", handlers.CreateServer)
			r.Post("/deploy", handlers.DeployApp)
			r.Get("/deployments", handlers.GetDeploymentStatus)
		})
	})

	return r
}