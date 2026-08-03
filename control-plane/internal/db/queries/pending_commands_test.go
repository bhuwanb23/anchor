package queries

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupPendingDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE servers (id TEXT PRIMARY KEY, name TEXT);
		INSERT INTO servers (id, name) VALUES ('srv-1', 'test');
		CREATE TABLE pending_commands (
			id TEXT PRIMARY KEY,
			server_id TEXT NOT NULL,
			command_type TEXT NOT NULL,
			payload TEXT NOT NULL,
			project_key TEXT,
			expires_at TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE CASCADE
		);
	`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

// Scenario 4 (CP): offline queue stores → list for hello_ack → deploy dedupe.
func TestPendingCommands_OfflineQueueAndDeployDedupe(t *testing.T) {
	db := setupPendingDB(t)
	defer db.Close()

	env1, _ := json.Marshal(map[string]interface{}{
		"id": "c1", "type": "set_env", "payload": map[string]string{"project_name": "a"},
	})
	dep1, _ := json.Marshal(map[string]interface{}{
		"id": "d1", "type": "deploy", "payload": map[string]string{"app_name": "shop", "image": "old"},
	})
	dep2, _ := json.Marshal(map[string]interface{}{
		"id": "d2", "type": "deploy", "payload": map[string]string{"app_name": "shop", "image": "new"},
	})

	if err := EnqueuePendingCommand(db, "c1", "srv-1", "set_env", string(env1), "a", ""); err != nil {
		t.Fatal(err)
	}
	if err := EnqueuePendingCommand(db, "d1", "srv-1", "deploy", string(dep1), "shop", ""); err != nil {
		t.Fatal(err)
	}
	// Newer deploy for same project should replace older
	if err := EnqueuePendingCommand(db, "d2", "srv-1", "deploy", string(dep2), "shop", ""); err != nil {
		t.Fatal(err)
	}

	cmds, err := ListPendingCommands(db, "srv-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 2 {
		t.Fatalf("len=%d want 2 (env + newest deploy)", len(cmds))
	}
	foundNewDeploy := false
	for _, c := range cmds {
		if c.ID == "d1" {
			t.Fatal("old deploy should have been removed")
		}
		if c.ID == "d2" {
			foundNewDeploy = true
		}
	}
	if !foundNewDeploy {
		t.Fatal("newest deploy missing")
	}

	raw, err := PendingCommandsAsJSON(db, "srv-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 2 {
		t.Fatalf("json len=%d", len(raw))
	}

	if err := DeletePendingCommands(db, "srv-1"); err != nil {
		t.Fatal(err)
	}
	cmds, _ = ListPendingCommands(db, "srv-1")
	if len(cmds) != 0 {
		t.Fatal("expected empty after delete")
	}
}

func TestPendingCommands_ExpiredSkipped(t *testing.T) {
	db := setupPendingDB(t)
	defer db.Close()

	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	future := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	_ = EnqueuePendingCommand(db, "old", "srv-1", "get_state", `{"id":"old","type":"get_state"}`, "", past)
	_ = EnqueuePendingCommand(db, "new", "srv-1", "get_state", `{"id":"new","type":"get_state"}`, "", future)

	cmds, err := ListPendingCommands(db, "srv-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 1 || cmds[0].ID != "new" {
		t.Fatalf("got %+v, want only non-expired", cmds)
	}
}
