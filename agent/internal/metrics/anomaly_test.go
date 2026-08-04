package metrics

import (
	"sync"
	"testing"
	"time"
)

// fakeAlertSender records anomaly alerts sent by the detector.
type fakeAlertSender struct {
	mu     sync.Mutex
	alerts []AnomalyAlert
}

func (s *fakeAlertSender) SendJSON(v interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := v.(map[string]interface{}); ok {
		if a, ok := m["payload"].(AnomalyAlert); ok {
			s.alerts = append(s.alerts, a)
		}
	}
	return nil
}

func (s *fakeAlertSender) all() []AnomalyAlert {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]AnomalyAlert, len(s.alerts))
	copy(out, s.alerts)
	return out
}

func (s *fakeAlertSender) ofType(typ string) []AnomalyAlert {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []AnomalyAlert
	for _, a := range s.alerts {
		if a.Type == typ {
			out = append(out, a)
		}
	}
	return out
}

func (s *fakeAlertSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.alerts)
}

func levels(alerts []AnomalyAlert) []string {
	out := make([]string, len(alerts))
	for i, a := range alerts {
		out[i] = a.Level
	}
	return out
}

func newTestDetector() (*AnomalyDetector, *fakeAlertSender) {
	s := &fakeAlertSender{}
	return NewAnomalyDetector(s), s
}

func cpuReport(pct float64) HealthReport {
	return HealthReport{
		Server:   ServerMetrics{CPUPercent: pct},
		Platform: PlatformMetrics{CaddyRunning: true},
	}
}

// serverReport builds a report with only server metrics and a healthy
// platform so threshold tests don't trip platform alerts.
func serverReport(s ServerMetrics) HealthReport {
	return HealthReport{
		Server:   s,
		Platform: PlatformMetrics{CaddyRunning: true},
	}
}

func containerReport(proj, role string, c ContainerMetrics) HealthReport {
	c.Project = proj
	c.Role = role
	// Platform is kept healthy so container tests don't trip caddy alerts.
	return HealthReport{
		Containers: []ContainerMetrics{c},
		Platform:   PlatformMetrics{CaddyRunning: true},
	}
}

// crashAlerts returns the crash machine's alerts across both of its types
// (single-crash warnings use "container_crash", crash loops use
// "container_crash_loop").
func crashAlerts(s *fakeAlertSender) []AnomalyAlert {
	return append(s.ofType("container_crash"), s.ofType("container_crash_loop")...)
}

func intPtr(v int) *int { return &v }

// --- CPU sustained-duration detection (Step 4C / done conditions) ---

func TestCPU_WarningFiresAfter10SustainedSamples(t *testing.T) {
	d, s := newTestDetector()
	for i := 0; i < 9; i++ {
		d.Evaluate(cpuReport(85))
	}
	if s.count() != 0 {
		t.Fatalf("expected no alert after 9 samples, got %d", s.count())
	}
	d.Evaluate(cpuReport(85)) // 10th consecutive sample
	alerts := s.ofType("cpu")
	if len(alerts) != 1 {
		t.Fatalf("expected 1 cpu alert after 10 samples, got %d", len(alerts))
	}
	if alerts[0].Level != sevNameWarning {
		t.Errorf("level = %s, want warning", alerts[0].Level)
	}
}

func TestCPU_CriticalFiresAfter4SustainedSamples(t *testing.T) {
	d, s := newTestDetector()
	// 10 samples at 85% → warning.
	for i := 0; i < 10; i++ {
		d.Evaluate(cpuReport(85))
	}
	// 4 consecutive samples at 97% → critical (escalation).
	for i := 0; i < 4; i++ {
		d.Evaluate(cpuReport(97))
	}
	alerts := s.ofType("cpu")
	if len(alerts) != 2 {
		t.Fatalf("expected warning+critical, got %d alerts", len(alerts))
	}
	if alerts[0].Level != sevNameWarning || alerts[1].Level != sevNameCritical {
		t.Errorf("levels = %v, want [warning critical]", levels(alerts))
	}
}

func TestCPU_CriticalSkipsWarning(t *testing.T) {
	d, s := newTestDetector()
	for i := 0; i < 4; i++ {
		d.Evaluate(cpuReport(97))
	}
	alerts := s.ofType("cpu")
	if len(alerts) != 1 || alerts[0].Level != sevNameCritical {
		t.Fatalf("expected a single critical alert, got %v", levels(alerts))
	}
}

