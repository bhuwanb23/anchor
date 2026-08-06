package handlers_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	appmiddleware "github.com/yourname/yourplatform/control-plane/internal/api/middleware"
	"github.com/yourname/yourplatform/control-plane/internal/auth"
	"github.com/yourname/yourplatform/control-plane/internal/config"
)

const testFrontendURL = "http://localhost:3000"

// integrationRouter mirrors the production router wiring for the auth surface:
// RequestID → SecurityHeaders → CORS → (protected) Auth.
func integrationRouter(db *sql.DB, cfg *config.Config) http.Handler {
	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(appmiddleware.SecurityHeaders)
	r.Use(appmiddleware.CORS(cfg.FrontendURL))
	h := newAuthHandler(db)
	r.Post("/api/v1/auth/register", h.Register)
	r.Post("/api/v1/auth/login", h.Login)
	r.Group(func(r chi.Router) {
		r.Use(appmiddleware.Auth(db, cfg.JWTSecret))
		r.Get("/api/v1/auth/me", h.Me)
	})
	return r
}

func integrationCfg() *config.Config {
	return &config.Config{JWTSecret: "test-secret", JWTExpiryHrs: 24, FrontendURL: testFrontendURL}
}

func postJSON(router http.Handler, path, token string, body map[string]string) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestIntegration_RegisterLoginAccessProtectedEndpoint(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	router := integrationRouter(db, integrationCfg())

	// Register: 201, no token in the response.
	reg := postJSON(router, "/api/v1/auth/register", "", map[string]string{
		"name": "Alice Smith", "email": "alice@example.com", "password": "correct horse battery staple",
	})
	if reg.Code != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d: %s", reg.Code, reg.Body.String())
	}

	// Login: access + refresh tokens returned.
	login := postJSON(router, "/api/v1/auth/login", "", map[string]string{"email": "alice@example.com", "password": "correct horse battery staple"})
	if login.Code != http.StatusOK {
		t.Fatalf("login: expected 200, got %d: %s", login.Code, login.Body.String())
	}
	var loginResp map[string]interface{}
	json.NewDecoder(login.Body).Decode(&loginResp)
	accessToken, _ := loginResp["access_token"].(string)
	if accessToken == "" || loginResp["refresh_token"] == "" {
		t.Fatalf("login must return access + refresh tokens: %v", loginResp)
	}

	// The access token validates on a protected endpoint.
	me := doJSON(router, http.MethodGet, "/api/v1/auth/me", accessToken, nil)
	if me.Code != http.StatusOK {
		t.Fatalf("me with valid token: expected 200, got %d: %s", me.Code, me.Body.String())
	}

	// Without a token the same endpoint is rejected.
	if w := doJSON(router, http.MethodGet, "/api/v1/auth/me", "", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("me without token: expected 401, got %d", w.Code)
	}
}

func TestIntegration_SecurityHeadersOnEveryResponse(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	router := integrationRouter(db, integrationCfg())

	for name, w := range map[string]*httptest.ResponseRecorder{
		"success": postJSON(router, "/api/v1/auth/register", "", map[string]string{
			"name": "Alice Smith", "email": "alice@example.com", "password": "correct horse battery staple",
		}),
		"error": postJSON(router, "/api/v1/auth/register", "", map[string]string{
			"name": "Alice Smith", "email": "alice@example.com", "password": "correct horse battery staple",
		}), // duplicate → 409 error response
	} {
		if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s: X-Content-Type-Options = %q, want nosniff", name, got)
		}
		if got := w.Header().Get("X-Frame-Options"); got != "DENY" {
			t.Errorf("%s: X-Frame-Options = %q, want DENY", name, got)
		}
		if got := w.Header().Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
			t.Errorf("%s: Referrer-Policy = %q, want strict-origin-when-cross-origin", name, got)
		}
	}
}

func TestIntegration_CORSAllowsOnlyConfiguredOrigin(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	router := integrationRouter(db, integrationCfg())

	// Foreign origin: rejected with 403 before any handler runs (Layer 6
	// Step 2C). Never a wildcard, never the attacker's origin echoed back.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("foreign origin: expected 403, got %d", w.Code)
	}
	if allow := w.Header().Get("Access-Control-Allow-Origin"); allow != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want unset for rejected origin", allow)
	}
	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)
	if body["error"] != "origin_not_allowed" {
		t.Errorf("error = %v, want origin_not_allowed", body["error"])
	}

	// Dashboard origin: allowed through (reaches Auth → 401 without a token).
	ok := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	ok.Header.Set("Origin", testFrontendURL)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, ok)
	if w2.Code != http.StatusUnauthorized {
		t.Errorf("dashboard origin: expected 401 (reached auth), got %d", w2.Code)
	}
	if got := w2.Header().Get("Access-Control-Allow-Origin"); got != testFrontendURL {
		t.Errorf("dashboard origin: Allow-Origin = %q, want %q", got, testFrontendURL)
	}
}

func TestIntegration_PreflightGetsSecurityAndCORSHeaders(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	router := integrationRouter(db, integrationCfg())

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/auth/login", nil)
	req.Header.Set("Origin", testFrontendURL)
	req.Header.Set("Access-Control-Request-Method", "POST")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("preflight: expected 204, got %d", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != testFrontendURL {
		t.Errorf("preflight ACAO = %q, want %q", got, testFrontendURL)
	}
	// Security headers must survive the CORS early-return (Step 8B ordering).
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("preflight X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := w.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("preflight X-Frame-Options = %q, want DENY", got)
	}
}

func TestIntegration_TamperedJWTRejected(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	router := integrationRouter(db, integrationCfg())

	registerRequest(t, newAuthHandler(db), map[string]string{
		"name": "Alice Smith", "email": "alice@example.com", "password": "correct horse battery staple",
	})
	login := postJSON(router, "/api/v1/auth/login", "", map[string]string{"email": "alice@example.com", "password": "correct horse battery staple"})
	var loginResp map[string]interface{}
	json.NewDecoder(login.Body).Decode(&loginResp)
	token := loginResp["access_token"].(string)

	// Flip a character in the signature section.
	tampered := token[:len(token)-2] + "xx"
	w := doJSON(router, http.MethodGet, "/api/v1/auth/me", tampered, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("tampered token: expected 401, got %d", w.Code)
	}
	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)
	if body["error"] != "invalid_token" {
		t.Errorf("error = %v, want invalid_token", body["error"])
	}
}

func TestIntegration_ExpiredAccessTokenGetsXTokenExpired(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	router := integrationRouter(db, integrationCfg())

	h := newAuthHandler(db)
	registerRequest(t, h, map[string]string{
		"name": "Alice Smith", "email": "alice@example.com", "password": "correct horse battery staple",
	})
	var userID string
	if err := db.QueryRow("SELECT id FROM users WHERE email = 'alice@example.com'").Scan(&userID); err != nil {
		t.Fatalf("lookup user: %v", err)
	}

	// Craft an already-expired access token with the same secret.
	expired, err := auth.GenerateAccessToken(userID, "sess", "alice@example.com", "Alice Smith", "test-secret", -time.Hour)
	if err != nil {
		t.Fatalf("generate expired token: %v", err)
	}

	w := doJSON(router, http.MethodGet, "/api/v1/auth/me", expired, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expired token: expected 401, got %d", w.Code)
	}
	if got := w.Header().Get("X-Token-Expired"); got != "true" {
		t.Errorf("X-Token-Expired = %q, want true", got)
	}
	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)
	if body["error"] != "token_expired" {
		t.Errorf("error = %v, want token_expired", body["error"])
	}
}
