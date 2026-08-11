package db_test

// Layer 5C Overall Done Condition — the full 8-test sequence, executed
// against a real on-disk SQLite database (via db.Open, the production entry
// point with WAL + PRAGMAs), not mocks.
//
//	Test 1 — Fresh start
//	Test 2 — Second start (no re-migration, fast)
//	Test 3 — Data integrity (FK enforcement, delete semantics)
//	Test 4 — Concurrent access (10 readers + writers, no SQLITE_BUSY)
//	Test 5 — Metrics lifecycle (rollup + retention tiers)
//	Test 6 — Cleanup jobs (expired tokens)
//	Test 7 — Database backup while the database is being written
//	Test 8 — Full recovery (delete DB, restore, verify)

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yourname/yourplatform/control-plane/internal/alerts"
	"github.com/yourname/yourplatform/control-plane/internal/api"
	"github.com/yourname/yourplatform/control-plane/internal/config"
	"github.com/yourname/yourplatform/control-plane/internal/db"
	"github.com/yourname/yourplatform/control-plane/internal/db/queries"
	"github.com/yourname/yourplatform/control-plane/internal/mailer"
	"github.com/yourname/yourplatform/control-plane/internal/ws"
)

// newE2EDB opens a fresh migrated database file in a temp dir. The path is
// nested one level deep so parent-directory creation is exercised too.
func newE2EDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "data", "yourplatform.db")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	if err := db.Migrate(database); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	return database, dbPath
}

func rowCount(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return n
}

// rfc3339 returns an RFC3339 UTC timestamp offset by d — the exact format
// the agent uses for metric recorded_at values.
func rfc3339(d time.Duration) string {
	return time.Now().UTC().Add(d).Format(time.RFC3339)
}

// ---------------------------------------------------------------------------
// Test 1 — Fresh start
// ---------------------------------------------------------------------------

func Test5C_FreshStart(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "control-plane", "yourplatform.db")

	// No database file exists yet.
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("expected no database file before Open, stat err=%v", err)
	}

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer database.Close()

	// File + parent directory created.
	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("database file not created: %v", err)
	}
	if info.IsDir() {
		t.Fatal("database path is a directory")
	}
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("database file mode = %o, want 600", perm)
		}
		if dperm, err := os.Stat(filepath.Join(dir, "control-plane")); err == nil {
			if dperm.Mode().Perm() != 0o700 {
				t.Errorf("parent dir mode = %o, want 700", dperm.Mode().Perm())
			}
		}
	}

	// WAL mode active (Test 4 precondition).
	var mode string
	if err := database.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}

	// All migrations applied, in order.
	if err := db.Migrate(database); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if got := rowCount(t, database, "SELECT COUNT(*) FROM schema_migrations"); got != 27 {
		t.Fatalf("schema_migrations count = %d, want 27", got)
	}
	// Every migration name recorded.
	if got := rowCount(t, database, `SELECT COUNT(*) FROM schema_migrations WHERE name LIKE '%.sql'`); got != 27 {
		t.Errorf("recorded migration names = %d, want 27", got)
	}

	// Control plane is ready to accept requests: boot the real router against
	// this database and hit /health.
	cfg := &config.Config{FrontendURL: "http://localhost:3000", BaseDomain: "example.com", JWTSecret: "test-secret"}
	hub := ws.NewHub()
	sender := mailer.NewFromConfig(cfg)
	delivery := alerts.NewDelivery(database, sender, cfg)
	router := api.NewRouter(database, cfg, hub, delivery, sender)

	server := httptest.NewServer(router)
	defer server.Close()

	resp, err := http.Get(server.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /health status = %d, want 200", resp.StatusCode)
	}

	// The database is usable: insert and read back a user through the query
	// layer on the same handle the router is wired to.
	if err := queries.InsertUser(database, "u-e2e", "e2e@example.com", "E2E", "hash"); err != nil {
		t.Fatalf("InsertUser: %v", err)
	}
	u, err := queries.GetUserByEmail(database, "e2e@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if u.ID != "u-e2e" {
		t.Errorf("round-trip user id = %q, want u-e2e", u.ID)
	}
}

// ---------------------------------------------------------------------------
// Test 2 — Second start (no re-migration, fast)
// ---------------------------------------------------------------------------