func TestCPU_ResolvedFiresWhenBelow70(t *testing.T) {
	d, s := newTestDetector()
	for i := 0; i < 10; i++ {
		d.Evaluate(cpuReport(85))
	}
	for i := 0; i < 4; i++ {
		d.Evaluate(cpuReport(97))
	}
	d.Evaluate(cpuReport(50)) // drops below 70%
	alerts := s.ofType("cpu")
	if len(alerts) != 3 {
		t.Fatalf("expected warning+critical+resolved, got %v", levels(alerts))
	}
	if alerts[2].Level != sevNameResolved {
		t.Errorf("last level = %s, want resolved", alerts[2].Level)
	}
}

func TestCPU_NoFlappingOnSingleSpike(t *testing.T) {
	d, s := newTestDetector()
	// One 97% spike followed by normal — counters reset, no alert.
	d.Evaluate(cpuReport(97))
	d.Evaluate(cpuReport(30))
	if s.count() != 0 {
		t.Fatalf("expected no alerts from a single spike, got %d", s.count())
	}
}

// --- RAM / disk / load immediate thresholds ---

func TestRAM_WarningCriticalResolved(t *testing.T) {
	d, s := newTestDetector()
	d.Evaluate(serverReport(ServerMetrics{RAMPercent: 82}))
	d.Evaluate(serverReport(ServerMetrics{RAMPercent: 92}))
	d.Evaluate(serverReport(ServerMetrics{RAMPercent: 60}))

	alerts := s.ofType("ram")
	if len(alerts) != 3 {
		t.Fatalf("expected warning+critical+resolved, got %v", levels(alerts))
	}
	if alerts[0].Level != sevNameWarning || alerts[1].Level != sevNameCritical || alerts[2].Level != sevNameResolved {
		t.Errorf("levels = %v, want [warning critical resolved]", levels(alerts))
	}
}

func TestRAM_HysteresisPreventsBoundaryFlap(t *testing.T) {
	d, s := newTestDetector()
	d.Evaluate(serverReport(ServerMetrics{RAMPercent: 82})) // warning
	d.Evaluate(serverReport(ServerMetrics{RAMPercent: 78})) // still >= resolve (75) → hold
	d.Evaluate(serverReport(ServerMetrics{RAMPercent: 79})) // still held

	alerts := s.ofType("ram")
	if len(alerts) != 1 {
		t.Fatalf("expected 1 warning (no resolved flapping), got %v", levels(alerts))
	}

	d.Evaluate(serverReport(ServerMetrics{RAMPercent: 74})) // below resolve → resolved
	alerts = s.ofType("ram")
	if len(alerts) != 2 || alerts[1].Level != sevNameResolved {
		t.Fatalf("expected resolved after dropping below 75%%, got %v", levels(alerts))
	}
}

func TestRAM_DedupPersistentWarning(t *testing.T) {
	d, s := newTestDetector()
	for i := 0; i < 3; i++ {
		d.Evaluate(serverReport(ServerMetrics{RAMPercent: 85}))
	}
	if s.count() != 1 {
		t.Fatalf("expected 1 alert for persistent condition, got %d", s.count())
	}
}

func TestDisk_WarningCritical(t *testing.T) {
	d, s := newTestDetector()
	d.Evaluate(serverReport(ServerMetrics{DiskPercent: 76}))
	d.Evaluate(serverReport(ServerMetrics{DiskPercent: 91}))

	alerts := s.ofType("disk")
	if len(alerts) != 2 {
		t.Fatalf("expected warning+critical, got %v", levels(alerts))
	}
	if alerts[0].Level != sevNameWarning || alerts[1].Level != sevNameCritical {
		t.Errorf("levels = %v, want [warning critical]", levels(alerts))
	}
}

