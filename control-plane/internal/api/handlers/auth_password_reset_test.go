package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yourname/yourplatform/control-plane/internal/api/handlers"
	"github.com/yourname/yourplatform/control-plane/internal/auth"
)

// emailMsg is a single captured email from fakeMailer.
type emailMsg struct {
	to      string
	subject string
	body    string
}

// fakeMailer records emails instead of sending them, so tests can assert on
// the reset link without needing SMTP.
type fakeMailer struct {
	mu    sync.Mutex
	sends []emailMsg
}

func (f *fakeMailer) Send(to, subject, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sends = append(f.sends, emailMsg{to: to, subject: subject, body: body})
	return nil
}

func (f *fakeMailer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sends)
}

func (f *fakeMailer) last() emailMsg {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sends) == 0 {
		return emailMsg{}
	}
	return f.sends[len(f.sends)-1]
}

// resetTokenFromBody extracts the raw pw_ token from a reset email body.
func resetTokenFromBody(body string) string {
	const marker = "/reset?token="
	i := strings.Index(body, marker)
	if i < 0 {
		return ""
	}
	rest := body[i+len(marker):]
	if len(rest) >= 67 { // "pw_" + 64 hex chars
		return rest[:67]
	}
	return rest
}

// forgotRequest posts to ForgotPassword and returns the recorder.
func forgotRequest(h *handlers.Auth, email string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"email": email})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/forgot-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ForgotPassword(w, req)
	return w
}

// resetRequest posts to ResetPassword and returns the recorder.
func resetRequest(h *handlers.Auth, token, newPassword string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"token": token, "new_password": newPassword})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/reset-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ResetPassword(w, req)
	return w
}

// requestResetFor registers a fresh user (unique email) and performs a forgot
// request, returning the raw reset token captured from the email and the
// user's email address.
func requestResetFor(t *testing.T, h *handlers.Auth, mail *fakeMailer) (string, string) {
	t.Helper()
	email := "reset" + uniqueSuffix() + "@example.com"
	if w := registerRequest(t, h, map[string]string{
		"name":     "Alice Smith",
		"email":    email,
		"password": "correct horse battery staple",
	}); w.Code != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d", w.Code)
	}

	w := forgotRequest(h, email)
	if w.Code != http.StatusOK {
		t.Fatalf("forgot: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	token := resetTokenFromBody(mail.last().body)
	if token == "" {
		t.Fatalf("forgot: no reset token found in email body: %q", mail.last().body)
	}
	return token, email
}

func TestForgotPassword_ExistingEmail(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	mail := &fakeMailer{}
	h := newAuthHandler(db)
	h.Mailer = mail

	email := "alice@example.com"
	registerRequest(t, h, map[string]string{
		"name":     "Alice Smith",
		"email":    email,
		"password": "correct horse battery staple",
	})

	w := forgotRequest(h, email)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["message"] != "If an account exists with this email, a reset link has been sent." {
		t.Errorf("message = %v, want generic response", resp["message"])
	}

	// Email was sent to the right address with a raw pw_ token in the link.
	if mail.count() != 1 {
		t.Fatalf("emails sent = %d, want 1", mail.count())
	}
	sent := mail.last()
	if sent.to != "alice@example.com" {
		t.Errorf("email to = %q, want alice@example.com", sent.to)
	}
	if !strings.Contains(sent.body, "/reset?token=pw_") {
		t.Errorf("email body missing reset link: %q", sent.body)
	}

	// DB stores the HASH, not the raw token, with a ~1-hour expiry.
	token := resetTokenFromBody(sent.body)
	var storedHash, expiresAt string
	if err := db.QueryRow("SELECT token_hash, expires_at FROM password_resets").Scan(&storedHash, &expiresAt); err != nil {
		t.Fatalf("query password_resets: %v", err)
	}
	if storedHash == token {
		t.Error("raw reset token must never be stored in the database")
	}
	if storedHash != auth.HashResetToken(token) {
		t.Error("stored hash does not match SHA-256 of the raw token")
	}
	if strings.Contains(storedHash, "pw_") {
		t.Error("stored hash should be hex only, not contain the pw_ prefix")
	}
	exp, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		t.Fatalf("parse expires_at: %v", err)
	}
	if d := time.Until(exp); d < 50*time.Minute || d > 70*time.Minute {
		t.Errorf("expiry = %v from now, want ~1 hour", d)
	}
}

func TestForgotPassword_UnknownEmailSameResponse(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	mail := &fakeMailer{}
	h := newAuthHandler(db)
	h.Mailer = mail

	w := forgotRequest(h, "nobody@example.com")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["message"] != "If an account exists with this email, a reset link has been sent." {
		t.Errorf("message = %v, want identical generic response", resp["message"])
	}

	// No token row and no email — the endpoint must not reveal the account is missing.
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM password_resets").Scan(&count); err != nil {
		t.Fatalf("count password_resets: %v", err)
	}
	if count != 0 {
		t.Errorf("password_resets rows = %d, want 0", count)
	}
	if mail.count() != 0 {
		t.Errorf("emails sent = %d, want 0", mail.count())
	}
}

