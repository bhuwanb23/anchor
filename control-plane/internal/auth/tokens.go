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