func TestLoad_WarningCriticalResolved(t *testing.T) {
	d, s := newTestDetector()
	d.Evaluate(serverReport(ServerMetrics{LoadPerCore: 0.9}))
	d.Evaluate(serverReport(ServerMetrics{LoadPerCore: 1.6}))
	d.Evaluate(serverReport(ServerMetrics{LoadPerCore: 0.5}))

	alerts := s.ofType("load")
	if len(alerts) != 3 {
		t.Fatalf("expected warning+critical+resolved, got %v", levels(alerts))
	}
	if alerts[0].Level != sevNameWarning || alerts[1].Level != sevNameCritical || alerts[2].Level != sevNameResolved {
		t.Errorf("levels = %v, want [warning critical resolved]", levels(alerts))
	}
}

// --- Container detection (Step 4D) ---

func TestContainer_OOMFiresCriticalOnce(t *testing.T) {
	d, s := newTestDetector()
	rep := containerReport("myshop", "app", ContainerMetrics{
		Status: "running", OOMKilled: true, RAMLimitMB: 512, RAMUsedMB: 100,
	})
	d.Evaluate(rep)
	d.Evaluate(rep) // persistent → no duplicate

	alerts := s.ofType("container_oom")
	if len(alerts) != 1 {
		t.Fatalf("expected 1 OOM alert, got %d", len(alerts))
	}
	if alerts[0].Level != sevNameCritical {
		t.Errorf("level = %s, want critical", alerts[0].Level)
	}
	if alerts[0].Project != "myshop" || alerts[0].Container != "app" {
		t.Errorf("project/container = %s/%s, want myshop/app", alerts[0].Project, alerts[0].Container)
	}

	// New container (redeploy) clears OOM → resolved.
	d.Evaluate(containerReport("myshop", "app", ContainerMetrics{Status: "running"}))
	alerts = s.ofType("container_oom")
	if len(alerts) != 2 || alerts[1].Level != sevNameResolved {
		t.Fatalf("expected OOM resolved after recreate, got %v", levels(alerts))
	}
}

func TestContainer_OOMViaExitCode137(t *testing.T) {
	d, s := newTestDetector()
	d.Evaluate(containerReport("myshop", "app", ContainerMetrics{
		Status: "exited", ExitCode: intPtr(137),
	}))
	alerts := s.ofType("container_oom")
	if len(alerts) != 1 || alerts[0].Level != sevNameCritical {
		t.Fatalf("expected critical OOM alert from exit code 137, got %v", levels(alerts))
	}
	// Must NOT also fire a generic "stopped" alert for the same event.
	if len(s.ofType("container_stopped")) != 0 {
		t.Error("stopped alert should not fire alongside an OOM alert")
	}
}

func TestContainer_OOMDoesNotDoubleFireCrash(t *testing.T) {
	// An OOM-killed container that Docker restarts shows both OOMKilled=true
	// and an increased RestartCount — but only the specific OOM alert should
	// fire, not a generic crash alert too.
	d, s := newTestDetector()
	d.Evaluate(containerReport("myshop", "app", ContainerMetrics{
		Status: "running", OOMKilled: true, RestartCount: 1, RAMLimitMB: 512, RAMUsedMB: 500,
	}))

	if len(s.ofType("container_oom")) != 1 {
		t.Fatal("expected OOM alert")
	}
	if len(crashAlerts(s)) != 0 {
		t.Fatalf("expected no generic crash alert alongside OOM, got %v", levels(crashAlerts(s)))
	}
}

func TestContainer_SingleCrashWarning(t *testing.T) {
	d, s := newTestDetector()
	d.Evaluate(containerReport("myshop", "app", ContainerMetrics{Status: "running", RestartCount: 0}))
	d.Evaluate(containerReport("myshop", "app", ContainerMetrics{Status: "running", RestartCount: 1})) // delta 1

	alerts := crashAlerts(s)
	if len(alerts) != 1 || alerts[0].Level != sevNameWarning {
		t.Fatalf("expected 1 crash warning, got %v", levels(alerts))
	}

	// No further restart → no duplicate.
	d.Evaluate(containerReport("myshop", "app", ContainerMetrics{Status: "running", RestartCount: 1}))
	if s.count() != 1 {
		t.Fatalf("expected no duplicate alerts, got %d", s.count())
	}
}

