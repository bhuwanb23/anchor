package db

import (
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// Step 5A — a local backup round-trips: snapshot + gzip → restore → the
// restored file is a valid SQLite database containing the same data.
func TestLocalBackupRoundTrip(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "live.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open live db: %v", err)
	}
	defer db.Close()

	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := db.Exec(
		"INSERT INTO users (id, email, name, password_hash) VALUES (?, ?, ?, ?)",
		"u-1", "alice@example.com", "Alice", "hash"); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	backupDir := filepath.Join(dir, "backups")
	gzPath, err := CreateLocalBackup(db, dbPath, backupDir)
	if err != nil {
		t.Fatalf("CreateLocalBackup: %v", err)
	}

	// The backup exists, is gzipped, and sits in the backup dir.
	if _, err := os.Stat(gzPath); err != nil {
		t.Fatalf("backup file missing: %v", err)
	}
	if !strings.HasSuffix(gzPath, ".gz") {
		t.Errorf("backup %q not gzipped", gzPath)
	}
	if filepath.Dir(gzPath) != backupDir {
		t.Errorf("backup in %q, want %q", filepath.Dir(gzPath), backupDir)
	}

	// Restore into a fresh path and verify the data survived.
	restoredPath := filepath.Join(dir, "restored.db")
	if err := RestoreFromLocal(gzPath, restoredPath); err != nil {
		t.Fatalf("RestoreFromLocal: %v", err)
	}
	restored, err := sql.Open("sqlite", restoredPath)
	if err != nil {
		t.Fatalf("open restored db: %v", err)
	}
	defer restored.Close()

	var count int
	if err := restored.QueryRow("SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		t.Fatalf("count restored users: %v", err)
	}
	if count != 1 {
		t.Errorf("restored user count = %d, want 1", count)
	}
}

// Step 5A — only the newest 4 local backups are kept; older ones are pruned.
func TestPruneLocalBackups(t *testing.T) {
	dir := t.TempDir()
	// Names embed sortable UTC timestamps: oldest first.
	names := []string{
		"yourplatform-20260701-000000.db.gz",
		"yourplatform-20260701-060000.db.gz",
		"yourplatform-20260701-120000.db.gz",
		"yourplatform-20260701-180000.db.gz",
		"yourplatform-20260702-000000.db.gz",
		"yourplatform-20260702-060000.db.gz",
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Non-backup files must be ignored.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	removed, err := pruneLocalBackups(dir, 4)
	if err != nil {
		t.Fatalf("pruneLocalBackups: %v", err)
	}
	if removed != 2 {
		t.Errorf("removed %d, want 2", removed)
	}

	entries, _ := os.ReadDir(dir)
	var kept []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".gz") {
			kept = append(kept, e.Name())
		}
	}
	if len(kept) != 4 {
		t.Fatalf("kept %d backups, want 4: %v", len(kept), kept)
	}
	if kept[0] != "yourplatform-20260701-120000.db.gz" || kept[3] != "yourplatform-20260702-060000.db.gz" {
		t.Errorf("kept the wrong backups: %v", kept)
	}
}

// Step 5A — S3 object pruning keeps the newest 30 and returns the older keys.
// Distinct sortable names verify the OLDEST objects are pruned, not just a
// count.
func TestS3ObjectsToPrune(t *testing.T) {
	// 20260701-000000 … 000034, lexicographically oldest first.
	var names []string
	for i := 0; i < 35; i++ {
		names = append(names, fmt.Sprintf("control-plane/yourplatform-20260701-%05d.db.gz.enc", i))
	}

	toPrune := s3ObjectsToPrune(names, s3BackupKeep)
	if len(toPrune) != 5 {
		t.Fatalf("toPrune = %d, want 5", len(toPrune))
	}
	if toPrune[0] != "control-plane/yourplatform-20260701-00000.db.gz.enc" ||
		toPrune[4] != "control-plane/yourplatform-20260701-00004.db.gz.enc" {
		t.Errorf("pruned the wrong objects (want the 5 oldest): %v", toPrune)
	}

	// Fewer than the keep count → nothing to prune.
	if got := s3ObjectsToPrune(names[:s3BackupKeep], s3BackupKeep); got != nil {
		t.Errorf("expected nothing to prune, got %v", got)
	}
	if got := s3ObjectsToPrune(nil, s3BackupKeep); got != nil {
		t.Errorf("expected nil for empty input, got %v", got)
	}
}

// Step 5A — S3 backups are compressed and encrypted: the blob is not the
// plaintext, decrypts back to it, and rejects a wrong passphrase or tampering.
func TestEncryptDecryptRoundTrip(t *testing.T) {
	plain := []byte("compressed sqlite bytes here")
	pass := []byte("correct horse battery staple")

	enc, err := encryptBytes(plain, pass)
	if err != nil {
		t.Fatalf("encryptBytes: %v", err)
	}
	if bytes.Equal(enc, plain) {
		t.Error("ciphertext equals plaintext — nothing was encrypted")
	}

	dec, err := decryptBytes(enc, pass)
	if err != nil {
		t.Fatalf("decryptBytes: %v", err)
	}
	if !bytes.Equal(dec, plain) {
		t.Error("round-trip mismatch")
	}

	if _, err := decryptBytes(enc, []byte("wrong passphrase")); err == nil {
		t.Error("wrong passphrase decrypted successfully")
	}

	// Tampered ciphertext (flip the first data byte) must fail GCM auth.
	tampered := append([]byte(nil), enc...)
	tampered[len(tampered)-1] ^= 0xff
	if _, err := decryptBytes(tampered, pass); err == nil {
		t.Error("tampered ciphertext decrypted successfully")
	}
}

// Step 5A — the encrypted blob carries a format-version header so future
// format changes fail loudly on restore.
func TestDecryptRejectsUnknownVersion(t *testing.T) {
	enc, err := encryptBytes([]byte("data"), []byte("pass"))
	if err != nil {
		t.Fatal(err)
	}
	enc[0] = 99
	if _, err := decryptBytes(enc, []byte("pass")); err == nil {
		t.Error("unknown version byte accepted")
	}
	if _, err := decryptBytes([]byte{1}, []byte("pass")); err == nil {
		t.Error("truncated blob accepted")
	}
}

// Step 5A — S3 upload path is skipped until all S3 settings are present,
// including the encryption passphrase.
func TestS3Configured(t *testing.T) {
	base := BackupSettings{
		S3Endpoint:  "https://s3.example.com",
		S3AccessKey: "ak",
		S3SecretKey: "sk",
		S3Bucket:    "backups",
	}
	if base.S3Configured() {
		t.Error("configured without a passphrase")
	}
	base.S3Passphrase = "pw"
	if !base.S3Configured() {
		t.Error("not configured with all fields set")
	}
	base.S3Bucket = ""
	if base.S3Configured() {
		t.Error("configured with an empty bucket")
	}
}
