package queries

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func setupEventsDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		PRAGMA foreign_keys=ON;
		CREATE TABLE servers (id TEXT PRIMARY KEY, name TEXT NOT NULL);
		CREATE TABLE server_events (
			id TEXT PRIMARY KEY,
			server_id TEXT NOT NULL,
			event_type TEXT NOT NULL,
			check_name TEXT,
			message TEXT,
			details TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE CASCADE
		);
		INSERT INTO servers (id, name) VALUES ('srv-1', 'prod');
	`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestInsertAndListServerEvents(t *testing.T) {
	db := setupEventsDB(t)
	defer db.Close()

	if err := InsertServerEvent(db, "e-1", "srv-1", "auto_fixed", "docker_check", "restarted container", "exit 137"); err != nil {
		t.Fatal(err)
	}
	if err := InsertServerEvent(db, "e-2", "srv-1", "warning", "disk", "disk at 85%", ""); err != nil {
		t.Fatal(err)
	}

	events, err := ListEventsByServer(db, "srv-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("len=%d want 2", len(events))
	}
	// Newest first.
	if events[0].ID != "e-2" || events[1].ID != "e-1" {
		t.Fatalf("order wrong: %s, %s", events[0].ID, events[1].ID)
	}
	if events[0].EventType != "warning" || !events[0].CheckName.Valid || events[0].CheckName.String != "disk" {
		t.Fatalf("event-2 fields wrong: %+v", events[0])
	}
	if events[1].Message.String != "restarted container" || events[1].Details.String != "exit 137" {
		t.Fatalf("event-1 message/details wrong: %+v", events[1])
	}

	// Limit applies.
	limited, err := ListEventsByServer(db, "srv-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 1 || limited[0].ID != "e-2" {
		t.Fatalf("limit not applied: %+v", limited)
	}

	// Server with no events: empty slice, not nil.
	none, err := ListEventsByServer(db, "srv-none", 0)
	if err != nil {
		t.Fatal(err)
	}
	if none == nil || len(none) != 0 {
		t.Fatalf("expected empty slice, got %v", none)
	}
}

func TestDeleteOldEvents(t *testing.T) {
	db := setupEventsDB(t)
	defer db.Close()

	if err := InsertServerEvent(db, "old", "srv-1", "warning", "disk", "old event", ""); err != nil {
		t.Fatal(err)
	}
	// Backdate the old event beyond the retention window.
	if _, err := db.Exec(`UPDATE server_events SET created_at = datetime('now', '-120 days') WHERE id = 'old'`); err != nil {
		t.Fatal(err)
	}
	if err := InsertServerEvent(db, "new", "srv-1", "warning", "disk", "recent event", ""); err != nil {
		t.Fatal(err)
	}

	n, err := DeleteOldEvents(db, 90)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("deleted=%d want 1", n)
	}

	events, err := ListEventsByServer(db, "srv-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ID != "new" {
		t.Fatalf("expected only 'new' to remain, got %+v", events)
	}
}
