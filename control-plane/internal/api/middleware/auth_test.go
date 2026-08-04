package middleware_test

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	_ "modernc.org/sqlite"

	"github.com/yourname/yourplatform/control-plane/internal/api/middleware"
	"github.com/yourname/yourplatform/control-plane/internal/auth"
)

const testSecret = "a-test-secret-that-is-long-enough"

// testEnv wires a fresh in-memory DB with one user and a protected handler.
type testEnv struct {
	db        *sql.DB
	userID    string
	protected http.Handler
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(`
		CREATE TABLE users (
			id            TEXT PRIMARY KEY,
			email         TEXT NOT NULL UNIQUE,
			name          TEXT NOT NULL,
			password_hash TEXT NOT NULL,
			created_at    TEXT NOT NULL,
			updated_at    TEXT NOT NULL
		);
	`); err != nil {
		t.Fatalf("create users table: %v", err)
	}

	const userID = "user-123"
	if _, err := db.Exec(
		"INSERT INTO users (id, email, name, password_hash, created_at, updated_at) VALUES (?, ?, ?, ?, datetime('now'), datetime('now'))",
		userID, "alice@example.com", "Alice Smith", "not-a-real-hash",
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	protected := middleware.Auth(db, testSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := middleware.UserFromContext(r.Context())
		if !ok {
			http.Error(w, "user missing from context", http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{
			"id":    user.ID,
			"email": user.Email,
			"name":  user.Name,
		})
	}))

	return &testEnv{db: db, userID: userID, protected: protected}
}

func (e *testEnv) serve(req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	e.protected.ServeHTTP(w, req)
	return w
}

func bearerRequest(token string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

// decodeErrorCode pulls the {"error": code} field out of a 401 body.
func decodeErrorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body %q: %v", w.Body.String(), err)
	}
	return body.Error
}

func TestAuth_ValidTokenAttachesUser(t *testing.T) {
	env := newTestEnv(t)

	token, err := auth.GenerateAccessToken(env.userID, "sess-1", "alice@example.com", "Alice Smith", testSecret, time.Hour)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	w := env.serve(bearerRequest(token))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"id":"user-123"`) || !strings.Contains(w.Body.String(), "Alice Smith") {
		t.Errorf("handler did not receive the attached user: %s", w.Body.String())
	}
}

func TestAuth_MissingHeader(t *testing.T) {
	env := newTestEnv(t)

	w := env.serve(bearerRequest(""))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if code := decodeErrorCode(t, w); code != "authentication_required" {
		t.Errorf("error code = %q, want authentication_required", code)
	}
}

func TestAuth_MalformedToken(t *testing.T) {
	env := newTestEnv(t)

	w := env.serve(bearerRequest("not-a-jwt-at-all"))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if code := decodeErrorCode(t, w); code != "invalid_token" {
		t.Errorf("error code = %q, want invalid_token", code)
	}
}

func TestAuth_ExpiredToken(t *testing.T) {
	env := newTestEnv(t)

	// Expired an hour ago — well past the 30s clock-skew leeway.
	token, err := auth.GenerateAccessToken(env.userID, "sess-1", "alice@example.com", "Alice Smith", testSecret, -time.Hour)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	w := env.serve(bearerRequest(token))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if code := decodeErrorCode(t, w); code != "token_expired" {
		t.Errorf("error code = %q, want token_expired", code)
	}
	// Frontend relies on this header to trigger a silent refresh.
	if got := w.Header().Get("X-Token-Expired"); got != "true" {
		t.Errorf("X-Token-Expired = %q, want true", got)
	}
}

func TestAuth_ExpiredTokenHandcrafted(t *testing.T) {
	env := newTestEnv(t)

	// Hand-craft the token the way an external client would: iat and exp both
	// in the past, correctly signed. Must still be classified as expired.
	claims := auth.Claims{
		Email: "alice@example.com",
		Name:  "Alice Smith",
		Type:  auth.TokenTypeAccess,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   env.userID,
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token, err := tok.SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	w := env.serve(bearerRequest(token))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if code := decodeErrorCode(t, w); code != "token_expired" {
		t.Errorf("error code = %q, want token_expired", code)
	}
	if got := w.Header().Get("X-Token-Expired"); got != "true" {
		t.Errorf("X-Token-Expired = %q, want true", got)
	}
}

func TestAuth_TamperedToken(t *testing.T) {
	env := newTestEnv(t)

	valid, err := auth.GenerateAccessToken(env.userID, "sess-1", "alice@example.com", "Alice Smith", testSecret, time.Hour)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	// Flip one char in the signature so it no longer matches. The last char
	// of a base64url signature is always in [A-Za-z0-9_-], so replacing it
	// with 'A' (or 'B' if it already is 'A') is always a valid flip.
	last := valid[len(valid)-1]
	flip := byte('A')
	if last == 'A' {
		flip = 'B'
	}
	tampered := valid[:len(valid)-1] + string(flip)

	w := env.serve(bearerRequest(tampered))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if code := decodeErrorCode(t, w); code != "invalid_token" {
		t.Errorf("error code = %q, want invalid_token", code)
	}
}

func TestAuth_RefreshTokenRejected(t *testing.T) {
	env := newTestEnv(t)

	// A well-formed, correctly signed JWT with type "refresh" — must never
	// authenticate an API request.
	claims := auth.Claims{
		Email: "alice@example.com",
		Name:  "Alice Smith",
		Type:  "refresh",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   env.userID,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	refreshJWT, err := tok.SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign refresh JWT: %v", err)
	}

	w := env.serve(bearerRequest(refreshJWT))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestAuth_NoneAlgorithmRejected(t *testing.T) {
	env := newTestEnv(t)

	// The classic JWT attack: alg "none" with no signature.
	header := base64URL(`{"alg":"none","typ":"JWT"}`)
	payload := base64URL(`{"sub":"user-123","type":"access","exp":9999999999}`)
	noneToken := header + "." + payload + "."

	w := env.serve(bearerRequest(noneToken))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestAuth_UserNotInDatabase(t *testing.T) {
	env := newTestEnv(t)

	// Token validly signed for a user that does not exist (account deleted).
	token, err := auth.GenerateAccessToken("deleted-user", "", "gone@example.com", "Gone", testSecret, time.Hour)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	w := env.serve(bearerRequest(token))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if code := decodeErrorCode(t, w); code != "user_not_found" {
		t.Errorf("error code = %q, want user_not_found", code)
	}
}

func TestAuth_QueryParamTokenRejected(t *testing.T) {
	env := newTestEnv(t)

	token, err := auth.GenerateAccessToken(env.userID, "sess-1", "alice@example.com", "Alice Smith", testSecret, time.Hour)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	// Tokens in query strings are only accepted for WebSocket upgrades, never
	// on regular HTTP endpoints (they leak into logs).
	req := httptest.NewRequest(http.MethodGet, "/protected?token="+token, nil)
	w := env.serve(req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 (no header), got %d", w.Code)
	}
}

// base64URL encodes a raw JSON blob for a JWT segment.
func base64URL(seg string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(seg))
}
