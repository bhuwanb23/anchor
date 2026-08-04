package metrics

import (
	"strings"
	"testing"
	"time"
)

// --- Step 5A: Alert structure ---

func TestAlert_StructureHasAllFields(t *testing.T) {
	d, s := newTestDetector()
	d.Evaluate(serverReport(ServerMetrics{RAMPercent: 85}))

	alerts := s.ofType("ram")
	if len(alerts) != 1 {
		t.Fatalf("expected 1 ram alert, got %d", len(alerts))
	}
	a := alerts[0]
	if a.ID == "" || !strings.HasPrefix(a.ID, "alert-") {
		t.Errorf("id = %q, want alert- prefixed", a.ID)
	}
	if a.ServerID != "srv-test" {
		t.Errorf("server_id = %q, want srv-test", a.ServerID)
	}
	if a.Severity != sevNameWarning {
		t.Errorf("severity = %q, want warning", a.Severity)
	}
	if a.Status != "active" {
		t.Errorf("status = %q, want active", a.Status)
	}
	if a.Title == "" || a.Message == "" {
		t.Errorf("title/message must not be empty: %+v", a)
	}
	if a.FiredAt == "" {
		t.Error("fired_at must be set")
	}
	if a.ResolvedAt != nil {
		t.Error("resolved_at must be nil while active")
	}
}

func TestAlert_ResolvedHasResolvedAtAndStatus(t *testing.T) {
	d, s := newTestDetector()
	d.Evaluate(serverReport(ServerMetrics{RAMPercent: 85}))
	d.Evaluate(serverReport(ServerMetrics{RAMPercent: 60}))

	alerts := s.ofType("ram")
	if len(alerts) != 2 {
		t.Fatalf("expected warning+resolved, got %d", len(alerts))
	}
	r := alerts[1]
	if r.Status != "resolved" {
		t.Errorf("status = %q, want resolved", r.Status)
	}
	if r.ResolvedAt == nil {
		t.Error("resolved_at must be set on resolved alert")
	}
	if r.Level != sevNameResolved {
		t.Errorf("level = %q, want resolved", r.Level)
	}
	if r.Severity != sevNameWarning {
		t.Errorf("severity = %q, want warning (the level being resolved)", r.Severity)
	}
}

func TestAlert_EscalationReusesActiveID(t *testing.T) {
	d, s := newTestDetector()
	d.Evaluate(serverReport(ServerMetrics{RAMPercent: 85})) // warning
	d.Evaluate(serverReport(ServerMetrics{RAMPercent: 92})) // critical (escalation)

	alerts := s.ofType("ram")
	if len(alerts) != 2 {
		t.Fatalf("expected warning+critical, got %d", len(alerts))
	}
	if alerts[0].ID == "" || alerts[0].ID != alerts[1].ID {
		t.Errorf("escalation must reuse the active alert id: %q vs %q", alerts[0].ID, alerts[1].ID)
	}
	if alerts[1].Severity != sevNameCritical {
		t.Errorf("escalated severity = %q, want critical", alerts[1].Severity)
	}
}

func TestAlert_ResolvedReusesIDAndReportsPriorSeverity(t *testing.T) {
	d, s := newTestDetector()
	// Warning then critical then resolved.
	d.Evaluate(serverReport(ServerMetrics{RAMPercent: 85}))
	d.Evaluate(serverReport(ServerMetrics{RAMPercent: 92}))
	d.Evaluate(serverReport(ServerMetrics{RAMPercent: 60}))

	alerts := s.ofType("ram")
	if len(alerts) != 3 {
		t.Fatalf("expected warning+critical+resolved, got %d", len(alerts))
	}
	// Resolution reuses the same id as the escalation.
	if alerts[2].ID != alerts[1].ID {
		t.Errorf("resolved must reuse the active alert id: %q vs %q", alerts[2].ID, alerts[1].ID)
	}
	// And reports the severity it is clearing (critical).
	if alerts[2].Severity != sevNameCritical {
		t.Errorf("resolved severity = %q, want critical (the level being resolved)", alerts[2].Severity)
	}
	if alerts[2].Status != "resolved" || alerts[2].ResolvedAt == nil {
		t.Errorf("resolved alert missing status/resolved_at: %+v", alerts[2])
	}
}

// --- Step 5B: templates ---

func TestAlert_TemplatesFillPlaceholders(t *testing.T) {
	d, s := newTestDetector()
	// Container OOM with limit/used values.
	d.Evaluate(containerReport("myshop", "app", ContainerMetrics{
		Status: "running", OOMKilled: true, RAMLimitMB: 512, RAMUsedMB: 511,
	}))

	alerts := s.ofType("container_oom")
	if len(alerts) != 1 {
		t.Fatalf("expected 1 oom alert, got %d", len(alerts))
	}
	a := alerts[0]
	for _, f := range []string{a.Title, a.Message, a.Detail, a.Action} {
		if strings.Contains(f, "{") {
			t.Errorf("unfilled placeholder in %q", f)
		}
	}
	if a.Title != "myshop ran out of memory and was restarted" {
		t.Errorf("title = %q, want template title", a.Title)
	}
	if !strings.Contains(a.Message, "512MB limit") {
		t.Errorf("message should contain filled limit: %q", a.Message)
	}
	if !strings.Contains(a.Detail, "511MB of 512MB limit") {
		t.Errorf("detail should contain used/limit: %q", a.Detail)
	}
	if a.Action == "" {
		t.Error("action must be set from template")
	}
	// Metrics payload present.
	if a.Metrics["ram_used_mb"] != int64(511) || a.Metrics["ram_limit_mb"] != int64(512) {
		t.Errorf("metrics = %v, want ram_used_mb/ram_limit_mb", a.Metrics)
	}
}

