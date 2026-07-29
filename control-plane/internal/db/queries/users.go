package queries

import (
	"database/sql"
)

func InsertUser(db *sql.DB, id, email, passwordHash string) error {
	_, err := db.Exec(
		"INSERT INTO users (id, email, password_hash) VALUES (?, ?, ?)",
		id, email, passwordHash,
	)
	return err
}

func GetUserByEmail(db *sql.DB, email string) (id string, passwordHash string, err error) {
	err = db.QueryRow(
		"SELECT id, password_hash FROM users WHERE email = ?",
		email,
	).Scan(&id, &passwordHash)
	return
}

func GetUserByID(db *sql.DB, id string) (email string, err error) {
	err = db.QueryRow(
		"SELECT email FROM users WHERE id = ?",
		id,
	).Scan(&email)
	return
}