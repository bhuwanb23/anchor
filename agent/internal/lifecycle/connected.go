package lifecycle

import (
	"os"
	"path/filepath"
	"time"
)

const (
	DefaultDataDir     = "/var/lib/yourplatform"
	ConnectedFileName  = "agent.connected"
)

// ConnectedPath returns the path to the agent.connected marker file.
func ConnectedPath(dataDir string) string {
	if dataDir == "" {
		dataDir = DefaultDataDir
	}
	return filepath.Join(dataDir, ConnectedFileName)
}

// MarkConnected writes the agent.connected marker for install.sh to detect.
func MarkConnected(dataDir string) error {
	path := ConnectedPath(dataDir)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0600)
}

// ClearConnected removes the agent.connected marker on disconnect.
func ClearConnected(dataDir string) {
	_ = os.Remove(ConnectedPath(dataDir))
}
