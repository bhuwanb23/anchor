package queries

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func setupTransactionDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		CREATE TABLE accounts (id TEXT PRIMARY KEY, balance INTEGER NOT NULL);
		INSERT INTO accounts (id, balance) VALUES ('a', 100);
	`); err != nil {
		t.Fatal(err)
	}
	return db
}

// Layer 5C Step 3B — a nil fn result commits all writes atomically.
func TestWithTransaction_CommitsOnNil(t *testing.T) {
	db := setupTransactionDB(t)
	defer db.Close()

	err := WithTransaction(db, func(tx *sql.Tx) error {
		if _, err := tx.Exec("UPDATE accounts SET balance = balance - 40 WHERE id = 'a'"); err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT INTO accounts (id, balance) VALUES ('b', 40)"); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	var a, b int
	if err := db.QueryRow("SELECT balance FROM accounts WHERE id = 'a'").Scan(&a); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT balance FROM accounts WHERE id = 'b'").Scan(&b); err != nil {
		t.Fatal(err)
	}
	if a != 60 || b != 40 {
		t.Fatalf("commit not applied: a=%d b=%d, want 60/40", a, b)
	}
}

// Layer 5C Step 3B — a fn error rolls back every write in the transaction.
func TestWithTransaction_RollsBackOnError(t *testing.T) {
	db := setupTransactionDB(t)
	defer db.Close()

	err := WithTransaction(db, func(tx *sql.Tx) error {
		if _, err := tx.Exec("UPDATE accounts SET balance = 0 WHERE id = 'a'"); err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT INTO accounts (id, balance) VALUES ('b', 40)"); err != nil {
			return err
		}
		// Simulate a mid-transaction failure after the writes above.
		if _, err := tx.Exec("UPDATE missing_table SET x = 1"); err != nil {
			return err
		}
		return nil
	})
	if err == nil {
		t.Fatal("expected error to propagate from fn")
	}

	var a int
	if err := db.QueryRow("SELECT balance FROM accounts WHERE id = 'a'").Scan(&a); err != nil {
		t.Fatal(err)
	}
	if a != 100 {
		t.Fatalf("rollback not applied: a=%d want 100", a)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM accounts").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("row count=%d want 1 (insert must be rolled back)", count)
	}
}

// The helper must also surface a Begin failure cleanly (closed DB).
func TestWithTransaction_BeginError(t *testing.T) {
	db := setupTransactionDB(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	err := WithTransaction(db, func(tx *sql.Tx) error { return nil })
	if err == nil {
		t.Fatal("expected error from closed database")
	}
}
