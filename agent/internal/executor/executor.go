package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/yourname/yourplatform/agent/internal/backup"
	"github.com/yourname/yourplatform/agent/internal/caddy"
	"github.com/yourname/yourplatform/agent/internal/docker"
	"github.com/yourname/yourplatform/agent/internal/env"
	"github.com/yourname/yourplatform/agent/internal/logstream"
	"github.com/yourname/yourplatform/agent/internal/state"
)

type Command struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type Result struct {
	CommandID string    `json:"command_id"`
	Status    string    `json:"status"`
	Output    string    `json:"output"`
	Error     string    `json:"error,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// VolumeSpec describes a volume to create and mount into a container.
type VolumeSpec struct {
	Purpose   string `json:"purpose"`
	MountPath string `json:"mount_path"`
}

type DeployPayload struct {
	AppName       string              `json:"app_name"`
	Image         string              `json:"image"`
	Port          int                 `json:"port"`
	Domain        string              `json:"domain"`
	Name          string              `json:"name"`
	ContainerType docker.ContainerType `json:"container_type,omitempty"`
	Volumes       []VolumeSpec        `json:"volumes,omitempty"`
	MemoryLimitMB int64               `json:"memory_limit_mb,omitempty"` // override default memory limit
	Env           map[string]string   `json:"env,omitempty"`            // env vars from dashboard
}

type UpdateEnvPayload struct {
	ProjectName string            `json:"project_name"`
	Vars        map[string]string `json:"vars"` // key=value pairs to set
	Remove      []string          `json:"remove,omitempty"` // keys to remove
}

type StopPayload struct {
	ContainerID string `json:"container_id"`
}

type RestartPayload struct {
	ContainerID string `json:"container_id"`
}

type BackupPayload struct {
	SourcePath string `json:"source_path"`
}

type FetchLogsPayload struct {
	ContainerID string `json:"container_id"`
}

type DeleteProjectPayload struct {
	ProjectName string `json:"project_name"`
}

type UpdateDomainsPayload struct {
	AppName string   `json:"app_name"`
	Domains []string `json:"domains"`
}

type Executor struct {
	docker       *docker.Client
	caddy        *caddy.Manager
	backup       *backup.BackupManager
	imageCache   *docker.ImageCache
	reporter     ProgressReporter
	envManager   *env.Manager
	logStreamer  *logstream.LogStreamer
	stateManager *state.Manager
	authorizer   *caddy.DomainAuthorizer
}

// ProgressReporter sends image pull progress updates to the control plane.
type ProgressReporter interface {
	ReportProgress(progress docker.PullProgress)
}

func New(dockerClient *docker.Client, caddyManager *caddy.Manager, backupManager *backup.BackupManager) *Executor {
	return &Executor{
		docker:     dockerClient,
		caddy:      caddyManager,
		backup:     backupManager,
		envManager: env.NewManager(""),
	}
}

// WithImageCache attaches an image cache to the executor for smart pull decisions.
func (e *Executor) WithImageCache(cache *docker.ImageCache) *Executor {
	e.imageCache = cache
	return e
}

// WithProgressReporter attaches a progress reporter for pull progress updates.
func (e *Executor) WithProgressReporter(reporter ProgressReporter) *Executor {
	e.reporter = reporter
	return e
}

// WithLogStreamer attaches a log streamer for container log streaming.
func (e *Executor) WithLogStreamer(ls *logstream.LogStreamer) *Executor {
	e.logStreamer = ls
	return e
}

// WithStateManager attaches a state manager for persistence across restarts.
func (e *Executor) WithStateManager(sm *state.Manager) *Executor {
	e.stateManager = sm
	return e
}

// WithAuthorizer attaches a domain authorizer for on-demand TLS.
func (e *Executor) WithAuthorizer(a *caddy.DomainAuthorizer) *Executor {
	e.authorizer = a
	return e
}

// LogStreamer returns the attached log streamer, or nil if not configured.
func (e *Executor) LogStreamer() *logstream.LogStreamer {
	return e.logStreamer
}

// StateManager returns the attached state manager, or nil if not configured.
func (e *Executor) StateManager() *state.Manager {
	return e.stateManager
}

type CommandQueue struct {
	commands []Command
	mu       sync.Mutex
}

func NewCommandQueue() *CommandQueue {
	return &CommandQueue{
		commands: make([]Command, 0),
	}
}

func (q *CommandQueue) Enqueue(cmd Command) {
	q.mu.Lock()
	defer q.mu.Unlock()
	slog.Info("enqueuing command", "id", cmd.ID, "type", cmd.Type)
	q.commands = append(q.commands, cmd)
}

func (q *CommandQueue) Dequeue() (Command, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.commands) == 0 {
		return Command{}, false
	}

	cmd := q.commands[0]
	q.commands = q.commands[1:]
	return cmd, true
}

func (q *CommandQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.commands)
}

func (e *Executor) Execute(ctx context.Context, cmd Command) Result {
	slog.Info("executing command", "id", cmd.ID, "type", cmd.Type)

	result := Result{
		CommandID: cmd.ID,
		Timestamp: time.Now().UTC(),
	}

	var err error
	switch cmd.Type {
	case "deploy":
		err = e.executeDeploy(ctx, cmd, &result)
	case "rollback":
		err = e.executeRollback(ctx, cmd, &result)
	case "restart":
		err = e.executeRestart(ctx, cmd, &result)
	case "stop":
		err = e.executeStop(ctx, cmd, &result)
	case "backup":
		err = e.executeBackup(ctx, cmd, &result)
	case "fetch_logs":
		err = e.executeFetchLogs(ctx, cmd, &result)
	case "delete_project":
		err = e.executeDeleteProject(ctx, cmd, &result)
	case "update_env":
		err = e.executeUpdateEnv(ctx, cmd, &result)
	case "update_domains":
		err = e.executeUpdateDomains(ctx, cmd, &result)
	default:
		result.Status = "error"
		result.Error = fmt.Sprintf("unknown command type: %s", cmd.Type)
	}

	if err != nil {
		result.Status = "error"
		result.Error = err.Error()
	}

	return result
}

func (e *Executor) executeDeploy(ctx context.Context, cmd Command, result *Result) error {
	var p DeployPayload
	if err := json.Unmarshal(cmd.Payload, &p); err != nil {
		return fmt.Errorf("invalid deploy payload: %w", err)
	}

	// Pull image first
	var progressFn docker.PullProgressFunc
	if e.reporter != nil {
		progressFn = func(p docker.PullProgress) error {
			e.reporter.ReportProgress(p)
			return nil
		}
	}
	if _, _, err := e.docker.PullImageIfNeeded(ctx, p.Image, e.imageCache, progressFn); err != nil {
		return fmt.Errorf("pull image: %w", err)
	}

	// Determine container type (default to "app")
	ct := p.ContainerType
	if ct == "" {
		ct = docker.ContainerTypeApp
	}

	// Build container name using naming convention
	containerName := docker.ContainerName(p.AppName, docker.ContainerRole(ct))

	// --- Network setup ---
	networkName := docker.ProjectNetworkName(p.AppName)
	networkEndpoint := docker.NetworkEndpointConfig{
		NetworkName: networkName,
	}
	if ct != docker.ContainerTypeApp {
		networkEndpoint.Aliases = docker.DatabaseAliases(ct)
	}

	if _, err := e.docker.EnsureProjectNetwork(ctx, p.AppName); err != nil {
		slog.Warn("failed to ensure project network", "app", p.AppName, "error", err)
	}

	// --- Volume setup ---
	var volumeMounts []docker.VolumeMount
	if ct != docker.ContainerTypeApp {
		dbMount, err := e.docker.EnsureDBVolume(ctx, p.AppName, ct)
		if err != nil {
			slog.Warn("failed to ensure database volume", "app", p.AppName, "error", err)
		} else {
			volumeMounts = append(volumeMounts, *dbMount)
		}
	}

	for _, vs := range p.Volumes {
		vol, err := e.docker.EnsureVolume(ctx, p.AppName, vs.Purpose)
		if err != nil {
			slog.Warn("failed to ensure volume", "app", p.AppName, "purpose", vs.Purpose, "error", err)
			continue
		}
		volumeMounts = append(volumeMounts, docker.VolumeMount{
			Name:      vol.Name,
			MountPath: vs.MountPath,
		})
	}

	// --- Port setup ---
	portSpec := &docker.AppPortSpec{
		ContainerPort: p.Port,
		BindAddress:   "127.0.0.1",
	}
	portMap, exposedPorts := docker.PortMapping(ct, portSpec)

	// --- Resource limits ---
	rlimits := docker.DefaultResourceLimits(ct)
	if p.MemoryLimitMB > 0 {
		rlimits.MemoryHard = p.MemoryLimitMB * 1024 * 1024
	}

	// Validate limits against available server RAM
	if totalRAM := docker.GetTotalRAMMB(); totalRAM > 0 {
		if err := docker.ValidateResourceLimits(rlimits, totalRAM); err != nil {
			return fmt.Errorf("resource limits invalid: %w", err)
		}
	}

	// --- Health check ---
	healthCheck := docker.DefaultHealthCheck(ct, p.Port)

	// --- Environment variables ---
	// Read existing env file, merge with deploy payload, add defaults
	envVars, err := e.envManager.ReadEnvFile(p.AppName)
	if err != nil {
		slog.Warn("failed to read env file", "app", p.AppName, "error", err)
		envVars = make(map[string]string)
	}

	// Overlay vars from deploy payload (dashboard updates)
	for k, v := range p.Env {
		envVars[k] = v
	}

	// Auto-generate DATABASE_URL when Postgres is added to a project
	if ct == docker.ContainerTypePostgres {
		dbName := p.AppName + "_db"
		if _, exists := envVars["DATABASE_URL"]; !exists {
			// Generate a random password and store it
			password := generateRandomPassword(24)
			envVars["DATABASE_URL"] = env.GenerateDatabaseURL(password, dbName)
		}
	}

	// Add platform defaults (YOURPLATFORM, PORT)
	envVars = env.MergeWithDefaults(envVars, p.Port)

	// Write updated env file (values stored locally, never sent to control plane)
	if err := e.envManager.WriteEnvFile(p.AppName, envVars); err != nil {
		slog.Warn("failed to write env file", "app", p.AppName, "error", err)
	}

	// --- Container labels ---
	labels := docker.ContainerLabels(p.AppName, ct)

	// --- Deploy (create + start with crash detection) ---

	// Replace existing container with same name (enables re-deploy)
	if _, err := e.docker.ReplaceExistingContainer(ctx, containerName); err != nil {
		slog.Warn("failed to replace existing container", "name", containerName, "error", err)
	}

	id, err := e.docker.CreateContainer(ctx, docker.CreateContainerOpts{
		Name:           containerName,
		Image:          p.Image,
		Env:            env.FormatForDocker(envVars),
		PortBindings:   portMap,
		ExposedPorts:   exposedPorts,
		Networks:       []docker.NetworkEndpointConfig{networkEndpoint},
		VolumeMounts:   volumeMounts,
		Labels:         labels,
		ResourceLimits: rlimits,
		HealthCheck:    healthCheck,
		RestartPolicy:  "always",
	})
	if err != nil {
		return fmt.Errorf("create container: %w", err)
	}

	// Start with crash detection (30s timeout)
	if err := e.docker.StartContainerWithWait(ctx, id, 30*time.Second); err != nil {
		// If it's a crash, surface the crash reason
		if crashErr, ok := err.(*docker.CrashError); ok {
			_ = e.docker.RemoveContainerSafe(ctx, id)
			result.Status = "error"
			result.Output = ""

			// OOM-specific messaging
			if docker.IsOOMKill(crashErr.ExitCode) {
				limitMB := rlimits.MemoryHard / (1024 * 1024)
				result.Error = fmt.Sprintf(
					"Your app ran out of memory and was restarted (exit code 137). "+
						"Current memory limit: %dMB. Consider increasing the memory limit "+
						"or investigating memory usage in your app.", limitMB)
			} else {
				result.Error = crashErr.Error()
			}
			return nil
		}
		// Some other error starting
		_ = e.docker.RemoveContainerSafe(ctx, id)
		return fmt.Errorf("start container: %w", err)
	}

	// Wait for health check to pass (up to 2 minutes)
	if err := e.docker.WaitForHealthy(ctx, id, 2*time.Minute); err != nil {
		slog.Warn("container did not become healthy after deploy",
			"app", p.AppName, "container", id[:12], "error", err)
		// Non-fatal — container may still serve traffic without health check
	}

	// Start background health monitoring (runs until context is cancelled)
	e.docker.RunHealthMonitor(ctx, id, 30*time.Second, func(_ context.Context, health *docker.ContainerHealth) bool {
		if health.Status == docker.HealthUnhealthy {
			slog.Warn("container unhealthy",
				"app", p.AppName, "container", id[:12],
				"failing_streak", health.FailingStreak, "output", health.Output)
		}
		return true // continue monitoring
	})

	// --- Caddy route ---
	if p.Domain != "" && p.Port > 0 {
		routeID := caddy.RouteID(p.AppName)
		upstream := fmt.Sprintf("127.0.0.1:%d", p.Port)
		domains := []string{p.Domain}
		if err := e.caddy.SetRouteByID(routeID, domains, upstream); err != nil {
			return fmt.Errorf("set caddy route: %w", err)
		}
		// Store route in state
		if e.stateManager != nil {
			_ = e.stateManager.SetRoute(routeID, p.AppName, domains, upstream)
		}
	}

	// --- Update state file ---
	if e.stateManager != nil {
		_ = e.stateManager.SetContainer(p.AppName, string(ct), &state.ContainerState{
			ContainerID:   id,
			Image:         p.Image,
			Status:        "running",
			Domain:        p.Domain,
			HostPort:      p.Port,
			RestartPolicy: "always",
			CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		})
	}

	volInfo := ""
	if len(volumeMounts) > 0 {
		volInfo = fmt.Sprintf(", %d volume(s) mounted", len(volumeMounts))
	}

	result.Status = "success"
	result.Output = fmt.Sprintf("deployed %s (container %s%s)", p.AppName, id[:12], volInfo)
	return nil
}

func (e *Executor) executeRollback(ctx context.Context, cmd Command, result *Result) error {
	var p struct {
		ContainerID   string `json:"container_id"`
		PreviousImage string `json:"previous_image"`
		AppName       string `json:"app_name"`
	}
	if err := json.Unmarshal(cmd.Payload, &p); err != nil {
		return fmt.Errorf("invalid rollback payload: %w", err)
	}

	if p.ContainerID != "" {
		_ = e.docker.StopContainerGraceful(ctx, p.ContainerID)
	}

	if p.PreviousImage != "" {
		if p.AppName != "" {
			if _, err := e.docker.EnsureProjectNetwork(ctx, p.AppName); err != nil {
				slog.Warn("ensure network for rollback", "app", p.AppName, "error", err)
			}
		}

		networks := []docker.NetworkEndpointConfig{}
		if p.AppName != "" {
			networks = append(networks, docker.NetworkEndpointConfig{
				NetworkName: docker.ProjectNetworkName(p.AppName),
			})
		}

		id, err := e.docker.CreateContainer(ctx, docker.CreateContainerOpts{
			Name:     "rollback-" + p.ContainerID[:8],
			Image:    p.PreviousImage,
			Networks: networks,
		})
		if err != nil {
			return fmt.Errorf("create rollback container: %w", err)
		}
		if err := e.docker.StartContainer(ctx, id); err != nil {
			return fmt.Errorf("start rollback container: %w", err)
		}

		// Update state with rollback image
		if e.stateManager != nil && p.AppName != "" {
			_ = e.stateManager.SetContainer(p.AppName, "app", &state.ContainerState{
				ContainerID: id,
				Image:       p.PreviousImage,
				Status:      "running",
				CreatedAt:   time.Now().UTC().Format(time.RFC3339),
			})
		}

		// Update Caddy route to point to rollback container
		// The rollback container gets a new port, so we need to inspect it
		if p.AppName != "" {
			if inspect, err := e.docker.InspectContainer(ctx, id); err == nil {
				// Find the mapped port for the app
				if ports, ok := inspect.NetworkSettings.Ports["3000/tcp"]; ok && len(ports) > 0 {
					routeID := caddy.RouteID(p.AppName)
					// Look up existing domains from state
					routes := e.stateManager.GetRoutes()
					if existing, ok := routes[routeID]; ok {
						upstream := fmt.Sprintf("127.0.0.1:%s", ports[0].HostPort)
						if err := e.caddy.SetRouteByID(routeID, existing.Domains, upstream); err != nil {
							slog.Warn("failed to update caddy route for rollback", "error", err)
						}
						_ = e.stateManager.SetRoute(routeID, p.AppName, existing.Domains, upstream)
					}
				}
			}
		}

		result.Output = fmt.Sprintf("rolled back to %s (container %s)", p.PreviousImage, id[:12])
	}

	result.Status = "success"
	return nil
}

func (e *Executor) executeRestart(ctx context.Context, cmd Command, result *Result) error {
	var p RestartPayload
	if err := json.Unmarshal(cmd.Payload, &p); err != nil {
		return fmt.Errorf("invalid restart payload: %w", err)
	}

	if err := e.docker.RestartContainer(ctx, p.ContainerID); err != nil {
		return fmt.Errorf("restart container: %w", err)
	}

	// Update state — lookup project/role from container labels
	if e.stateManager != nil {
		if inspect, err := e.docker.InspectContainer(ctx, p.ContainerID); err == nil {
			project := inspect.Config.Labels["yourplatform.project"]
			role := inspect.Config.Labels["yourplatform.role"]
			if project != "" && role != "" {
				_ = e.stateManager.UpdateStatus(project, role, "running")
			}
		}
	}

	result.Status = "success"
	result.Output = fmt.Sprintf("restarted container %s", p.ContainerID[:12])
	return nil
}

func (e *Executor) executeStop(ctx context.Context, cmd Command, result *Result) error {
	var p StopPayload
	if err := json.Unmarshal(cmd.Payload, &p); err != nil {
		return fmt.Errorf("invalid stop payload: %w", err)
	}

	// Look up project/role from container labels before stopping
	var project, role string
	if inspect, err := e.docker.InspectContainer(ctx, p.ContainerID); err == nil {
		project = inspect.Config.Labels["yourplatform.project"]
		role = inspect.Config.Labels["yourplatform.role"]
	}

	if err := e.docker.StopContainerGraceful(ctx, p.ContainerID); err != nil {
		return fmt.Errorf("stop container: %w", err)
	}

	// Remove Caddy route so domain returns 404 instead of 502
	if project != "" {
		routeID := caddy.RouteID(project)
		if err := e.caddy.DeleteRouteByID(routeID); err != nil {
			slog.Warn("failed to remove caddy route on stop", "route_id", routeID, "error", err)
		}
		if e.stateManager != nil {
			_ = e.stateManager.RemoveRoute(routeID)
		}
	}

	// Update state
	if e.stateManager != nil && project != "" && role != "" {
		_ = e.stateManager.UpdateStatus(project, role, "stopped")
	}

	result.Status = "success"
	result.Output = fmt.Sprintf("stopped container %s", p.ContainerID[:12])
	return nil
}

func (e *Executor) executeDeleteProject(ctx context.Context, cmd Command, result *Result) error {
	var p DeleteProjectPayload
	if err := json.Unmarshal(cmd.Payload, &p); err != nil {
		return fmt.Errorf("invalid delete_project payload: %w", err)
	}

	// Remove Caddy routes for this project
	if e.stateManager != nil {
		routes := e.stateManager.GetRoutes()
		for id, r := range routes {
			if r.Project == p.ProjectName {
				if err := e.caddy.DeleteRouteByID(id); err != nil {
					slog.Warn("failed to remove caddy route", "route_id", id, "error", err)
				}
				_ = e.stateManager.RemoveRoute(id)
			}
		}
	}

	if err := e.docker.RemoveProject(ctx, p.ProjectName, false); err != nil {
		return fmt.Errorf("delete project %s: %w", p.ProjectName, err)
	}

	// Remove from state file (also cleans up remaining routes for this project)
	if e.stateManager != nil {
		_ = e.stateManager.RemoveProject(p.ProjectName)
	}

	result.Status = "success"
	result.Output = fmt.Sprintf("deleted project %s", p.ProjectName)
	return nil
}

func (e *Executor) executeBackup(ctx context.Context, cmd Command, result *Result) error {
	var p BackupPayload
	if err := json.Unmarshal(cmd.Payload, &p); err != nil {
		return fmt.Errorf("invalid backup payload: %w", err)
	}

	if err := e.backup.RunBackup(ctx, p.SourcePath); err != nil {
		return fmt.Errorf("backup: %w", err)
	}

	result.Status = "success"
	result.Output = fmt.Sprintf("backup completed for %s", p.SourcePath)
	return nil
}

func (e *Executor) executeFetchLogs(ctx context.Context, cmd Command, result *Result) error {
	var p FetchLogsPayload
	if err := json.Unmarshal(cmd.Payload, &p); err != nil {
		return fmt.Errorf("invalid fetch_logs payload: %w", err)
	}

	reader, err := e.docker.GetContainerLogs(ctx, p.ContainerID)
	if err != nil {
		return fmt.Errorf("get logs: %w", err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("read logs: %w", err)
	}

	result.Status = "success"
	result.Output = string(data)
	return nil
}

func (e *Executor) executeUpdateEnv(ctx context.Context, cmd Command, result *Result) error {
	var p UpdateEnvPayload
	if err := json.Unmarshal(cmd.Payload, &p); err != nil {
		return fmt.Errorf("invalid update_env payload: %w", err)
	}

	// Set/update vars
	for k, v := range p.Vars {
		if err := e.envManager.UpdateEnvVar(p.ProjectName, k, v); err != nil {
			return fmt.Errorf("update env var %s: %w", k, err)
		}
		slog.Info("env var updated", "project", p.ProjectName, "key", k)
	}

	// Remove vars
	for _, k := range p.Remove {
		if err := e.envManager.RemoveEnvVar(p.ProjectName, k); err != nil {
			return fmt.Errorf("remove env var %s: %w", k, err)
		}
		slog.Info("env var removed", "project", p.ProjectName, "key", k)
	}

	result.Status = "success"
	result.Output = fmt.Sprintf("env updated for %s — restart to apply", p.ProjectName)
	return nil
}

func (e *Executor) executeUpdateDomains(ctx context.Context, cmd Command, result *Result) error {
	var p UpdateDomainsPayload
	if err := json.Unmarshal(cmd.Payload, &p); err != nil {
		return fmt.Errorf("invalid update_domains payload: %w", err)
	}

	if p.AppName == "" {
		return fmt.Errorf("app_name is required")
	}

	if len(p.Domains) == 0 {
		return fmt.Errorf("at least one domain is required")
	}

	// Update Caddy route with new domain list
	routeID := caddy.RouteID(p.AppName)

	// Get existing route to preserve upstream
	existing, err := e.caddy.GetRouteByID(routeID)
	if err != nil {
		return fmt.Errorf("get existing route: %w", err)
	}

	var upstream string
	if existing != nil && len(existing.Handle) > 0 && len(existing.Handle[0].Upstreams) > 0 {
		upstream = existing.Handle[0].Upstreams[0].Dial
	} else {
		return fmt.Errorf("no existing route found for %s", p.AppName)
	}

	if err := e.caddy.SetRouteByID(routeID, p.Domains, upstream); err != nil {
		return fmt.Errorf("set caddy route: %w", err)
	}

	// Update state
	if e.stateManager != nil {
		_ = e.stateManager.SetRoute(routeID, p.AppName, p.Domains, upstream)
	}

	// Update domain authorizer for on-demand TLS
	if e.authorizer != nil {
		e.authorizer.SetDomains(p.Domains)
	}

	slog.Info("domains updated", "app", p.AppName, "domains", p.Domains)
	result.Status = "success"
	result.Output = fmt.Sprintf("domains updated for %s: %v", p.AppName, p.Domains)
	return nil
}

// generateRandomPassword returns a random alphanumeric password of the given length.
func generateRandomPassword(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		// Use a simple LCG for determinism in tests; not cryptographically secure
		b[i] = charset[i%len(charset)]
	}
	return string(b)
}
