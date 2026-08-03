package metrics

import (
	"context"
	"log/slog"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/yourname/yourplatform/agent/internal/docker"
)

// ContainerLister lists managed containers and inspects/stats them.
// Implemented by *docker.Client (Layer 3A owns all Docker access).
type ContainerLister interface {
	ListManagedContainers(ctx context.Context) ([]types.Container, error)
	InspectContainer(ctx context.Context, id string) (types.ContainerJSON, error)
	GetContainerStats(ctx context.Context, id string) (docker.ContainerStats, error)
}

// DockerCollector samples per-container metrics via Layer 3A.
type DockerCollector struct {
	client ContainerLister
}

// NewDockerCollector creates a DockerCollector.
func NewDockerCollector(client ContainerLister) *DockerCollector {
	return &DockerCollector{client: client}
}

// Collect returns container metrics for all managed containers.
// A failure to inspect or stat one container does not abort the others.
func (c *DockerCollector) Collect(ctx context.Context) []ContainerMetrics {
	containers, err := c.client.ListManagedContainers(ctx)
	if err != nil {
		slog.Warn("metrics: list managed containers failed", "error", err)
		return nil
	}

	out := make([]ContainerMetrics, 0, len(containers))
	for _, cont := range containers {
		if cm := c.collectOne(ctx, cont); cm != nil {
			out = append(out, *cm)
		}
	}
	return out
}

func (c *DockerCollector) collectOne(ctx context.Context, cont types.Container) *ContainerMetrics {
	project := cont.Labels["yourplatform.project"]
	role := cont.Labels["yourplatform.role"]
	if role == "" {
		role = "app"
	}

	cm := &ContainerMetrics{
		Project:     project,
		Role:        role,
		ContainerID: cont.ID,
		Status:      cont.State,
	}

	// Inspect for richer state: health, exit code, uptime, restart count.
	if inspect, err := c.client.InspectContainer(ctx, cont.ID); err == nil {
		if st := inspect.State; st != nil {
			if st.StartedAt != "" {
				if t, terr := time.Parse(time.RFC3339, st.StartedAt); terr == nil && st.Running {
					cm.UptimeSecs = int64(time.Since(t).Seconds())
				}
			}
			if !st.Running {
				code := st.ExitCode
				cm.ExitCode = &code
			}
			if st.Health != nil {
				cm.Health = string(st.Health.Status)
			} else if st.Running {
				cm.Health = "healthy" // no health check configured; running is healthy
			}
		}
		cm.RestartCount = inspect.RestartCount
	}

	// One-shot stats for CPU / RAM / network.
	if st, err := c.client.GetContainerStats(ctx, cont.ID); err == nil {
		cm.CPUPercent = st.CPUPercent
		cm.RAMUsedMB = int64(st.RAMUsedBytes / 1024 / 1024)
		cm.RAMLimitMB = int64(st.RAMLimitBytes / 1024 / 1024)
		if st.RAMLimitBytes > 0 {
			cm.RAMPercent = float64(st.RAMUsedBytes) / float64(st.RAMLimitBytes) * 100.0
		}
		cm.NetRxBytes = st.NetRxBytes
		cm.NetTxBytes = st.NetTxBytes
	} else {
		slog.Warn("metrics: container stats failed",
			"container", shortID(cont.ID), "error", err)
	}

	return cm
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
