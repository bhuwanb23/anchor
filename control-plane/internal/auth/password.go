package auth

import (
	"golang.org/x/crypto/bcrypt"
)

// bcryptCost is the work factor for password hashing (Layer 5A Step 1B).
// At cost 12 a hash takes ~300ms on a modern CPU — imperceptible for a
// login, expensive enough to slow offline brute force by ~2^12.
const bcryptCost = 12

func HashPassword(password string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(hashed), nil
}

func VerifyPassword(password, hash string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}