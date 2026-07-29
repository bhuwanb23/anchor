package queries

import (
	"database/sql"
	"time"
)

func InsertServer(db *sql.DB, id, userID, name, token string) error {
	_, err := db.Exec(
		"INSERT INTO servers (id, user_id, name, token) VALUES (?, ?, ?, ?)",
		id, userID, name, token,
	)
	return err
}

func QueryServersByUser(db *sql.DB, userID string) (*sql.Rows, error) {
	return db.Query(
		"SELECT id, name, status, connected_at, last_seen FROM servers WHERE user_id = ?",
		userID,
	)
}

func GetServerByToken(db *sql.DB, token string) (id, userID, name string, err error) {
	err = db.QueryRow(
		"SELECT id, user_id, name FROM servers WHERE token = ?",
		token,
	).Scan(&id, &userID, &name)
	return
}

func UpdateServerStatus(db *sql.DB, serverID, status string) error {
	_, err := db.Exec(
		"UPDATE servers SET status = ?, last_seen = ? WHERE id = ?",
		status, time.Now().UTC().Format(time.RFC3339), serverID,
	)
	return err
}