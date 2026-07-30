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

type Executor struct {
	docker     *docker.Client
	caddy      *caddy.Manager
	backup     *backup.BackupManager
	imageCache *docker.ImageCache
	reporter   ProgressReporter
}

// ProgressReporter sends image pull progress updates to the control plane.
type ProgressReporter interface {
	ReportProgress(progress docker.PullProgress)
}

func New(dockerClient *docker.Client, caddyManager *caddy.Manager, backupManager *backup.BackupManager) *Executor {
	return &Executor{
		docker: dockerClient,
		caddy:  caddyManager,
		backup: backupManager,
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
	// No validation against total RAM here (agent may not know it)

	// --- Health check ---
	healthCheck := docker.DefaultHealthCheck(ct, p.Port)

	// --- Container labels ---
	labels := docker.ContainerLabels(p.AppName, ct)

	// --- Deploy (create + start with crash detection) ---
	id, err := e.docker.CreateContainer(ctx, docker.CreateContainerOpts{
		Name:           containerName,
		Image:          p.Image,
		Env:            []string{},
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
			result.Error = crashErr.Error()
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

	// --- Caddy route ---
	if p.Domain != "" && p.Port > 0 {
		if err := e.caddy.SetRoute(p.Domain, p.Port); err != nil {
			slog.Warn("failed to set caddy route", "error", err)
		}
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

	result.Status = "success"
	result.Output = fmt.Sprintf("restarted container %s", p.ContainerID[:12])
	return nil
}

func (e *Executor) executeStop(ctx context.Context, cmd Command, result *Result) error {
	var p StopPayload
	if err := json.Unmarshal(cmd.Payload, &p); err != nil {
		return fmt.Errorf("invalid stop payload: %w", err)
	}

	if err := e.docker.StopContainerGraceful(ctx, p.ContainerID); err != nil {
		return fmt.Errorf("stop container: %w", err)
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

	if err := e.docker.RemoveProject(ctx, p.ProjectName, false); err != nil {
		return fmt.Errorf("delete project %s: %w", p.ProjectName, err)
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
