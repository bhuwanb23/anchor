package handlers_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/yourname/yourplatform/control-plane/internal/api/handlers"
	"github.com/yourname/yourplatform/control-plane/internal/config"

	_ "modernc.org/sqlite"
)

// setupAuthTestDB creates an in-memory DB with the users table as defined by
// migrations 001 + 017 (including the Layer 5A name/updated_at columns).
func setupAuthTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	// With ":memory:" each pooled connection gets its own private database.
	// Pin to one connection so the schema survives across queries, including
	// the concurrent-registration test.
	db.SetMaxOpenConns(1)

	_, err = db.Exec(`
		CREATE TABLE users (
			id TEXT PRIMARY KEY,
			email TEXT UNIQUE NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			password_hash TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT
		);
		CREATE INDEX idx_users_email ON users(email);
		CREATE TABLE refresh_tokens (
			id TEXT PRIMARY KEY,
			token_hash TEXT NOT NULL UNIQUE,
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			expires_at TEXT NOT NULL,
			last_used_at TEXT,
			user_agent TEXT,
			ip_address TEXT,
			revoked_at TEXT
		);
		CREATE INDEX idx_refresh_tokens_user ON refresh_tokens(user_id);
		CREATE INDEX idx_refresh_tokens_hash ON refresh_tokens(token_hash);
	`)
	if err != nil {
		t.Fatalf("create users table: %v", err)
	}
	return db
}

func newAuthHandler(db *sql.DB) *handlers.Auth {
	return &handlers.Auth{
		DB: db,
		Cfg: &config.Config{
			JWTSecret:    "test-secret",
			JWTExpiryHrs: 24,
		},
	}
}

func registerRequest(t *testing.T, h *handlers.Auth, body map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Register(w, req)
	return w
}

func TestRegister_Success(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	h := newAuthHandler(db)

	w := registerRequest(t, h, map[string]string{
		"name":     "Alice Smith",
		"email":    "Alice@Example.COM",
		"password": "correct horse battery staple",
	})

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Response contains only a message — no token, no user data.
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["message"] != "Account created successfully" {
		t.Errorf("message = %v, want Account created successfully", resp["message"])
	}
	if _, hasToken := resp["token"]; hasToken {
		t.Error("registration must not return a token")
	}

	// Row was stored with normalized email, name, and a bcrypt hash.
	var email, name, hash string
	err := db.QueryRow("SELECT email, name, password_hash FROM users WHERE email = ?", "alice@example.com").Scan(&email, &name, &hash)
	if err != nil {
		t.Fatalf("query stored user: %v", err)
	}
	if email != "alice@example.com" {
		t.Errorf("stored email = %q, want lowercased alice@example.com", email)
	}
	if name != "Alice Smith" {
		t.Errorf("stored name = %q, want Alice Smith", name)
	}
	if hash == "" || hash == "correct horse battery staple" {
		t.Errorf("stored password_hash = %q, want a bcrypt hash", hash)
	}
	if !strings.HasPrefix(hash, "$2") {
		t.Errorf("stored password_hash = %q, want bcrypt prefix $2", hash)
	}
}

