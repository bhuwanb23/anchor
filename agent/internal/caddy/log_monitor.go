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

// caddyRequest represents the request field in Caddy JSON logs.
type caddyRequest struct {
	Method   string `json:"method"`
	URI      string `json:"uri"`
	Host     string `json:"host"`
	RemoteIP string `json:"remote_ip"`
}

// caddyResp represents the response field in Caddy JSON logs.
type caddyResp struct {
	Status  int   `json:"status"`
	Size    int64 `json:"size"`
	Elapsed float64 `json:"elapsed"`
}

// caddyTLS represents TLS-specific fields in Caddy JSON logs.
type caddyTLS struct {
	HandshakeComplete bool   `json:"handshake_complete"`
	Version           string `json:"version"`
	CipherSuite       string `json:"cipher_suite"`
	ServerName        string `json:"server_name"`
}

// caddyLogEntry represents a parsed Caddy JSON log line.
type caddyLogEntry struct {
	Level    string        `json:"level"`
	Msg      string        `json:"msg"`
	Logger   string        `json:"logger"`
	Error    string        `json:"error"`
	Request  *caddyRequest `json:"request"`
	Resp     *caddyResp    `json:"resp"`
	Duration float64       `json:"duration"`
	TLS      *caddyTLS     `json:"tls"`
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
	window         time.Duration
	threshold      int
	reporter       ErrorAlerter
	rateLimitTrack *RateLimitTracker
	eventRecorder  *EventRecorder
	mu             sync.Mutex
	recent502      []time.Time
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

// SetRateLimitTracker sets the rate limit tracker for prevention.
func (m *LogMonitor) SetRateLimitTracker(tracker *RateLimitTracker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rateLimitTrack = tracker
}

// SetEventRecorder sets the event recorder for server events.
func (m *LogMonitor) SetEventRecorder(recorder *EventRecorder) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventRecorder = recorder
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
	// Determine HTTP status from resp or legacy status field
	status := 0
	if entry.Resp != nil {
		status = entry.Resp.Status
	}

	switch {
	case status == 502:
		m.handle502(entry, status)
	case entry.Level == "error" && (strings.Contains(entry.Error, "rateLimited") || strings.Contains(entry.Error, "rate limit")):
		m.handleRateLimit(entry)
	case entry.Logger == "tls" && (strings.Contains(entry.Msg, "obtained") || strings.Contains(entry.Msg, "renewed")):
		m.handleCertSuccess(entry)
	case entry.Logger == "tls" && entry.Level == "error" && strings.Contains(entry.Msg, "renewal"):
		m.handleCertRenewalFail(entry)
	case entry.Level == "error" && strings.Contains(entry.Error, "acme_"):
		m.handleCertError(entry)
	case entry.Level == "error" && strings.Contains(entry.Error, "dns") && strings.Contains(entry.Error, "challenge"):
		m.handleACMEDNSError(entry)
	case entry.Level == "error" && strings.Contains(entry.Error, "timeout"):
		m.handleACMETimeout(entry)
	}
}

func (m *LogMonitor) handle502(entry caddyLogEntry, status int) {
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

	m.mu.Lock()
	tracker := m.rateLimitTrack
	m.mu.Unlock()

	// Check if already rate-limited (prevent duplicate alerts)
	if tracker != nil && tracker.IsRateLimited(domain) {
		slog.Debug("domain already rate-limited, skipping alert", "domain", domain)
		return
	}

	slog.Warn("ACME rate limit detected", "domain", domain, "error", entry.Error)

	// Mark as rate-limited
	if tracker != nil {
		tracker.MarkRateLimited(domain)
	}

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

	// Record event
	m.mu.Lock()
	recorder := m.eventRecorder
	m.mu.Unlock()
	if recorder != nil {
		recorder.Record(ServerEvent{
			Type:    "cert_failed",
			Domain:  domain,
			Message: reason,
		})
	}

	if m.reporter != nil {
		alert := AlertCertFailed(domain, reason)
		if err := m.reporter.SendErrorAlert(alert); err != nil {
			slog.Error("failed to send cert error alert", "error", err)
		}
	}
}

func (m *LogMonitor) handleCertSuccess(entry caddyLogEntry) {
	domain := extractDomainFromEntry(entry)
	if domain == "" {
		return
	}

	slog.Info("certificate event", "domain", domain, "msg", entry.Msg)

	// Record event
	m.mu.Lock()
	recorder := m.eventRecorder
	tracker := m.rateLimitTrack
	m.mu.Unlock()

	if recorder != nil {
		eventType := "cert_issued"
		if strings.Contains(entry.Msg, "renewed") {
			eventType = "cert_renewed"
		}
		recorder.Record(ServerEvent{
			Type:    eventType,
			Domain:  domain,
			Message: entry.Msg,
		})
	}

	// Clear rate limit on successful issuance
	if tracker != nil {
		tracker.ClearRateLimit(domain)
	}
}

func (m *LogMonitor) handleCertRenewalFail(entry caddyLogEntry) {
	domain := extractDomainFromEntry(entry)
	if domain == "" {
		return
	}

	reason := entry.Error
	if reason == "" {
		reason = entry.Msg
	}

	slog.Warn("certificate renewal failed", "domain", domain, "reason", reason)

	// Record event
	m.mu.Lock()
	recorder := m.eventRecorder
	m.mu.Unlock()
	if recorder != nil {
		recorder.Record(ServerEvent{
			Type:    "cert_renewal_failed",
			Domain:  domain,
			Message: reason,
		})
	}

	if m.reporter != nil {
		alert := AlertCertRenewalFailed(domain, reason)
		if err := m.reporter.SendErrorAlert(alert); err != nil {
			slog.Error("failed to send cert renewal alert", "error", err)
		}
	}
}

func (m *LogMonitor) handleACMEDNSError(entry caddyLogEntry) {
	domain := extractDomainFromEntry(entry)

	slog.Warn("ACME DNS challenge failed", "domain", domain, "error", entry.Error)

	if m.reporter != nil {
		alert := AlertACMEDNSError(domain, entry.Error)
		if err := m.reporter.SendErrorAlert(alert); err != nil {
			slog.Error("failed to send ACME DNS alert", "error", err)
		}
	}
}

func (m *LogMonitor) handleACMETimeout(entry caddyLogEntry) {
	domain := extractDomainFromEntry(entry)

	slog.Warn("ACME connection timeout", "domain", domain, "error", entry.Error)

	if m.reporter != nil {
		alert := AlertACMETimeout(domain)
		if err := m.reporter.SendErrorAlert(alert); err != nil {
			slog.Error("failed to send ACME timeout alert", "error", err)
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
	if entry.Request != nil && entry.Request.Host != "" {
		return entry.Request.Host
	}

	if entry.TLS != nil && entry.TLS.ServerName != "" {
		return entry.TLS.ServerName
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
