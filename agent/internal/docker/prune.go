package docker

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/docker/docker/api/types/filters"
)

// PruneReport summarizes one auto-remediation cleanup pass
// (Layer 4C Step 7, Case 1).
type PruneReport struct {
	ImagesRemoved       int    `json:"images_removed"`
	ContainersRemoved   int    `json:"containers_removed"`
	SpaceReclaimedBytes uint64 `json:"space_reclaimed_bytes"`
}

// PruneUnusedResources runs `docker image prune -f` and `docker container
// prune -f` and reports what was freed.
//
// Safe by construction: Docker only removes unused (dangling) images and
// stopped containers. Running containers, images referenced by a container,
// volumes, networks, and user data are never touched — the "never delete user
// data" invariant of Layer 4C Step 7.
func (c *Client) PruneUnusedResources(ctx context.Context) (*PruneReport, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, fmt.Errorf("docker unavailable: %w", err)
	}

	imgReport, err := c.cliUnsafe().ImagesPrune(ctx, filters.NewArgs())
	if err != nil {
		return nil, fmt.Errorf("prune images: %w", err)
	}
	containerReport, err := c.cliUnsafe().ContainersPrune(ctx, filters.NewArgs())
	if err != nil {
		return nil, fmt.Errorf("prune containers: %w", err)
	}

	report := &PruneReport{
		ImagesRemoved:       len(imgReport.ImagesDeleted),
		ContainersRemoved:   len(containerReport.ContainersDeleted),
		SpaceReclaimedBytes: imgReport.SpaceReclaimed + containerReport.SpaceReclaimed,
	}

	slog.Info("pruned unused docker resources",
		"images", report.ImagesRemoved,
		"containers", report.ContainersRemoved,
		"reclaimed_bytes", report.SpaceReclaimedBytes)

	return report, nil
}
