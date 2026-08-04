package metrics

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Layer 4C Step 5 — Alert Generation.
//
// An alert is a structured, human-readable event: what happened (plain
// English), how severe it is, what the user can do about it, when it fired
// and when it resolved. Alerts are generated on state transitions (Step 4),
// deduplicated (one per condition until it resolves), rate limited (5D), and
// persisted by the control plane (Step 5 done conditions).

// Alert is the payload sent to the control plane as an
// {"type":"anomaly_alert","payload":{...}} message.
//
// `Level` is a legacy alias retained for backward compatibility with older
// control-plane handlers: "warning" | "critical" | "resolved". The richer
// Step 5 fields are `Severity` (warning|critical) and `Status`
// (active|resolved).
type Alert struct {
	ID         string                 `json:"id"`
	ServerID   string                 `json:"server_id"`
	Project    string                 `json:"project,omitempty"`
	Container  string                 `json:"container,omitempty"`
	Level      string                 `json:"level"`    // legacy alias
	Severity   string                 `json:"severity"` // warning | critical
	Type       string                 `json:"type"`     // machine-readable type
	Status     string                 `json:"status"`   // active | resolved
	Title      string                 `json:"title"`
	Message    string                 `json:"message"`
	Detail     string                 `json:"detail,omitempty"`
	Action     string                 `json:"action,omitempty"`
	FiredAt    string                 `json:"fired_at"`
	ResolvedAt *string                `json:"resolved_at,omitempty"`
	Metrics    map[string]interface{} `json:"metrics,omitempty"`
}

// alertSpec carries the raw data a detector closure has in scope; the Step 5
// renderer turns it into a full Alert using the plain-English templates.
type alertSpec struct {
	typ       string            // machine-readable type, e.g. "container_oom"
	project   string            // container alerts only
	container string            // container role (app, postgres, ...)
	params    map[string]string // template placeholders, e.g. {"project": "myshop"}
	metrics   map[string]interface{}
	subject   string // human-readable subject for resolved messages
}

// newAlertID returns a short unique alert id, e.g. "alert-4f2a9c".
func newAlertID() string {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("alert-%d", time.Now().UnixNano())
	}
	return "alert-" + hex.EncodeToString(b)
}

// Severity/Status/lifecycle helpers shared by the detector.

// isResolved reports whether the target transition clears the condition.
func isResolvedTarget(target alertSeverity) bool { return target == sevNormal }

// levelFromTarget maps a state-machine target to the legacy wire level.
func levelFromTarget(target alertSeverity) string {
	switch target {
	case sevCritical:
		return sevNameCritical
	case sevWarning:
		return sevNameWarning
	default:
		return sevNameResolved
	}
}

// severityFromTarget maps a state-machine target to the Step 5 severity.
func severityFromTarget(target alertSeverity) string {
	switch target {
	case sevCritical:
		return sevNameCritical
	default:
		return sevNameWarning
	}
}

// ---------------------------------------------------------------------------
// Rate limiting (Step 5D)
// ---------------------------------------------------------------------------

const (
	// Max 3 alerts per project per hour.
	projectAlertsPerHour = 3
	// Max 5 server-level alerts per hour.
	serverAlertsPerHour = 5
	// rateWindow is the rolling window for rate limiting.
	rateWindow = time.Hour
)

// rateBucket counts alerts fired for one scope (a project or the server)
// within a rolling hour window.
type rateBucket struct {
	hourStart      time.Time
	count          int
	suppressionSent bool
}

// rateKey returns the rate-limit scope for an alert: project alerts are
// limited per project, everything else per server.
func rateKey(a Alert) string {
	if a.Project != "" {
		return "project:" + a.Project
	}
	return "server"
}

func rateLimitFor(a Alert) int {
	if a.Project != "" {
		return projectAlertsPerHour
	}
	return serverAlertsPerHour
}

// rateLimited checks the bucket for the alert's scope. It returns true when
// the alert is allowed through, false when it must be suppressed. A
// suppression message is emitted at most once per hour per scope.
func (d *AnomalyDetector) rateLimited(a Alert) bool {
	if a.Status != "active" {
		// Resolved alerts are never rate limited.
		return true
	}
	key := rateKey(a)
	b, ok := d.rates[key]
	if !ok {
		b = &rateBucket{hourStart: d.now()}
		d.rates[key] = b
	}
	now := d.now()
	if now.Sub(b.hourStart) >= rateWindow {
		b.hourStart = now
		b.count = 0
		b.suppressionSent = false
	}
	if b.count >= rateLimitFor(a) {
		if !b.suppressionSent {
			b.suppressionSent = true
			d.sendSuppression(a)
		}
		return false
	}
	b.count++
	return true
}

// sendSuppression emits the "multiple alerts suppressed" message for a scope.
func (d *AnomalyDetector) sendSuppression(a Alert) {
	scope := "your server"
	if a.Project != "" {
		scope = a.Project
	}
	now := d.now().UTC()
	alert := Alert{
		ID:       newAlertID(),
		ServerID: d.serverID,
		Project:  a.Project,
		Level:    sevNameWarning,
		Severity: sevNameWarning,
		Type:     "alerts_suppressed",
		Status:   "active",
		Title:    "Some alerts were suppressed",
		Message: fmt.Sprintf(
			"Multiple issues detected with %s in the last hour. Some alerts were suppressed to avoid overwhelming you. Check the logs for details.",
			scope),
		FiredAt: now.Format(time.RFC3339),
	}
	d.emit(alert)
}

// emit sends one alert to the control plane.
func (d *AnomalyDetector) emit(a Alert) {
	if d.sender == nil {
		return
	}
	msg := map[string]interface{}{
		"type":    "anomaly_alert",
		"payload": a,
	}
	if err := d.sender.SendJSON(msg); err != nil {
		slog.Warn("failed to send anomaly alert", "type", a.Type, "level", a.Level, "error", err)
		return
	}
	// Drop the resolved noise from logs; keep warning/critical visible.
	if a.Level != sevNameResolved {
		slog.Info("anomaly alert", "level", a.Level, "type", a.Type,
			"project", strings.TrimSpace(a.Project), "container", strings.TrimSpace(a.Container))
	}
}
