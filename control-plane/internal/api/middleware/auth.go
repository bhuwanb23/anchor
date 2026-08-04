package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/yourname/yourplatform/control-plane/internal/auth"
)

func Auth(cfg *auth.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" {
				http.Error(w, "missing authorization header", http.StatusUnauthorized)
				return
			}

			parts := strings.SplitN(header, " ", 2)
			if len(parts) != 2 || parts[0] != "Bearer" {
				http.Error(w, "invalid authorization header format", http.StatusUnauthorized)
				return
			}

			tokenString := parts[1]
			claims, err := auth.ValidateJWT(tokenString, cfg.Secret)
			if err != nil {
				http.Error(w, "invalid or expired token", http.StatusUnauthorized)
				return
			}
			// Only access tokens are valid here — never refresh tokens
			// (Layer 5A Step 3B).
			if claims.Type != auth.TokenTypeAccess {
				http.Error(w, "invalid or expired token", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), "user_id", claims.UserID())
			ctx = context.WithValue(ctx, "email", claims.Email)
			ctx = context.WithValue(ctx, "name", claims.Name)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}