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
	CreatedAt  string
	ExpiresAt  string
	LastUsedAt sql.NullString
	UserAgent  string
	IPAddress  string
	RevokedAt  sql.NullString
}

// CreateRefreshToken stores a hashed refresh token for a user session.
// Only the hash is ever stored — the raw token is returned to the client once.
//
// created_at is written explicitly in RFC3339 (not the migration's SQLite
// datetime('now') default) so that every timestamp column in this table has a
// uniform format: the sessions view sorts and renders them consistently, and
// `new Date()` in the dashboard parses RFC3339 in every browser.
func CreateRefreshToken(db *sql.DB, id, tokenHash, userID, expiresAt, userAgent, ipAddress string) error {
	_, err := db.Exec(
		`INSERT INTO refresh_tokens (id, token_hash, user_id, created_at, expires_at, user_agent, ip_address)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, tokenHash, userID, time.Now().UTC().Format(time.RFC3339), expiresAt, userAgent, ipAddress,
	)
	return err
}

// GetRefreshTokenByHash looks up a session by its token hash.
func GetRefreshTokenByHash(db *sql.DB, tokenHash string) (RefreshToken, error) {
	var rt RefreshToken
	err := db.QueryRow(
		`SELECT id, token_hash, user_id, created_at, expires_at, last_used_at, user_agent, ip_address, revoked_at
		 FROM refresh_tokens WHERE token_hash = ?`,
		tokenHash,
	).Scan(&rt.ID, &rt.TokenHash, &rt.UserID, &rt.CreatedAt, &rt.ExpiresAt, &rt.LastUsedAt, &rt.UserAgent, &rt.IPAddress, &rt.RevokedAt)
	return rt, err
}

// ListSessionsByUser returns the active (non-revoked, non-expired) sessions
// for a user, most recently used first — the Layer 5A Step 4B sessions view.
func ListSessionsByUser(db *sql.DB, userID string) ([]RefreshToken, error) {
	rows, err := db.Query(
		`SELECT id, token_hash, user_id, created_at, expires_at, last_used_at, user_agent, ip_address, revoked_at
		 FROM refresh_tokens
		 WHERE user_id = ? AND revoked_at IS NULL AND expires_at > ?
		 ORDER BY COALESCE(last_used_at, created_at) DESC`,
		userID, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []RefreshToken
	for rows.Next() {
		var rt RefreshToken
		if err := rows.Scan(&rt.ID, &rt.TokenHash, &rt.UserID, &rt.CreatedAt, &rt.ExpiresAt, &rt.LastUsedAt, &rt.UserAgent, &rt.IPAddress, &rt.RevokedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, rt)
	}
	return sessions, rows.Err()
}

// RevokeAllRefreshTokens revokes every active session for a user (Layer 5A
// Step 4A Level 2 — logout all devices). Returns how many were revoked.
func RevokeAllRefreshTokens(db *sql.DB, userID string) (int64, error) {
	res, err := db.Exec(
		"UPDATE refresh_tokens SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL",
		time.Now().UTC().Format(time.RFC3339), userID,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// RevokeSessionForUser revokes one session, but only if it belongs to the
// given user and is still active — preventing cross-user revocation. Returns
// whether the session was actually revoked.
func RevokeSessionForUser(db *sql.DB, sessionID, userID string) (bool, error) {
	res, err := db.Exec(
		"UPDATE refresh_tokens SET revoked_at = ? WHERE id = ? AND user_id = ? AND revoked_at IS NULL",
		time.Now().UTC().Format(time.RFC3339), sessionID, userID,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// DeleteExpiredRefreshTokens removes refresh tokens whose expiry has passed
// (Layer 5A Step 4A Level 3 — periodic cleanup). Returns how many were
// deleted. Tokens must be deleted, not just revoked: expired sessions can no
// longer be refreshed, so the rows are garbage.
func DeleteExpiredRefreshTokens(db *sql.DB, cutoff string) (int64, error) {
	res, err := db.Exec(
		"DELETE FROM refresh_tokens WHERE expires_at < ?", cutoff,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// RevokeRefreshToken atomically invalidates a session (used by logout and
// rotation). It only revokes a token that is still active; the returned bool
// reports whether the token was actually revoked (false = already used), which
// lets the refresh handler detect a raced/replayed token.
func RevokeRefreshToken(db *sql.DB, id string) (bool, error) {
	res, err := db.Exec(
		"UPDATE refresh_tokens SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL",
		time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
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
