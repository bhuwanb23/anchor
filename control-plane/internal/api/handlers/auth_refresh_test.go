package handlers_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yourname/yourplatform/control-plane/internal/api/handlers"
	"github.com/yourname/yourplatform/control-plane/internal/auth"
)

// loginHelper registers a fresh user and returns the login response.
func loginHelper(t *testing.T, h *handlers.Auth) map[string]interface{} {
	t.Helper()
	if w := registerRequest(t, h, map[string]string{
		"name":     "Alice Smith",
		"email":    "alice@example.com",
		"password": "correct horse battery staple",
	}); w.Code != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d", w.Code)
	}

	body := map[string]string{"email": "alice@example.com", "password": "correct horse battery staple"}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Login(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	return resp
}

// refreshHelper posts a refresh_token and returns the recorder.
func refreshHelper(h *handlers.Auth, refreshToken string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"refresh_token": refreshToken})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Refresh(w, req)
	return w
}

func TestRefresh_Success(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	h := newAuthHandler(db)

	loginResp := loginHelper(t, h)
	refreshToken := loginResp["refresh_token"].(string)

	w := refreshHelper(h, refreshToken)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["access_token"] == "" {
		t.Error("refresh should return a new access_token")
	}
	if resp["refresh_token"] == "" {
		t.Error("refresh should return a rotated refresh_token")
	}
	if resp["expires_in"] != 86400 {
		t.Errorf("expires_in = %v, want 86400", resp["expires_in"])
	}

	// The new access token must be valid and carry the right claims.
	claims, err := auth.ValidateJWT(resp["access_token"].(string), "test-secret")
	if err != nil {
		t.Fatalf("validate new access token: %v", err)
	}
	if claims.UserID() == "" || claims.Email != "alice@example.com" || claims.Name != "Alice Smith" {
		t.Errorf("claims = {sub:%s email:%s name:%s}, want alice@example.com / Alice Smith", claims.UserID(), claims.Email, claims.Name)
	}
	if claims.Type != auth.TokenTypeAccess {
		t.Errorf("claim type = %q, want access", claims.Type)
	}
}

func TestRefresh_RotationRevokesOldToken(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	h := newAuthHandler(db)

	loginResp := loginHelper(t, h)
	oldToken := loginResp["refresh_token"].(string)

	// First refresh succeeds and rotates.
	if w := refreshHelper(h, oldToken); w.Code != http.StatusOK {
		t.Fatalf("first refresh: expected 200, got %d", w.Code)
	}
	// Reusing the old (now revoked) token must fail — stolen-token protection.
	if w := refreshHelper(h, oldToken); w.Code != http.StatusUnauthorized {
		t.Errorf("reuse of rotated token: expected 401, got %d", w.Code)
	}
}

func TestRefresh_UnknownToken(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	h := newAuthHandler(db)

	w := refreshHelper(h, "rt_definitely-not-a-real-token")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestRefresh_ExpiredToken(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	h := newAuthHandler(db)

	loginResp := loginHelper(t, h)
	refreshToken := loginResp["refresh_token"].(string)

	// Backdate the session so it is expired.
	_, err := db.Exec(
		"UPDATE refresh_tokens SET expires_at = ? WHERE token_hash = ?",
		time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
		auth.HashRefreshToken(refreshToken),
	)
	if err != nil {
		t.Fatalf("backdate refresh token: %v", err)
	}

	if w := refreshHelper(h, refreshToken); w.Code != http.StatusUnauthorized {
		t.Errorf("expired token: expected 401, got %d", w.Code)
	}
}

func TestRefresh_RevokedToken(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	h := newAuthHandler(db)

	loginResp := loginHelper(t, h)
	refreshToken := loginResp["refresh_token"].(string)

	if _, err := db.Exec(
		"UPDATE refresh_tokens SET revoked_at = datetime('now') WHERE token_hash = ?",
		auth.HashRefreshToken(refreshToken),
	); err != nil {
		t.Fatalf("revoke refresh token: %v", err)
	}

	if w := refreshHelper(h, refreshToken); w.Code != http.StatusUnauthorized {
		t.Errorf("revoked token: expected 401, got %d", w.Code)
	}
}

func TestRefresh_SlidingExpiry(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	h := newAuthHandler(db)

	loginResp := loginHelper(t, h)
	refreshToken := loginResp["refresh_token"].(string)

	// Confirm the stored session has a 30-day expiry.
	var expiresAt string
	if err := db.QueryRow(
		"SELECT expires_at FROM refresh_tokens WHERE token_hash = ?",
		auth.HashRefreshToken(refreshToken),
	).Scan(&expiresAt); err != nil {
		t.Fatalf("query expires_at: %v", err)
	}
	exp, _ := time.Parse(time.RFC3339, expiresAt)
	if days := time.Until(exp).Hours() / 24; days < 29 || days > 31 {
		t.Errorf("initial expiry = %v (%.0f days), want ~30 days", exp, days)
	}

	// Refresh rotates to a fresh token with a fresh 30-day window.
	w := refreshHelper(h, refreshToken)
	if w.Code != http.StatusOK {
		t.Fatalf("refresh: expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	newToken := resp["refresh_token"].(string)

	var newExpiresAt string
	if err := db.QueryRow(
		"SELECT expires_at FROM refresh_tokens WHERE token_hash = ?",
		auth.HashRefreshToken(newToken),
	).Scan(&newExpiresAt); err != nil {
		t.Fatalf("query new expires_at: %v", err)
	}
	newExp, _ := time.Parse(time.RFC3339, newExpiresAt)
	if days := time.Until(newExp).Hours() / 24; days < 29 || days > 31 {
		t.Errorf("rotated expiry = %v (%.0f days), want ~30 days", newExp, days)
	}
}

func TestLogin_ConcurrentDevicesIndependentSessions(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	h := newAuthHandler(db)

	// Two logins on "different devices" create two independent sessions.
	loginResp1 := loginHelper(t, h)
	loginResp2 := loginHelper(t, h)

	if loginResp1["refresh_token"] == loginResp2["refresh_token"] {
		t.Error("two logins must not share a refresh token")
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM refresh_tokens").Scan(&count); err != nil {
		t.Fatalf("count refresh tokens: %v", err)
	}
	if count != 2 {
		t.Errorf("refresh token rows = %d, want 2", count)
	}

	// Refreshing on one device does not affect the other.
	if w := refreshHelper(h, loginResp1["refresh_token"].(string)); w.Code != http.StatusOK {
		t.Errorf("device 1 refresh: expected 200, got %d", w.Code)
	}
	if w := refreshHelper(h, loginResp2["refresh_token"].(string)); w.Code != http.StatusOK {
		t.Errorf("device 2 refresh: expected 200, got %d", w.Code)
	}
}

func TestLogin_StoresHashedRefreshToken(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	h := newAuthHandler(db)

	loginResp := loginHelper(t, h)
	rawToken := loginResp["refresh_token"].(string)

	var storedHash string
	if err := db.QueryRow("SELECT token_hash FROM refresh_tokens").Scan(&storedHash); err != nil {
		t.Fatalf("query token_hash: %v", err)
	}
	if storedHash == rawToken {
		t.Error("raw refresh token must never be stored in the database")
	}
	if storedHash != auth.HashRefreshToken(rawToken) {
		t.Error("stored hash does not match SHA-256 of the raw token")
	}
	if strings.Contains(storedHash, "rt_") {
		t.Error("stored hash should be hex only, not contain the rt_ prefix")
	}
}
