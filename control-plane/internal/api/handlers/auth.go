package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/yourname/yourplatform/control-plane/internal/auth"
	"github.com/yourname/yourplatform/control-plane/internal/config"
	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
)

type Auth struct {
	DB  *sql.DB
	Cfg *config.Config
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// Register implements Layer 5A Step 1C.
//
// It validates email/password/name, rejects duplicate emails with a clear
// 409, bcrypt-hashes the password, and returns 201 WITHOUT issuing any token
// or returning user data — the user must log in separately.
func (a *Auth) Register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	email := auth.NormalizeEmail(req.Email)
	if err := auth.ValidateEmail(email); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := auth.ValidatePassword(req.Password); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := auth.ValidateName(req.Name); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// Uniqueness check before hashing — hashing is ~300ms, so fail fast.
	// The UNIQUE index on email is the backstop for concurrent registrations.
	exists, err := queries.EmailExists(a.DB, email)
	if err != nil {
		slog.Error("check email exists", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	if exists {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "An account with this email already exists"})
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		slog.Error("hash password", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	userID := uuid.New().String()

	if err := queries.InsertUser(a.DB, userID, email, req.Name, hash); err != nil {
		// Concurrent registration with the same email hits the UNIQUE index.
		if isUniqueViolation(err) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "An account with this email already exists"})
			return
		}
		slog.Error("insert user", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	slog.Info("user registered", "user_id", userID)
	writeJSON(w, http.StatusCreated, map[string]string{"message": "Account created successfully"})
}

func (a *Auth) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.Email == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email and password are required"})
		return
	}

	user, err := queries.GetUserByEmail(a.DB, auth.NormalizeEmail(req.Email))
	if err == sql.ErrNoRows {
		// Same message for unknown email and wrong password — prevents
		// email enumeration (Layer 5A Step 2A).
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid email or password"})
		return
	}
	if err != nil {
		slog.Error("query user", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	if err := auth.VerifyPassword(req.Password, user.PasswordHash); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid email or password"})
		return
	}

	token, err := auth.GenerateJWT(user.ID, user.Email, a.Cfg.JWTSecret, time.Duration(a.Cfg.JWTExpiryHrs)*time.Hour)
	if err != nil {
		slog.Error("generate jwt", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"token": token,
		"user": map[string]interface{}{
			"id":    user.ID,
			"email": user.Email,
			"name":  user.Name,
		},
	})
}

func (a *Auth) Me(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value("user_id").(string)

	user, err := queries.GetUserByID(a.DB, userID)
	if err != nil {
		// Account deleted while the token was still valid.
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "user not found"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":    user.ID,
		"email": user.Email,
		"name":  user.Name,
	})
}

// isUniqueViolation reports whether a SQLite error is a UNIQUE constraint
// violation (SQLITE_CONSTRAINT_UNIQUE / SQLITE_CONSTRAINT_PRIMARYKEY).
func isUniqueViolation(err error) bool {
	var sqliteErr interface {
		Error() string
	}
	if errors.As(err, &sqliteErr) {
		msg := sqliteErr.Error()
		return containsAny(msg, "UNIQUE constraint failed", "PRIMARY KEY must be unique")
	}
	return false
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(sub) > 0 && len(s) >= len(sub) && indexOf(s, sub) >= 0 {
			return true
		}
	}
	return false
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
