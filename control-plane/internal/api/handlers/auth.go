package handlers

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strings"
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

// dummyPasswordHash is compared against when the requested email does not
// exist, so unknown-email and wrong-password responses take the same time.
// It is generated once at startup (never used to authenticate anyone).
var dummyPasswordHash = func() string {
	h, err := auth.HashPassword("timing-equalizer-not-a-real-password")
	if err != nil {
		panic(err)
	}
	return h
}()

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
	name := strings.TrimSpace(req.Name)
	if err := auth.ValidateName(name); err != nil {
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

	if err := queries.InsertUser(a.DB, userID, email, name, hash); err != nil {
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

// loginResponse is the shape returned by /auth/login and /auth/refresh
// (Layer 5A Step 2A/2D).
type loginResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	User         struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Name  string `json:"name"`
	} `json:"user"`
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
		// email enumeration (Layer 5A Step 2A). A dummy bcrypt compare keeps
		// the response time uniform so the endpoint does not reveal whether
		// an email exists.
		_ = auth.VerifyPassword(req.Password, dummyPasswordHash)
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

	resp, err := a.issueTokens(user, r)
	if err != nil {
		slog.Error("issue tokens", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// Refresh implements Layer 5A Step 2D. Given a valid, unexpired, unrevoked
// refresh token it issues a fresh access token, rotates the refresh token
// (revoking the old one) so a stolen token is useless after first use, and
// slides the session expiry forward by another full lifetime.
func (a *Auth) Refresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.RefreshToken == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "refresh_token is required"})
		return
	}

	rt, err := queries.GetRefreshTokenByHash(a.DB, auth.HashRefreshToken(req.RefreshToken))
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired refresh token"})
		return
	}
	if err != nil {
		slog.Error("query refresh token", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	if rt.RevokedAt.Valid {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired refresh token"})
		return
	}
	expiresAt, err := time.Parse(time.RFC3339, rt.ExpiresAt)
	if err != nil || time.Now().After(expiresAt) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired refresh token"})
		return
	}

	user, err := queries.GetUserByID(a.DB, rt.UserID)
	if err != nil {
		// Account deleted while the refresh token was still valid.
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired refresh token"})
		return
	}

	// Rotate: the presented token can only ever be used once. If the client
	// presents it again it is already revoked → 401 (stolen-token protection).
	if err := queries.RevokeRefreshToken(a.DB, rt.ID); err != nil {
		slog.Error("revoke refresh token", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	_ = queries.UpdateRefreshTokenLastUsed(a.DB, rt.ID)

	resp, err := a.issueTokens(user, r)
	if err != nil {
		slog.Error("issue tokens", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// issueTokens creates an access token (Step 2B) and a stored, hashed refresh
// token (Step 2C) for a user, and builds the shared login response.
func (a *Auth) issueTokens(user queries.User, r *http.Request) (loginResponse, error) {
	accessTTL := time.Duration(a.Cfg.JWTExpiryHrs) * time.Hour
	accessToken, err := auth.GenerateAccessToken(user.ID, user.Email, user.Name, a.Cfg.JWTSecret, accessTTL)
	if err != nil {
		return loginResponse{}, err
	}

	rawRefresh, hashedRefresh, err := auth.GenerateRefreshToken()
	if err != nil {
		return loginResponse{}, err
	}

	expiresAt := time.Now().UTC().Add(time.Duration(a.refreshTokenDays()) * 24 * time.Hour).Format(time.RFC3339)
	if err := queries.CreateRefreshToken(
		a.DB,
		uuid.New().String(),
		hashedRefresh,
		user.ID,
		expiresAt,
		r.UserAgent(),
		clientIP(r),
	); err != nil {
		return loginResponse{}, err
	}

	var resp loginResponse
	resp.AccessToken = accessToken
	resp.RefreshToken = rawRefresh
	resp.TokenType = "Bearer"
	resp.ExpiresIn = int(accessTTL.Seconds())
	resp.User.ID = user.ID
	resp.User.Email = user.Email
	resp.User.Name = user.Name
	return resp, nil
}

// refreshTokenDays returns the configured refresh token lifetime, defaulting
// to 30 days when unset (0).
func (a *Auth) refreshTokenDays() int {
	if a.Cfg != nil && a.Cfg.RefreshTokenDays > 0 {
		return a.Cfg.RefreshTokenDays
	}
	return 30
}

// clientIP extracts the remote address without the port.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
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
// modernc.org/sqlite returns messages like
// "constraint failed: UNIQUE constraint failed: t.email (2067)".
func isUniqueViolation(err error) bool {
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
