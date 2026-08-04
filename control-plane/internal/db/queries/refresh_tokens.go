package queries

import (
	"database/sql"
	"time"
)

// RefreshToken is a row of the refresh_tokens table.
type RefreshToken struct {
	ID         string
	TokenHash  string
	UserID     string
	ExpiresAt  string
	LastUsedAt sql.NullString
	RevokedAt  sql.NullString
}

// CreateRefreshToken stores a hashed refresh token for a user session.
// Only the hash is ever stored — the raw token is returned to the client once.
func CreateRefreshToken(db *sql.DB, id, tokenHash, userID, expiresAt, userAgent, ipAddress string) error {
	_, err := db.Exec(
		`INSERT INTO refresh_tokens (id, token_hash, user_id, expires_at, user_agent, ip_address)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, tokenHash, userID, expiresAt, userAgent, ipAddress,
	)
	return err
}

// GetRefreshTokenByHash looks up a session by its token hash.
func GetRefreshTokenByHash(db *sql.DB, tokenHash string) (RefreshToken, error) {
	var rt RefreshToken
	err := db.QueryRow(
		`SELECT id, token_hash, user_id, expires_at, last_used_at, revoked_at
		 FROM refresh_tokens WHERE token_hash = ?`,
		tokenHash,
	).Scan(&rt.ID, &rt.TokenHash, &rt.UserID, &rt.ExpiresAt, &rt.LastUsedAt, &rt.RevokedAt)
	return rt, err
}

// RevokeRefreshToken invalidates a session (used by logout and rotation).
func RevokeRefreshToken(db *sql.DB, id string) error {
	_, err := db.Exec(
		"UPDATE refresh_tokens SET revoked_at = ? WHERE id = ?",
		time.Now().UTC().Format(time.RFC3339), id,
	)
	return err
}

// UpdateRefreshTokenLastUsed records the most recent use of a session
// (sliding-expiry bookkeeping for the sessions view).
func UpdateRefreshTokenLastUsed(db *sql.DB, id string) error {
	_, err := db.Exec(
		"UPDATE refresh_tokens SET last_used_at = ? WHERE id = ?",
		time.Now().UTC().Format(time.RFC3339), id,
	)
	return err
}

// RevokeAllUserRefreshTokens invalidates every session for a user
// (logout-all / password reset).
func RevokeAllUserRefreshTokens(db *sql.DB, userID string) error {
	_, err := db.Exec(
		"UPDATE refresh_tokens SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL",
		time.Now().UTC().Format(time.RFC3339), userID,
	)
	return err
}
