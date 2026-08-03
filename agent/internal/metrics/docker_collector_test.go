package metrics

import (
	"context"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/yourname/yourplatform/agent/internal/docker"
)

type fakeContainerLister struct {
	containers []types.Container
	inspected  map[string]types.ContainerJSON
	stats      map[string]docker.ContainerStats
}

func (f *fakeContainerLister) ListManagedContainers(ctx context.Context) ([]types.Container, error) {
	return f.containers, nil
}

func (f *fakeContainerLister) InspectContainer(ctx context.Context, id string) (types.ContainerJSON, error) {
	return f.inspected[id], nil
}

func (f *fakeContainerLister) GetContainerStats(ctx context.Context, id string) (docker.ContainerStats, error) {
	return f.stats[id], nil
}

func mkRunning(exitCode int, startedAt string, health string) *types.ContainerState {
	st := &types.ContainerState{Running: true, StartedAt: startedAt, ExitCode: exitCode}
	if health != "" {
		st.Health = &types.Health{Status: health}
	}
	return st
}

func TestDockerCollector_BasicLifecycle(t *testing.T) {
	f := &fakeContainerLister{
		containers: []types.Container{
			{ID: "aaaaaaaaaaaa", State: "running", Labels: map[string]string{
				"yourplatform.project": "myshop",
				"yourplatform.role":    "app",
			}},
			{ID: "bbbbbbbbbbbb", State: "exited", Labels: map[string]string{
				"yourplatform.project": "myblog",
				"yourplatform.role":    "postgres",
			}},
		},
		inspected: map[string]types.ContainerJSON{
			"aaaaaaaaaaaa": {ContainerJSONBase: &types.ContainerJSONBase{
				RestartCount: 2,
				State:        mkRunning(0, "2026-01-01T00:00:00Z", "healthy"),
			}},
			"bbbbbbbbbbbb": {ContainerJSONBase: &types.ContainerJSONBase{
				RestartCount: 5,
				State: &types.ContainerState{
					Running:  false,
					ExitCode: 137,
				},
			}},
		},
		stats: map[string]docker.ContainerStats{
			"aaaaaaaaaaaa": {
				CPUPercent:    12.5,
				RAMUsedBytes:  200 * 1024 * 1024,
				RAMLimitBytes: 512 * 1024 * 1024,
				NetRxBytes:    1000,
				NetTxBytes:    2000,
			},
			"bbbbbbbbbbbb": {},
		},
	}

	c := NewDockerCollector(f)
	got := c.Collect(context.Background())

	if len(got) != 2 {
		t.Fatalf("collected %d containers, want 2", len(got))
	}

	app := got[0]
	if app.Project != "myshop" || app.Role != "app" {
		t.Errorf("app project/role = %s/%s, want myshop/app", app.Project, app.Role)
	}
	if app.Status != "running" {
		t.Errorf("app status = %s, want running", app.Status)
	}
	if app.Health != "healthy" {
		t.Errorf("app health = %s, want healthy", app.Health)
	}
	if app.RestartCount != 2 {
		t.Errorf("app restart count = %d, want 2", app.RestartCount)
	}
	if app.CPUPercent != 12.5 {
		t.Errorf("app cpu = %f, want 12.5", app.CPUPercent)
	}
	if app.RAMUsedMB != 200 {
		t.Errorf("app ram used = %d, want 200", app.RAMUsedMB)
	}
	if app.RAMLimitMB != 512 {
		t.Errorf("app ram limit = %d, want 512", app.RAMLimitMB)
	}
	if app.RAMPercent < 39 || app.RAMPercent > 39.1 {
		t.Errorf("app ram percent = %f, want ~39.06", app.RAMPercent)
	}
	if app.NetRxBytes != 1000 || app.NetTxBytes != 2000 {
		t.Errorf("app net = %d/%d, want 1000/2000", app.NetRxBytes, app.NetTxBytes)
	}
	if app.ExitCode != nil {
		t.Errorf("running container should have nil exit code, got %v", *app.ExitCode)
	}

	db := got[1]
	if db.Status != "exited" {
		t.Errorf("db status = %s, want exited", db.Status)
	}
	if db.ExitCode == nil || *db.ExitCode != 137 {
		t.Errorf("db exit code = %v, want 137", db.ExitCode)
	}
	if db.RestartCount != 5 {
		t.Errorf("db restart count = %d, want 5", db.RestartCount)
	}
	if db.RAMUsedMB != 0 {
		t.Errorf("db ram used for empty stats = %d, want 0", db.RAMUsedMB)
	}
}
