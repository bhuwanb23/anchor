package handlers_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yourname/yourplatform/control-plane/internal/ratelimit"
)

func TestLogin_RateLimited(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	h := newAuthHandler(db)
	h.Limiter = ratelimit.NewWithLimits(1, 1, 1, 1, 1, 15*time.Minute)

	registerRequest(t, h, map[string]string{
		"name":     "Alice Smith",
		"email":    "alice@example.com",
		"password": "correct horse battery staple",
	})

	// First attempt passes the limiter and fails auth.
	if w := loginWith(h, "alice@example.com", "wrong-password"); w.Code != http.StatusUnauthorized {
		t.Fatalf("first attempt: expected 401, got %d", w.Code)
	}

	// Second attempt trips the per-IP limit: 429 with a retry hint.
	w := loginWith(h, "alice@example.com", "wrong-password")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("second attempt: expected 429, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "rate_limited") {
		t.Errorf("body missing rate_limited code: %s", body)
	}
	if !strings.Contains(body, "Too many login attempts") {
		t.Errorf("body missing retry message: %s", body)
	}
	if got := w.Header().Get("Retry-After"); got == "" {
		t.Error("expected Retry-After header on 429")
	}
}

func TestForgotPassword_RateLimited(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	h := newAuthHandler(db)
	h.Limiter = ratelimit.NewWithLimits(10, 5, 1, 1, 10, 15*time.Minute)

	// First request passes the limiter (and returns the generic response).
	if w := forgotRequest(h, "nobody@example.com"); w.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", w.Code)
	}

	// Second request from the same IP is throttled.
	w := forgotRequest(h, "nobody@example.com")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: expected 429, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Too many password reset requests") {
		t.Errorf("body missing retry message: %s", w.Body.String())
	}
}

func TestResetPassword_RateLimited(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	h := newAuthHandler(db)
	h.Limiter = ratelimit.NewWithLimits(10, 5, 10, 5, 1, 15*time.Minute)

	// First request passes the limiter; the bogus token then fails normally.
	if w := resetRequest(h, "pw_not-a-real-token", "brand new strong password"); w.Code != http.StatusBadRequest {
		t.Fatalf("first request: expected 400 (invalid token), got %d", w.Code)
	}

	// Second request is throttled per IP.
	w := resetRequest(h, "pw_not-a-real-token", "brand new strong password")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: expected 429, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Too many reset attempts") {
		t.Errorf("body missing retry message: %s", w.Body.String())
	}
}
