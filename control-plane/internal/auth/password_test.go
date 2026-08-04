package auth

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestHashPassword_ReturnsBcryptHash(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword error: %v", err)
	}

	// bcrypt hashes have the form $2a$cost$... — the version prefix proves it
	// is bcrypt and the stored hash must never contain the plaintext.
	if !strings.HasPrefix(hash, "$2") {
		t.Errorf("hash = %q, want bcrypt prefix $2", hash)
	}
	if strings.Contains(hash, "correct horse") {
		t.Error("hash must never contain the plaintext password")
	}

	// Unique salt: hashing the same password twice yields different hashes.
	hash2, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword error: %v", err)
	}
	if hash == hash2 {
		t.Error("two hashes of the same password should differ (random salt)")
	}
}

func TestHashPassword_UsesCostTwelve(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword error: %v", err)
	}
	cost, err := bcrypt.Cost([]byte(hash))
	if err != nil {
		t.Fatalf("bcrypt.Cost error: %v", err)
	}
	if cost != 12 {
		t.Errorf("cost = %d, want 12", cost)
	}
}

func TestVerifyPassword(t *testing.T) {
	hash, err := HashPassword("s3cr3t-password")
	if err != nil {
		t.Fatalf("HashPassword error: %v", err)
	}

	if err := VerifyPassword("s3cr3t-password", hash); err != nil {
		t.Errorf("VerifyPassword(correct) = %v, want nil", err)
	}
	if err := VerifyPassword("wrong-password", hash); err == nil {
		t.Error("VerifyPassword(wrong) = nil, want error")
	}
}

func TestVerifyPassword_RejectsMalformedHash(t *testing.T) {
	if err := VerifyPassword("anything", "not-a-bcrypt-hash"); err == nil {
		t.Error("VerifyPassword with malformed hash = nil, want error")
	}
}
