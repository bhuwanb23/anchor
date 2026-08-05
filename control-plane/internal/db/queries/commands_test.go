package queries

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func setupCommandsDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE servers (
			id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'disconnected',
			ip_address TEXT NOT NULL DEFAULT '', token TEXT
		);
		CREATE TABLE commands (
			id TEXT PRIMARY KEY, server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
			command_type TEXT NOT NULL, project_key TEXT NOT NULL DEFAULT '',
			payload TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'queued',
			issued_by TEXT NOT NULL, created_at TEXT NOT NULL,
			started_at TEXT, completed_at TEXT, result TEXT
		);
		INSERT INTO servers (id, user_id, name, status) VALUES ('srv-1', 'user-1', 'prod', 'connected');
	`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestInsertCommandAndGetByID(t *testing.T) {
	db := setupCommandsDB(t)
	if err := InsertCommand(db, "cmd-1", "srv-1", "deploy", `{"project":"myshop"}`, "myshop", "queued", "user-1", "2026-08-05T00:00:00Z"); err != nil {
		t.Fatalf("InsertCommand: %v", err)
	}

	rec, err := GetCommandByID(db, "cmd-1")
	if err != nil {
		t.Fatalf("GetCommandByID: %v", err)
	}
	if rec == nil {
		t.Fatal("expected command row")
	}
	if rec.Status != "queued" || rec.IssuedBy != "user-1" || rec.ProjectKey != "myshop" {
		t.Fatalf("unexpected record: %+v", rec)
	}

	missing, err := GetCommandByID(db, "cmd-nope")
	if err != nil || missing != nil {
		t.Fatalf("unknown command: rec=%v err=%v", missing, err)
	}
}

func TestHasInProgressCommand(t *testing.T) {
	db := setupCommandsDB(t)
	_ = InsertCommand(db, "cmd-1", "srv-1", "deploy", "{}", "myshop", "in_progress", "user-1", "t")
	_ = InsertCommand(db, "cmd-2", "srv-1", "backup", "{}", "myshop", "queued", "user-1", "t")
	_ = InsertCommand(db, "cmd-3", "srv-1", "deploy", "{}", "other", "success", "user-1", "t")

	ok, err := HasInProgressCommand(db, "srv-1", "deploy", "myshop")
	if err != nil || !ok {
		t.Fatalf("deploy/myshop should be in progress: ok=%v err=%v", ok, err)
	}
	ok, err = HasInProgressCommand(db, "srv-1", "backup", "myshop")
	if err != nil || !ok {
		t.Fatalf("backup/myshop should be queued-in-progress: ok=%v err=%v", ok, err)
	}
	ok, err = HasInProgressCommand(db, "srv-1", "deploy", "other")
	if err != nil || ok {
		t.Fatalf("completed command must not block: ok=%v err=%v", ok, err)
	}
	// No project key: never deduped.
	ok, err = HasInProgressCommand(db, "srv-1", "deploy", "")
	if err != nil || ok {
		t.Fatalf("empty project must not dedup: ok=%v err=%v", ok, err)
	}
}

func TestUpdateCommandStatusLifecycle(t *testing.T) {
	db := setupCommandsDB(t)
	_ = InsertCommand(db, "cmd-1", "srv-1", "deploy", "{}", "myshop", "queued", "user-1", "t")

	if err := UpdateCommandStatus(db, "cmd-1", "in_progress", ""); err != nil {
		t.Fatalf("mark in_progress: %v", err)
	}
	rec, _ := GetCommandByID(db, "cmd-1")
	if rec.Status != "in_progress" || rec.StartedAt == "" {
		t.Fatalf("expected in_progress with started_at, got %+v", rec)
	}

	if err := UpdateCommandStatus(db, "cmd-1", "success", `{"status":"success"}`); err != nil {
		t.Fatalf("mark success: %v", err)
	}
	rec, _ = GetCommandByID(db, "cmd-1")
	if rec.Status != "success" || rec.CompletedAt == "" || rec.Result == "" {
		t.Fatalf("expected success with completed_at+result, got %+v", rec)
	}
}

func TestUpdateCommandStatusIfActiveGuardsTerminal(t *testing.T) {
	db := setupCommandsDB(t)
	_ = InsertCommand(db, "cmd-1", "srv-1", "deploy", "{}", "myshop", "queued", "user-1", "t")
	_ = InsertCommand(db, "cmd-2", "srv-1", "deploy", "{}", "myshop", "success", "user-1", "t")

	// Active command: the timeout advances it.
	if err := UpdateCommandStatusIfActive(db, "cmd-1", "timeout", "late"); err != nil {
		t.Fatalf("UpdateCommandStatusIfActive: %v", err)
	}
	rec, _ := GetCommandByID(db, "cmd-1")
	if rec.Status != "timeout" {
		t.Fatalf("active command should be timed out, got %q", rec.Status)
	}

	// Terminal command: the timeout must NOT overwrite it.
	if err := UpdateCommandStatusIfActive(db, "cmd-2", "timeout", "late"); err != nil {
		t.Fatalf("UpdateCommandStatusIfActive (terminal): %v", err)
	}
	rec, _ = GetCommandByID(db, "cmd-2")
	if rec.Status != "success" {
		t.Fatalf("terminal command must not be overwritten, got %q", rec.Status)
	}
}

func TestUpdateCommandResultLateAudit(t *testing.T) {
	db := setupCommandsDB(t)
	_ = InsertCommand(db, "cmd-1", "srv-1", "deploy", "{}", "myshop", "timeout", "user-1", "t")

	if err := UpdateCommandResult(db, "cmd-1", `{"status":"success","late":true}`); err != nil {
		t.Fatalf("UpdateCommandResult: %v", err)
	}
	rec, _ := GetCommandByID(db, "cmd-1")
	// Terminal status is preserved; the late result is recorded for audit.
	if rec.Status != "timeout" {
		t.Fatalf("status should stay timeout, got %q", rec.Status)
	}
	if rec.Result == "" || rec.CompletedAt == "" {
		t.Fatalf("late result not recorded: %+v", rec)
	}
}
