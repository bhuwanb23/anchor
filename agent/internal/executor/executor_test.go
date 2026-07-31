package executor

import (
	"context"
	"testing"

	"github.com/yourname/yourplatform/agent/internal/backup"
	"github.com/yourname/yourplatform/agent/internal/caddy"
	"github.com/yourname/yourplatform/agent/internal/state"
)

func TestExecutor_WithStateManager(t *testing.T) {
	exec := New(nil, caddy.NewManager(""), backup.NewManager(""))
	if exec.StateManager() != nil {
		t.Fatal("expected nil state manager initially")
	}

	sm := state.NewManager("")
	exec.WithStateManager(sm)
	if exec.StateManager() == nil {
		t.Fatal("expected state manager after WithStateManager")
	}
	if exec.StateManager() != sm {
		t.Fatal("StateManager() returned wrong instance")
	}
}

func TestExecutor_BuilderChaining(t *testing.T) {
	sm := state.NewManager("")
	exec := New(nil, caddy.NewManager(""), backup.NewManager("")).WithStateManager(sm)

	if exec.StateManager() != sm {
		t.Fatal("builder chaining broken")
	}
}

func TestExecutor_Execute_UnknownCommand(t *testing.T) {
	exec := New(nil, caddy.NewManager(""), backup.NewManager(""))

	result := exec.Execute(context.Background(), Command{
		ID:   "test-1",
		Type: "unknown_type",
	})

	if result.Status != "error" {
		t.Fatalf("expected error status, got %s", result.Status)
	}
	if result.Error == "" {
		t.Fatal("expected error message")
	}
	if result.CommandID != "test-1" {
		t.Fatalf("expected command ID test-1, got %s", result.CommandID)
	}
}