package caddy

import (
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"
)

const (
	defaultWindow    = 1 * time.Minute
	defaultThreshold = 10
	rateLimitCooldown = 24 * time.Hour
)

// caddyLogEntry represents a parsed Caddy JSON log line.
type caddyLogEntry struct {
	Level  string         `json:"level"`
	Msg    string         `json:"msg"`
	Status int            `json:"status"`
	Request map[string]any `json:"request"`
	Error  string         `json:"error"`
}

// LogMonitorConfig configures the log monitor.
type LogMonitorConfig struct {
	Window    time.Duration
	Threshold int
}

// ErrorAlerter sends error alerts.
type ErrorAlerter interface {
	SendErrorAlert(alert ErrorAlert) error
}

// ContainerChecker checks if a container is running.
type ContainerChecker interface {
	IsContainerRunning(ctx interface{}, containerID string) (bool, error)
}

// LogMonitor parses Caddy stderr and detects error patterns.
type LogMonitor struct {
	window    time.Duration
	threshold int
	reporter  ErrorAlerter
	mu        sync.Mutex
	recent502 []time.Time
}

// NewLogMonitor creates a new log monitor.
func NewLogMonitor(cfg LogMonitorConfig, reporter ErrorAlerter) *LogMonitor {
	if cfg.Window == 0 {
		cfg.Window = defaultWindow
	}
	if cfg.Threshold == 0 {
		cfg.Threshold = defaultThreshold
	}
	return &LogMonitor{
		window:    cfg.Window,
		threshold: cfg.Threshold,
		reporter:  reporter,
	}
}

// ProcessLine parses a single line of Caddy stderr output.
func (m *LogMonitor) ProcessLine(line string) {
	line = strings.TrimSpace(line)
	if line == "" || !strings.HasPrefix(line, "{") {
		return
	}

	var entry caddyLogEntry
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		return
	}

	m.processEntry(entry)
}

func (m *LogMonitor) processEntry(entry caddyLogEntry) {
	switch {
	case entry.Status == 502:
		m.handle502(entry)
	case entry.Status == 429 || strings.Contains(entry.Error, "rateLimited") || strings.Contains(entry.Error, "rate limit"):
		m.handleRateLimit(entry)
	case entry.Level == "error" && (strings.Contains(entry.Error, "acme_") || strings.Contains(entry.Msg, "obtain") || strings.Contains(entry.Msg, "renew")):
		m.handleCertError(entry)
	}
}

func (m *LogMonitor) handle502(entry caddyLogEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	m.recent502 = append(m.recent502, now)

	cutoff := now.Add(-m.window)
	pruned := m.recent502[:0]
	for _, t := range m.recent502 {
		if t.After(cutoff) {
			pruned = append(pruned, t)
		}
	}
	m.recent502 = pruned

	if len(m.recent502) >= m.threshold {
		slog.Warn("502 error threshold exceeded",
			"count", len(m.recent502),
			"window", m.window,
			"threshold", m.threshold)

		domain := extractDomainFromEntry(entry)
		alert := Alert502Errors(domain, len(m.recent502), len(m.recent502))

		if m.reporter != nil {
			if err := m.reporter.SendErrorAlert(alert); err != nil {
				slog.Error("failed to send 502 alert", "error", err)
			}
		}

		m.recent502 = nil
	}
}

func (m *LogMonitor) handleRateLimit(entry caddyLogEntry) {
	domain := extractDomainFromEntry(entry)
	if domain == "" {
		return
	}

	slog.Warn("ACME rate limit detected", "domain", domain, "error", entry.Error)

	if m.reporter != nil {
		resetDate := time.Now().Add(rateLimitCooldown)
		alert := AlertRateLimit(domain, resetDate)
		if err := m.reporter.SendErrorAlert(alert); err != nil {
			slog.Error("failed to send rate limit alert", "error", err)
		}
	}
}

func (m *LogMonitor) handleCertError(entry caddyLogEntry) {
	domain := extractDomainFromEntry(entry)
	if domain == "" {
		return
	}

	reason := entry.Error
	if reason == "" {
		reason = entry.Msg
	}

	slog.Warn("certificate issuance failed", "domain", domain, "reason", reason)

	if m.reporter != nil {
		alert := AlertCertFailed(domain, reason)
		if err := m.reporter.SendErrorAlert(alert); err != nil {
			slog.Error("failed to send cert error alert", "error", err)
		}
	}
}

// Recent502Count returns the number of 502 errors in the current window.
func (m *LogMonitor) Recent502Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.recent502)
}

func extractDomainFromEntry(entry caddyLogEntry) string {
	if entry.Request != nil {
		if host, ok := entry.Request["Host"].(string); ok && host != "" {
			return host
		}
	}

	if entry.Error != "" {
		parts := strings.Fields(entry.Error)
		for _, part := range parts {
			if strings.Contains(part, ".") && !strings.HasPrefix(part, "{") {
				return strings.Trim(part, "\"'")
			}
		}
	}

	return "unknown"
}
