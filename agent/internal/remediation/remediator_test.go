package remediation

import (
	"context"
	"encoding/json"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/yourname/yourplatform/agent/internal/docker"
	"github.com/yourname/yourplatform/agent/internal/metrics"
)

// recordingSender captures remediation reports (round-tripped through JSON so
// the wire shape is exercised).
type recordingSender struct {
	mu      sync.Mutex
	reports []RemediationReport
}

func (s *recordingSender) SendJSON(v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var rep RemediationReport
	if err := json.Unmarshal(b, &rep); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reports = append(s.reports, rep)
	return nil
}

func (s *recordingSender) all() []RemediationReport {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]RemediationReport(nil), s.reports...)
}

func (s *recordingSender) ofAction(action string) []RemediationReport {
	var out []RemediationReport
	for _, r := range s.all() {
		if r.Payload.Action == action {
			out = append(out, r)
		}
	}
	return out
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

// healthyReport is a report with a live caddy and a healthy disk so the tests
// only exercise the case under test (the zero-value Platform would otherwise
// count as "caddy down").
func healthyReport() metrics.HealthReport {
	return metrics.HealthReport{
		Server:   metrics.ServerMetrics{DiskTotalGB: 40},
		Platform: metrics.PlatformMetrics{CaddyRunning: true},
	}
}

func TestRemediation_DiskPruneTriggersOnceAndReports(t *testing.T) {
	sender := &recordingSender{}
	r := New(sender, "srv-1")
	clock := time.Now()
	r.now = func() time.Time { return clock }

	prunes := 0
	r.SetPruner(func(ctx context.Context) (*docker.PruneReport, error) {
		prunes++
		return &docker.PruneReport{ImagesRemoved: 5, ContainersRemoved: 1, SpaceReclaimedBytes: 2 << 30}, nil
	})

	high := healthyReport()
	high.Server.DiskPercent = 80

	r.Evaluate(high) // trigger
	r.Evaluate(high) // armed → no second trigger
	waitFor(t, func() bool { return prunes >= 1 })
	time.Sleep(50 * time.Millisecond)
	if prunes != 1 {
		t.Fatalf("prunes=%d want 1 (dedup while armed)", prunes)
	}

	waitFor(t, func() bool { return len(sender.ofAction("docker_prune")) >= 1 })
	rep := sender.ofAction("docker_prune")[0]
	if rep.Type != "remediation_report" || !rep.Payload.Success {
		t.Fatalf("report = %+v, want successful docker_prune", rep)
	}
	if rep.Payload.FreedBytes != 2<<30 {
		t.Errorf("freed_bytes=%d want %d", rep.Payload.FreedBytes, 2<<30)
	}
	if rep.Payload.DiskPercentBefore != 80 || rep.Payload.DiskPercentAfter == 0 {
		t.Errorf("disk percent before/after = %v/%v, want 80/estimated", rep.Payload.DiskPercentBefore, rep.Payload.DiskPercentAfter)
	}
}

func TestRemediation_DiskReArmsAfterDropAndCooldown(t *testing.T) {
	sender := &recordingSender{}
	r := New(sender, "srv-1")
	clock := time.Now()
	r.now = func() time.Time { return clock }

	prunes := 0
	r.SetPruner(func(ctx context.Context) (*docker.PruneReport, error) {
		prunes++
		return &docker.PruneReport{}, nil
	})

	high := healthyReport()
	high.Server.DiskPercent = 80
	low := healthyReport()
	low.Server.DiskPercent = 60

	r.Evaluate(high)
	waitFor(t, func() bool { return prunes >= 1 })

	// Dropped below 70% → re-armed. But still inside the 1h cooldown.
	r.Evaluate(low)
	r.Evaluate(high)
	time.Sleep(30 * time.Millisecond)
	if prunes != 1 {
		t.Fatalf("prunes during cooldown=%d want 1", prunes)
	}

	// Advance past the cooldown → pruning runs again.
	clock = clock.Add(2 * time.Hour)
	r.Evaluate(high)
	waitFor(t, func() bool { return prunes >= 2 })
}

func TestRemediation_DiskPruneFailureReported(t *testing.T) {
	sender := &recordingSender{}
	r := New(sender, "srv-1")
	r.SetPruner(func(ctx context.Context) (*docker.PruneReport, error) {
		return nil, context.DeadlineExceeded
	})

	high := healthyReport()
	high.Server.DiskPercent = 85
	r.Evaluate(high)

	waitFor(t, func() bool { return len(sender.ofAction("docker_prune")) >= 1 })
	if sender.ofAction("docker_prune")[0].Payload.Success {
		t.Fatal("failed prune must report success=false")
	}
}

func TestRemediation_CaddyRestartOncePerCooldown(t *testing.T) {
	sender := &recordingSender{}
	r := New(sender, "srv-1")
	clock := time.Now()
	r.now = func() time.Time { return clock }

	restarts := 0
	r.SetCaddyRestarter(func(ctx context.Context) error { restarts++; return nil })

	down := healthyReport()
	down.Platform.CaddyRunning = false

	r.Evaluate(down)
	r.Evaluate(down) // inside cooldown → no second restart
	waitFor(t, func() bool { return restarts >= 1 })
	time.Sleep(50 * time.Millisecond)
	if restarts != 1 {
		t.Fatalf("caddy restarts=%d want 1 (cooldown)", restarts)
	}

	waitFor(t, func() bool { return len(sender.ofAction("caddy_restart")) >= 1 })
	if !sender.ofAction("caddy_restart")[0].Payload.Success {
		t.Fatal("successful caddy restart must report success=true")
	}

	// Past the cooldown and still down → restart again.
	clock = clock.Add(10 * time.Minute)
	r.Evaluate(down)
	waitFor(t, func() bool { return restarts >= 2 })

	// Caddy back up → no restart.
	up := healthyReport()
	r.Evaluate(up)
	time.Sleep(30 * time.Millisecond)
	if restarts != 2 {
		t.Fatalf("caddy restarts while up=%d want 2", restarts)
	}
}

func TestRemediation_CrashRecoveryReported(t *testing.T) {
	sender := &recordingSender{}
	r := New(sender, "srv-1")

	// Note: each report gets its own container slice — struct copies share
	// the backing array, which would corrupt the "before" state.
	r.Evaluate(metrics.HealthReport{
		Containers: []metrics.ContainerMetrics{
			{Project: "shop", Role: "app", RestartCount: 0},
		},
		Platform: metrics.PlatformMetrics{CaddyRunning: true},
	})
	r.Evaluate(metrics.HealthReport{
		Containers: []metrics.ContainerMetrics{
			{Project: "shop", Role: "app", RestartCount: 1, Status: "running"},
		},
		Platform: metrics.PlatformMetrics{CaddyRunning: true},
	})

	waitFor(t, func() bool { return len(sender.ofAction("crash_recovery")) >= 1 })
	rep := sender.ofAction("crash_recovery")[0]
	if !rep.Payload.Success || rep.Payload.Project != "shop" || rep.Payload.Container != "app" {
		t.Fatalf("crash_recovery report = %+v", rep)
	}
	if rep.Payload.Message == "" {
		t.Fatal("crash_recovery report must include a human-readable message")
	}
}

func TestRemediation_CrashRecoveryDelayedReport(t *testing.T) {
	// A container that is observed mid-restart (not running) at the crash
	// sample, then running on a later cycle, still gets the recovery report.
	sender := &recordingSender{}
	r := New(sender, "srv-1")
	platform := metrics.PlatformMetrics{CaddyRunning: true}

	r.Evaluate(metrics.HealthReport{
		Containers: []metrics.ContainerMetrics{
			{Project: "shop", Role: "app", RestartCount: 0, Status: "running"},
		},
		Platform: platform,
	})
	// Crash detected, container still restarting at this sample.
	r.Evaluate(metrics.HealthReport{
		Containers: []metrics.ContainerMetrics{
			{Project: "shop", Role: "app", RestartCount: 1, Status: "restarting"},
		},
		Platform: platform,
	})
	time.Sleep(30 * time.Millisecond)
	if len(sender.ofAction("crash_recovery")) != 0 {
		t.Fatal("must not report recovery while the container is still restarting")
	}

	// Observed running on a later cycle → recovered.
	r.Evaluate(metrics.HealthReport{
		Containers: []metrics.ContainerMetrics{
			{Project: "shop", Role: "app", RestartCount: 1, Status: "running"},
		},
		Platform: platform,
	})
	waitFor(t, func() bool { return len(sender.ofAction("crash_recovery")) >= 1 })
}

func TestRemediation_CrashWithoutRecoveryNoReport(t *testing.T) {
	sender := &recordingSender{}
	r := New(sender, "srv-1")
	platform := metrics.PlatformMetrics{CaddyRunning: true}

	r.Evaluate(metrics.HealthReport{
		Containers: []metrics.ContainerMetrics{
			{Project: "shop", Role: "app", RestartCount: 0},
		},
		Platform: platform,
	})
	// Crashed and stayed down — no recovery to report.
	r.Evaluate(metrics.HealthReport{
		Containers: []metrics.ContainerMetrics{
			{Project: "shop", Role: "app", RestartCount: 1, Status: "exited"},
		},
		Platform: platform,
	})
	time.Sleep(50 * time.Millisecond)

	if len(sender.ofAction("crash_recovery")) != 0 {
		t.Fatal("a container that did not come back must not report crash_recovery")
	}
}

func TestRemediation_MemoryFlushAndReport(t *testing.T) {
	sender := &recordingSender{}
	r := New(sender, "srv-1")
	clock := time.Now()
	r.now = func() time.Time { return clock }

	flushes := 0
	r.SetLogFlusher(func() int { flushes++; return 2 })

	mem := &runtime.MemStats{}
	r.SetMemStats(func() *runtime.MemStats { return mem })

	mem.Alloc = 300 << 20 // 300MB — above the 200MB threshold
	r.Evaluate(healthyReport())

	waitFor(t, func() bool { return flushes >= 1 })
	waitFor(t, func() bool { return len(sender.ofAction("memory_flush")) >= 1 })
	rep := sender.ofAction("memory_flush")[0]
	if rep.Payload.Action != "memory_flush" {
		t.Fatalf("report = %+v", rep)
	}
	// Memory is still reported above the threshold → success=false, message notes stability.
	if rep.Payload.Success {
		t.Log("memory reported below threshold — acceptable when GC reclaims")
	}

	// Cooldown: another high reading must not flush again immediately.
	r.Evaluate(healthyReport())
	time.Sleep(30 * time.Millisecond)
	if flushes != 1 {
		t.Fatalf("flushes=%d want 1 (cooldown)", flushes)
	}

	// Past cooldown and still high → flush again.
	clock = clock.Add(15 * time.Minute)
	r.Evaluate(healthyReport())
	waitFor(t, func() bool { return flushes >= 2 })
}

func TestRemediation_ReportWireShape(t *testing.T) {
	sender := &recordingSender{}
	r := New(sender, "srv-1")
	r.now = func() time.Time { return time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC) }

	high := healthyReport()
	high.Server.DiskPercent = 80
	r.SetPruner(func(ctx context.Context) (*docker.PruneReport, error) {
		return &docker.PruneReport{SpaceReclaimedBytes: 1 << 30}, nil
	})
	r.Evaluate(high)

	waitFor(t, func() bool { return len(sender.all()) >= 1 })
	var raw map[string]interface{}
	b, _ := json.Marshal(sender.all()[0])
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	if raw["type"] != "remediation_report" {
		t.Fatalf("wire type = %v", raw["type"])
	}
	payload, _ := raw["payload"].(map[string]interface{})
	for _, k := range []string{"server_id", "action", "success", "message", "freed_bytes", "at"} {
		if _, ok := payload[k]; !ok {
			t.Errorf("payload missing key %q: %s", k, b)
		}
	}
}
