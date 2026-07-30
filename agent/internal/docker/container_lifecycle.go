package docker

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
)

// ---------------------------------------------------------------------------
// Container naming convention
// ---------------------------------------------------------------------------

// containerNamePrefix is the prefix for all containers managed by the agent.
const containerNamePrefix = "yourplatform_"

// ContainerName returns the standard name for a project's container.
//
//	ContainerName("myshop", "app")      → "yourplatform_myshop_app"
//	ContainerName("My Blog", "postgres") → "yourplatform_my-blog_postgres"
func ContainerName(projectName, role string) string {
	return containerNamePrefix + SanitizeProjectName(projectName) + "_" + role
}

// Role constants for container naming.
const (
	RoleApp      = "app"
	RolePostgres = "postgres"
	RoleMySQL    = "mysql"
	RoleRedis    = "redis"
)

// ContainerRole returns the standard role string for a container type.
func ContainerRole(ct ContainerType) string {
	switch ct {
	case ContainerTypeApp:
		return RoleApp
	case ContainerTypePostgres:
		return RolePostgres
	case ContainerTypeMySQL:
		return RoleMySQL
	case ContainerTypeRedis:
		return RoleRedis
	default:
		return RoleApp
	}
}

// ContainerLabels returns the standard labels for a managed container.
func ContainerLabels(projectName string, ct ContainerType) map[string]string {
	return map[string]string{
		containerLabelOwner:     containerLabelOwnerVal,
		containerLabelProject:   SanitizeProjectName(projectName),
		containerLabelRole:      string(ct),
		containerLabelManagedBy: "yourplatform-agent",
	}
}

// ---------------------------------------------------------------------------
// 5A — Full container creation (orchestrator)
// ---------------------------------------------------------------------------

// CreateContainerResult holds all details from a successful container creation.
type CreateContainerResult struct {
	ContainerID string `json:"container_id"`
	Name        string `json:"name"`
	HostPort    int    `json:"host_port,omitempty"` // the assigned host port (random high port)
}

// DeployContainer is the full container creation orchestrator.
// It coordinates image, network, volumes, env, ports, limits, health checks,
// restart policy, and labels into a single operation.
func (c *Client) DeployContainer(ctx context.Context, opts CreateContainerOpts) (*CreateContainerResult, error) {
	slog.Info("deploying container",
		"name", opts.Name,
		"image", opts.Image,
		"type", "container",
	)

	// Step 1: Ensure image is pulled (caller should have done this)
	// Validate the image exists locally before creating
	if _, err := c.VerifyImage(ctx, opts.Image); err != nil {
		return nil, fmt.Errorf("image %s not available locally: %w", opts.Image, err)
	}

	// Step 2: Set defaults for fields not explicitly configured
	if opts.RestartPolicy == "" {
		opts.RestartPolicy = container.RestartPolicyMode("always")
	}

	// Step 3: Create the container
	id, err := c.CreateContainer(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("create container %s: %w", opts.Name, err)
	}

	slog.Info("container created",
		"name", opts.Name,
		"id", id[:12],
	)

	return &CreateContainerResult{
		ContainerID: id,
		Name:        opts.Name,
	}, nil
}

// ---------------------------------------------------------------------------
// 5B — Starting and stopping
// ---------------------------------------------------------------------------

