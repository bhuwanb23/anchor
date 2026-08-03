package state

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/yourname/yourplatform/agent/internal/caddy"
)

const (
	// Label keys used to identify managed containers.
	labelOwner   = "yourplatform.owner"
	labelProject = "yourplatform.project"
	labelRole    = "yourplatform.role"

	// ManagedBy is the value of the owner label.
	ManagedBy = "yourplatform-agent"

	// MaxRestartAttempts is the max number of restart attempts for crashed containers.
	MaxRestartAttempts = 3
)

// DockerClient is the interface needed from Docker for reconciliation.
type DockerClient interface {
	ListManagedContainers(ctx context.Context) ([]types.Container, error)
	InspectContainer(ctx context.Context, id string) (types.ContainerJSON, error)
	StartContainer(ctx context.Context, id string) error
	StopContainerGraceful(ctx context.Context, id string) error
}

// ReconcileResult summarizes what happened during reconciliation.
type ReconcileResult struct {
	Running   int      `json:"running"`
	Restarted int      `json:"restarted"`
	Failed    int      `json:"failed"`
	Adopted   int      `json:"adopted"`
	Ignored   int      `json:"ignored"`
	Messages  []string `json:"messages"`
}

// AddMessage appends a message to the result.
func (r *ReconcileResult) AddMessage(format string, args ...interface{}) {
	r.Messages = append(r.Messages, fmt.Sprintf(format, args...))
}

// discoveredContainer pairs a Docker container with its project/role labels.
type discoveredContainer struct {
	Container types.Container
	Project   string
	Role      string
}

// Reconcile compares the agent's state file against Docker's actual state
// and takes corrective action.
func Reconcile(ctx context.Context, mgr *Manager, dockerClient DockerClient) (*ReconcileResult, error) {
	result := &ReconcileResult{}
	state := mgr.GetState()

	// Step 1: Discover all containers with our labels
	discovered, err := dockerClient.ListManagedContainers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list managed containers: %w", err)
	}

	// Build map of discovered containers by project/role
	discoveredMap := make(map[string]*discoveredContainer)
	for _, cont := range discovered {
		project := cont.Labels[labelProject]
		role := cont.Labels[labelRole]
		if project == "" || role == "" {
			continue
		}
		key := project + "/" + role
		discoveredMap[key] = &discoveredContainer{
			Container: cont,
			Project:   project,
			Role:      role,
		}
	}

	// Step 2: Adopt orphans (in Docker but not in state)
	for key, dc := range discoveredMap {
		projectState, inState := state.Projects[dc.Project]
		if inState {
			if _, containerInState := projectState.Containers[dc.Role]; containerInState {
				continue // Already tracked
			}
		}

		slog.Info("adopting orphan container",
			"project", dc.Project,
			"role", dc.Role,
			"id", dc.Container.ID[:12])

		mgr.SetContainer(dc.Project, dc.Role, &ContainerState{
			ContainerID: dc.Container.ID,
			Image:       dc.Container.Image,
			Status:      dc.Container.State,
			CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		})
		result.Adopted++
		result.AddMessage("adopted %s/%s (id: %s)", dc.Project, dc.Role, dc.Container.ID[:12])
		delete(discoveredMap, key) // Mark as processed
	}

	// Step 3: Check state entries against Docker
	for project, projectState := range state.Projects {
		for role, containerState := range projectState.Containers {
			dc, found := discoveredMap[project+"/"+role]

			if !found {
				// Container in state but not found in Docker
				slog.Warn("container missing from Docker",
					"project", project,
					"role", role,
					"id", containerState.ContainerID[:12])

				if containerState.RestartPolicy == "always" || containerState.RestartPolicy == "unless-stopped" {
					if attemptRestart(ctx, dockerClient, mgr, project, role, containerState) {
						result.Restarted++
						result.AddMessage("restarted %s/%s (id: %s)", project, role, containerState.ContainerID[:12])
					} else {
						result.Failed++
						mgr.UpdateStatus(project, role, "failed")
						result.AddMessage("failed to restart %s/%s (id: %s)", project, role, containerState.ContainerID[:12])
					}
				} else {
					mgr.UpdateStatus(project, role, "exited")
					result.AddMessage("container %s/%s exited (no restart policy)", project, role)
				}
				continue
			}

			// Container found in Docker — check status
			if dc.Container.State == "running" {
				result.Running++
				if containerState.Status != "running" {
					mgr.UpdateStatus(project, role, "running")
				}
			} else {
				slog.Warn("container not running",
					"project", project,
					"role", role,
					"state", dc.Container.State)

				if dc.Container.State == "exited" || dc.Container.State == "dead" {
					if containerState.RestartPolicy == "always" || containerState.RestartPolicy == "unless-stopped" {
						if attemptRestart(ctx, dockerClient, mgr, project, role, containerState) {
							result.Restarted++
							result.AddMessage("restarted %s/%s (was %s)", project, role, dc.Container.State)
						} else {
							result.Failed++
							mgr.UpdateStatus(project, role, "failed")
							result.AddMessage("failed to restart %s/%s (was %s)", project, role, dc.Container.State)
						}
					} else {
						mgr.UpdateStatus(project, role, dc.Container.State)
						result.AddMessage("container %s/%s is %s (no restart policy)", project, role, dc.Container.State)
					}
				}
			}
		}
	}

	return result, nil
}

