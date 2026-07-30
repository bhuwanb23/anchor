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
	networkPrefix   = "yourplatform_"
	labelOwner      = "yourplatform.owner"
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
	projectSafe := SanitizeProjectName(projectName)
	resp, err := c.cliUnsafe().NetworkCreate(ctx, networkName, types.NetworkCreate{
		Driver:     "bridge",
		Internal:   false, // containers can reach internet for npm install etc.
		Attachable: true,
		Labels: map[string]string{
			labelOwner:            labelOwnerValue,
			"yourplatform.project": projectSafe,
		},
	})
	if err != nil {
		// If network was just created by a concurrent caller, look it up
		if existing, lookupErr := c.findNetworkByName(ctx, networkName); lookupErr == nil && existing != nil {
			slog.Debug("network was already created by concurrent operation",
				"network", networkName, "id", existing.ID[:12])
			return existing.ID, nil
		}
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

// ConnectContainerWithAliases connects a container to a project network
// with DNS aliases. This is used for database containers so that
// other containers on the same network can reach them by alias.
//
// Standard aliases:
//   postgres → "postgres", "db"
//   mysql    → "mysql", "db"
//   redis    → "redis", "cache"
func (c *Client) ConnectContainerWithAliases(ctx context.Context, containerID, projectName string, aliases []string) error {
	if err := c.ensureConnected(ctx); err != nil {
		return fmt.Errorf("docker unavailable: %w", err)
	}

	networkID, err := c.EnsureProjectNetwork(ctx, projectName)
	if err != nil {
		return err
	}

	if err := c.cliUnsafe().NetworkConnect(ctx, networkID, containerID, &network.EndpointSettings{
		Aliases: aliases,
	}); err != nil {
		return fmt.Errorf("connect container %s to network %s with aliases: %w",
			containerID[:12], networkID[:12], err)
	}

	slog.Debug("connected container to project network with aliases",
		"container", containerID[:12],
		"project", SanitizeProjectName(projectName),
		"aliases", aliases,
	)

	return nil
}

// ContainerType identifies what kind of container we're creating.
type ContainerType string

const (
	ContainerTypeApp      ContainerType = "app"
	ContainerTypePostgres ContainerType = "postgres"
	ContainerTypeMySQL    ContainerType = "mysql"
	ContainerTypeRedis    ContainerType = "redis"
)

// DatabaseAliases returns the standard DNS aliases for a database container type.
func DatabaseAliases(dbType ContainerType) []string {
	switch dbType {
	case ContainerTypePostgres:
		return []string{"postgres", "db"}
	case ContainerTypeMySQL:
		return []string{"mysql", "db"}
	case ContainerTypeRedis:
		return []string{"redis", "cache"}
	default:
		return nil
	}
}

// ---------------------------------------------------------------------------
// Project lifecycle
// ---------------------------------------------------------------------------

// RemoveProject stops all containers in the project, removes the network,
// and optionally removes associated volumes. This is the complete teardown
// for when a project is deleted from the dashboard.
//
// Order matters:
//   1. List all containers on the project network
//   2. Stop and remove each container
//   3. Remove the project network
//   4. Optionally remove volumes
func (c *Client) RemoveProject(ctx context.Context, projectName string, removeVolumes bool) error {
	projectSafe := SanitizeProjectName(projectName)

	// Find the project network
	networkName := ProjectNetworkName(projectName)
	network, err := c.findNetworkByName(ctx, networkName)
	if err != nil {
		return fmt.Errorf("find network for project %s: %w", projectSafe, err)
	}
	if network == nil {
		slog.Info("no project network found, nothing to remove", "project", projectSafe)
		return nil
	}

	// Step 1: Stop and remove all containers on this network
	// network.Containers is map[string]types.EndpointResource where key = container ID
	for cid := range network.Containers {
		slog.Info("stopping and removing container in project",
			"project", projectSafe,
			"container", cid[:12],
		)

		if err := c.StopContainer(ctx, cid); err != nil {
			slog.Warn("failed to stop container, force removing",
				"container", cid[:12], "error", err)
		}

		if err := c.RemoveContainer(ctx, cid); err != nil {
			slog.Warn("failed to remove container",
				"container", cid[:12], "error", err)
		}
	}

	// Step 2: Remove the project network
	if err := c.cliUnsafe().NetworkRemove(ctx, network.ID); err != nil {
		return fmt.Errorf("remove network %s: %w", networkName, err)
	}

	slog.Info("removed network for project",
		"project", projectSafe,
		"network", networkName,
	)

	// Step 3: Optionally remove volumes
	if removeVolumes {
		c.removeProjectVolumes(ctx, projectSafe)
	}

	return nil
}

// removeProjectVolumes removes all Docker volumes associated with a project.
func (c *Client) removeProjectVolumes(ctx context.Context, projectSafe string) {
	volumes, err := c.cliUnsafe().VolumeList(ctx, filterProjectVolumes(projectSafe))
	if err != nil {
		slog.Warn("failed to list project volumes", "project", projectSafe, "error", err)
		return
	}

	for _, v := range volumes.Volumes {
		slog.Info("removing project volume", "project", projectSafe, "volume", v.Name)
		if err := c.cliUnsafe().VolumeRemove(ctx, v.Name, true); err != nil {
			slog.Warn("failed to remove volume", "volume", v.Name, "error", err)
		}
	}
}

// filterProjectVolumes builds filter args for volumes with a specific project label.
func filterProjectVolumes(projectSafe string) filters.Args {
	return labelFilter("yourplatform.project", projectSafe)
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
