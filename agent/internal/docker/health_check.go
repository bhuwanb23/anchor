package docker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/strslice"
)

// HealthCheckConfig defines a Docker health check for a container.
type HealthCheckConfig struct {
	Test        []string      // Command to run (e.g., ["CMD-SHELL", "pg_isready -U postgres"])
	Interval    time.Duration // How often to run the check (default 30s)
	Timeout     time.Duration // Each check must complete within this (default 10s)
	StartPeriod time.Duration // Give the app time to boot before first check (default 60s)
	Retries     int           // Consecutive failures before marking unhealthy (default 3)
}

// ToDockerConfig converts the health check to Docker's format.
func (hc *HealthCheckConfig) ToDockerConfig() *container.HealthConfig {
	interval := hc.Interval
	if interval == 0 {
		interval = 30 * time.Second
	}
	timeout := hc.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	startPeriod := hc.StartPeriod
	if startPeriod == 0 {
		startPeriod = 60 * time.Second
	}
	retries := hc.Retries
	if retries == 0 {
		retries = 3
	}

	return &container.HealthConfig{
		Test:        hc.Test,
		Interval:    interval,
		Timeout:     timeout,
		StartPeriod: startPeriod,
		Retries:     retries,
	}
}

// ---------------------------------------------------------------------------
// Standard health check configurations per container type
// ---------------------------------------------------------------------------

// DefaultHealthCheck returns the appropriate health check for a container type.
// Returns nil for unknown types (generic fallback — just check process is running).
func DefaultHealthCheck(ct ContainerType, appPort int) *HealthCheckConfig {
	switch ct {
	case ContainerTypeApp:
		return appHealthCheck(appPort)
	case ContainerTypePostgres:
		return postgresHealthCheck()
	case ContainerTypeMySQL:
		return mysqlHealthCheck()
	case ContainerTypeRedis:
		return redisHealthCheck()
	default:
		return nil // fallback to Docker's default (process check)
	}
}

// appHealthCheck checks a web app by making an HTTP GET to its port.
// Healthy if response status < 500.
func appHealthCheck(port int) *HealthCheckConfig {
	if port <= 0 {
		return nil
	}
	return &HealthCheckConfig{
		Test: []string{
			"CMD-SHELL",
			fmt.Sprintf("wget -qO- http://localhost:%d/ || curl -sf http://localhost:%d/", port, port),
		},
		Interval:    30 * time.Second,
		Timeout:     10 * time.Second,
		StartPeriod: 60 * time.Second,
		Retries:     3,
	}
}

// postgresHealthCheck uses pg_isready which ships with Postgres.
func postgresHealthCheck() *HealthCheckConfig {
	return &HealthCheckConfig{
		Test:        []string{"CMD-SHELL", "pg_isready -U postgres || exit 1"},
		Interval:    30 * time.Second,
		Timeout:     10 * time.Second,
		StartPeriod: 60 * time.Second,
		Retries:     3,
	}
}

// mysqlHealthCheck uses mysqladmin ping.
func mysqlHealthCheck() *HealthCheckConfig {
	return &HealthCheckConfig{
		Test:        []string{"CMD-SHELL", "mysqladmin ping -u root --silent || exit 1"},
		Interval:    30 * time.Second,
		Timeout:     10 * time.Second,
		StartPeriod: 60 * time.Second,
		Retries:     3,
	}
}

// redisHealthCheck uses redis-cli ping.
func redisHealthCheck() *HealthCheckConfig {
	return &HealthCheckConfig{
		Test:        []string{"CMD-SHELL", "redis-cli ping | grep -q PONG || exit 1"},
		Interval:    30 * time.Second,
		Timeout:     10 * time.Second,
		StartPeriod: 30 * time.Second,
		Retries:     3,
	}
}

// ---------------------------------------------------------------------------
// Health check monitoring
// ---------------------------------------------------------------------------

// HealthStatus represents the current health state of a container.
type HealthStatus string

const (
	HealthUnknown   HealthStatus = "unknown"
	HealthStarting  HealthStatus = "starting"
	HealthHealthy   HealthStatus = "healthy"
	HealthUnhealthy HealthStatus = "unhealthy"
)

// ContainerHealth holds the health state for a single container.
type ContainerHealth struct {
	ContainerID string       `json:"container_id"`
	Status      HealthStatus `json:"status"`
	Output      string       `json:"output,omitempty"`
	FailingStreak int        `json:"failing_streak"`
	LastChecked  time.Time   `json:"last_checked"`
}

// GetContainerHealth inspects a container and returns its current health status.
func (c *Client) GetContainerHealth(ctx context.Context, containerID string) (*ContainerHealth, error) {
	inspect, err := c.InspectContainer(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("inspect container %s: %w", containerID[:12], err)
	}

	ch := &ContainerHealth{
		ContainerID: containerID,
		LastChecked: time.Now().UTC(),
	}

	if inspect.State == nil {
		ch.Status = HealthUnknown
		return ch, nil
	}

	if !inspect.State.Running {
		ch.Status = HealthUnknown
		ch.Output = fmt.Sprintf("container exited with code %d", inspect.State.ExitCode)
		return ch, nil
	}

	if inspect.State.Health == nil {
		// No health check configured — check if process is running
		if inspect.State.Running {
			ch.Status = HealthHealthy
		}
		return ch, nil
	}

	ch.FailingStreak = inspect.State.Health.FailingStreak

	switch inspect.State.Health.Status {
	case "healthy":
		ch.Status = HealthHealthy
	case "unhealthy":
		ch.Status = HealthUnhealthy
	case "starting":
		ch.Status = HealthStarting
	default:
		ch.Status = HealthUnknown
	}

	// Grab the last log output if available
	if len(inspect.State.Health.Log) > 0 {
		last := inspect.State.Health.Log[len(inspect.State.Health.Log)-1]
		ch.Output = last.Output
	}

	return ch, nil
}

// MonitorHealthCallback is called periodically with health updates.
// Return true to continue monitoring, false to stop.
type MonitorHealthCallback func(ctx context.Context, health *ContainerHealth) bool

// RunHealthMonitor starts a background goroutine that periodically checks
// a container's health and reports via the callback. Stops when ctx is cancelled.
func (c *Client) RunHealthMonitor(ctx context.Context, containerID string, interval time.Duration, callback MonitorHealthCallback) {
	if interval == 0 {
		interval = 30 * time.Second
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// Give the container a grace period to start
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				health, err := c.GetContainerHealth(ctx, containerID)
				if err != nil {
					slog.Warn("health check failed", "container", containerID[:12], "error", err)
					continue
				}

				if !callback(ctx, health) {
					return
				}
			}
		}
	}()
}