// attemptRestart tries to start a container, up to MaxRestartAttempts times.
func attemptRestart(ctx context.Context, dockerClient DockerClient, mgr *Manager, project, role string, cs *ContainerState) bool {
	slog.Info("attempting container restart",
		"project", project,
		"role", role,
		"id", cs.ContainerID[:12])

	for attempt := 1; attempt <= MaxRestartAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return false
		default:
		}

		if err := dockerClient.StartContainer(ctx, cs.ContainerID); err != nil {
			slog.Warn("restart attempt failed",
				"project", project,
				"role", role,
				"attempt", attempt,
				"error", err)
			time.Sleep(time.Duration(attempt) * time.Second)
			continue
		}

		// Wait and verify
		time.Sleep(2 * time.Second)

		inspect, err := dockerClient.InspectContainer(ctx, cs.ContainerID)
		if err != nil {
			slog.Warn("failed to inspect after restart",
				"project", project,
				"role", role,
				"error", err)
			continue
		}

		if inspect.State.Running {
			slog.Info("container restarted successfully",
				"project", project,
				"role", role,
				"attempt", attempt)
			mgr.UpdateStatus(project, role, "running")
			return true
		}

		slog.Warn("container not running after restart",
			"project", project,
			"role", role,
			"attempt", attempt,
			"exit_code", inspect.State.ExitCode)
	}

	return false
}

// CaddyClient is the interface needed from Caddy for route reconciliation.
type CaddyClient interface {
	SetRouteByID(routeID string, domains []string, upstream string) error
	DeleteRouteByID(routeID string) error
	GetRoutes() ([]caddy.CaddyRoute, error)
}

// ReconcileCaddy synchronizes routes between state.json and Caddy.
// It re-registers all state routes with Caddy and removes orphaned routes.
func ReconcileCaddy(ctx context.Context, stateMgr *Manager, caddyClient CaddyClient) (restored int, orphaned int, err error) {
	slog.Info("reconciling caddy routes")

	stateRoutes := stateMgr.GetRoutes()

	// Step 1: Re-register all state routes with Caddy
	for routeID, rs := range stateRoutes {
		select {
		case <-ctx.Done():
			return restored, orphaned, ctx.Err()
		default:
		}

		if err := caddyClient.SetRouteByID(routeID, rs.Domains, rs.Upstream); err != nil {
			slog.Warn("failed to restore route", "route_id", routeID, "error", err)
			continue
		}
		restored++
		slog.Info("restored route", "route_id", routeID, "domains", rs.Domains, "upstream", rs.Upstream)
	}

	// Step 2: Get current routes from Caddy, remove orphans not in state
	caddyRoutes, err := caddyClient.GetRoutes()
	if err != nil {
		return restored, orphaned, fmt.Errorf("get caddy routes: %w", err)
	}

	routeIDs := make(map[string]bool, len(stateRoutes))
	for id := range stateRoutes {
		routeIDs[id] = true
	}

	for _, cr := range caddyRoutes {
		select {
		case <-ctx.Done():
			return restored, orphaned, ctx.Err()
		default:
		}

		// Only clean up routes with our prefix ("yourplatform-")
		if cr.ID == "" || len(cr.ID) < 14 || cr.ID[:13] != "yourplatform-" {
			continue
		}

		if !routeIDs[cr.ID] {
			slog.Info("removing orphaned caddy route", "route_id", cr.ID)
			if err := caddyClient.DeleteRouteByID(cr.ID); err != nil {
				slog.Warn("failed to remove orphaned route", "route_id", cr.ID, "error", err)
				continue
			}
			orphaned++
		}
	}

	slog.Info("caddy reconciliation complete", "restored", restored, "orphaned_removed", orphaned)
	return restored, orphaned, nil
}

// CheckPortMismatches verifies that each route's upstream port matches
// the container's current host port. If a mismatch is found, the route
// is updated with the correct port. Returns the number of fixed routes.
func CheckPortMismatches(ctx context.Context, stateMgr *Manager, caddyClient CaddyClient) (fixed int, err error) {
	state := stateMgr.GetState()
	if state == nil {
		return 0, nil
	}

	for routeID, rs := range state.Routes {
		select {
		case <-ctx.Done():
			return fixed, ctx.Err()
		default:
		}

		if rs.Project == "" {
			continue
		}

		// Find the container for this project
		ps, ok := state.Projects[rs.Project]
		if !ok || ps.Containers == nil {
			continue
		}

		// Get the first container in the project (main app container)
		for _, cs := range ps.Containers {
			if cs.HostPort == 0 || cs.ContainerID == "" {
				continue
			}

			// Parse the current upstream port
			currentUpstream := rs.Upstream
			_, currentPort, err := parseUpstream(currentUpstream)
			if err != nil {
				continue
			}

			// Check if the port matches
			if currentPort != cs.HostPort {
				newUpstream := fmt.Sprintf("127.0.0.1:%d", cs.HostPort)
				slog.Warn("port mismatch detected, updating route",
					"route_id", routeID,
					"old_upstream", currentUpstream,
					"new_upstream", newUpstream,
					"project", rs.Project)

				if err := caddyClient.SetRouteByID(routeID, rs.Domains, newUpstream); err != nil {
					slog.Warn("failed to fix port mismatch",
						"route_id", routeID,
						"error", err)
					continue
				}

				// Update state
				stateMgr.SetRoute(routeID, rs.Project, rs.Domains, newUpstream)
				fixed++
			}

			break // only check the first container
		}
	}

	if fixed > 0 {
		slog.Info("port mismatch reconciliation complete", "fixed", fixed)
	}
	return fixed, nil
}

func parseUpstream(upstream string) (host string, port int, err error) {
	parts := strings.SplitN(upstream, ":", 2)
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("invalid upstream format: %s", upstream)
	}
	port, err = strconv.Atoi(parts[1])
	if err != nil {
		return "", 0, fmt.Errorf("invalid port in upstream: %s", upstream)
	}
	return parts[0], port, nil
}
