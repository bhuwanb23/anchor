package metrics

import (
	"context"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/yourname/yourplatform/agent/internal/docker"
)

// emptyLister returns no containers and no stats — lets the collection loop
// run without a real Docker daemon.
type emptyLister struct{}

func (emptyLister) ListManagedContainers(context.Context) ([]types.Container, error) { return nil, nil }
func (emptyLister) InspectContainer(context.Context, string) (types.ContainerJSON, error) {
	return types.ContainerJSON{}, nil
}
func (emptyLister) GetContainerStats(context.Context, string) (docker.ContainerStats, error) {
	return docker.ContainerStats{}, nil
}

func TestManager_RunsCollectionLoop(t *testing.T) {
	sender := &captureSender{}
	reporter := NewReporter(sender, 8)

	sys := NewSystemCollector(nil, nil)
	dc := NewDockerCollector(emptyLister{})

	mgr := NewManager("srv-test", sys, dc, reporter).
		WithInterval(20 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		mgr.Run(ctx)
		close(done)
	}()

	// Wait for at least two reports.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(sender.got) >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if len(sender.got) < 2 {
		t.Fatalf("expected at least 2 health reports, got %d", len(sender.got))
	}

	first := sender.got[0]
	if first.Type != "health_report" {
		t.Errorf("report type = %s, want health_report", first.Type)
	}
	if first.ServerID != "srv-test" {
		t.Errorf("report server_id = %s, want srv-test", first.ServerID)
	}
	// Host metrics should be populated by gopsutil on real host.
	if first.Server.RAMTotalMB <= 0 {
		t.Errorf("server RAM total = %d, want > 0 (host has memory)", first.Server.RAMTotalMB)
	}

	cancel()
	<-done
}

func TestManager_NoContainersNoPanic(t *testing.T) {
	sender := &captureSender{}
	reporter := NewReporter(sender, 4)

	mgr := NewManager("srv-test", NewSystemCollector(nil, nil), NewDockerCollector(emptyLister{}), reporter).
		WithInterval(10 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		mgr.Run(ctx)
		close(done)
	}()
	time.Sleep(30 * time.Millisecond)
	cancel()
	<-done
}
