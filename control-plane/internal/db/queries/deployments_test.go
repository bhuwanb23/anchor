package queries

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func setupDeploymentsDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		PRAGMA foreign_keys=ON;
		CREATE TABLE servers (id TEXT PRIMARY KEY, name TEXT NOT NULL);
		CREATE TABLE deployments (
			id TEXT PRIMARY KEY, server_id TEXT NOT NULL, app_name TEXT NOT NULL,
			image TEXT NOT NULL, port INTEGER NOT NULL, domain TEXT,
			status TEXT NOT NULL DEFAULT 'deploying',
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now')),
			FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE CASCADE
		);
		INSERT INTO servers (id, name) VALUES ('srv-1', 'prod');
		INSERT INTO deployments (id, server_id, app_name, image, port, domain, status, created_at) VALUES
			('d-1', 'srv-1', 'shop', 'shop:v1', 3000, 'shop.example.com', 'success', '2026-01-01T10:00:00Z'),
			('d-2', 'srv-1', 'shop', 'shop:v2', 3000, 'shop.example.com', 'success', '2026-01-02T10:00:00Z'),
			('d-3', 'srv-1', 'blog', 'blog:v1', 4000, NULL, 'failed', '2026-01-03T10:00:00Z'),
			('d-4', 'srv-1', 'shop', 'shop:v3', 3000, 'shop.example.com', 'running', '2026-01-04T10:00:00Z');
	`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

// Pattern 1: Get by ID; not found returns nil, nil.
func TestGetDeploymentByID(t *testing.T) {
	db := setupDeploymentsDB(t)
	defer db.Close()

	d, err := GetDeploymentByID(db, "d-2")
	if err != nil {
		t.Fatal(err)
	}
	if d == nil || d.AppName != "shop" || d.Image != "shop:v2" || d.Status != "success" {
		t.Fatalf("unexpected deployment: %+v", d)
	}
	if !d.Domain.Valid || d.Domain.String != "shop.example.com" {
		t.Fatalf("domain not scanned: %+v", d.Domain)
	}

	missing, err := GetDeploymentByID(db, "nope")
	if err != nil {
		t.Fatalf("expected nil error for missing row, got %v", err)
	}
	if missing != nil {
		t.Fatalf("expected nil, got %+v", missing)
	}
}

// Step 3C #2: list deployments for an app, most recent first, limit applies.
func TestListDeploymentsByApp_NewestFirstWithLimit(t *testing.T) {
	db := setupDeploymentsDB(t)
	defer db.Close()

	deploys, err := ListDeploymentsByApp(db, "srv-1", "shop", 0) // default limit 20
	if err != nil {
		t.Fatal(err)
	}
	if len(deploys) != 3 {
		t.Fatalf("len=%d want 3", len(deploys))
	}
	// Newest first.
	if deploys[0].ID != "d-4" || deploys[1].ID != "d-2" || deploys[2].ID != "d-1" {
		t.Fatalf("order wrong: %s, %s, %s", deploys[0].ID, deploys[1].ID, deploys[2].ID)
	}

	limited, err := ListDeploymentsByApp(db, "srv-1", "shop", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 2 || limited[0].ID != "d-4" {
		t.Fatalf("limit not applied: %+v", limited)
	}

	// App with no deployments: empty slice, not nil.
	none, err := ListDeploymentsByApp(db, "srv-1", "ghost", 0)
	if err != nil {
		t.Fatal(err)
	}
	if none == nil || len(none) != 0 {
		t.Fatalf("expected empty slice, got %v", none)
	}
}

func TestListDeploymentsByServer(t *testing.T) {
	db := setupDeploymentsDB(t)
	defer db.Close()

	deploys, err := ListDeploymentsByServer(db, "srv-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(deploys) != 4 {
		t.Fatalf("len=%d want 4", len(deploys))
	}
	if deploys[0].ID != "d-4" {
		t.Fatalf("newest first: got %s want d-4", deploys[0].ID)
	}

	// Null domain survives the round trip (d-3).
	for _, d := range deploys {
		if d.ID == "d-3" && d.Domain.Valid {
			t.Fatalf("d-3 domain should be NULL, got %q", d.Domain.String)
		}
	}
}
