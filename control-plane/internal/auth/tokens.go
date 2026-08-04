package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

func GenerateRegistrationToken() (rawToken string, hashedToken string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generate random token: %w", err)
	}

	rawToken = "reg_" + hex.EncodeToString(b)

	hash := sha256.Sum256([]byte(rawToken))
	hashedToken = hex.EncodeToString(hash[:])

	return rawToken, hashedToken, nil
}

func HashRegistrationToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

// GenerateRefreshToken creates a new opaque refresh token (Layer 5A Step 2C).
// It is deliberately NOT a JWT: refresh tokens must be revocable, so they are
// stored (hashed) in the database, and a random string is simpler and equally
// secure. The "rt_" prefix distinguishes them from access tokens.
func GenerateRefreshToken() (rawToken string, hashedToken string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generate refresh token: %w", err)
	}

	rawToken = "rt_" + hex.EncodeToString(b)
	hashedToken = HashRefreshToken(rawToken)
	return rawToken, hashedToken, nil
}

// HashRefreshToken hashes a refresh token with SHA-256 so only the hash is
// ever stored (same policy as registration tokens).
func HashRefreshToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}
