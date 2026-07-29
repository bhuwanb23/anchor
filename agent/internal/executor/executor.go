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
	AppName string `json:"app_name"`
	Image   string `json:"image"`
	Port    int    `json:"port"`
	Domain  string `json:"domain"`
	Name    string `json:"name"`
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

type Executor struct {
	docker  *docker.Client
	caddy   *caddy.Manager
	backup  *backup.BackupManager
}

func New(dockerClient *docker.Client, caddyManager *caddy.Manager, backupManager *backup.BackupManager) *Executor {
	return &Executor{
		docker: dockerClient,
		caddy:  caddyManager,
		backup: backupManager,
	}
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

	if err := e.docker.PullImage(ctx, p.Image); err != nil {
		return fmt.Errorf("pull image: %w", err)
	}

	containerName := p.Name
	if containerName == "" {
		containerName = p.AppName
	}

	id, err := e.docker.CreateContainer(ctx, docker.CreateContainerOpts{
		Name:  containerName,
		Image: p.Image,
		Env:   []string{},
	})
	if err != nil {
		return fmt.Errorf("create container: %w", err)
	}

	if err := e.docker.StartContainer(ctx, id); err != nil {
		return fmt.Errorf("start container: %w", err)
	}

	if p.Domain != "" && p.Port > 0 {
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
		ContainerID string `json:"container_id"`
		PreviousImage string `json:"previous_image"`
	}
	if err := json.Unmarshal(cmd.Payload, &p); err != nil {
		return fmt.Errorf("invalid rollback payload: %w", err)
	}

	if p.ContainerID != "" {
		_ = e.docker.StopContainer(ctx, p.ContainerID)
	}

	if p.PreviousImage != "" {
		id, err := e.docker.CreateContainer(ctx, docker.CreateContainerOpts{
			Name:  "rollback-" + p.ContainerID[:8],
			Image: p.PreviousImage,
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
