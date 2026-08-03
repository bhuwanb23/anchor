package lifecycle

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMarkAndClearConnected(t *testing.T) {
	dir := t.TempDir()
	if err := MarkConnected(dir); err != nil {
		t.Fatalf("MarkConnected: %v", err)
	}
	path := ConnectedPath(dir)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("connected file missing: %v", err)
	}
	ClearConnected(dir)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("connected file should be removed")
	}
}

func TestWSURLFromControlPlane(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"https://api.example.com", "wss://api.example.com/ws/agent"},
		{"http://localhost:8080", "ws://localhost:8080/ws/agent"},
		{"ws://localhost:8080/ws/agent", "ws://localhost:8080/ws/agent"},
	}
	for _, tt := range tests {
		got := WSURLFromControlPlane(tt.in)
		if got != tt.want {
			t.Errorf("WSURLFromControlPlane(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestConnectedPath(t *testing.T) {
	got := ConnectedPath("/tmp/x")
	want := filepath.Join("/tmp/x", ConnectedFileName)
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
