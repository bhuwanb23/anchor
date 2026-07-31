package state

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/docker/docker/api/types"
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
