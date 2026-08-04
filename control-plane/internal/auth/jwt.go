package auth

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Config struct {
	Secret    string
	ExpiryHrs int
}

// TokenTypeAccess marks an access JWT so refresh tokens can never be confused
// with them (refresh tokens are opaque strings, not JWTs).
const TokenTypeAccess = "access"

// Claims is the JWT payload for access tokens (Layer 5A Step 2B).
//
//	{
//	  "sub":   "550e8400-...",  // user ID (standard subject)
//	  "email": "user@example.com",
//	  "name":  "Alice Smith",
//	  "iat":   1705312800,
//	  "exp":   1705399200,
//	  "type":  "access"
//	}
//
// Only identity claims are embedded — permissions are fetched from the DB on
// access so they can change at runtime. Never put secrets or large data here.
type Claims struct {
	Email string `json:"email"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	jwt.RegisteredClaims
}

// UserID returns the subject claim (the user's ID).
func (c *Claims) UserID() string {
	return c.Subject
}

// GenerateAccessToken issues a signed HS256 access token for a user.
func GenerateAccessToken(userID, email, name, secret string, expiry time.Duration) (string, error) {
	claims := Claims{
		Email: email,
		Name:  name,
		Type:  TokenTypeAccess,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(expiry)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ValidateJWT parses and verifies an access token's signature and expiry.
func ValidateJWT(tokenString, secret string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, jwt.ErrTokenInvalidClaims
}
