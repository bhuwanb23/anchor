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
	Version       int                      `json:"version"`
	AgentVersion  string                   `json:"agent_version,omitempty"`
	LastUpdated   string                   `json:"last_updated,omitempty"`
	ShutdownClean bool                     `json:"shutdown_clean"`
	Connection    *ConnectionState         `json:"connection,omitempty"`
	Update        *UpdateState             `json:"update,omitempty"`
	Projects      map[string]*ProjectState `json:"projects"`
	Routes        map[string]*RouteState   `json:"routes"`
	Certificates  map[string]*CertState    `json:"certificates,omitempty"`
	Backup        *BackupState             `json:"backup,omitempty"`
}

// ConnectionState tracks WebSocket connection history.
type ConnectionState struct {
	LastConnected     string `json:"last_connected,omitempty"`
	LastDisconnected  string `json:"last_disconnected,omitempty"`
	ReconnectAttempts int    `json:"reconnect_attempts,omitempty"`
}

// UpdateState tracks self-update status.
type UpdateState struct {
	CurrentVersion   string `json:"current_version,omitempty"`
	LastCheck        string `json:"last_check,omitempty"`
	AvailableVersion string `json:"available_version,omitempty"`
}

// ProjectState tracks all containers belonging to a project.
type ProjectState struct {
	Containers         map[string]*ContainerState `json:"containers"`
	LastDeployment     *DeploymentRecord          `json:"last_deployment,omitempty"`
	PreviousDeployment *DeploymentRecord          `json:"previous_deployment,omitempty"`
}

// DeploymentRecord stores enough info to roll back a deploy.
type DeploymentRecord struct {
	Image       string `json:"image"`
	Port        int    `json:"port"`
	Domain      string `json:"domain,omitempty"`
	ContainerID string `json:"container_id,omitempty"`
	DeployedAt  string `json:"deployed_at"`
}

