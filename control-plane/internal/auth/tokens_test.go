package auth

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
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
	// Expire an hour in the past — well beyond the 30s clock-skew leeway —
	// so validation must reject the token.
	token, err := GenerateAccessToken("u1", "a@b.com", "A", secret, -time.Hour)
	if err != nil {
		t.Fatalf("GenerateAccessToken error: %v", err)
	}
	if _, err := ValidateJWT(token, secret); err == nil {
		t.Error("expired access token validated successfully, want error")
	}
}

func TestValidateAccessToken_LeewayAcceptsBrieflyExpired(t *testing.T) {
	const secret = "a-test-secret-that-is-long-enough"
	// Expired 10 seconds ago: inside the 30s clock-skew leeway, so a server
	// whose clock drifted by a few seconds still accepts the token.
	token, err := GenerateAccessToken("u1", "a@b.com", "A", secret, -10*time.Second)
	if err != nil {
		t.Fatalf("GenerateAccessToken error: %v", err)
	}
	if _, err := ValidateAccessToken(token, secret); err != nil {
		t.Errorf("token expired within leeway rejected: %v", err)
	}
}

func TestValidateAccessToken_RejectsNonAccessType(t *testing.T) {
	const secret = "a-test-secret-that-is-long-enough"

	// Build a well-formed, correctly signed JWT whose type is "refresh" —
	// this is exactly what a refresh token must never do.
	claims := Claims{
		Email: "a@b.com",
		Name:  "A",
		Type:  "refresh",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "u1",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	refreshJWT, err := tok.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	if _, err := ValidateAccessToken(refreshJWT, secret); !errors.Is(err, ErrTokenNotAccess) {
		t.Errorf("refresh-typed JWT accepted, want ErrTokenNotAccess (got %v)", err)
	}
}

func TestValidateAccessToken_RejectsNoneAlgorithm(t *testing.T) {
	const secret = "a-test-secret-that-is-long-enough"

	// The classic JWT attack: an unsigned token with alg "none".
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"u1","type":"access","exp":9999999999}`))
	noneToken := header + "." + payload + "."

	if _, err := ValidateAccessToken(noneToken, secret); err == nil {
		t.Error("alg=none token accepted, want error")
	}
}

func TestValidateAccessToken_RejectsNonHS256Algorithm(t *testing.T) {
	const secret = "a-test-secret-that-is-long-enough"

	// A token signed with a different (non-HS256) algorithm must be rejected:
	// we only ever issue HS256, so anything else is suspect.
	claims := Claims{
		Email: "a@b.com",
		Name:  "A",
		Type:  TokenTypeAccess,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "u1",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS512, claims)
	hs512Token, err := tok.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	if _, err := ValidateAccessToken(hs512Token, secret); err == nil {
		t.Error("HS512 token accepted, want error")
	}
}
