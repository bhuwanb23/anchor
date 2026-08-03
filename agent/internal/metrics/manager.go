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
)

// Manager runs the Layer 4C metrics collection loop: gather host, container,
// and platform metrics at a fixed interval, then send each snapshot to the
// control plane (and keep a bounded buffer for offline catch-up).
type Manager struct {
	serverID string
	system   *SystemCollector
	docker   *DockerCollector
	reporter *Reporter
	interval time.Duration
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

	if report.CollectedInMS > 5000 {
		slog.Warn("metrics collection took longer than 5s",
			"elapsed_ms", report.CollectedInMS)
	}

	m.reporter.Send(report)
}
