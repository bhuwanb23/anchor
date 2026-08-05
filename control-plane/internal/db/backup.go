package db

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"golang.org/x/crypto/pbkdf2"
)

// BackupSettings carries everything the database-backup runner needs. The
// S3 fields are optional: uploads are skipped until all of them (including
// an encryption passphrase) are present.
type BackupSettings struct {
	DBPath        string        // live database file (source of the snapshot)
	BackupDir     string        // local backup directory
	LocalInterval time.Duration // local backup cadence; 0 → 6 hours
	S3Endpoint    string
	S3AccessKey   string
	S3SecretKey   string
	S3Bucket      string
	S3Region      string
	S3Passphrase  string // derives the AES-256 key; S3 uploads are never sent in the clear
}

const (
	// S3BackupPrefix is the object-key prefix for control-plane DB backups in
	// the shared bucket ("control-plane/" per Layer 5C Step 5A).
	S3BackupPrefix = "control-plane/"
	// localBackupKeep is how many local backups are retained — 24 hours of
	// 6-hourly history.
	localBackupKeep = 4
	// s3BackupKeep is how many daily S3 backups are retained — 30 days.
	s3BackupKeep = 30
	// backupEncVersion prefixes every encrypted blob so future format
	// changes can be detected on restore.
	backupEncVersion = 1
	// backupSaltSize and backupKDFIterations harden the passphrase-derived
	// key: PBKDF2 with a random per-backup salt makes brute-forcing a weak
	// passphrase impractical (a raw SHA-256 of the passphrase would not).
	backupSaltSize      = 16
	backupKDFIterations = 100_000
)

// S3Configured reports whether the S3 upload half can run.
func (s BackupSettings) S3Configured() bool {
	return s.S3Endpoint != "" && s.S3AccessKey != "" && s.S3SecretKey != "" &&
		s.S3Bucket != "" && s.S3Passphrase != ""
}

// snapshotInto creates a consistent copy of the live database at dstPath
// using SQLite's VACUUM INTO statement (Layer 5C Step 5A).
//
// modernc.org/sqlite does not expose the C sqlite3_backup API, and VACUUM
// INTO is its equivalent for online backups: it writes a consistent snapshot
// while the database stays open and in use, works with WAL mode, and never
// takes a long lock (writes continue during the copy). It is NOT a "copy the
// file while it is running" approach.
//
// The destination must not already exist (VACUUM INTO refuses to overwrite),
// and the path is embedded as a literal in SQL, so single quotes are escaped.
func snapshotInto(db *sql.DB, dstPath string) error {
	if err := os.Remove(dstPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale backup target: %w", err)
	}
	escaped := strings.ReplaceAll(dstPath, "'", "''")
	if _, err := db.Exec("VACUUM INTO '" + escaped + "'"); err != nil {
		return fmt.Errorf("vacuum into: %w", err)
	}
	return nil
}

// CreateLocalBackup snapshots the database, gzips it, and prunes old local
// backups, keeping only the newest 4 (Layer 5C Step 5A). It returns the path
// of the new .gz backup.
func CreateLocalBackup(db *sql.DB, dbPath, backupDir string) (string, error) {
	if dbPath == "" {
		return "", errors.New("database path is empty")
	}
	if backupDir == "" {
		return "", errors.New("backup directory is empty")
	}
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return "", fmt.Errorf("create backup dir: %w", err)
	}

	ts := time.Now().UTC().Format("20060102-150405")
	rawPath := filepath.Join(backupDir, "yourplatform-"+ts+".db")
	gzPath := rawPath + ".gz"

	if err := snapshotInto(db, rawPath); err != nil {
		return "", err
	}
	if err := gzipFile(rawPath, gzPath); err != nil {
		_ = os.Remove(rawPath)
		return "", fmt.Errorf("gzip backup: %w", err)
	}
	_ = os.Remove(rawPath)

	pruned, err := pruneLocalBackups(backupDir, localBackupKeep)
	if err != nil {
		slog.Warn("prune local database backups", "error", err)
	}
	slog.Info("database backup created", "path", gzPath, "pruned", pruned)
	return gzPath, nil
}

