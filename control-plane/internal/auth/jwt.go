package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TokenTypeAccess marks an access JWT so refresh tokens can never be confused
// with them (refresh tokens are opaque strings, not JWTs).
const TokenTypeAccess = "access"

// Claims is the JWT payload for access tokens (Layer 5A Step 2B).
//
//	{
//	  "sub":   "550e8400-...",  // user ID (standard subject)
//	  "email": "user@example.com",
//	  "name":  "Alice Smith",
//	  "sid":   "token-record-id",  // refresh-token row ID (session)
//	  "iat":   1705312800,
//	  "exp":   1705399200,
//	  "type":  "access"
//	}
//
// Only identity claims are embedded — permissions are fetched from the DB on
// access so they can change at runtime. Never put secrets or large data here.
//
// SessionID ("sid") links the access token back to the refresh-token row it
// was issued alongside. The sessions view (Layer 5A Step 4B) uses it to mark
// which session is making the current request. It is empty for tokens issued
// before this field existed, in which case no session is marked current.
type Claims struct {
	Email     string `json:"email"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	SessionID string `json:"sid,omitempty"`
	jwt.RegisteredClaims
}

// UserID returns the subject claim (the user's ID).
func (c *Claims) UserID() string {
	return c.Subject
}

// GenerateAccessToken issues a signed HS256 access token for a user.
// sessionID is the refresh-token row ID this token was issued alongside; it is
// embedded as the "sid" claim so the sessions view can identify the current
// session. Pass "" when no session is associated.
func GenerateAccessToken(userID, sessionID, email, name, secret string, expiry time.Duration) (string, error) {
	claims := Claims{
		Email:     email,
		Name:      name,
		Type:      TokenTypeAccess,
		SessionID: sessionID,
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
// It is the strict Layer 5A Step 3B validator (HS256 only, 30s clock skew,
// access-type only) — see ValidateAccessToken.
func ValidateJWT(tokenString, secret string) (*Claims, error) {
	return ValidateAccessToken(tokenString, secret)
}

// ErrTokenNotAccess is returned when a well-formed, correctly signed token
// carries a type claim other than "access" (e.g. a refresh token or a token
// from a future token type).
var ErrTokenNotAccess = errors.New("token is not an access token")

// ValidateAccessToken parses, verifies, and type-checks an access token
// (Layer 5A Step 3B). Validation order:
//
//  1. structure — the JWT must parse (three base64url parts)
//  2. signature — verified against the secret (catches tampering)
//  3. algorithm — must be HS256; "none" and asymmetric algs are rejected
//  4. expiry — exp must be in the future, with 30 seconds of clock-skew
//     leeway for servers that drift
//  5. type — the type claim must be "access", so a refresh token can never
//     be accepted as an access token
func ValidateAccessToken(tokenString, secret string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(
		tokenString,
		&Claims{},
		func(token *jwt.Token) (interface{}, error) {
			return []byte(secret), nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithLeeway(30*time.Second),
	)
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, jwt.ErrTokenInvalidClaims
	}
	if claims.Type != TokenTypeAccess {
		return nil, ErrTokenNotAccess
	}
	return claims, nil
}