func TestAlert_TemplateForEveryType(t *testing.T) {
	// Every alert type the detector can emit must have a template entry so
	// title/message are never empty.
	for typ, sevMap := range alertTemplates {
		for sev, tmpl := range sevMap {
			if tmpl.Title == "" || tmpl.Message == "" {
				t.Errorf("type %s/%s has empty title or message", typ, sev)
			}
		}
	}
	// And the emitted types must all have entries.
	emitted := []string{"cpu", "ram", "disk", "load", "container_oom", "container_crash",
		"container_stopped", "container_unhealthy", "container_ram", "caddy_down", "backup_overdue"}
	for _, typ := range emitted {
		if _, ok := alertTemplates[typ]; !ok {
			t.Errorf("no template for emitted type %q", typ)
		}
	}
}

// --- Step 5D: rate limiting ---

func TestRateLimit_ProjectAlertsLimitedTo3PerHour(t *testing.T) {
	d, s := newTestDetector()
	base := time.Unix(1700000000, 0)
	d.now = func() time.Time { return base }

	// Fire 4 distinct project alerts by cycling a container through states
	// that trigger different types within the same hour.
	fireContainerAlert := func() {
		d.Evaluate(containerReport("myshop", "app", ContainerMetrics{
			Status: "exited", ExitCode: intPtr(1), RestartCount: 0,
		}))
	}
	// Each stopped event fires once; simulate by resetting the machine between.
	for i := 0; i < 4; i++ {
		d.machines = map[string]*metricState{}
		fireContainerAlert()
	}
	// 3 project alerts allowed, 4th suppressed → suppression message emitted.
	if n := len(s.ofType("container_stopped")); n != projectAlertsPerHour {
		t.Errorf("container_stopped alerts = %d, want %d (rate limited)", n, projectAlertsPerHour)
	}
	supp := s.ofType("alerts_suppressed")
	if len(supp) != 1 {
		t.Fatalf("expected 1 suppression message, got %d", len(supp))
	}
	if !strings.Contains(supp[0].Message, "myshop") {
		t.Errorf("suppression should mention project: %q", supp[0].Message)
	}
}

func TestRateLimit_ServerAlertsLimitedTo5PerHour(t *testing.T) {
	d, s := newTestDetector()
	base := time.Unix(1700000000, 0)
	d.now = func() time.Time { return base }

	// Fire 6 distinct server-level conditions (different machines) in one hour.
	report := serverReport(ServerMetrics{RAMPercent: 85})
	for i := 0; i < 2; i++ {
		d.machines = map[string]*metricState{}
		d.evalServer(report.Server)
	}
	// Add disk and load to cross 5 with distinct machines.
	d.machines = map[string]*metricState{}
	d.evalServer(ServerMetrics{RAMPercent: 85})
	d.machines = map[string]*metricState{}
	d.evalServer(ServerMetrics{RAMPercent: 0, DiskPercent: 91})
	d.machines = map[string]*metricState{}
	d.evalServer(ServerMetrics{RAMPercent: 0, DiskPercent: 91})
	d.machines = map[string]*metricState{}
	d.evalServer(ServerMetrics{RAMPercent: 0, LoadPerCore: 1.6})

	// s.count() includes the suppression message, so subtract it.
	total := s.count() - len(s.ofType("alerts_suppressed"))
	if total != serverAlertsPerHour {
		t.Errorf("total server alerts = %d, want %d (rate limited)", total, serverAlertsPerHour)
	}
	if len(s.ofType("alerts_suppressed")) != 1 {
		t.Error("expected suppression message after hitting server limit")
	}
}

func TestRateLimit_ResetsAfterAnHour(t *testing.T) {
	d, s := newTestDetector()
	base := time.Unix(1700000000, 0)
	d.now = func() time.Time { return base }

	for i := 0; i < 3; i++ {
		d.machines = map[string]*metricState{}
		d.evalServer(ServerMetrics{RAMPercent: 85})
	}
	// Advance past the hour: the bucket resets and alerts flow again.
	d.now = func() time.Time { return base.Add(time.Hour + time.Minute) }
	d.machines = map[string]*metricState{}
	d.evalServer(ServerMetrics{RAMPercent: 85})

	total := s.count()
	if total != projectAlertsPerHour+1 {
		t.Errorf("total alerts after reset = %d, want %d", total, projectAlertsPerHour+1)
	}
}

func TestRateLimit_ResolvedAlertsNotLimited(t *testing.T) {
	d, _ := newTestDetector()
	// Resolved alerts never consume the budget: rateLimited returns true
	// for them regardless of bucket count.
	resolved := Alert{Status: "resolved", Project: "myshop"}
	for i := 0; i < 10; i++ {
		if !d.rateLimited(resolved) {
			t.Fatal("resolved alert was rate limited")
		}
	}
	// And they never create buckets.
	if len(d.rates) != 0 {
		t.Errorf("resolved alerts should not create rate buckets, got %d", len(d.rates))
	}
}

// --- Suppression wire shape ---

func TestSuppression_IsAValidAlert(t *testing.T) {
	d, s := newTestDetector()
	d.sendSuppression(Alert{Project: "myshop"})

	supp := s.ofType("alerts_suppressed")
	if len(supp) != 1 {
		t.Fatalf("expected 1 suppression alert, got %d", len(supp))
	}
	a := supp[0]
	if a.ID == "" || a.Severity != sevNameWarning || a.Status != "active" {
		t.Errorf("suppression alert incomplete: %+v", a)
	}
}
