package handlers

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/yourname/yourplatform/control-plane/internal/api/middleware"
	"github.com/yourname/yourplatform/control-plane/internal/auth"
	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
)

type Token struct {
	DB *sql.DB
}

func (t *Token) CreateRegistrationToken(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	rawToken, hashedToken, err := auth.GenerateRegistrationToken()
	if err != nil {
		slog.Error("generate registration token", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	tokenID := uuid.New().String()
	expiresAt := time.Now().UTC().Add(1 * time.Hour).Format(time.RFC3339)

	if err := queries.CreateRegistrationToken(t.DB, tokenID, hashedToken, userID, req.Name, expiresAt); err != nil {
		slog.Error("insert registration token", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	scheme := "http://"
	if r.TLS != nil {
		scheme = "https://"
	}
	baseURL := scheme + r.Host
	installCommand := fmt.Sprintf("curl -fsSL %s/install.sh | sudo sh -s -- --token=%s --base-url=%s", baseURL, rawToken, baseURL)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{
		"token":           rawToken,
		"install_command": installCommand,
		"expires_at":      expiresAt,
	})
}