func TestForgotPassword_InvalidEmailFormatSameResponse(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	h := newAuthHandler(db)

	w := forgotRequest(h, "not-an-email")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["message"] != "If an account exists with this email, a reset link has been sent." {
		t.Errorf("message = %v, want identical generic response", resp["message"])
	}
}

func TestResetPassword_Success(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	mail := &fakeMailer{}
	h := newAuthHandler(db)
	h.Mailer = mail

	token, email := requestResetFor(t, h, mail)

	w := resetRequest(h, token, "brand new strong password")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["message"] != "Password updated. Please log in." {
		t.Errorf("message = %v, want generic success", resp["message"])
	}

	// Token is now consumed.
	var usedAt string
	if err := db.QueryRow("SELECT used_at FROM password_resets").Scan(&usedAt); err != nil {
		t.Fatalf("query used_at: %v", err)
	}
	if usedAt == "" {
		t.Error("token should be marked used after reset")
	}

	// Old password is dead, new password works.
	if w := loginWith(h, email, "correct horse battery staple"); w.Code != http.StatusUnauthorized {
		t.Errorf("old password: expected 401, got %d", w.Code)
	}
	if w := loginWith(h, email, "brand new strong password"); w.Code != http.StatusOK {
		t.Errorf("new password: expected 200, got %d", w.Code)
	}
}

func TestResetPassword_RevokesAllSessions(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	mail := &fakeMailer{}
	h := newAuthHandler(db)
	h.Mailer = mail

	// Register a user and log in BEFORE the reset so a session exists.
	email := "reset" + uniqueSuffix() + "@example.com"
	registerRequest(t, h, map[string]string{"name": "Alice Smith", "email": email, "password": "correct horse battery staple"})
	loginW := loginWith(h, email, "correct horse battery staple")
	if loginW.Code != http.StatusOK {
		t.Fatalf("login: expected 200, got %d", loginW.Code)
	}
	var loginResp map[string]interface{}
	json.NewDecoder(loginW.Body).Decode(&loginResp)
	oldRefresh := loginResp["refresh_token"].(string)

	// Same user requests the reset.
	forgotW := forgotRequest(h, email)
	if forgotW.Code != http.StatusOK {
		t.Fatalf("forgot: expected 200, got %d", forgotW.Code)
	}
	token := resetTokenFromBody(mail.last().body)
	if token == "" {
		t.Fatalf("forgot: no reset token in email body")
	}
	if w := resetRequest(h, token, "brand new strong password"); w.Code != http.StatusOK {
		t.Fatalf("reset: expected 200, got %d", w.Code)
	}

	// The pre-reset session must no longer refresh — forced logout everywhere.
	if w := refreshHelper(h, oldRefresh); w.Code != http.StatusUnauthorized {
		t.Errorf("refresh after reset: expected 401, got %d", w.Code)
	}
}

func TestResetPassword_UsedToken(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	mail := &fakeMailer{}
	h := newAuthHandler(db)
	h.Mailer = mail

	token, _ := requestResetFor(t, h, mail)
	if w := resetRequest(h, token, "brand new strong password"); w.Code != http.StatusOK {
		t.Fatalf("first reset: expected 200, got %d", w.Code)
	}

	w := resetRequest(h, token, "yet another password")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("second reset: expected 400, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "This reset link has already been used" {
		t.Errorf("error = %v, want used-token message", resp["error"])
	}
}

func TestResetPassword_ExpiredToken(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	mail := &fakeMailer{}
	h := newAuthHandler(db)
	h.Mailer = mail

	token, _ := requestResetFor(t, h, mail)

	// Backdate the expiry so the link is dead.
	if _, err := db.Exec(
		"UPDATE password_resets SET expires_at = ?",
		time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
	); err != nil {
		t.Fatalf("backdate expiry: %v", err)
	}

	w := resetRequest(h, token, "brand new strong password")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "This reset link has expired. Please request a new one." {
		t.Errorf("error = %v, want expired message", resp["error"])
	}
}

func TestResetPassword_UnknownToken(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	h := newAuthHandler(db)

	w := resetRequest(h, "pw_not-a-real-token", "brand new strong password")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "Invalid or expired reset link" {
		t.Errorf("error = %v, want generic invalid message", resp["error"])
	}
}

func TestResetPassword_InvalidNewPassword(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	mail := &fakeMailer{}
	h := newAuthHandler(db)
	h.Mailer = mail

	token, _ := requestResetFor(t, h, mail)

	for _, pw := range []string{"", "short", "password", "12345678"} {
		w := resetRequest(h, token, pw)
		if w.Code != http.StatusBadRequest {
			t.Errorf("password %q: expected 400, got %d", pw, w.Code)
		}
	}
}

// loginWith posts to Login and returns the recorder.
func loginWith(h *handlers.Auth, email, password string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Login(w, req)
	return w
}
