package env

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/joho/godotenv"
)

const (
	// DefaultEnvDir is the default directory for project env files.
	DefaultEnvDir = "/etc/yourplatform/envs"

	// filePerms is the permission mode for env files (owner read/write only).
	filePerms = 0600

	// dirPerms is the permission mode for the env directory.
	dirPerms = 0700
)

// Manager handles reading and writing .env files for deployed applications.
type Manager struct {
	envDir string
}

// NewManager creates a new env file manager.
func NewManager(envDir string) *Manager {
	if envDir == "" {
		envDir = DefaultEnvDir
	}
	return &Manager{envDir: envDir}
}

// envFilePath returns the path to a project's env file.
func (m *Manager) envFilePath(projectName string) string {
	return filepath.Join(m.envDir, projectName+".env")
}

// ReadEnvFile reads and parses a project's .env file.
// Returns an empty map (no error) if the file does not exist.
func (m *Manager) ReadEnvFile(projectName string) (map[string]string, error) {
	path := m.envFilePath(projectName)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]string), nil
		}
		return nil, fmt.Errorf("read env file %s: %w", path, err)
	}

	vars, err := godotenv.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parse env file %s: %w", path, err)
	}

	slog.Debug("read env file",
		"project", projectName,
		"keys", len(vars),
	)

	return vars, nil
}

// WriteEnvFile writes a complete env file for a project.
// Creates the env directory if it does not exist.
// Overwrites the entire file with the provided vars.
func (m *Manager) WriteEnvFile(projectName string, vars map[string]string) error {
	// Ensure env directory exists
	if err := os.MkdirAll(m.envDir, dirPerms); err != nil {
		return fmt.Errorf("create env directory %s: %w", m.envDir, err)
	}

	path := m.envFilePath(projectName)

	// Sort keys for deterministic output
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build file content
	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(fmt.Sprintf("%s=%s\n", k, vars[k]))
	}

	if err := os.WriteFile(path, []byte(sb.String()), filePerms); err != nil {
		return fmt.Errorf("write env file %s: %w", path, err)
	}

	slog.Info("wrote env file",
		"project", projectName,
		"keys", len(vars),
		"path", path,
	)

	return nil
}

// UpdateEnvVar updates a single env var in a project's .env file.
// Creates the file if it does not exist.
func (m *Manager) UpdateEnvVar(projectName, key, value string) error {
	vars, err := m.ReadEnvFile(projectName)
	if err != nil {
		return err
	}

	vars[key] = value
	return m.WriteEnvFile(projectName, vars)
}

// RemoveEnvVar removes a single env var from a project's .env file.
// No-op if the key does not exist.
func (m *Manager) RemoveEnvVar(projectName, key string) error {
	vars, err := m.ReadEnvFile(projectName)
	if err != nil {
		return err
	}

	if _, exists := vars[key]; !exists {
		return nil
	}

	delete(vars, key)
	return m.WriteEnvFile(projectName, vars)
}

// ListEnvKeys returns the env var key names for a project (no values).
func (m *Manager) ListEnvKeys(projectName string) ([]string, error) {
	vars, err := m.ReadEnvFile(projectName)
	if err != nil {
		return nil, err
	}

	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, nil
}

// FormatForDocker converts a map of env vars to Docker's ["KEY=VALUE"] format.
func FormatForDocker(vars map[string]string) []string {
	if len(vars) == 0 {
		return []string{}
	}

	env := make([]string, 0, len(vars))
	for k, v := range vars {
		env = append(env, k+"="+v)
	}
	return env
}

// MergeWithDefaults adds standard platform env vars to the given map.
// Always adds YOURPLATFORM=true and PORT={port}.
func MergeWithDefaults(vars map[string]string, port int) map[string]string {
	merged := make(map[string]string, len(vars)+2)
	for k, v := range vars {
		merged[k] = v
	}

	merged["YOURPLATFORM"] = "true"
	merged["PORT"] = fmt.Sprintf("%d", port)

	return merged
}

// MaskEnvVars returns a copy with all values replaced by "••••••".
// Used when sending key names to the control plane.
func MaskEnvVars(vars map[string]string) map[string]string {
	masked := make(map[string]string, len(vars))
	for k := range vars {
		masked[k] = "••••••"
	}
	return masked
}

// GenerateDatabaseURL builds a DATABASE_URL connection string for Postgres.
func GenerateDatabaseURL(password, dbName string) string {
	return fmt.Sprintf("postgres://yourplatform:%s@postgres:5432/%s?sslmode=disable", password, dbName)
}
