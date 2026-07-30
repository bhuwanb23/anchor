package docker

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
)

const (
	networkPrefix  = "yourplatform_"
	labelOwner     = "yourplatform.owner"
	labelProject   = "yourplatform.project"
	labelOwnerValue = "yourplatform-agent"
)

// ---------------------------------------------------------------------------
// Project name sanitization
// ---------------------------------------------------------------------------

var nonAlphanumeric = regexp.MustCompile(`[^a-z0-9-]`)

// SanitizeProjectName converts a project name into a safe identifier.
//   "My Shop!"        → "my-shop"
//   "Next.js App 3"   → "nextjs-app-3"
//   "my_project"      → "my-project"
//   "  Hello World  " → "hello-world"
func SanitizeProjectName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.ReplaceAll(s, "_", "-")
	s = nonAlphanumeric.ReplaceAllString(s, "")
	s = regexp.MustCompile(`-+`).ReplaceAllString(s, "-")  // collapse multiple hyphens
	s = strings.Trim(s, "-")
	if s == "" {
		s = "project"
	}
	return s
}

// ProjectNetworkName returns the Docker network name for a project.
func ProjectNetworkName(projectName string) string {
	return networkPrefix + SanitizeProjectName(projectName)
}

// ---------------------------------------------------------------------------
// Network creation
// ---------------------------------------------------------------------------

// EnsureProjectNetwork creates a project network if it doesn't exist,
// or returns the existing network ID. This is idempotent.
func (c *Client) EnsureProjectNetwork(ctx context.Context, projectName string) (string, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return "", fmt.Errorf("docker unavailable: %w", err)
	}

	networkName := ProjectNetworkName(projectName)

	// Check if network already exists
	network, err := c.findNetworkByName(ctx, networkName)
	if err != nil {
		return "", err
	}
	if network != nil {
		slog.Debug("reusing existing project network",
			"project", SanitizeProjectName(projectName),
			"network", networkName,
			"id", network.ID[:12],
		)
		return network.ID, nil
	}

	// Create the network
	resp, err := c.cliUnsafe().NetworkCreate(ctx, networkName, types.NetworkCreate{
		Driver:     "bridge",
		Internal:   false, // containers can reach internet for npm install etc.
		Attachable: true,
		Labels: map[string]string{
			labelOwner:   labelOwnerValue,
			labelProject: SanitizeProjectName(projectName),
		},
	})
	if err != nil {
		return "", fmt.Errorf("create network %s: %w", networkName, err)
	}

	slog.Info("created project network",
		"project", SanitizeProjectName(projectName),
		"network", networkName,
		"id", resp.ID[:12],
	)

	return resp.ID, nil
}

// RemoveProjectNetwork removes a project's Docker network.
// Returns nil even if the network doesn't exist (idempotent).
func (c *Client) RemoveProjectNetwork(ctx context.Context, projectName string) error {
	if err := c.ensureConnected(ctx); err != nil {
		return fmt.Errorf("docker unavailable: %w", err)
	}

	networkName := ProjectNetworkName(projectName)

	network, err := c.findNetworkByName(ctx, networkName)
	if err != nil {
		return err
	}
	if network == nil {
		slog.Debug("project network does not exist, nothing to remove",
			"project", SanitizeProjectName(projectName),
		)
		return nil
	}

	if err := c.cliUnsafe().NetworkRemove(ctx, network.ID); err != nil {
		return fmt.Errorf("remove network %s: %w", networkName, err)
	}

	slog.Info("removed project network",
		"project", SanitizeProjectName(projectName),
		"network", networkName,
	)

	return nil
}

// ---------------------------------------------------------------------------
// Container network connection
// ---------------------------------------------------------------------------

