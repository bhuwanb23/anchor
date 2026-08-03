package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUncleanShutdownDetection(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

	if mgr.WasUncleanShutdown() {
		t.Fatal("fresh state should not be unclean")
	}

	if err := mgr.MarkStartup("1.0.0"); err != nil {
		t.Fatal(err)
	}
	mgr2 := NewManager(dir)
	if !mgr2.WasUncleanShutdown() {
		t.Fatal("expected unclean shutdown after crash")
	}

	if err := mgr2.MarkCleanShutdown(); err != nil {
		t.Fatal(err)
	}
	mgr3 := NewManager(dir)
	if mgr3.WasUncleanShutdown() {
		t.Fatal("expected clean after MarkCleanShutdown")
	}

	if _, err := os.Stat(filepath.Join(dir, StateFileName)); err != nil {
		t.Fatal(err)
	}
}

func TestRecordConnected(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	_ = mgr.MarkStartup("1.0.0")
	if err := mgr.RecordConnected(); err != nil {
		t.Fatal(err)
	}
	st := mgr.GetState()
	if st.Connection == nil || st.Connection.LastConnected == "" {
		t.Fatal("expected last_connected")
	}
	if err := mgr.RecordDisconnected(); err != nil {
		t.Fatal(err)
	}
	st = mgr.GetState()
	if st.Connection.LastDisconnected == "" {
		t.Fatal("expected last_disconnected")
	}
}
