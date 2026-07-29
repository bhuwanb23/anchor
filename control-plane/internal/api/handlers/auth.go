package handlers

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/yourname/yourplatform/control-plane/internal/auth"
	"github.com/yourname/yourplatform/control-plane/internal/config"
)

type Auth struct {
	DB  *sql.DB
	Cfg *config.Config
}

func (a *Auth) Register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Email == "" || req.Password == "" {
		http.Error(w, "email and password are required", http.StatusBadRequest)
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		slog.Error("hash password", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	userID := uuid.New().String()

	_, err = a.DB.Exec(
		"INSERT INTO users (id, email, password_hash) VALUES (?, ?, ?)",
		userID, req.Email, hash,
	)
	if err != nil {
		slog.Error("insert user", "error", err)
		http.Error(w, "email already exists or database error", http.StatusConflict)
		return
	}

	token, err := auth.GenerateJWT(userID, req.Email, a.Cfg.JWTSecret, time.Duration(a.Cfg.JWTExpiryHrs)*time.Hour)
	if err != nil {
		slog.Error("generate jwt", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"token": token})
}

func (a *Auth) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Email == "" || req.Password == "" {
		http.Error(w, "email and password are required", http.StatusBadRequest)
		return
	}

	var userID, passwordHash string
	err := a.DB.QueryRow(
		"SELECT id, password_hash FROM users WHERE email = ?",
		req.Email,
	).Scan(&userID, &passwordHash)
	if err == sql.ErrNoRows {
		http.Error(w, "invalid email or password", http.StatusUnauthorized)
		return
	}
	if err != nil {
		slog.Error("query user", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if err := auth.VerifyPassword(req.Password, passwordHash); err != nil {
		http.Error(w, "invalid email or password", http.StatusUnauthorized)
		return
	}

	token, err := auth.GenerateJWT(userID, req.Email, a.Cfg.JWTSecret, time.Duration(a.Cfg.JWTExpiryHrs)*time.Hour)
	if err != nil {
		slog.Error("generate jwt", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"token": token})
}

func (a *Auth) Me(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value("user_id").(string)
	email := r.Context().Value("email").(string)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"id":    userID,
		"email": email,
	})
}