// ContainerState tracks a single container's known state.
type ContainerState struct {
	ContainerID   string `json:"container_id"`
	Image         string `json:"image"`
	Status        string `json:"status"` // "running", "stopped", "exited", "failed"
	Domain        string `json:"domain,omitempty"`
	HostPort      int    `json:"host_port,omitempty"`
	RestartPolicy string `json:"restart_policy,omitempty"` // "always", "unless-stopped", "no"
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

// RouteState tracks a Caddy route managed by the agent.
type RouteState struct {
	RouteID   string   `json:"route_id"`
	Project   string   `json:"project"`
	Domains   []string `json:"domains"`
	Upstream  string   `json:"upstream"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

// CertState tracks an HTTPS certificate's status.
type CertState struct {
	Domain    string `json:"domain"`
	Expiry    string `json:"expiry"`     // RFC3339
	Issuer    string `json:"issuer"`
	Status    string `json:"status"`     // "valid", "expiring_soon", "expired", "unknown"
	CheckedAt string `json:"checked_at"` // last monitoring check
}

// BackupState tracks the last backup execution for missed-backup detection.
type BackupState struct {
	LastBackupAt   string `json:"last_backup_at,omitempty"`    // RFC3339 timestamp
	LastSnapshotID string `json:"last_snapshot_id,omitempty"`  // restic snapshot ID
	LastDurationMs int64  `json:"last_duration_ms,omitempty"`  // backup duration in milliseconds
	LastTotalBytes int64  `json:"last_total_bytes,omitempty"`  // total bytes backed up
}

// DefaultStatePath returns the default path for the state file.
func DefaultStatePath() string {
	return filepath.Join(DefaultStateDir, StateFileName)
}

// NewState creates a new empty state.
func NewState() *State {
	return &State{
		Version:      StateVersion,
		Projects:     make(map[string]*ProjectState),
		Routes:       make(map[string]*RouteState),
		Certificates: make(map[string]*CertState),
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

// RemoveProject removes an entire project and its routes from the state.
func (m *Manager) RemoveProject(project string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state == nil || m.state.Projects == nil {
		return nil
	}

	delete(m.state.Projects, project)

	// Remove all routes belonging to this project
	if m.state.Routes != nil {
		for id, r := range m.state.Routes {
			if r.Project == project {
				delete(m.state.Routes, id)
			}
		}
	}

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

// SetRoute adds or updates a route in the state.
func (m *Manager) SetRoute(routeID, project string, domains []string, upstream string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state == nil {
		m.state = LoadState(filepath.Join(m.stateDir, StateFileName))
	}

	if m.state.Routes == nil {
		m.state.Routes = make(map[string]*RouteState)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	existing := m.state.Routes[routeID]
	if existing != nil {
		existing.Domains = domains
		existing.Upstream = upstream
		existing.UpdatedAt = now
	} else {
		m.state.Routes[routeID] = &RouteState{
			RouteID:   routeID,
			Project:   project,
			Domains:   domains,
			Upstream:  upstream,
			CreatedAt: now,
			UpdatedAt: now,
		}
	}

	return m.save()
}

// RemoveRoute removes a route from the state.
func (m *Manager) RemoveRoute(routeID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state == nil || m.state.Routes == nil {
		return nil
	}

	delete(m.state.Routes, routeID)
	return m.save()
}

// GetRoutes returns all routes from the state.
func (m *Manager) GetRoutes() map[string]*RouteState {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state == nil {
		m.state = LoadState(filepath.Join(m.stateDir, StateFileName))
	}

	if m.state.Routes == nil {
		return make(map[string]*RouteState)
	}

	out := make(map[string]*RouteState, len(m.state.Routes))
	for k, v := range m.state.Routes {
		out[k] = v
	}
	return out
}

// SetCertificate adds or updates a certificate entry in the state.
func (m *Manager) SetCertificate(domain, expiry, issuer, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state == nil {
		m.state = LoadState(filepath.Join(m.stateDir, StateFileName))
	}

	if m.state.Certificates == nil {
		m.state.Certificates = make(map[string]*CertState)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	m.state.Certificates[domain] = &CertState{
		Domain:    domain,
		Expiry:    expiry,
		Issuer:    issuer,
		Status:    status,
		CheckedAt: now,
	}

	return m.save()
}

// RemoveCertificate removes a certificate entry from the state.
func (m *Manager) RemoveCertificate(domain string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state == nil || m.state.Certificates == nil {
		return nil
	}

	delete(m.state.Certificates, domain)
	return m.save()
}

// GetCertificates returns all certificate entries from the state.
func (m *Manager) GetCertificates() map[string]*CertState {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state == nil {
		m.state = LoadState(filepath.Join(m.stateDir, StateFileName))
	}

	if m.state.Certificates == nil {
		return make(map[string]*CertState)
	}

	out := make(map[string]*CertState, len(m.state.Certificates))
	for k, v := range m.state.Certificates {
		out[k] = v
	}
	return out
}

// GetCertificate returns a single certificate entry for a domain.
func (m *Manager) GetCertificate(domain string) *CertState {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state == nil {
		m.state = LoadState(filepath.Join(m.stateDir, StateFileName))
	}

	if m.state == nil || m.state.Certificates == nil {
		return nil
	}

	return m.state.Certificates[domain]
}

// GetLastBackupTime returns the time of the last successful backup.
// If no backup has been recorded, returns zero time.
func (m *Manager) GetLastBackupTime() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state == nil {
		m.state = LoadState(filepath.Join(m.stateDir, StateFileName))
	}

	if m.state == nil || m.state.Backup == nil || m.state.Backup.LastBackupAt == "" {
		return time.Time{}
	}

	t, err := time.Parse(time.RFC3339, m.state.Backup.LastBackupAt)
	if err != nil {
		slog.Warn("failed to parse last backup time", "error", err)
		return time.Time{}
	}
	return t
}

// RecordBackupCompletion saves the backup result to state for missed-backup detection.
func (m *Manager) RecordBackupCompletion(snapshotID string, duration time.Duration, totalBytes int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state == nil {
		m.state = LoadState(filepath.Join(m.stateDir, StateFileName))
	}

	if m.state.Backup == nil {
		m.state.Backup = &BackupState{}
	}

	m.state.Backup.LastBackupAt = time.Now().UTC().Format(time.RFC3339)
	m.state.Backup.LastSnapshotID = snapshotID
	m.state.Backup.LastDurationMs = duration.Milliseconds()
	m.state.Backup.LastTotalBytes = totalBytes

	return m.save()
}

// WasUncleanShutdown returns true if the previous process did not shut down cleanly.
func (m *Manager) WasUncleanShutdown() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == nil {
		m.state = LoadState(filepath.Join(m.stateDir, StateFileName))
	}
	// Fresh state (no file) has ShutdownClean=false by default; treat as clean if never started.
	if m.state.LastUpdated == "" && m.state.AgentVersion == "" {
		return false
	}
	return !m.state.ShutdownClean
}

// MarkStartup records agent start and clears the clean-shutdown flag until exit.
func (m *Manager) MarkStartup(agentVersion string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == nil {
		m.state = LoadState(filepath.Join(m.stateDir, StateFileName))
	}
	m.state.AgentVersion = agentVersion
	m.state.LastUpdated = time.Now().UTC().Format(time.RFC3339)
	m.state.ShutdownClean = false
	if m.state.Update == nil {
		m.state.Update = &UpdateState{}
	}
	m.state.Update.CurrentVersion = agentVersion
	return m.save()
}

// MarkCleanShutdown sets shutdown_clean=true before process exit.
func (m *Manager) MarkCleanShutdown() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == nil {
		m.state = LoadState(filepath.Join(m.stateDir, StateFileName))
	}
	m.state.ShutdownClean = true
	m.state.LastUpdated = time.Now().UTC().Format(time.RFC3339)
	return m.save()
}

// RecordConnected updates connection timestamps after WS connect.
func (m *Manager) RecordConnected() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == nil {
		m.state = LoadState(filepath.Join(m.stateDir, StateFileName))
	}
	if m.state.Connection == nil {
		m.state.Connection = &ConnectionState{}
	}
	m.state.Connection.LastConnected = time.Now().UTC().Format(time.RFC3339)
	m.state.Connection.ReconnectAttempts = 0
	m.state.LastUpdated = time.Now().UTC().Format(time.RFC3339)
	return m.save()
}

// RecordDisconnected updates disconnection timestamp.
func (m *Manager) RecordDisconnected() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == nil {
		m.state = LoadState(filepath.Join(m.stateDir, StateFileName))
	}
	if m.state.Connection == nil {
		m.state.Connection = &ConnectionState{}
	}
	m.state.Connection.LastDisconnected = time.Now().UTC().Format(time.RFC3339)
	m.state.Connection.ReconnectAttempts++
	m.state.LastUpdated = time.Now().UTC().Format(time.RFC3339)
	return m.save()
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
	if state.Routes == nil {
		state.Routes = make(map[string]*RouteState)
	}

	slog.Info("loaded state file",
		"path", path,
		"projects", len(state.Projects),
		"routes", len(state.Routes))
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