// gzipFile compresses src into dst (best compression, 0600 perms).
func gzipFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_ = out.Chmod(0o600)

	// NewWriterLevel only errors on an invalid level; BestCompression is valid.
	gz, err := gzip.NewWriterLevel(out, gzip.BestCompression)
	if err != nil {
		out.Close()
		return err
	}

	if _, err := io.Copy(gz, in); err != nil {
		gz.Close()
		out.Close()
		return err
	}
	if err := gz.Close(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// pruneLocalBackups removes all but the newest `keep` .gz backups in dir,
// newest judged by filename (the generated names embed a UTC timestamp that
// sorts lexicographically). Returns how many were removed.
func pruneLocalBackups(dir string, keep int) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".gz") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // ascending = oldest first
	removed := 0
	for i := 0; i < len(names)-keep; i++ {
		if err := os.Remove(filepath.Join(dir, names[i])); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

// ---------------------------------------------------------------------------
// Encryption (AES-256-GCM)
// ---------------------------------------------------------------------------

// encryptBytes seals data with AES-256-GCM. The key is derived from the
// passphrase with PBKDF2 (a random per-backup salt is stored in the blob),
// and the format version byte is authenticated as GCM additional data so it
// cannot be tampered with. Output layout:
// [version][16-byte salt][12-byte nonce][ciphertext].
func encryptBytes(data, passphrase []byte) ([]byte, error) {
	salt := make([]byte, backupSaltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, err
	}
	key := pbkdf2.Key(passphrase, salt, backupKDFIterations, 32, sha256.New)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	sealed := gcm.Seal(nil, nonce, data, []byte{backupEncVersion})
	out := make([]byte, 0, 1+backupSaltSize+gcm.NonceSize()+len(sealed))
	out = append(out, backupEncVersion)
	out = append(out, salt...)
	out = append(out, nonce...)
	out = append(out, sealed...)
	return out, nil
}

// decryptBytes reverses encryptBytes.
func decryptBytes(data, passphrase []byte) ([]byte, error) {
	if len(data) < 1+backupSaltSize {
		return nil, errors.New("ciphertext too short")
	}
	version, salt := data[0], data[1:1+backupSaltSize]
	if version != backupEncVersion {
		return nil, fmt.Errorf("unsupported backup format version %d", version)
	}
	key := pbkdf2.Key(passphrase, salt, backupKDFIterations, 32, sha256.New)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(data) < 1+backupSaltSize+nonceSize {
		return nil, errors.New("ciphertext too short")
	}
	nonce := data[1+backupSaltSize : 1+backupSaltSize+nonceSize]
	return gcm.Open(nil, nonce, data[1+backupSaltSize+nonceSize:], []byte{version})
}

// ---------------------------------------------------------------------------
// S3 upload (Layer 5C Step 5A)
// ---------------------------------------------------------------------------

// newS3Client builds a minio client. An http:// endpoint disables TLS;
// anything else (including bare hostnames) uses TLS.
func newS3Client(s BackupSettings) (*minio.Client, error) {
	useTLS := !strings.HasPrefix(s.S3Endpoint, "http://")
	endpoint := strings.TrimPrefix(strings.TrimPrefix(s.S3Endpoint, "https://"), "http://")
	return minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(s.S3AccessKey, s.S3SecretKey, ""),
		Secure: useTLS,
		Region: s.S3Region,
	})
}

// UploadBackupToS3 uploads a .gz backup as an encrypted object under
// control-plane/<filename>.enc, then prunes old objects keeping the newest
// 30. Returns the object key.
//
// The whole .gz file is held in memory for the GCM seal — fine at the plan's
// ~200MB scale; revisit before supporting multi-GB databases.
func UploadBackupToS3(ctx context.Context, client *minio.Client, bucket, gzPath, passphrase string) (string, error) {
	data, err := os.ReadFile(gzPath)
	if err != nil {
		return "", err
	}
	enc, err := encryptBytes(data, []byte(passphrase))
	if err != nil {
		return "", err
	}

	objectName := S3BackupPrefix + filepath.Base(gzPath) + ".enc"
	_, err = client.PutObject(ctx, bucket, objectName, bytes.NewReader(enc), int64(len(enc)),
		minio.PutObjectOptions{ContentType: "application/octet-stream"})
	if err != nil {
		return "", fmt.Errorf("put object: %w", err)
	}

	pruned, err := pruneS3Backups(ctx, client, bucket, S3BackupPrefix, s3BackupKeep)
	if err != nil {
		slog.Warn("prune s3 database backups", "error", err)
	}
	slog.Info("database backup uploaded to S3", "object", objectName, "pruned", pruned)
	return objectName, nil
}

// pruneS3Backups removes all but the newest `keep` objects under prefix.
func pruneS3Backups(ctx context.Context, client *minio.Client, bucket, prefix string, keep int) (int, error) {
	var names []string
	for obj := range client.ListObjects(ctx, bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if obj.Err != nil {
			return 0, obj.Err
		}
		names = append(names, obj.Key)
	}
	toPrune := s3ObjectsToPrune(names, keep)
	for _, name := range toPrune {
		if err := client.RemoveObject(ctx, bucket, name, minio.RemoveObjectOptions{}); err != nil {
			return 0, err
		}
	}
	return len(toPrune), nil
}

