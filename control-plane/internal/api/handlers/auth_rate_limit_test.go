package handlers_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yourname/yourplatform/control-plane/internal/ratelimit"
)

// TestRegister_RateLimited mirrors the Layer 6 Step 2E rule: registration is
// capped per IP (default 5 per hour). Distinct emails keep the per-email
// dimension out of the way so the per-IP limit is what trips.
func TestRegister_RateLimited(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	h := newAuthHandler(db)
	h.Limiter = ratelimit.New() // production defaults: 5/IP per hour

	prefix := fmt.Sprintf("newbie-%d", time.Now().UnixNano())
	for i := 0; i < 5; i++ {
		email := fmt.Sprintf("%s-%d@example.com", prefix, i)
		if w := registerRequest(t, h, map[string]string{
			"name":     "New User",
			"email":    email,
			"password": "correct horse battery staple",
		}); w.Code != http.StatusCreated {
			t.Fatalf("registration %d: expected 201, got %d: %s", i+1, w.Code, w.Body.String())
		}
	}

	// The 6th registration from the same IP trips the limit.
	w := registerRequest(t, h, map[string]string{
		"name":     "New User",
		"email":    prefix + "-6@example.com",
		"password": "correct horse battery staple",
	})
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("6th registration: expected 429, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Too many registration attempts") {
		t.Errorf("body missing retry message: %s", w.Body.String())
	}
	if got := w.Header().Get("Retry-After"); got == "" {
		t.Error("expected Retry-After header on 429")
	}
}

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

// TestLogin_DefaultIPLimitTenAttempts mirrors the plan's exact rule: with the
// production defaults (10 attempts per IP per 15 min), the 11th failed login
// from one IP returns 429. Distinct emails keep the per-email limit out of
// the way so the per-IP limit is what trips.
func TestLogin_DefaultIPLimitTenAttempts(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	h := newAuthHandler(db)
	h.Limiter = ratelimit.New() // production defaults: 10/IP, 5/email

	prefix := fmt.Sprintf("attack-%d", time.Now().UnixNano())
	for i := 0; i < 10; i++ {
		email := fmt.Sprintf("%s-%d@example.com", prefix, i)
		if w := loginWith(h, email, "wrong-password"); w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d", i+1, w.Code)
		}
	}

	// The 11th attempt from the same IP trips the limit.
	w := loginWith(h, prefix+"-final@example.com", "wrong-password")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("11th attempt: expected 429, got %d: %s", w.Code, w.Body.String())
	}
}

// TestLogin_RecoversAfterWindow verifies the limiter opens up again once the
// sliding window passes (the plan's "wait 15 minutes: login works again",
// tested with a compressed window). The window must be longer than the ~300ms
// bcrypt compare, otherwise each attempt expires before the next arrives.
func TestLogin_RecoversAfterWindow(t *testing.T) {
	db := setupAuthTestDB(t)
	defer db.Close()
	h := newAuthHandler(db)
	h.Limiter = ratelimit.NewWithLimits(2, 2, 2, 2, 2, 3*time.Second)

	if w := loginWith(h, "alice@example.com", "wrong-password"); w.Code != http.StatusUnauthorized {
		t.Fatalf("first attempt: expected 401, got %d", w.Code)
	}
	if w := loginWith(h, "alice@example.com", "wrong-password"); w.Code != http.StatusUnauthorized {
		t.Fatalf("second attempt: expected 401, got %d", w.Code)
	}
	if w := loginWith(h, "alice@example.com", "wrong-password"); w.Code != http.StatusTooManyRequests {
		t.Fatalf("third attempt: expected 429, got %d", w.Code)
	}

	time.Sleep(3500 * time.Millisecond) // comfortably past the 3s window
	if w := loginWith(h, "alice@example.com", "wrong-password"); w.Code != http.StatusUnauthorized {
		t.Errorf("after window: expected 401, got %d (limiter did not recover)", w.Code)
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
