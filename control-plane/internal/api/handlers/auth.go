package handlers

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/yourname/yourplatform/control-plane/internal/api/middleware"
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

	// Rotate: the presented token can only ever be used once. The revoke is
	// atomic (conditional on revoked_at IS NULL), so if two requests race with
	// the same token only the first wins — the loser is treated as replay.
	revoked, err := queries.RevokeRefreshToken(a.DB, rt.ID)
	if err != nil {
		slog.Error("revoke refresh token", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	if !revoked {
		// Another request already rotated this token — treat as stolen/replayed.
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired refresh token"})
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
//
// The refresh-token row ID becomes the access token's "sid" claim, so the
// sessions view (Step 4B) can mark which session is making a request.
func (a *Auth) issueTokens(user queries.User, r *http.Request) (loginResponse, error) {
	sessionID := uuid.New().String()

	accessTTL := time.Duration(a.Cfg.JWTExpiryHrs) * time.Hour
	accessToken, err := auth.GenerateAccessToken(user.ID, sessionID, user.Email, user.Name, a.Cfg.JWTSecret, accessTTL)
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
		sessionID,
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

// Logout implements Layer 5A Step 4A Level 1 — revoke THIS device's session.
// It requires a valid access token (route is protected) and takes the refresh
// token that identifies the session being logged out.
//
// The access token itself stays valid until it expires (24h) — JWTs cannot be
// invalidated without a database blocklist, which we deliberately avoid.
//
// Sequence: hash the refresh token → find it → verify it belongs to the
// authenticated user (prevents cross-user revocation) → set revoked_at → 200.
func (a *Auth) Logout(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}

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
		// Unknown token: same response whether the token is invalid or already
		// gone — do not reveal anything about other users' sessions.
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid session"})
		return
	}
	if err != nil {
		slog.Error("lookup refresh token", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	// Cross-user revocation attempt: the token belongs to someone else.
	// Reply as if invalid so we never confirm another session's existence.
	if rt.UserID != userID {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid session"})
		return
	}

	if _, err := queries.RevokeRefreshToken(a.DB, rt.ID); err != nil {
		slog.Error("revoke refresh token", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "Logged out"})
}

// LogoutAll implements Layer 5A Step 4A Level 2 — revoke every active session
// for the authenticated user, logging them out on all devices.
func (a *Auth) LogoutAll(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}

	if _, err := queries.RevokeAllRefreshTokens(a.DB, userID); err != nil {
		slog.Error("revoke all refresh tokens", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "Logged out from all devices"})
}

// sessionView is the JSON shape of one entry in the sessions list (Layer 5A
// Step 4B). last_used_at is null until the session has been refreshed.
type sessionView struct {
	ID         string  `json:"id"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at"`
	ExpiresAt  string  `json:"expires_at"`
	UserAgent  string  `json:"user_agent"`
	IPAddress  string  `json:"ip_address"`
	Current    bool    `json:"current"`
}

// Sessions implements Layer 5A Step 4B — the active sessions view. It lists
// every active (non-revoked, non-expired) session for the authenticated user
// with device info, marking the one that made this request as current.
func (a *Auth) Sessions(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}

	sessions, err := queries.ListSessionsByUser(a.DB, userID)
	if err != nil {
		slog.Error("list sessions", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	// The access token's sid claim identifies the current session.
	currentID := ""
	if claims := middleware.ClaimsFromContext(r.Context()); claims != nil {
		currentID = claims.SessionID
	}

	out := make([]sessionView, 0, len(sessions))
	for _, s := range sessions {
		view := sessionView{
			ID:         s.ID,
			CreatedAt:  s.CreatedAt,
			ExpiresAt:  s.ExpiresAt,
			UserAgent:  s.UserAgent,
			IPAddress:  s.IPAddress,
			Current:    s.ID == currentID,
		}
		if s.LastUsedAt.Valid {
			view.LastUsedAt = &s.LastUsedAt.String
		}
		out = append(out, view)
	}

	writeJSON(w, http.StatusOK, out)
}

// DeleteSession implements Layer 5A Step 4B — revoke a specific session from
// the sessions view. The session must belong to the authenticated user.
func (a *Auth) DeleteSession(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}

	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session id is required"})
		return
	}

	revoked, err := queries.RevokeSessionForUser(a.DB, sessionID, userID)
	if err != nil {
		slog.Error("revoke session", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	if !revoked {
		// Not found, already revoked, or belongs to another user — all 404.
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "Session revoked"})
}

func (a *Auth) Me(w http.ResponseWriter, r *http.Request) {
	// The middleware already loaded the user from the DB (Layer 5A Step 3B.6),
	// so the handler does zero auth work — it just reads the context.
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
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
