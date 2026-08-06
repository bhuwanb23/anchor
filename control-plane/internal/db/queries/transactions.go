package queries

import "database/sql"

// WithTransaction runs fn inside a single database transaction (Layer 5C
// Step 3B). If fn returns nil the transaction is committed; if fn returns an
// error the transaction is rolled back and the error is returned unchanged.
//
// Usage for operations that touch more than one table atomically:
//
//	err := queries.WithTransaction(db, func(tx *sql.Tx) error {
//		if err := createUser(tx, user); err != nil {
//			return err // triggers rollback
//		}
//		return nil // triggers commit
//	})
//
// Transactions are used for: user registration (user + personal team),
// server registration (server + used token), deployment records (deployment
// + app update), and any other multi-table write.
func WithTransaction(db *sql.DB, fn func(tx *sql.Tx) error) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
