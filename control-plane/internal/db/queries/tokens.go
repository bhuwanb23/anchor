package queries

import (
	"database/sql"
)

func CreateRegistrationToken(db *sql.DB, id, tokenHash, userID, serverName, expiresAt string) error {
	_, err := db.Exec(
		"INSERT INTO registration_tokens (id, token_hash, user_id, server_name, expires_at) VALUES (?, ?, ?, ?, ?)",
		id, tokenHash, userID, serverName, expiresAt,
	)
	return err
}

func FindRegistrationTokenByHash(db *sql.DB, tokenHash string) (id, userID, serverName, expiresAt string, usedAt sql.NullString, err error) {
	err = db.QueryRow(
		"SELECT id, user_id, server_name, expires_at, used_at FROM registration_tokens WHERE token_hash = ?",
		tokenHash,
	).Scan(&id, &userID, &serverName, &expiresAt, &usedAt)
	return
}

func MarkRegistrationTokenUsed(db *sql.DB, tokenID, ip string) error {
	_, err := db.Exec(
		"UPDATE registration_tokens SET used_at = datetime('now'), used_by_ip = ? WHERE id = ?",
		ip, tokenID,
	)
	return err
}

func DeleteExpiredRegistrationTokens(db *sql.DB) (int64, error) {
	result, err := db.Exec(
		"DELETE FROM registration_tokens WHERE expires_at < datetime('now') AND used_at IS NULL",
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