func TestContainer_CrashLoopCritical(t *testing.T) {
	d, s := newTestDetector()
	// 3 restarts within 5 minutes → critical.
	for i := 1; i <= 3; i++ {
		d.Evaluate(containerReport("myshop", "app", ContainerMetrics{Status: "running", RestartCount: i}))
	}
	alerts := crashAlerts(s)
	if len(alerts) != 2 {
		t.Fatalf("expected warning then critical, got %v", levels(alerts))
	}
	if alerts[0].Level != sevNameWarning || alerts[1].Level != sevNameCritical {
		t.Errorf("levels = %v, want [warning critical]", levels(alerts))
	}
}

func TestContainer_CrashLoopResolvesAfterQuietWindow(t *testing.T) {
	base := time.Unix(1700000000, 0)
	d, s := newTestDetector()
	d.now = func() time.Time { return base }

	d.Evaluate(containerReport("myshop", "app", ContainerMetrics{Status: "running", RestartCount: 1}))
	if len(crashAlerts(s)) != 1 {
		t.Fatal("expected crash warning")
	}

	// 6 minutes pass with no new crash → window prunes → resolved.
	d.now = func() time.Time { return base.Add(6 * time.Minute) }
	d.Evaluate(containerReport("myshop", "app", ContainerMetrics{Status: "running", RestartCount: 1}))

	alerts := crashAlerts(s)
	if len(alerts) != 2 || alerts[1].Level != sevNameResolved {
		t.Fatalf("expected resolved after quiet window, got %v", levels(alerts))
	}
}

func TestContainer_StoppedNonZeroExitWarns(t *testing.T) {
	d, s := newTestDetector()
	d.Evaluate(containerReport("myshop", "app", ContainerMetrics{
		Status: "exited", ExitCode: intPtr(1), RestartCount: 0,
	}))
	alerts := s.ofType("container_stopped")
	if len(alerts) != 1 || alerts[0].Level != sevNameWarning {
		t.Fatalf("expected stopped warning, got %v", levels(alerts))
	}
}

func TestContainer_CleanStopNoAlert(t *testing.T) {
	d, s := newTestDetector()
	d.Evaluate(containerReport("myshop", "app", ContainerMetrics{
		Status: "exited", ExitCode: intPtr(0), RestartCount: 0,
	}))
	if s.count() != 0 {
		t.Fatalf("expected no alert for clean stop (exit 0), got %d", s.count())
	}
}

func TestContainer_UnhealthyWarns(t *testing.T) {
	d, s := newTestDetector()
	unhealthy := "unhealthy"
	healthy := "healthy"
	d.Evaluate(containerReport("myshop", "app", ContainerMetrics{Status: "running", Health: &unhealthy}))
	d.Evaluate(containerReport("myshop", "app", ContainerMetrics{Status: "running", Health: &unhealthy})) // dedup
	d.Evaluate(containerReport("myshop", "app", ContainerMetrics{Status: "running", Health: &healthy}))

	alerts := s.ofType("container_unhealthy")
	if len(alerts) != 2 {
		t.Fatalf("expected warning+resolved, got %v", levels(alerts))
	}
	if alerts[0].Level != sevNameWarning || alerts[1].Level != sevNameResolved {
		t.Errorf("levels = %v, want [warning resolved]", levels(alerts))
	}
}

func TestContainer_RAMWarningCriticalResolved(t *testing.T) {
	d, s := newTestDetector()
	d.Evaluate(containerReport("myshop", "app", ContainerMetrics{Status: "running", RAMLimitMB: 512, RAMPercent: 85}))
	d.Evaluate(containerReport("myshop", "app", ContainerMetrics{Status: "running", RAMLimitMB: 512, RAMPercent: 96}))
	d.Evaluate(containerReport("myshop", "app", ContainerMetrics{Status: "running", RAMLimitMB: 512, RAMPercent: 65}))

	alerts := s.ofType("container_ram")
	if len(alerts) != 3 {
		t.Fatalf("expected warning+critical+resolved, got %v", levels(alerts))
	}
	if alerts[0].Level != sevNameWarning || alerts[1].Level != sevNameCritical || alerts[2].Level != sevNameResolved {
		t.Errorf("levels = %v, want [warning critical resolved]", levels(alerts))
	}
}

func TestContainer_NoRAMLimitSkipsRAMCheck(t *testing.T) {
	d, s := newTestDetector()
	d.Evaluate(containerReport("myshop", "app", ContainerMetrics{Status: "running", RAMLimitMB: 0, RAMPercent: 99}))
	if len(s.ofType("container_ram")) != 0 {
		t.Fatal("expected no container_ram alert when limit is unknown")
	}
}