func Test5C_SecondStartNoRemigration(t *testing.T) {
	database, dbPath := newE2EDB(t)
	before := rowCount(t, database, "SELECT COUNT(*) FROM schema_migrations")
	database.Close()

	// Restart: reopen the same file and run the migration check again.
	reopened, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()

	start := time.Now()
	if err := db.Migrate(reopened); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	elapsed := time.Since(start)

	after := rowCount(t, reopened, "SELECT COUNT(*) FROM schema_migrations")
	if after != before {
		t.Errorf("migration count changed across restart: %d -> %d", before, after)
	}
	if elapsed >= time.Second {
		t.Errorf("migration check took %v, plan requires < 1s", elapsed)
	}
}

// ---------------------------------------------------------------------------
// Test 3 — Data integrity
// ---------------------------------------------------------------------------

func Test5C_DataIntegrity(t *testing.T) {
	database, _ := newE2EDB(t)

	// Create a user, a server, and a team the user owns.
	if err := queries.InsertUser(database, "u1", "owner@example.com", "Owner", "hash"); err != nil {
		t.Fatalf("InsertUser: %v", err)
	}
	if err := queries.InsertServer(database, "s1", "u1", "prod", "tok-1"); err != nil {
		t.Fatalf("InsertServer: %v", err)
	}
	if err := queries.CreateTeam(database, "team-1", "Agency", "u1"); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if err := queries.AddTeamMember(database, "m1", "team-1", "u1", "owner", ""); err != nil {
		t.Fatalf("AddTeamMember: %v", err)
	}
	if err := queries.LinkServerToTeam(database, "s1", "team-1"); err != nil {
		t.Fatalf("LinkServerToTeam: %v", err)
	}

	// Foreign key violations are caught: a server for a nonexistent user.
	if err := queries.InsertServer(database, "s-bad", "no-such-user", "x", "tok-bad"); err == nil {
		t.Error("inserted a server for a nonexistent user (FK violation not caught)")
	}

	// Orphaned records cannot be created: same check via raw SQL.
	if _, err := database.Exec(
		"INSERT INTO servers (id, user_id, name, token) VALUES ('s-orphan', 'ghost', 'y', 'tok-orphan')",
	); err == nil {
		t.Error("created an orphaned server row")
	}

	// Duplicate email is rejected (UNIQUE).
	if err := queries.InsertUser(database, "u-dup", "owner@example.com", "Dup", "hash"); err == nil {
		t.Error("duplicate email accepted")
	}

	// Deleting the user who owns a team is BLOCKED (teams.owner_id has NO
	// ACTION — the plan's RESTRICT-style leg). Team ownership survives.
	if _, err := database.Exec("DELETE FROM users WHERE id = 'u1'"); err == nil {
		t.Error("deleted a user who owns a team")
	}
	if got := rowCount(t, database, "SELECT COUNT(*) FROM users WHERE id = 'u1'"); got != 1 {
		t.Errorf("owner user count = %d, want 1 (deletion must be blocked)", got)
	}
	if got := rowCount(t, database, "SELECT COUNT(*) FROM servers WHERE id = 's1'"); got != 1 {
		t.Errorf("server count = %d, want 1 (server survived the blocked delete)", got)
	}

	// Deleting a user WITHOUT a team cascades their servers away (servers
	// user_id is ON DELETE CASCADE — the plan's cascade leg).
	if err := queries.InsertUser(database, "u2", "no-team@example.com", "Alone", "hash"); err != nil {
		t.Fatalf("InsertUser u2: %v", err)
	}
	if err := queries.InsertServer(database, "s2", "u2", "loner", "tok-2"); err != nil {
		t.Fatalf("InsertServer s2: %v", err)
	}
	if _, err := database.Exec("DELETE FROM users WHERE id = 'u2'"); err != nil {
		t.Fatalf("deleting team-less user failed: %v", err)
	}
	if got := rowCount(t, database, "SELECT COUNT(*) FROM servers WHERE id = 's2'"); got != 0 {
		t.Errorf("cascade delete left server rows: %d, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// Test 4 — Concurrent access
// ---------------------------------------------------------------------------

func Test5C_ConcurrentAccess(t *testing.T) {
	database, _ := newE2EDB(t)

	if err := queries.InsertUser(database, "u-conc", "conc@example.com", "Conc", "hash"); err != nil {
		t.Fatal(err)
	}
	if err := queries.InsertServer(database, "s-conc", "u-conc", "conc", "tok-conc"); err != nil {
		t.Fatal(err)
	}

	const goroutines = 10
	const iterations = 25
	errs := make(chan error, goroutines*iterations)
	var wg sync.WaitGroup

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				var err error
				if g%2 == 0 {
					// Reader.
					var n int
					err = database.QueryRow(
						"SELECT COUNT(*) FROM servers WHERE user_id = ?", "u-conc",
					).Scan(&n)
				} else {
					// Writer: raw metric insert with a unique id.
					err = queries.InsertMetric(database,
						fmt.Sprintf("m-%d-%d", g, i), "s-conc",
						rfc3339(-time.Duration(g+i)*time.Minute),
						int64(g+i), float64(g+i), 1024, 2048, 50.0,
						10.0, 20.0, 50.0, 0.5, 0.25, true, 3, nil, 2)
				}
				if err != nil {
					errs <- fmt.Errorf("goroutine %d iter %d: %w", g, i, err)
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if strings.Contains(err.Error(), "SQLITE_BUSY") {
			t.Errorf("SQLITE_BUSY under concurrent access: %v", err)
		} else {
			t.Errorf("concurrent access error: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 5 — Metrics lifecycle
// ---------------------------------------------------------------------------

func Test5C_MetricsLifecycle(t *testing.T) {
	database, _ := newE2EDB(t)
	if err := queries.InsertUser(database, "u-met", "met@example.com", "Met", "hash"); err != nil {
		t.Fatal(err)
	}
	if err := queries.InsertServer(database, "s-met", "u-met", "met", "tok-met"); err != nil {
		t.Fatal(err)
	}

	// 100 raw rows in the real agent format (RFC3339 T-format):
	//   - 60 rows ~2-3h old, inside the hourly rollup window (2 hour buckets)
	//   - 30 rows 9 days old, past the 7-day raw retention
	//   - 10 rows 1 minute old, fresh (should survive everything)
	//
	// Each bucket reuses one timestamp (second precision) so all its rows
	// land in the same hour bucket deterministically.
	buckets := []struct {
		offset time.Duration
		cpu    float64
		count  int
	}{
		{-2 * time.Hour, 40.0, 30}, // hour bucket A
		{-3 * time.Hour, 80.0, 30}, // hour bucket B
		{-9 * 24 * time.Hour, 10.0, 30},
		{-time.Minute, 55.0, 10},
	}
	id := 0
	for _, b := range buckets {
		ts := rfc3339(b.offset)
		for i := 0; i < b.count; i++ {
			id++
			if err := queries.InsertMetric(database,
				fmt.Sprintf("m-%d", id), "s-met", ts,
				int64(i), b.cpu, 1024, 2048, 50.0, 10.0, 20.0, 50.0,
				0.5, 0.25, true, 3, nil, 2); err != nil {
				t.Fatalf("InsertMetric: %v", err)
			}
		}
	}
	if got := rowCount(t, database, "SELECT COUNT(*) FROM metrics_history"); got != 100 {
		t.Fatalf("seeded %d rows, want 100", got)
	}

	// Rollup runs and creates hourly averages (one per hour bucket).
	n, err := queries.RollupHourlyMetrics(database)
	if err != nil {
		t.Fatalf("RollupHourlyMetrics: %v", err)
	}
	if n != 2 {
		t.Fatalf("hourly rollup inserted %d rows, want 2 (got %d; today's T-format raw rows must roll up, see format handling in RollupHourlyMetrics)", n, n)
	}

	// Hourly averages are correct: bucket A avg = 40, bucket B avg = 80.
	rows, err := database.Query("SELECT cpu_percent FROM metrics_history WHERE granularity='hourly'")
	if err != nil {
		t.Fatalf("query hourly rows: %v", err)
	}
	defer rows.Close()
	cpus := map[float64]bool{}
	for rows.Next() {
		var v float64
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		cpus[v] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !cpus[40.0] || !cpus[80.0] || len(cpus) != 2 {
		t.Errorf("hourly averages = %v, want {40, 80}", cpus)
	}
	if got := rowCount(t, database, "SELECT COUNT(*) FROM metrics_history WHERE granularity='hourly'"); got != 2 {
		t.Fatalf("hourly rows = %d, want 2", got)
	}

	// Raw rows older than 7 days are deleted; fresh raw rows survive.
	deleted, err := queries.DeleteOldRawMetrics(database)
	if err != nil {
		t.Fatalf("DeleteOldRawMetrics: %v", err)
	}
	if deleted != 30 {
		t.Errorf("raw retention deleted %d rows, want 30", deleted)
	}
	if got := rowCount(t, database,
		"SELECT COUNT(*) FROM metrics_history WHERE granularity='raw' AND recorded_at < datetime('now','-7 days')"); got != 0 {
		t.Errorf("old raw rows remaining = %d, want 0", got)
	}
	if got := rowCount(t, database, "SELECT COUNT(*) FROM metrics_history WHERE granularity='raw'"); got != 70 {
		t.Errorf("raw rows after retention = %d, want 70", got)
	}

	// Hourly rows older than 30 days are deleted; fresh hourly survives.
	if _, err := database.Exec(`INSERT INTO metrics_history
		(id, server_id, recorded_at, cpu_percent, granularity)
		VALUES ('h-old', 's-met', datetime('now','-31 days'), 10, 'hourly')`); err != nil {
		t.Fatal(err)
	}
	hDeleted, err := queries.DeleteOldHourlyMetrics(database)
	if err != nil {
		t.Fatalf("DeleteOldHourlyMetrics: %v", err)
	}
	if hDeleted != 1 {
		t.Errorf("hourly retention deleted %d rows, want 1", hDeleted)
	}
	if got := rowCount(t, database, "SELECT COUNT(*) FROM metrics_history WHERE granularity='hourly'"); got != 2 {
		t.Errorf("hourly rows after retention = %d, want 2", got)
	}

	// Daily rollup + 12-month retention.
	if _, err := database.Exec(`INSERT INTO metrics_history
		(id, server_id, recorded_at, cpu_percent, granularity)
		VALUES ('d-old', 's-met', datetime('now','-400 days'), 10, 'daily')`); err != nil {
		t.Fatal(err)
	}
	if _, err := queries.RollupDailyMetrics(database); err != nil {
		t.Fatalf("RollupDailyMetrics: %v", err)
	}
	dDeleted, err := queries.DeleteOldDailyMetrics(database)
	if err != nil {
		t.Fatalf("DeleteOldDailyMetrics: %v", err)
	}
	if dDeleted != 1 {
		t.Errorf("daily retention deleted %d rows, want 1", dDeleted)
	}

	// server_metrics_latest always holds the most recent values.
	if err := queries.UpsertServerMetricsLatest(database, "s-met", rfc3339(-2*time.Minute), 1.0, 100, 1024, 1.0, 10.0, 0.1); err != nil {
		t.Fatalf("UpsertServerMetricsLatest: %v", err)
	}
	if err := queries.UpsertServerMetricsLatest(database, "s-met", rfc3339(-time.Minute), 99.0, 900, 1024, 9.0, 10.0, 0.9); err != nil {
		t.Fatalf("UpsertServerMetricsLatest: %v", err)
	}
	latest, err := queries.GetServerMetricsLatest(database, "s-met")
	if err != nil {
		t.Fatalf("GetServerMetricsLatest: %v", err)
	}
	if latest.CPUPercent == nil || *latest.CPUPercent != 99.0 {
		t.Errorf("latest cpu = %v, want 99", latest.CPUPercent)
	}
	if latest.RAMUsedMB == nil || *latest.RAMUsedMB != 900 {
		t.Errorf("latest ram = %v, want 900", latest.RAMUsedMB)
	}

	// The live snapshot query returns the newest RAW sample, not a rolled-up
	// average (a rolled-up row must never outrank a live sample).
	live, err := queries.GetLatestMetric(database, "s-met")
	if err != nil {
		t.Fatalf("GetLatestMetric: %v", err)
	}
	if live.CPUPercent == nil || *live.CPUPercent != 55.0 {
		t.Errorf("live cpu = %v, want 55 (the freshest raw sample)", live.CPUPercent)
	}
}

// ---------------------------------------------------------------------------
// Test 6 — Cleanup jobs
// ---------------------------------------------------------------------------

func Test5C_CleanupJobs(t *testing.T) {
	database, _ := newE2EDB(t)
	if err := queries.InsertUser(database, "u-clean", "clean@example.com", "Clean", "hash"); err != nil {
		t.Fatal(err)
	}

	// Expired + fresh registration tokens.
	if err := queries.CreateRegistrationToken(database, "reg-exp", "h-exp", "u-clean", "srv", datetimeOffset(-1*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := queries.CreateRegistrationToken(database, "reg-fresh", "h-fresh", "u-clean", "srv", datetimeOffset(time.Hour)); err != nil {
		t.Fatal(err)
	}
	// Expired + fresh refresh tokens. Production stores expires_at in RFC3339
	// T-format (see queries/refresh_tokens.go), so the seed mirrors that —
	// the cleanup cutoff is RFC3339 too, and a space-format seed would only
	// pass by coincidence of ' ' < 'T'.
	if _, err := database.Exec(`INSERT INTO refresh_tokens
		(id, token_hash, user_id, created_at, expires_at)
		VALUES ('rt-exp', 'r-exp', 'u-clean', ?, ?)`, rfc3339(-2*time.Hour), rfc3339(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO refresh_tokens
		(id, token_hash, user_id, created_at, expires_at)
		VALUES ('rt-fresh', 'r-fresh', 'u-clean', ?, ?)`, rfc3339(-2*time.Hour), rfc3339(24*time.Hour)); err != nil {
		t.Fatal(err)
	}

	report := db.RunHourlyMaintenance(database)

	if got := rowCount(t, database, "SELECT COUNT(*) FROM registration_tokens WHERE id='reg-exp'"); got != 0 {
		t.Error("expired registration token survived cleanup")
	}
	if got := rowCount(t, database, "SELECT COUNT(*) FROM registration_tokens WHERE id='reg-fresh'"); got != 1 {
		t.Error("fresh registration token was deleted")
	}
	if got := rowCount(t, database, "SELECT COUNT(*) FROM refresh_tokens WHERE id='rt-exp'"); got != 0 {
		t.Error("expired refresh token survived cleanup")
	}
	if got := rowCount(t, database, "SELECT COUNT(*) FROM refresh_tokens WHERE id='rt-fresh'"); got != 1 {
		t.Error("fresh refresh token was deleted")
	}
	if report.Jobs["expired_registration_tokens"] != 1 || report.Jobs["expired_refresh_tokens"] != 1 {
		t.Errorf("cleanup counts wrong: %v", report.Jobs)
	}
}

// datetimeOffset returns a UTC timestamp string offset from now, in the same
// space-separated format SQLite's datetime('now') produces (registration and
// refresh token expiry comparisons use that format).
func datetimeOffset(d time.Duration) string {
	return time.Now().UTC().Add(d).Format("2006-01-02 15:04:05")
}

// ---------------------------------------------------------------------------
// Test 7 — Database backup while the database is being written
// ---------------------------------------------------------------------------

func Test5C_BackupDuringWrites(t *testing.T) {
	database, dbPath := newE2EDB(t)

	if err := queries.InsertUser(database, "u-bak", "bak@example.com", "Bak", "hash"); err != nil {
		t.Fatal(err)
	}
	if err := queries.InsertServer(database, "s-bak", "u-bak", "bak", "tok-bak"); err != nil {
		t.Fatal(err)
	}

	// A concurrent writer keeps inserting raw metrics while the backup runs.
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
				i++
				_ = queries.InsertMetric(database,
					fmt.Sprintf("w-%d", i), "s-bak", rfc3339(-time.Minute),
					int64(i), 42.0, 1024, 2048, 50.0, 10.0, 20.0, 50.0,
					0.5, 0.25, true, 3, nil, 2)
			}
		}
	}()

	backupDir := filepath.Join(t.TempDir(), "backups")
	gzPath, err := db.CreateLocalBackup(database, dbPath, backupDir)
	close(stop)
	wg.Wait()

	if err != nil {
		t.Fatalf("backup while writing failed: %v", err)
	}

	// The backup file is a valid SQLite database with the right data.
	restored := filepath.Join(t.TempDir(), "restored.db")
	if err := db.RestoreFromLocal(gzPath, restored); err != nil {
		t.Fatalf("restore: %v", err)
	}
	backupDB, err := sql.Open("sqlite", restored)
	if err != nil {
		t.Fatalf("open restored: %v", err)
	}
	defer backupDB.Close()

	// sqlite_master is readable → valid database.
	if got := rowCount(t, backupDB, "SELECT COUNT(*) FROM sqlite_master"); got == 0 {
		t.Error("backup has no tables — not a valid database")
	}
	if got := rowCount(t, backupDB, "SELECT COUNT(*) FROM users"); got != 1 {
		t.Errorf("backup users = %d, want 1", got)
	}
	if got := rowCount(t, backupDB, "SELECT COUNT(*) FROM servers"); got != 1 {
		t.Errorf("backup servers = %d, want 1", got)
	}
}

// ---------------------------------------------------------------------------
// Test 8 — Full recovery
// ---------------------------------------------------------------------------

func Test5C_FullRecovery(t *testing.T) {
	database, dbPath := newE2EDB(t)

	// Seed a realistic dataset: users, servers (with agent credentials so we
	// can later prove agents can reconnect), and a metric sample.
	if err := queries.InsertUser(database, "u-rec", "rec@example.com", "Rec", "hash"); err != nil {
		t.Fatal(err)
	}
	if err := queries.InsertUser(database, "u-rec2", "rec2@example.com", "Rec2", "hash2"); err != nil {
		t.Fatal(err)
	}
	if err := queries.InsertServer(database, "s-rec", "u-rec", "prod", "tok-rec"); err != nil {
		t.Fatal(err)
	}
	if err := queries.InsertServer(database, "s-rec2", "u-rec2", "staging", "tok-rec2"); err != nil {
		t.Fatal(err)
	}
	if err := queries.InsertMetric(database, "mr-1", "s-rec", rfc3339(-time.Minute),
		42, 42.0, 512, 1024, 50.0, 5.0, 10.0, 50.0, 0.5, 0.25, true, 3, nil, 2); err != nil {
		t.Fatal(err)
	}

	// Note the current state.
	usersBefore := rowCount(t, database, "SELECT COUNT(*) FROM users")
	serversBefore := rowCount(t, database, "SELECT COUNT(*) FROM servers")

	// Take a backup, then simulate total database loss: remove the file and
	// its WAL/shm companions while the process is stopped.
	backupDir := filepath.Join(t.TempDir(), "backups")
	gzPath, err := db.CreateLocalBackup(database, dbPath, backupDir)
	if err != nil {
		t.Fatalf("CreateLocalBackup: %v", err)
	}
	database.Close()

	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(dbPath + suffix); err != nil && !os.IsNotExist(err) {
			t.Fatalf("remove %s: %v", dbPath+suffix, err)
		}
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatal("database file still exists after simulated loss")
	}

	// Restore from the local backup and start the control plane again.
	if err := db.RestoreFromLocal(gzPath, dbPath); err != nil {
		t.Fatalf("RestoreFromLocal: %v", err)
	}
	restored, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen restored db: %v", err)
	}
	defer restored.Close()
	if err := db.Migrate(restored); err != nil {
		t.Fatalf("Migrate after restore: %v", err)
	}

	// User and server counts match what was noted before the loss.
	if got := rowCount(t, restored, "SELECT COUNT(*) FROM users"); got != usersBefore {
		t.Errorf("users after restore = %d, want %d", got, usersBefore)
	}
	if got := rowCount(t, restored, "SELECT COUNT(*) FROM servers"); got != serversBefore {
		t.Errorf("servers after restore = %d, want %d", got, serversBefore)
	}

	// Users can log in: their password hash survived the round trip.
	u, err := queries.GetUserByEmail(restored, "rec@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail after restore: %v", err)
	}
	if u.PasswordHash != "hash" {
		t.Errorf("password hash after restore = %q, want %q", u.PasswordHash, "hash")
	}

	// Agents reconnect without re-registration: the token used for agent
	// authentication still resolves to the same server.
	id, userID, name, err := queries.GetServerByToken(restored, "tok-rec")
	if err != nil {
		t.Fatalf("GetServerByToken after restore: %v", err)
	}
	if id != "s-rec" || userID != "u-rec" || name != "prod" {
		t.Errorf("restored server lookup = %q/%q/%q, want s-rec/u-rec/prod", id, userID, name)
	}

	// Metrics history survived too.
	if got := rowCount(t, restored, "SELECT COUNT(*) FROM metrics_history"); got != 1 {
		t.Errorf("metrics after restore = %d, want 1", got)
	}
}
