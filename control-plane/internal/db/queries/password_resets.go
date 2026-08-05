package queries

import (
	"database/sql"
	"time"
)

// PasswordReset is a row of the password_resets table (Layer 5A Step 7).
type PasswordReset struct {
	ID        string
	TokenHash string
	UserID    string
	CreatedAt string
	ExpiresAt string
	UsedAt    sql.NullString
}

// CreatePasswordReset stores a hashed, single-use password-reset token.
// Only the SHA-256 hash is ever stored — the raw token is emailed once.
// created_at is written explicitly in RFC3339 (matching refresh_tokens) so
// the columns have a uniform format.
func CreatePasswordReset(db *sql.DB, id, tokenHash, userID, expiresAt string) error {
	_, err := db.Exec(
		`INSERT INTO password_resets (id, token_hash, user_id, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?)`,
		id, tokenHash, userID, time.Now().UTC().Format(time.RFC3339), expiresAt,
	)
	return err
}

// GetPasswordResetByHash looks up a reset token by its hash.
func GetPasswordResetByHash(db *sql.DB, tokenHash string) (PasswordReset, error) {
	var pr PasswordReset
	err := db.QueryRow(
		`SELECT id, token_hash, user_id, created_at, expires_at, used_at
		 FROM password_resets WHERE token_hash = ?`,
		tokenHash,
	).Scan(&pr.ID, &pr.TokenHash, &pr.UserID, &pr.CreatedAt, &pr.ExpiresAt, &pr.UsedAt)
	return pr, err
}

// MarkPasswordResetUsed marks a reset token as consumed so it cannot be
// reused (Layer 5A Step 7B.7).
func MarkPasswordResetUsed(db *sql.DB, id string) error {
	_, err := db.Exec(
		"UPDATE password_resets SET used_at = ? WHERE id = ?",
		time.Now().UTC().Format(time.RFC3339), id,
	)
	return err
}

// DeleteExpiredPasswordResets removes reset tokens whose expiry has passed,
// whether used or not. Returns how many were deleted. Runs weekly alongside
// refresh-token pruning; used rows are also garbage once their window passes.
func DeleteExpiredPasswordResets(db *sql.DB, cutoff string) (int64, error) {
	res, err := db.Exec("DELETE FROM password_resets WHERE expires_at < ?", cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
