package queries

import (
	"database/sql"
)

// User is a row of the users table.
type User struct {
	ID           string
	Email        string
	Name         string
	PasswordHash string
}

func InsertUser(db *sql.DB, id, email, name, passwordHash string) error {
	_, err := db.Exec(
		"INSERT INTO users (id, email, name, password_hash, updated_at) VALUES (?, ?, ?, ?, datetime('now'))",
		id, email, name, passwordHash,
	)
	return err
}

// GetUserByEmail looks up a user by (already normalized) email.
func GetUserByEmail(db *sql.DB, email string) (User, error) {
	var u User
	err := db.QueryRow(
		"SELECT id, email, name, password_hash FROM users WHERE email = ?",
		email,
	).Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash)
	return u, err
}

func GetUserByID(db *sql.DB, id string) (User, error) {
	var u User
	err := db.QueryRow(
		"SELECT id, email, name FROM users WHERE id = ?",
		id,
	).Scan(&u.ID, &u.Email, &u.Name)
	return u, err
}

// EmailExists reports whether a user with the given (normalized) email exists.
func EmailExists(db *sql.DB, email string) (bool, error) {
	var exists int
	err := db.QueryRow("SELECT COUNT(*) FROM users WHERE email = ?", email).Scan(&exists)
	return exists > 0, err
}

// UpdateUserPassword replaces a user's bcrypt password hash (Layer 5A
// Step 7B.6 — password reset).
func UpdateUserPassword(db *sql.DB, userID, passwordHash string) error {
	_, err := db.Exec(
		"UPDATE users SET password_hash = ?, updated_at = datetime('now') WHERE id = ?",
		passwordHash, userID,
	)
	return err
}
