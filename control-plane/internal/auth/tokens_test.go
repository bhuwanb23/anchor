package auth

import (
	"strings"
	"testing"
	"time"
)

func TestGenerateRefreshToken(t *testing.T) {
	raw, hashed, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken error: %v", err)
	}

	if !strings.HasPrefix(raw, "rt_") {
		t.Errorf("raw = %q, want rt_ prefix", raw)
	}
	// 3 chars prefix + 64 hex chars (32 random bytes).
	if len(raw) != 3+64 {
		t.Errorf("raw length = %d, want %d", len(raw), 3+64)
	}
	if hashed == raw {
		t.Error("hash must differ from raw token")
	}
	if hashed != HashRefreshToken(raw) {
		t.Error("HashRefreshToken must round-trip consistently")
	}

	// Randomness: two tokens must differ.
	raw2, _, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken error: %v", err)
	}
	if raw == raw2 {
		t.Error("two refresh tokens must not be identical")
	}
}

func TestHashRefreshToken_IsSHA256Hex(t *testing.T) {
	raw, _, _ := GenerateRefreshToken()
	h := HashRefreshToken(raw)
	if len(h) != 64 {
		t.Errorf("hash length = %d, want 64 (SHA-256 hex)", len(h))
	}
	for _, c := range h {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Errorf("hash contains non-hex char %q", c)
			break
		}
	}
	if strings.Contains(h, "rt_") {
		t.Error("hash must not contain the rt_ prefix")
	}
}

func TestGenerateAccessToken_Claims(t *testing.T) {
	const secret = "a-test-secret-that-is-long-enough"
	token, err := GenerateAccessToken("user-123", "alice@example.com", "Alice Smith", secret, 24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateAccessToken error: %v", err)
	}

	claims, err := ValidateJWT(token, secret)
	if err != nil {
		t.Fatalf("ValidateJWT error: %v", err)
	}

	if claims.UserID() != "user-123" {
		t.Errorf("sub = %q, want user-123", claims.UserID())
	}
	if claims.Email != "alice@example.com" {
		t.Errorf("email = %q, want alice@example.com", claims.Email)
	}
	if claims.Name != "Alice Smith" {
		t.Errorf("name = %q, want Alice Smith", claims.Name)
	}
	if claims.Type != TokenTypeAccess {
		t.Errorf("type = %q, want access", claims.Type)
	}
	if claims.IssuedAt == nil || claims.ExpiresAt == nil {
		t.Fatal("iat/exp must be set")
	}
}

func TestGenerateAccessToken_WrongSecretFails(t *testing.T) {
	token, err := GenerateAccessToken("u1", "a@b.com", "A", "secret-one", time.Hour)
	if err != nil {
		t.Fatalf("GenerateAccessToken error: %v", err)
	}
	if _, err := ValidateJWT(token, "secret-two"); err == nil {
		t.Error("ValidateJWT with wrong secret = nil, want error")
	}
}

func TestGenerateAccessToken_Expiry(t *testing.T) {
	const secret = "a-test-secret-that-is-long-enough"
	// Expire immediately; validation must reject the token.
	token, err := GenerateAccessToken("u1", "a@b.com", "A", secret, time.Nanosecond)
	if err != nil {
		t.Fatalf("GenerateAccessToken error: %v", err)
	}
	if _, err := ValidateJWT(token, secret); err == nil {
		t.Error("expired access token validated successfully, want error")
	}
}
