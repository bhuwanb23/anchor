package state

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// StateVersion is the current state file format version.
	StateVersion = 1

	// DefaultStateDir is the default directory for agent state.
	DefaultStateDir = "/var/lib/yourplatform"

	// StateFileName is the name of the state file.
	StateFileName = "state.json"

	// filePerms is the permission mode for the state file.
	filePerms = 0600

	// dirPerms is the permission mode for the state directory.
	dirPerms = 0700
)

// State represents the full agent state persisted to disk.
type State struct {
	Version  int                      `json:"version"`
	Projects map[string]*ProjectState `json:"projects"`
}

// ProjectState tracks all containers belonging to a project.
type ProjectState struct {
	Containers map[string]*ContainerState `json:"containers"`
}

// ContainerState tracks a single container's known state.
type ContainerState struct {
	ContainerID   string `json:"container_id"`
	Image         string `json:"image"`
	Status        string `json:"status"` // "running", "stopped", "exited", "failed"
	HostPort      int    `json:"host_port,omitempty"`
	RestartPolicy string `json:"restart_policy,omitempty"` // "always", "unless-stopped", "no"
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

// DefaultStatePath returns the default path for the state file.
func DefaultStatePath() string {
	return filepath.Join(DefaultStateDir, StateFileName)
}

// NewState creates a new empty state.
func NewState() *State {
	return &State{
		Version:  StateVersion,
		Projects: make(map[string]*ProjectState),
	}
}

// Manager provides thread-safe access to the agent state.
type Manager struct {
	mu       sync.Mutex
	state    *State
	stateDir string
}

// NewManager creates a new state manager that persists to the given directory.
func NewManager(stateDir string) *Manager {
	if stateDir == "" {
		stateDir = DefaultStateDir
	}
	return &Manager{
		stateDir: stateDir,
	}
}

// GetState returns the current state (loads from disk if not yet loaded).
func (m *Manager) GetState() *State {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state == nil {
		m.state = LoadState(filepath.Join(m.stateDir, StateFileName))
	}
	return m.state
}

// SetContainer adds or updates a container in the state.
func (m *Manager) SetContainer(project, role string, container *ContainerState) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state == nil {
		m.state = LoadState(filepath.Join(m.stateDir, StateFileName))
	}

	if m.state.Projects == nil {
		m.state.Projects = make(map[string]*ProjectState)
	}
	projectState, ok := m.state.Projects[project]
	if !ok {
		projectState = &ProjectState{
			Containers: make(map[string]*ContainerState),
		}
		m.state.Projects[project] = projectState
	}

	if projectState.Containers == nil {
		projectState.Containers = make(map[string]*ContainerState)
	}

	container.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	projectState.Containers[role] = container

	return m.save()
}

// RemoveContainer removes a single container from the state.
func (m *Manager) RemoveContainer(project, role string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state == nil || m.state.Projects == nil {
		return nil
	}

	if projectState, ok := m.state.Projects[project]; ok {
		delete(projectState.Containers, role)
		// Clean up empty projects
		if len(projectState.Containers) == 0 {
			delete(m.state.Projects, project)
		}
		return m.save()
	}
	return nil
}

// RemoveProject removes an entire project from the state.
func (m *Manager) RemoveProject(project string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state == nil || m.state.Projects == nil {
		return nil
	}

	delete(m.state.Projects, project)
	return m.save()
}

// UpdateStatus updates only the status of a container.
func (m *Manager) UpdateStatus(project, role, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state == nil || m.state.Projects == nil {
		return nil
	}

	if projectState, ok := m.state.Projects[project]; ok {
		if container, ok := projectState.Containers[role]; ok {
			container.Status = status
			container.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			return m.save()
		}
	}
	return nil
}

// save writes the state to disk atomically.
func (m *Manager) save() error {
	return SaveState(filepath.Join(m.stateDir, StateFileName), m.state)
}

// LoadState reads the state file and returns a State.
// If the file doesn't exist, returns an empty state.
// If the file is corrupted, returns an empty state and logs a warning.
func LoadState(path string) *State {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Info("no state file found, starting fresh", "path", path)
			return NewState()
		}
		slog.Warn("failed to read state file", "path", path, "error", err)
		return NewState()
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		slog.Warn("corrupted state file, starting fresh", "path", path, "error", err)
		// Back up corrupted file
		bakPath := path + ".bak"
		if err := os.Rename(path, bakPath); err != nil {
			slog.Warn("failed to backup corrupted state file", "path", path, "error", err)
		}
		return NewState()
	}

	if state.Projects == nil {
		state.Projects = make(map[string]*ProjectState)
	}

	slog.Info("loaded state file",
		"path", path,
		"projects", len(state.Projects))
	return &state
}

// SaveState writes the state to disk atomically (temp file + rename).
func SaveState(path string, state *State) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirPerms); err != nil {
		return fmt.Errorf("create state directory %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	// Atomic write: write to temp file, then rename
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, filePerms); err != nil {
		return fmt.Errorf("write temp state file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp state file: %w", err)
	}

	slog.Debug("saved state file", "path", path, "bytes", len(data))
	return nil
}
