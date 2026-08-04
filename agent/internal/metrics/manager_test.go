package metrics

import (
	"context"
	"log/slog"
	"strings"
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

func TestManager_DefaultIntervalIs30s(t *testing.T) {
	if DefaultInterval != 30*time.Second {
		t.Errorf("DefaultInterval = %s, want 30s", DefaultInterval)
	}
}

func TestManager_SlowCollectionHook(t *testing.T) {
	m := NewManager("srv-test", NewSystemCollector(nil, nil),
		NewDockerCollector(emptyLister{}), NewReporter(&captureSender{}, 4))

	var gotMS int64 = -1
	m.WithSlowCollectionHook(func(ms int64) { gotMS = ms })

	// Above the 5s budget → hook fires with the measured duration.
	m.warnIfSlowCollection(5001)
	if gotMS != 5001 {
		t.Errorf("slow hook fired with %d, want 5001", gotMS)
	}

	// Within budget → hook must not fire.
	gotMS = -1
	m.warnIfSlowCollection(3000)
	if gotMS != -1 {
		t.Errorf("hook fired for a fast collection with %d, want no call", gotMS)
	}
}

// countingRemediator records how many times the Layer 4C Step 7
// auto-remediation manager was evaluated.
type countingRemediator struct{ calls int }

func (c *countingRemediator) Evaluate(report HealthReport) { c.calls++ }

func TestManager_EvaluatesRemediator(t *testing.T) {
	mgr := NewManager("srv-test", NewSystemCollector(nil, nil),
		NewDockerCollector(emptyLister{}), NewReporter(&captureSender{}, 4)).
		WithInterval(10 * time.Millisecond)
	rem := &countingRemediator{}
	mgr.WithRemediator(rem)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		mgr.Run(ctx)
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if rem.calls >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done

	if rem.calls < 2 {
		t.Fatalf("remediator evaluated %d times, want at least 2", rem.calls)
	}
}

// captureLogHandler records slog messages so tests can assert on them.
type captureLogHandler struct {
	msgs []string
}

func (h *captureLogHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *captureLogHandler) Handle(_ context.Context, r slog.Record) error {
	h.msgs = append(h.msgs, r.Message)
	return nil
}
func (h *captureLogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureLogHandler) WithGroup(string) slog.Handler       { return h }

func TestManager_SlowCollectionLogsWarningByDefault(t *testing.T) {
	handler := &captureLogHandler{}
	old := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(old)

	m := NewManager("srv-test", NewSystemCollector(nil, nil),
		NewDockerCollector(emptyLister{}), NewReporter(&captureSender{}, 4))

	m.warnIfSlowCollection(5001)

	for _, msg := range handler.msgs {
		if strings.Contains(msg, "longer than 5s") {
			return // default warning path verified
		}
	}
	t.Errorf("expected a slow-collection warning log, got messages: %v", handler.msgs)
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