// --- Platform detection ---

func TestCaddy_DownCriticalAndResolved(t *testing.T) {
	d, s := newTestDetector()
	d.Evaluate(HealthReport{Platform: PlatformMetrics{CaddyRunning: false}})
	d.Evaluate(HealthReport{Platform: PlatformMetrics{CaddyRunning: false}}) // dedup
	d.Evaluate(HealthReport{Platform: PlatformMetrics{CaddyRunning: true}})

	alerts := s.ofType("caddy_down")
	if len(alerts) != 2 {
		t.Fatalf("expected critical+resolved, got %v", levels(alerts))
	}
	if alerts[0].Level != sevNameCritical || alerts[1].Level != sevNameResolved {
		t.Errorf("levels = %v, want [critical resolved]", levels(alerts))
	}
}

func TestBackup_OverdueWarningAfter26h(t *testing.T) {
	d, s := newTestDetector()
	// 27 hours since last backup → warning.
	d.Evaluate(HealthReport{Platform: PlatformMetrics{CaddyRunning: true, LastBackupAgeSec: 27 * 3600}})
	alerts := s.ofType("backup_overdue")
	if len(alerts) != 1 || alerts[0].Level != sevNameWarning {
		t.Fatalf("expected backup overdue warning, got %v", levels(alerts))
	}
}

func TestBackup_OverdueCriticalAfter50h(t *testing.T) {
	d, s := newTestDetector()
	d.Evaluate(HealthReport{Platform: PlatformMetrics{CaddyRunning: true, LastBackupAgeSec: 27 * 3600}})
	d.Evaluate(HealthReport{Platform: PlatformMetrics{CaddyRunning: true, LastBackupAgeSec: 51 * 3600}})

	alerts := s.ofType("backup_overdue")
	if len(alerts) != 2 {
		t.Fatalf("expected warning+critical, got %v", levels(alerts))
	}
	if alerts[0].Level != sevNameWarning || alerts[1].Level != sevNameCritical {
		t.Errorf("levels = %v, want [warning critical]", levels(alerts))
	}

	// Fresh backup → resolved.
	d.Evaluate(HealthReport{Platform: PlatformMetrics{CaddyRunning: true, LastBackupAgeSec: 3600}})
	alerts = s.ofType("backup_overdue")
	if len(alerts) != 3 || alerts[2].Level != sevNameResolved {
		t.Fatalf("expected resolved after fresh backup, got %v", levels(alerts))
	}
}

func TestBackup_NeverRanNoAlert(t *testing.T) {
	d, s := newTestDetector()
	d.Evaluate(HealthReport{Platform: PlatformMetrics{CaddyRunning: true, LastBackupAgeSec: 0}})
	if len(s.ofType("backup_overdue")) != 0 {
		t.Fatal("expected no backup alert when a backup never ran")
	}
}

// --- Manager wiring ---

func TestManager_WithAnomalyDetectorEvaluates(t *testing.T) {
	alertSender := &fakeAlertSender{}
	detector := NewAnomalyDetector(alertSender)

	mgr := NewManager("srv-test",
		NewSystemCollector(fakeCaddyStatus{alive: true}, fakeBackupReader{}),
		NewDockerCollector(emptyLister{}),
		NewReporter(&captureSender{}, 4),
	).WithAnomalyDetector(detector)

	mgr.collectAndSend(time.Now())

	// The detector must have run over the fresh report: at least the server
	// + platform machines exist. (Healthy host → likely zero alerts, which is
	// itself the dedup baseline.)
	if len(detector.machines) < 4 {
		t.Errorf("expected anomaly detector to evaluate the report, machines = %d", len(detector.machines))
	}
}

func TestAnomalyAlert_WireShape(t *testing.T) {
	d, s := newTestDetector()
	d.Evaluate(HealthReport{
		Server:   ServerMetrics{RAMPercent: 85},
		Platform: PlatformMetrics{CaddyRunning: true},
	})
	alerts := s.all()
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	a := alerts[0]
	if a.Level != sevNameWarning || a.Type != "ram" || a.Message == "" {
		t.Errorf("unexpected alert: %+v", a)
	}
}