// ConnectContainerToNetwork connects a container to a project's network.
// This allows the container to reach other containers on the same network
// using container names as hostnames.
func (c *Client) ConnectContainerToNetwork(ctx context.Context, containerID, projectName string) error {
	if err := c.ensureConnected(ctx); err != nil {
		return fmt.Errorf("docker unavailable: %w", err)
	}

	networkID, err := c.EnsureProjectNetwork(ctx, projectName)
	if err != nil {
		return err
	}

	if err := c.cliUnsafe().NetworkConnect(ctx, networkID, containerID); err != nil {
		return fmt.Errorf("connect container %s to network %s: %w",
			containerID[:12], networkID[:12], err)
	}

	slog.Debug("connected container to project network",
		"container", containerID[:12],
		"project", SanitizeProjectName(projectName),
	)

	return nil
}

// DisconnectContainerFromNetwork disconnects a container from a project's network.
func (c *Client) DisconnectContainerFromNetwork(ctx context.Context, containerID, projectName string) error {
	if err := c.ensureConnected(ctx); err != nil {
		return fmt.Errorf("docker unavailable: %w", err)
	}

	networkName := ProjectNetworkName(projectName)

	network, err := c.findNetworkByName(ctx, networkName)
	if err != nil {
		return err
	}
	if network == nil {
		slog.Debug("network not found, nothing to disconnect",
			"project", SanitizeProjectName(projectName),
		)
		return nil
	}

	if err := c.cliUnsafe().NetworkDisconnect(ctx, network.ID, containerID, false); err != nil {
		return fmt.Errorf("disconnect container %s from network %s: %w",
			containerID[:12], networkName, err)
	}

	slog.Debug("disconnected container from project network",
		"container", containerID[:12],
		"project", SanitizeProjectName(projectName),
	)

	return nil
}

// ---------------------------------------------------------------------------
// Network listing and cleanup
// ---------------------------------------------------------------------------

// ListProjectNetworks returns all Docker networks owned by yourplatform.
func (c *Client) ListProjectNetworks(ctx context.Context) ([]types.NetworkResource, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, fmt.Errorf("docker unavailable: %w", err)
	}

	networks, err := c.cliUnsafe().NetworkList(ctx, types.NetworkListOptions{
		Filters: labelFilter(labelOwner, labelOwnerValue),
	})
	if err != nil {
		return nil, fmt.Errorf("list networks: %w", err)
	}

	return networks, nil
}

// CleanupOrphanedNetworks removes all yourplatform networks that have
// zero connected containers. Safe to call periodically.
func (c *Client) CleanupOrphanedNetworks(ctx context.Context) error {
	networks, err := c.ListProjectNetworks(ctx)
	if err != nil {
		return err
	}

	removed := 0
	for _, n := range networks {
		if len(n.Containers) == 0 {
			slog.Info("removing orphaned network", "network", n.Name, "id", n.ID[:12])
			if err := c.cliUnsafe().NetworkRemove(ctx, n.ID); err != nil {
				slog.Warn("failed to remove orphaned network", "network", n.Name, "error", err)
				continue
			}
			removed++
		}
	}

	if removed > 0 {
		slog.Info("cleaned up orphaned networks", "count", removed)
	} else {
		slog.Debug("no orphaned networks to remove")
	}

	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// findNetworkByName looks up a Docker network by name.
// Returns nil (no error) if the network doesn't exist.
func (c *Client) findNetworkByName(ctx context.Context, name string) (*types.NetworkResource, error) {
	networks, err := c.cliUnsafe().NetworkList(ctx, types.NetworkListOptions{
		Filters: filters.NewArgs(filters.Arg("name", name)),
	})
	if err != nil {
		return nil, fmt.Errorf("list networks by name %s: %w", name, err)
	}

	// Filter exact name match (Docker name filter is a contains check)
	for _, n := range networks {
		if n.Name == name {
			return &n, nil
		}
	}

	return nil, nil
}

// labelFilter builds Docker filter arguments for a label key=value pair.
func labelFilter(key, value string) filters.Args {
	return filters.NewArgs(filters.Arg("label", fmt.Sprintf("%s=%s", key, value)))
}