// s3ObjectsToPrune returns the object keys to delete: everything older than
// the newest `keep`. Our generated names embed a sortable UTC timestamp, so
// plain string sorting orders them oldest → newest.
func s3ObjectsToPrune(names []string, keep int) []string {
	if len(names) <= keep {
		return nil
	}
	sort.Strings(names)
	return names[:len(names)-keep]
}

// ---------------------------------------------------------------------------
// Restore (Layer 5C Step 5B)
// ---------------------------------------------------------------------------

// RestoreFromLocal decompresses a .gz database backup into dstPath.
func RestoreFromLocal(gzPath, dstPath string) error {
	in, err := os.Open(gzPath)
	if err != nil {
		return err
	}
	defer in.Close()
	gz, err := gzip.NewReader(in)
	if err != nil {
		return err
	}
	defer gz.Close()
	return writeRestoredFile(gz, dstPath)
}

// RestoreFromS3 downloads an encrypted object, decrypts it, and decompresses
// it into dstPath. The whole object is held in memory while it is decrypted
// (GCM is not a stream cipher); same scale caveat as UploadBackupToS3.
func RestoreFromS3(ctx context.Context, client *minio.Client, bucket, objectName, dstPath, passphrase string) error {
	obj, err := client.GetObject(ctx, bucket, objectName, minio.GetObjectOptions{})
	if err != nil {
		return err
	}
	data, err := io.ReadAll(obj)
	obj.Close()
	if err != nil {
		return err
	}
	plain, err := decryptBytes(data, []byte(passphrase))
	if err != nil {
		return err
	}
	return writeRestoredFile(bytes.NewReader(plain), dstPath)
}

// writeRestoredFile writes the decompressed stream to dstPath with 0600
// permissions.
func writeRestoredFile(r io.Reader, dstPath string) error {
	out, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	_ = out.Chmod(0o600)
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		return err
	}
	// fsync so a crash immediately after a restore cannot leave the live
	// database file truncated on disk.
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// ---------------------------------------------------------------------------
// Scheduler
// ---------------------------------------------------------------------------

// StartDatabaseBackups launches the background backup goroutine (Layer 5C
// Step 5A): a local compressed backup every LocalInterval (default 6h) and an
// encrypted S3 upload every 24h when S3 is configured. A first backup also
// runs ~30s after startup so a fresh install gets protection immediately.
// Failures are logged and never stop the loop.
func StartDatabaseBackups(db *sql.DB, settings BackupSettings) {
	go func() {
		time.Sleep(30 * time.Second)

		// One snapshot serves both destinations at startup: keep it locally
		// and, when configured, upload the same file to S3.
		if gzPath, err := CreateLocalBackup(db, settings.DBPath, settings.BackupDir); err != nil {
			slog.Warn("database backup failed at startup", "error", err)
		} else if settings.S3Configured() {
			runS3Backup(gzPath, settings)
		}

		localInterval := settings.LocalInterval
		if localInterval <= 0 {
			localInterval = 6 * time.Hour
		}
		localTicker := time.NewTicker(localInterval)
		defer localTicker.Stop()
		s3Ticker := time.NewTicker(24 * time.Hour)
		defer s3Ticker.Stop()

		for {
			select {
			case <-localTicker.C:
				runLocalBackup(db, settings)
			case <-s3Ticker.C:
				if !settings.S3Configured() {
					continue
				}
				// The daily S3 upload takes its own fresh snapshot, which the
				// local retention also keeps.
				gzPath, err := CreateLocalBackup(db, settings.DBPath, settings.BackupDir)
				if err != nil {
					slog.Warn("s3 database backup failed (snapshot)", "error", err)
					continue
				}
				runS3Backup(gzPath, settings)
			}
		}
	}()
}

func runLocalBackup(db *sql.DB, s BackupSettings) {
	if _, err := CreateLocalBackup(db, s.DBPath, s.BackupDir); err != nil {
		slog.Warn("local database backup failed", "error", err)
	}
}

// runS3Backup uploads an already-created snapshot and prunes old objects.
func runS3Backup(gzPath string, s BackupSettings) {
	client, err := newS3Client(s)
	if err != nil {
		slog.Warn("s3 database backup failed (client)", "error", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if _, err := UploadBackupToS3(ctx, client, s.S3Bucket, gzPath, s.S3Passphrase); err != nil {
		slog.Warn("s3 database backup failed (upload)", "error", err)
	}
}