// StartContainerWithWait starts a container and waits for it to reach
// the running state. If the container exits immediately, reads the exit
// code and last log lines to surface the crash reason.
func (c *Client) StartContainerWithWait(ctx context.Context, id string, timeout time.Duration) error {
	if err := c.StartContainer(ctx, id); err != nil {
		return fmt.Errorf("start container %s: %w", id[:12], err)
	}

	if timeout == 0 {
		timeout = 30 * time.Second
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		inspect, err := c.InspectContainer(ctx, id)
		if err != nil {
			return fmt.Errorf("inspect container %s after start: %w", id[:12], err)
		}

		if inspect.State.Running {
			slog.Info("container is running", "id", id[:12])
			return nil
		}

		if inspect.State.ExitCode != 0 {
			// Container crashed immediately — get last log lines
			logs := c.readLastLogLines(ctx, id, 20)
			return &CrashError{
				ContainerID: id[:12],
				ExitCode:    inspect.State.ExitCode,
				Logs:        logs,
			}
		}

		// Container still starting, wait a bit
		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("container %s did not reach running state within %v", id[:12], timeout)
}

// CrashError is returned when a container exits immediately after starting.
type CrashError struct {
	ContainerID string
	ExitCode    int
	Logs        string
}

func (e *CrashError) Error() string {
	if e.Logs != "" {
		return fmt.Sprintf("container %s crashed on startup (exit code %d). Last output:\n%s",
			e.ContainerID, e.ExitCode, e.Logs)
	}
	return fmt.Sprintf("container %s crashed on startup (exit code %d)",
		e.ContainerID, e.ExitCode)
}

// IsOOMKill returns true if the exit code indicates an OOM kill (code 137).
func IsOOMKill(exitCode int) bool {
	return exitCode == 137
}

// readLastLogLines reads the last N lines of container logs.
func (c *Client) readLastLogLines(ctx context.Context, id string, n int) string {
	reader, err := c.cliUnsafe().ContainerLogs(ctx, id, types.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       fmt.Sprintf("%d", n),
	})
	if err != nil {
		return fmt.Sprintf("(failed to read logs: %v)", err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Sprintf("(failed to read logs: %v)", err)
	}

	// Strip Docker log headers (8-byte per line in stream format)
	raw := string(data)
	if len(raw) > 8 {
		// Simple stripping: remove log line headers
		lines := strings.Split(raw, "\n")
		for i, line := range lines {
			if len(line) > 8 {
				lines[i] = line[8:]
			}
		}
		raw = strings.Join(lines, "\n")
	}

	return strings.TrimSpace(raw)
}

// StopContainerGraceful sends SIGTERM, waits up to 30s for graceful shutdown,
// then sends SIGKILL if still running.
func (c *Client) StopContainerGraceful(ctx context.Context, id string) error {
	if err := c.ensureConnected(ctx); err != nil {
		return fmt.Errorf("docker unavailable: %w", err)
	}

	slog.Info("stopping container gracefully", "id", id[:12])

	// Send SIGTERM with timeout
	timeout := 30 * time.Second
	if err := c.cliUnsafe().ContainerStop(ctx, id, &timeout); err != nil {
		// If stop failed and container is still running, force kill
		inspect, inspectErr := c.InspectContainer(ctx, id)
		if inspectErr == nil && inspect.State.Running {
			slog.Warn("graceful stop failed, force killing", "id", id[:12])
			if killErr := c.KillContainer(ctx, id); killErr != nil {
				return fmt.Errorf("force kill after failed stop: %v (stop error: %w)", killErr, err)
			}
		}
		return fmt.Errorf("stop container: %w", err)
	}

	return nil
}

// KillContainer sends SIGKILL immediately.
func (c *Client) KillContainer(ctx context.Context, id string) error {
	if err := c.ensureConnected(ctx); err != nil {
		return fmt.Errorf("docker unavailable: %w", err)
	}

	slog.Warn("force killing container", "id", id[:12])
	return c.cliUnsafe().ContainerKill(ctx, id, "SIGKILL")
}

// RestartContainer performs a controlled stop + start (not Docker restart).
// This gives us better error handling and crash detection.
func (c *Client) RestartContainer(ctx context.Context, id string) error {
	if err := c.StopContainerGraceful(ctx, id); err != nil {
		// If container was already stopped, that's fine
		if !strings.Contains(err.Error(), "is already stopped") {
			slog.Warn("stop during restart had issues", "id", id[:12], "error", err)
		}
	}

	if err := c.StartContainer(ctx, id); err != nil {
		return fmt.Errorf("start container after restart: %w", err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// 5C — Removal
// ---------------------------------------------------------------------------

// RemoveContainerSafe stops the container if running, then removes it.
// Does NOT remove volumes or networks.
func (c *Client) RemoveContainerSafe(ctx context.Context, id string) error {
	// Check if container is running
	inspect, err := c.InspectContainer(ctx, id)
	if err != nil {
		// Container might not exist
		return fmt.Errorf("inspect container %s: %w", id[:12], err)
	}

	if inspect.State.Running {
		slog.Info("stopping container before removal", "id", id[:12])
		if err := c.StopContainerGraceful(ctx, id); err != nil {
			slog.Warn("stop before removal had issues", "id", id[:12], "error", err)
		}
	}

	if err := c.RemoveContainer(ctx, id); err != nil {
		return fmt.Errorf("remove container %s: %w", id[:12], err)
	}

	return nil
}
