package metrics

import (
	"context"
	"log/slog"
	"time"
)

const (
	// DefaultInterval is the metrics collection interval (30 seconds).
	DefaultInterval = 30 * time.Second

	// bufferCapacity is the max number of recent reports kept in memory
	// for offline catch-up (100 reports ≈ 50 minutes of history).
	bufferCapacity = 100

	// slowCollectionThresholdMS is the maximum acceptable duration for a
	// single collection cycle (Layer 4C plan). Longer cycles are flagged.
	slowCollectionThresholdMS = 5000
)

// Manager runs the Layer 4C metrics collection loop: gather host, container,
// and platform metrics at a fixed interval, then send each snapshot to the
// control plane (and keep a bounded buffer for offline catch-up).
type Manager struct {
	serverID string
	system   *SystemCollector
	docker   *DockerCollector
	reporter *Reporter
	anomaly  *AnomalyDetector
	interval time.Duration

	// onSlowCollection is invoked when a collection cycle exceeds
	// slowCollectionThresholdMS. Defaults to a warning log; overridable in
	// tests so the slow path can be verified deterministically.
	onSlowCollection func(elapsedMS int64)
}

// NewManager creates a metrics Manager.
func NewManager(serverID string, system *SystemCollector, docker *DockerCollector, reporter *Reporter) *Manager {
	return &Manager{
		serverID: serverID,
		system:   system,
		docker:   docker,
		reporter: reporter,
		interval: DefaultInterval,
	}
}

// WithInterval overrides the collection interval (tests).
func (m *Manager) WithInterval(d time.Duration) *Manager {
	if d <= 0 {
		d = DefaultInterval
	}
	m.interval = d
	return m
}

// WithAnomalyDetector attaches the Layer 4C Step 4 anomaly detector, which is
// evaluated after every collection cycle (thresholds, state machines, alerts).
func (m *Manager) WithAnomalyDetector(d *AnomalyDetector) *Manager {
	m.anomaly = d
	return m
}

// WithSlowCollectionHook overrides the slow-collection callback (tests).
func (m *Manager) WithSlowCollectionHook(fn func(elapsedMS int64)) *Manager {
	m.onSlowCollection = fn
	return m
}

// warnIfSlowCollection flags collection cycles that exceeded the 5s budget.
func (m *Manager) warnIfSlowCollection(elapsedMS int64) {
	if elapsedMS <= slowCollectionThresholdMS {
		return
	}
	if m.onSlowCollection != nil {
		m.onSlowCollection(elapsedMS)
		return
	}
	slog.Warn("metrics collection took longer than 5s", "elapsed_ms", elapsedMS)
}

// Run starts the collection loop and blocks until ctx is cancelled.
func (m *Manager) Run(ctx context.Context) {
	slog.Info("metrics collector started",
		"server_id", m.serverID, "interval", m.interval.String())

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	// Run one collection immediately so a report is available right away.
	m.collectAndSend(time.Now())

	for {
		select {
		case <-ctx.Done():
			slog.Info("metrics collector stopped")
			return
		case now := <-ticker.C:
			m.collectAndSend(now)
		}
	}
}

// collectAndSend performs one full collection cycle and reports the result.
func (m *Manager) collectAndSend(_ time.Time) {
	started := time.Now()

	server, platform := m.system.Collect(context.Background())
	containers := m.docker.Collect(context.Background())

	report := HealthReport{
		Type:          "health_report",
		ServerID:      m.serverID,
		Timestamp:     time.Now().UTC(),
		CollectedInMS: time.Since(started).Milliseconds(),
		Server:        server,
		Containers:    containers,
		Platform:      platform,
	}

	m.warnIfSlowCollection(report.CollectedInMS)

	// Layer 4C 4: compare the fresh report against thresholds and emit alerts
	// on state transitions (dedup, escalation, resolution).
	if m.anomaly != nil {
		m.anomaly.Evaluate(report)
	}

	m.reporter.Send(report)
}
