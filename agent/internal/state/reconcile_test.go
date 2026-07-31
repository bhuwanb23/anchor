package state

import (
	"context"
	"fmt"
	"testing"

	"github.com/docker/docker/api/types"
)

// mockDockerClient is a test double for Docker reconciliation.
type mockDockerClient struct {
	containers   []types.Container
	inspectFunc  func(ctx context.Context, id string) (types.ContainerJSON, error)
	startFunc    func(ctx context.Context, id string) error
	startCalls   []string // records which container IDs were started
}

func (m *mockDockerClient) ListManagedContainers(ctx context.Context) ([]types.Container, error) {
	return m.containers, nil
}

func (m *mockDockerClient) InspectContainer(ctx context.Context, id string) (types.ContainerJSON, error) {
	if m.inspectFunc != nil {
		return m.inspectFunc(ctx, id)
	}
	return types.ContainerJSON{
		ContainerJSONBase: &types.ContainerJSONBase{
			State: &types.ContainerState{Running: true},
		},
	}, nil
}

func (m *mockDockerClient) StartContainer(ctx context.Context, id string) error {
	m.startCalls = append(m.startCalls, id)
	if m.startFunc != nil {
		return m.startFunc(ctx, id)
	}
	return nil
}

func (m *mockDockerClient) StopContainerGraceful(ctx context.Context, id string) error {
	return nil
}

func makeContainer(id, project, role, state string) types.Container {
	return types.Container{
		ID:    id,
		State: state,
		Labels: map[string]string{
			labelOwner:   ManagedBy,
			labelProject: project,
			labelRole:    role,
		},
	}
}

