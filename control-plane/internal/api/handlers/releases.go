package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
)

// LatestRelease serves /releases/latest.json for agent self-update.
// Prefers release/latest.json on disk; falls back to a default stub.
func LatestRelease(w http.ResponseWriter, r *http.Request) {
	path := filepath.Join(".", "release", "latest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"version":   "0.1.0",
			"checksums": map[string]string{},
		})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}
