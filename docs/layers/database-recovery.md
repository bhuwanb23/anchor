# Database Recovery Runbook — Layer 5C Step 5B

The control-plane database (`yourplatform.db`) is the platform: users, servers,
deployments, teams, alerts. If it is lost, every agent must be re-registered and
every account is gone. This runbook covers restoring it from a backup.

---

## Backup layout (what exists where)

```
Local (on the control-plane host):
  <DB_BACKUP_DIR>/yourplatform-YYYYMMDD-HHMMSS.db.gz
    → VACUUM INTO snapshot, gzip-compressed, 0600 perms
    → created every DB_BACKUP_INTERVAL_HOURS (default 6)
    → the newest 4 are kept; older ones are deleted

S3 (when configured):
  s3://<S3_BUCKET>/control-plane/yourplatform-YYYYMMDD-HHMMSS.db.gz.enc
    → gzip-compressed snapshot, encrypted with AES-256-GCM
    → key derived from DB_BACKUP_ENCRYPTION_KEY via PBKDF2 (100k iterations)
      with a random per-backup salt stored in the blob; the version byte is
      authenticated as GCM additional data
    → uploaded every 24 hours; the newest 30 are kept
```

The backup names embed a UTC timestamp, so newest = lexicographically largest
filename.

---

## Recovery procedure

### Step 1 — Stop the control plane

```bash
systemctl stop yourplatform-control-plane
# or: kill the process running control-plane/cmd/server
```

Never restore into a database file that the control plane still has open.

### Step 2 — Restore from a backup

From local:

```bash
# 1. find the newest backup
ls -lt <DB_BACKUP_DIR>/yourplatform-*.db.gz | head -1

# 2. decompress it into the live database path
gunzip -c <newest-backup>.db.gz > /var/lib/yourplatform/control-plane/yourplatform.db
chmod 600 /var/lib/yourplatform/control-plane/yourplatform.db
```

From S3 (encrypted):

```bash
# 1. list backups, newest first
#    (any S3 client: aws cli, mc, rclone …)
mc ls mybackups/control-plane/ | sort -r | head -1

# 2. download the newest object
mc cp mybackups/control-plane/<newest>.db.gz.enc ./restore.db.gz.enc

# 3. decrypt, then decompress, into the live database path
#    KEY = the same value as DB_BACKUP_ENCRYPTION_KEY
openssl enc -d -aes-256-gcm -K <sha256(KEY)> -iv <nonce> -in restore.db.gz.enc ... 
```

The `openssl` one-liner above is intentionally abbreviated: the Go tooling
handles the exact format (`[version][12-byte nonce][ciphertext]`, nonce stored
inline). The simplest reliable path is the built-in helper:

```go
// control-plane/internal/db: RestoreFromS3 / RestoreFromLocal
// RestoreFromS3(ctx, client, bucket, object, dstPath, passphrase)
//   → downloads → decrypts → decompresses → writes dstPath (0600)
```

### Step 3 — Verify the database is readable

```bash
sqlite3 /var/lib/yourplatform/control-plane/yourplatform.db \
  "SELECT COUNT(*) FROM users; SELECT COUNT(*) FROM servers;"

# sanity: schema_migrations should list every applied migration
sqlite3 ... "SELECT MAX(version) FROM schema_migrations;"
```

If either count is 0 when it should not be, or the migration max is lower than
expected, restore an older backup instead.

### Step 4 — Start the control plane

```bash
systemctl start yourplatform-control-plane
```

Migrations run automatically on startup; the restored database is already at
the right version, so nothing new is applied.

### Step 5 — Agents reconnect

Agents hold their `agent_id` + secret. The restored database still contains the
server rows, so authentication succeeds immediately on the next reconnect
(agents retry with backoff, so no agent restart is needed).

### Step 6 — Dashboard works

Users log in normally. Expect small gaps:

- metrics history: up to the last backup (~6h, or ~24h for S3-only)
- deployment/event history: whatever was written after the last backup is gone
- alerts: recent alert state may be stale until the next health report

---

## When NOT to restore

- Only the WAL files are damaged and the main `.db` is intact → restart the
  control plane first; SQLite replays the WAL.
- You want a *specific user's* data back (that is Layer 3C restic backup
  territory, not this runbook).

## Tested guarantee

`db/backup_test.go` covers the round trip: create data → `CreateLocalBackup`
(snapshot + gzip) → `RestoreFromLocal` → open the restored file and read the
data back, plus encryption/decryption round trips and retention pruning
(local 4-kept, S3 30-kept naming helpers).
