package queries

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// newRefreshTokenTestDB creates an in-memory DB with the refresh_tokens table
// as defined by migration 018, pinned to one connection (the :memory: database
// lives only while that connection is open).
func newRefreshTokenTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)

	_, err = db.Exec(`
		CREATE TABLE refresh_tokens (
			id           TEXT PRIMARY KEY,
			token_hash   TEXT NOT NULL UNIQUE,
			user_id      TEXT NOT NULL,
			created_at   TEXT NOT NULL DEFAULT (datetime('now')),
			expires_at   TEXT NOT NULL,
			last_used_at TEXT,
			user_agent   TEXT,
			ip_address   TEXT,
			revoked_at   TEXT
		);
	`)
	if err != nil {
		t.Fatalf("create refresh_tokens table: %v", err)
	}
	return db
}

// insertSession inserts a refresh-token row with the given overrides.
// revokedAt is an interface{} so we can pass a real SQL NULL (nil) instead of
// an empty string — queries filter on revoked_at IS NULL.
func insertSession(t *testing.T, db *sql.DB, id, userID, expiresAt string, revoked bool) {
	t.Helper()
	var revokedAt interface{}
	if revoked {
		revokedAt = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := db.Exec(
		`INSERT INTO refresh_tokens (id, token_hash, user_id, created_at, expires_at, last_used_at, user_agent, ip_address, revoked_at)
		 VALUES (?, ?, ?, datetime('now'), ?, NULL, 'test-agent', '1.2.3.4', ?)`,
		id, "hash-"+id, userID, expiresAt, revokedAt,
	)
	if err != nil {
		t.Fatalf("insert session %s: %v", id, err)
	}
}

func TestListSessionsByUser_ActiveOnly(t *testing.T) {
	db := newRefreshTokenTestDB(t)
	const user = "user-1"
	now := time.Now().UTC().Format(time.RFC3339)
	future := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)

	insertSession(t, db, "s1", user, future, false)  // active
	insertSession(t, db, "s2", user, future, true)   // revoked
	insertSession(t, db, "s3", "user-2", future, false) // other user
	insertSession(t, db, "s4", user, now, false)     // expired

	sessions, err := ListSessionsByUser(db, user)
	if err != nil {
		t.Fatalf("ListSessionsByUser: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1 (only active non-expired own sessions)", len(sessions))
	}
	if sessions[0].ID != "s1" {
		t.Errorf("session id = %q, want s1", sessions[0].ID)
	}
	if sessions[0].UserAgent != "test-agent" || sessions[0].IPAddress != "1.2.3.4" {
		t.Errorf("device info = %q/%q, want test-agent/1.2.3.4", sessions[0].UserAgent, sessions[0].IPAddress)
	}
	// last_used_at was inserted as NULL — it must scan to an invalid NullString.
	if sessions[0].LastUsedAt.Valid {
		t.Error("LastUsedAt should be NULL for a never-refreshed session")
	}
}

func TestRevokeAllRefreshTokens(t *testing.T) {
	db := newRefreshTokenTestDB(t)
	const user = "user-1"
	future := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)

	insertSession(t, db, "s1", user, future, false)
	insertSession(t, db, "s2", user, future, false)
	insertSession(t, db, "s3", "user-2", future, false) // untouched

	n, err := RevokeAllRefreshTokens(db, user)
	if err != nil {
		t.Fatalf("RevokeAllRefreshTokens: %v", err)
	}
	if n != 2 {
		t.Errorf("revoked = %d, want 2", n)
	}

	// Both of user-1's sessions are revoked, user-2's is not.
	var revokedCount int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM refresh_tokens WHERE user_id = ? AND revoked_at IS NOT NULL", user,
	).Scan(&revokedCount); err != nil {
		t.Fatalf("count revoked: %v", err)
	}
	if revokedCount != 2 {
		t.Errorf("revoked rows = %d, want 2", revokedCount)
	}
	var otherRevoked int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM refresh_tokens WHERE user_id = 'user-2' AND revoked_at IS NOT NULL",
	).Scan(&otherRevoked); err != nil {
		t.Fatalf("count other revoked: %v", err)
	}
	if otherRevoked != 0 {
		t.Errorf("other user revoked rows = %d, want 0", otherRevoked)
	}

	// Idempotent: a second call revokes nothing.
	n2, err := RevokeAllRefreshTokens(db, user)
	if err != nil {
		t.Fatalf("second RevokeAllRefreshTokens: %v", err)
	}
	if n2 != 0 {
		t.Errorf("second revoke = %d, want 0", n2)
	}
}

func TestRevokeSessionForUser_Ownership(t *testing.T) {
	db := newRefreshTokenTestDB(t)
	future := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)

	insertSession(t, db, "s1", "user-1", future, false)
	insertSession(t, db, "s2", "user-2", future, false)

	// Owner revokes their own session.
	revoked, err := RevokeSessionForUser(db, "s1", "user-1")
	if err != nil {
		t.Fatalf("RevokeSessionForUser: %v", err)
	}
	if !revoked {
		t.Error("expected s1 to be revoked by its owner")
	}

	// Another user cannot revoke it (already revoked / not theirs → false).
	revoked, err = RevokeSessionForUser(db, "s1", "user-2")
	if err != nil {
		t.Fatalf("cross-user RevokeSessionForUser: %v", err)
	}
	if revoked {
		t.Error("cross-user revocation must not succeed")
	}

	// Unknown session id.
	revoked, err = RevokeSessionForUser(db, "nope", "user-1")
	if err != nil {
		t.Fatalf("unknown RevokeSessionForUser: %v", err)
	}
	if revoked {
		t.Error("unknown session must not be revoked")
	}
}

func TestDeleteExpiredRefreshTokens(t *testing.T) {
	db := newRefreshTokenTestDB(t)
	const user = "user-1"
	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	future := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)

	insertSession(t, db, "old1", user, past, false)
	insertSession(t, db, "old2", user, past, true) // expired AND revoked
	insertSession(t, db, "fresh", user, future, false)

	cutoff := time.Now().UTC().Format(time.RFC3339)
	n, err := DeleteExpiredRefreshTokens(db, cutoff)
	if err != nil {
		t.Fatalf("DeleteExpiredRefreshTokens: %v", err)
	}
	if n != 2 {
		t.Errorf("deleted = %d, want 2", n)
	}

	var remaining int
	if err := db.QueryRow("SELECT COUNT(*) FROM refresh_tokens").Scan(&remaining); err != nil {
		t.Fatalf("count remaining: %v", err)
	}
	if remaining != 1 {
		t.Errorf("remaining = %d, want 1 (the future session)", remaining)
	}
}
