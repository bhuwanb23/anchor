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

type DeployPayload struct {
	AppName     string            `json:"app_name"`
	Image       string            `json:"image"`
	Port        int               `json:"port"`
	Domain      string            `json:"domain"`
	Name        string            `json:"name"`
	ContainerType docker.ContainerType `json:"container_type,omitempty"` // "app", "postgres", "mysql", "redis"
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

	if _, _, err := e.docker.PullImageIfNeeded(ctx, p.Image, e.imageCache, nil); err != nil {
		return fmt.Errorf("pull image: %w", err)
	}

	containerName := p.Name
	if containerName == "" {
		containerName = p.AppName
	}

	// Determine container type (default to "app")
	ct := p.ContainerType
	if ct == "" {
		ct = docker.ContainerTypeApp
	}

	// Build network config for attaching to project network at creation time
	networkName := docker.ProjectNetworkName(p.AppName)
	networkEndpoint := docker.NetworkEndpointConfig{
		NetworkName: networkName,
	}

	// For database containers, add standard DNS aliases
	if ct != docker.ContainerTypeApp {
		networkEndpoint.Aliases = docker.DatabaseAliases(ct)
	}

	// Build port mappings based on container type
	portSpec := &docker.AppPortSpec{
		ContainerPort: p.Port,
		// HostPort: 0 = random high port
		BindAddress: "127.0.0.1",
	}
	portMap, exposedPorts := docker.PortMapping(ct, portSpec)

	// Ensure the project network exists before creating the container
	// (The network must exist for the container to attach at creation time)
	if _, err := e.docker.EnsureProjectNetwork(ctx, p.AppName); err != nil {
		slog.Warn("failed to ensure project network exists", "app", p.AppName, "error", err)
		// Non-fatal — container can still be created without network
	}

	id, err := e.docker.CreateContainer(ctx, docker.CreateContainerOpts{
		Name:         containerName,
		Image:        p.Image,
		Env:          []string{},
		PortBindings: portMap,
		ExposedPorts: exposedPorts,
		Networks:     []docker.NetworkEndpointConfig{networkEndpoint},
	})
	if err != nil {
		return fmt.Errorf("create container: %w", err)
	}

	if err := e.docker.StartContainer(ctx, id); err != nil {
		return fmt.Errorf("start container: %w", err)
	}

	if p.Domain != "" && p.Port > 0 {
		// If we assigned a random high port, we need to know it to tell Caddy.
		// For now, we use the container port directly (Caddy will discover it).
		if err := e.caddy.SetRoute(p.Domain, p.Port); err != nil {
			slog.Warn("failed to set caddy route", "error", err)
		}
	}

	result.Status = "success"
	result.Output = fmt.Sprintf("deployed %s (container %s)", p.AppName, id[:12])
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
		_ = e.docker.StopContainer(ctx, p.ContainerID)
	}

	if p.PreviousImage != "" {
		// Ensure project network exists for rollback
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
			Name:      "rollback-" + p.ContainerID[:8],
			Image:     p.PreviousImage,
			Networks:  networks,
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

	if err := e.docker.StopContainer(ctx, p.ContainerID); err != nil {
		slog.Warn("stop container for restart failed", "error", err)
	}

	if err := e.docker.StartContainer(ctx, p.ContainerID); err != nil {
		return fmt.Errorf("start container: %w", err)
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

	if err := e.docker.StopContainer(ctx, p.ContainerID); err != nil {
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