func TestReconcile_EmptyState_NoContainers(t *testing.T) {
	mgr := NewManager(t.TempDir())
	docker := &mockDockerClient{containers: nil}

	result, err := Reconcile(context.Background(), mgr, docker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Running != 0 || result.Adopted != 0 || result.Failed != 0 {
		t.Errorf("expected all zeros, got running=%d adopted=%d failed=%d",
			result.Running, result.Adopted, result.Failed)
	}
}

func TestReconcile_AdoptsOrphanContainers(t *testing.T) {
	mgr := NewManager(t.TempDir())
	docker := &mockDockerClient{
		containers: []types.Container{
			makeContainer("aabbccdd0011", "myshop", "app", "running"),
			makeContainer("aabbccdd0022", "myshop", "postgres", "running"),
		},
	}

	result, err := Reconcile(context.Background(), mgr, docker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Adopted != 2 {
		t.Errorf("expected 2 adopted, got %d", result.Adopted)
	}

	state := mgr.GetState()
	if _, ok := state.Projects["myshop"]; !ok {
		t.Fatal("expected myshop project")
	}
	if len(state.Projects["myshop"].Containers) != 2 {
		t.Errorf("expected 2 containers, got %d", len(state.Projects["myshop"].Containers))
	}
}

func TestReconcile_IgnoresContainersWithoutLabels(t *testing.T) {
	mgr := NewManager(t.TempDir())
	docker := &mockDockerClient{
		containers: []types.Container{
			{ID: "nodeadbeef0001", State: "running", Labels: map[string]string{}},
		},
	}

	result, err := Reconcile(context.Background(), mgr, docker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Adopted != 0 {
		t.Errorf("expected 0 adopted, got %d", result.Adopted)
	}
}

func TestReconcile_RunningContainersMatchState(t *testing.T) {
	mgr := NewManager(t.TempDir())
	mgr.SetContainer("myshop", "app", &ContainerState{
		ContainerID: "aabbccdd0011",
		Image:       "nginx:latest",
		Status:      "stopped",
	})

	docker := &mockDockerClient{
		containers: []types.Container{
			makeContainer("aabbccdd0011", "myshop", "app", "running"),
		},
	}

	result, err := Reconcile(context.Background(), mgr, docker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Running != 1 {
		t.Errorf("expected 1 running, got %d", result.Running)
	}

	// Status should be updated to running
	c := mgr.GetState().Projects["myshop"].Containers["app"]
	if c.Status != "running" {
		t.Errorf("expected status 'running', got %q", c.Status)
	}
}

func TestReconcile_RestartPolicyAlways_RestartsMissing(t *testing.T) {
	mgr := NewManager(t.TempDir())
	mgr.SetContainer("myshop", "app", &ContainerState{
		ContainerID:   "aabbccdd0011",
		Image:         "nginx:latest",
		Status:        "running",
		RestartPolicy: "always",
	})

	// Container not in Docker — should attempt restart
	docker := &mockDockerClient{
		containers: nil, // empty — container is gone
		startFunc: func(ctx context.Context, id string) error {
			return nil // restart succeeds
		},
		inspectFunc: func(ctx context.Context, id string) (types.ContainerJSON, error) {
			return types.ContainerJSON{
				ContainerJSONBase: &types.ContainerJSONBase{
					State: &types.ContainerState{Running: true},
				},
			}, nil
		},
	}

	result, err := Reconcile(context.Background(), mgr, docker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Restarted != 1 {
		t.Errorf("expected 1 restarted, got %d", result.Restarted)
	}
	if len(docker.startCalls) != 1 {
		t.Errorf("expected 1 start call, got %d", len(docker.startCalls))
	}
}

func TestReconcile_RestartFails_MarksFailed(t *testing.T) {
	mgr := NewManager(t.TempDir())
	mgr.SetContainer("myshop", "app", &ContainerState{
		ContainerID:   "aabbccdd0011",
		Image:         "nginx:latest",
		Status:        "running",
		RestartPolicy: "always",
	})

	docker := &mockDockerClient{
		containers: nil,
		startFunc: func(ctx context.Context, id string) error {
			return fmt.Errorf("container not found")
		},
	}

	result, err := Reconcile(context.Background(), mgr, docker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Failed != 1 {
		t.Errorf("expected 1 failed, got %d", result.Failed)
	}

	c := mgr.GetState().Projects["myshop"].Containers["app"]
	if c.Status != "failed" {
		t.Errorf("expected status 'failed', got %q", c.Status)
	}
}

func TestReconcile_NoRestartPolicy_MarksExited(t *testing.T) {
	mgr := NewManager(t.TempDir())
	mgr.SetContainer("myshop", "app", &ContainerState{
		ContainerID:   "aabbccdd0011",
		Image:         "nginx:latest",
		Status:        "running",
		RestartPolicy: "no",
	})

	docker := &mockDockerClient{containers: nil}

	_, err := Reconcile(context.Background(), mgr, docker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c := mgr.GetState().Projects["myshop"].Containers["app"]
	if c.Status != "exited" {
		t.Errorf("expected status 'exited', got %q", c.Status)
	}
}

func TestReconcile_ExitedContainerInDocker_RestartsIfPolicyAllows(t *testing.T) {
	mgr := NewManager(t.TempDir())
	mgr.SetContainer("myshop", "app", &ContainerState{
		ContainerID:   "aabbccdd0011",
		Image:         "nginx:latest",
		Status:        "running",
		RestartPolicy: "always",
	})

	docker := &mockDockerClient{
		containers: []types.Container{
			makeContainer("aabbccdd0011", "myshop", "app", "exited"),
		},
		startFunc: func(ctx context.Context, id string) error {
			return nil
		},
		inspectFunc: func(ctx context.Context, id string) (types.ContainerJSON, error) {
			return types.ContainerJSON{
				ContainerJSONBase: &types.ContainerJSONBase{
					State: &types.ContainerState{Running: true},
				},
			}, nil
		},
	}

	result, err := Reconcile(context.Background(), mgr, docker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Restarted != 1 {
		t.Errorf("expected 1 restarted, got %d", result.Restarted)
	}
}

func TestReconcile_AlreadyAdopted_NotDuplicated(t *testing.T) {
	mgr := NewManager(t.TempDir())
	mgr.SetContainer("myshop", "app", &ContainerState{
		ContainerID: "aabbccdd0011",
		Image:       "nginx:latest",
		Status:      "running",
	})

	docker := &mockDockerClient{
		containers: []types.Container{
			makeContainer("aabbccdd0011", "myshop", "app", "running"),
		},
	}

	result, err := Reconcile(context.Background(), mgr, docker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Adopted != 0 {
		t.Errorf("expected 0 adopted (already tracked), got %d", result.Adopted)
	}
	if result.Running != 1 {
		t.Errorf("expected 1 running, got %d", result.Running)
	}
}