func TestRegister_DuplicateEmail(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	h := newAuthHandler(db)

	// First registration succeeds.
	body := map[string]string{
		"name":     "Alice Smith",
		"email":    "alice@example.com",
		"password": "correct horse battery staple",
	}
	if w := registerRequest(t, h, body); w.Code != http.StatusCreated {
		t.Fatalf("first register: expected 201, got %d", w.Code)
	}

	// Second registration with the same email (different case) → 409.
	w := registerRequest(t, h, map[string]string{
		"name":     "Alice Smith",
		"email":    "ALICE@example.com",
		"password": "another long password",
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "An account with this email already exists" {
		t.Errorf("error = %v, want clear duplicate message", resp["error"])
	}

	// Exactly one row exists.
	var count int
	db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if count != 1 {
		t.Errorf("user count = %d, want 1", count)
	}
}

func TestRegister_InvalidEmail(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	h := newAuthHandler(db)

	for _, email := range []string{"", "not-an-email", "user@nodot", "user@example.c"} {
		w := registerRequest(t, h, map[string]string{
			"name":     "Alice Smith",
			"email":    email,
			"password": "correct horse battery staple",
		})
		if w.Code != http.StatusBadRequest {
			t.Errorf("email %q: expected 400, got %d", email, w.Code)
		}
	}
}

func TestRegister_InvalidPassword(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	h := newAuthHandler(db)

	for _, password := range []string{"", "short", "password", "12345678"} {
		w := registerRequest(t, h, map[string]string{
			"name":     "Alice Smith",
			"email":    "alice@example.com",
			"password": password,
		})
		if w.Code != http.StatusBadRequest {
			t.Errorf("password %q: expected 400, got %d", password, w.Code)
		}
	}
}

func TestRegister_InvalidName(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	h := newAuthHandler(db)

	for _, name := range []string{"", "J", strings.Repeat("x", 101)} {
		w := registerRequest(t, h, map[string]string{
			"name":     name,
			"email":    "alice@example.com",
			"password": "correct horse battery staple",
		})
		if w.Code != http.StatusBadRequest {
			t.Errorf("name %q: expected 400, got %d", name, w.Code)
		}
	}
}

func TestRegister_UnicodeName(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	h := newAuthHandler(db)

	w := registerRequest(t, h, map[string]string{
		"name":     "山田 太郎",
		"email":    "taro@example.com",
		"password": "correct horse battery staple",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRegister_ConcurrentSameEmail(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	h := newAuthHandler(db)

	const goroutines = 5
	var wg sync.WaitGroup
	codes := make([]int, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w := registerRequest(t, h, map[string]string{
				"name":     "Alice Smith",
				"email":    "alice@example.com",
				"password": "correct horse battery staple",
			})
			codes[i] = w.Code
		}(i)
	}
	wg.Wait()

	created := 0
	conflicts := 0
	for _, code := range codes {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflicts++
		default:
			t.Errorf("unexpected status %d", code)
		}
	}
	if created != 1 {
		t.Errorf("created = %d, want exactly 1", created)
	}
	if conflicts != goroutines-1 {
		t.Errorf("conflicts = %d, want %d", conflicts, goroutines-1)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if count != 1 {
		t.Errorf("user count = %d, want 1", count)
	}
}

func TestLogin_CaseInsensitiveEmail(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	h := newAuthHandler(db)

	registerRequest(t, h, map[string]string{
		"name":     "Alice Smith",
		"email":    "alice@example.com",
		"password": "correct horse battery staple",
	})

	body := map[string]string{"email": "ALICE@example.com", "password": "correct horse battery staple"}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Login(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["access_token"] == "" {
		t.Error("login should return an access_token")
	}
	if resp["refresh_token"] == "" {
		t.Error("login should return a refresh_token")
	}
	if !strings.HasPrefix(resp["refresh_token"].(string), "rt_") {
		t.Errorf("refresh_token = %v, want rt_ prefix", resp["refresh_token"])
	}
	if resp["token_type"] != "Bearer" {
		t.Errorf("token_type = %v, want Bearer", resp["token_type"])
	}
	if resp["expires_in"] != 86400 {
		t.Errorf("expires_in = %v, want 86400", resp["expires_in"])
	}
	user := resp["user"].(map[string]interface{})
	if user["name"] != "Alice Smith" {
		t.Errorf("user.name = %v, want Alice Smith", user["name"])
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	h := newAuthHandler(db)

	registerRequest(t, h, map[string]string{
		"name":     "Alice Smith",
		"email":    "alice@example.com",
		"password": "correct horse battery staple",
	})

	body := map[string]string{"email": "alice@example.com", "password": "wrong-password"}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Login(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "invalid email or password" {
		t.Errorf("error = %v, want non-enumerating message", resp["error"])
	}
}
