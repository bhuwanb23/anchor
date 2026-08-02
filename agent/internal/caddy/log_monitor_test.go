package caddy

import (
	"testing"
	"time"
)

// mockErrorAlerter records error alerts.
type mockErrorAlerter struct {
	alerts []ErrorAlert
}

func (m *mockErrorAlerter) SendErrorAlert(alert ErrorAlert) error {
	m.alerts = append(m.alerts, alert)
	return nil
}

func TestLogMonitor_502BelowThreshold(t *testing.T) {
	alerter := &mockErrorAlerter{}
	monitor := NewLogMonitor(LogMonitorConfig{
		Window:    1 * time.Minute,
		Threshold: 10,
	}, alerter)

	// Send 9 502s (below threshold)
	for i := 0; i < 9; i++ {
		monitor.ProcessLine(`{"level":"error","msg":"dialing backend","resp":{"status":502},"request":{"Host":"myshop.example.com"}}`)
	}

	if len(alerter.alerts) != 0 {
		t.Errorf("expected 0 alerts below threshold, got %d", len(alerter.alerts))
	}
	if monitor.Recent502Count() != 9 {
		t.Errorf("expected 9 recent 502s, got %d", monitor.Recent502Count())
	}
}

func TestLogMonitor_502ExceedsThreshold(t *testing.T) {
	alerter := &mockErrorAlerter{}
	monitor := NewLogMonitor(LogMonitorConfig{
		Window:    1 * time.Minute,
		Threshold: 10,
	}, alerter)

	// Send 10 502s (hits threshold)
	for i := 0; i < 10; i++ {
		monitor.ProcessLine(`{"level":"error","msg":"dialing backend","resp":{"status":502},"request":{"Host":"myshop.example.com"}}`)
	}

	if len(alerter.alerts) != 1 {
		t.Fatalf("expected 1 alert at threshold, got %d", len(alerter.alerts))
	}
	if alerter.alerts[0].Type != "502_errors" {
		t.Errorf("expected alert type 502_errors, got %s", alerter.alerts[0].Type)
	}
	if alerter.alerts[0].Domain != "myshop.example.com" {
		t.Errorf("expected domain myshop.example.com, got %s", alerter.alerts[0].Domain)
	}
	// Counter should be reset after alerting
	if monitor.Recent502Count() != 0 {
		t.Errorf("expected counter reset after alert, got %d", monitor.Recent502Count())
	}
}

func TestLogMonitor_502WindowExpiry(t *testing.T) {
	alerter := &mockErrorAlerter{}
	monitor := NewLogMonitor(LogMonitorConfig{
		Window:    50 * time.Millisecond,
		Threshold: 5,
	}, alerter)

	// Send 4 502s
	for i := 0; i < 4; i++ {
		monitor.ProcessLine(`{"level":"error","msg":"dialing backend","resp":{"status":502},"request":{"Host":"test.com"}}`)
	}

	// Wait for window to expire
	time.Sleep(100 * time.Millisecond)

	// Send 1 more (should not trigger because old ones expired)
	monitor.ProcessLine(`{"level":"error","msg":"dialing backend","resp":{"status":502},"request":{"Host":"test.com"}}`)

	if len(alerter.alerts) != 0 {
		t.Errorf("expected 0 alerts after window expiry, got %d", len(alerter.alerts))
	}
}

func TestLogMonitor_RateLimitDetection(t *testing.T) {
	alerter := &mockErrorAlerter{}
	monitor := NewLogMonitor(LogMonitorConfig{}, alerter)

	monitor.ProcessLine(`{"level":"error","msg":"obtain certificate","error":"rateLimited: too many certificates already issued"}`)

	if len(alerter.alerts) != 1 {
		t.Fatalf("expected 1 rate limit alert, got %d", len(alerter.alerts))
	}
	if alerter.alerts[0].Type != "rate_limit" {
		t.Errorf("expected alert type rate_limit, got %s", alerter.alerts[0].Type)
	}
}

func TestLogMonitor_CertErrorDetection(t *testing.T) {
	alerter := &mockErrorAlerter{}
	monitor := NewLogMonitor(LogMonitorConfig{}, alerter)

	monitor.ProcessLine(`{"level":"error","msg":"obtain certificate","error":"acme: certificate issuance failed"}`)

	if len(alerter.alerts) != 1 {
		t.Fatalf("expected 1 cert error alert, got %d", len(alerter.alerts))
	}
	if alerter.alerts[0].Type != "cert_failed" {
		t.Errorf("expected alert type cert_failed, got %s", alerter.alerts[0].Type)
	}
}

func TestLogMonitor_NonJSONIgnored(t *testing.T) {
	alerter := &mockErrorAlerter{}
	monitor := NewLogMonitor(LogMonitorConfig{}, alerter)

	monitor.ProcessLine("not json at all")
	monitor.ProcessLine("")
	monitor.ProcessLine("   ")

	if len(alerter.alerts) != 0 {
		t.Errorf("expected 0 alerts for non-JSON, got %d", len(alerter.alerts))
	}
}

func TestLogMonitor_200Ignored(t *testing.T) {
	alerter := &mockErrorAlerter{}
	monitor := NewLogMonitor(LogMonitorConfig{}, alerter)

	monitor.ProcessLine(`{"level":"info","msg":"GET","resp":{"status":200}}`)

	if len(alerter.alerts) != 0 {
		t.Errorf("expected 0 alerts for 200 status, got %d", len(alerter.alerts))
	}
}

func TestLogMonitor_NilReporter(t *testing.T) {
	monitor := NewLogMonitor(LogMonitorConfig{}, nil)

	// Should not panic
	monitor.ProcessLine(`{"level":"error","msg":"test","resp":{"status":502},"request":{"Host":"test.com"}}`)
	monitor.ProcessLine(`{"level":"error","msg":"obtain","error":"rateLimited"}`)
	monitor.ProcessLine(`{"level":"error","msg":"obtain","error":"acme_error"}`)
	monitor.ProcessLine(`{"logger":"tls","msg":"obtained certificate","request":{"Host":"t.com"}}`)
	monitor.ProcessLine(`{"logger":"tls","level":"error","msg":"renewal failed","error":"timeout"}`)
	monitor.ProcessLine(`{"level":"error","msg":"challenge","error":"dns challenge failed"}`)
	monitor.ProcessLine(`{"level":"error","msg":"connect","error":"connection timeout"}`)
}

func TestLogMonitor_RateLimitDedup(t *testing.T) {
	alerter := &mockErrorAlerter{}
	tracker := NewRateLimitTracker(t.TempDir())
	monitor := NewLogMonitor(LogMonitorConfig{}, alerter)
	monitor.SetRateLimitTracker(tracker)

	// First rate limit hit
	monitor.ProcessLine(`{"level":"error","msg":"obtain","error":"rateLimited","request":{"Host":"dup.com"}}`)
	if len(alerter.alerts) != 1 {
		t.Fatalf("expected 1 alert on first hit, got %d", len(alerter.alerts))
	}

	// Second rate limit hit — should be deduplicated
	monitor.ProcessLine(`{"level":"error","msg":"obtain","error":"rateLimited","request":{"Host":"dup.com"}}`)
	if len(alerter.alerts) != 1 {
		t.Errorf("expected 1 alert after dedup, got %d", len(alerter.alerts))
	}
}

func TestLogMonitor_CertSuccess(t *testing.T) {
	alerter := &mockErrorAlerter{}
	recorder := NewEventRecorder(t.TempDir())
	monitor := NewLogMonitor(LogMonitorConfig{}, alerter)
	monitor.SetEventRecorder(recorder)

	monitor.ProcessLine(`{"logger":"tls","msg":"obtained certificate","request":{"Host":"success.com"}}`)

	// No alert for success
	if len(alerter.alerts) != 0 {
		t.Errorf("expected 0 alerts for cert success, got %d", len(alerter.alerts))
	}
	// Event should be recorded
	if recorder.Count() != 1 {
		t.Fatalf("expected 1 event, got %d", recorder.Count())
	}
	events := recorder.GetAll()
	if events[0].Type != "cert_issued" {
		t.Errorf("expected event type cert_issued, got %s", events[0].Type)
	}
}

func TestLogMonitor_CertRenewal(t *testing.T) {
	alerter := &mockErrorAlerter{}
	recorder := NewEventRecorder(t.TempDir())
	monitor := NewLogMonitor(LogMonitorConfig{}, alerter)
	monitor.SetEventRecorder(recorder)

	monitor.ProcessLine(`{"logger":"tls","msg":"renewed certificate","request":{"Host":"renewed.com"}}`)

	if recorder.Count() != 1 {
		t.Fatalf("expected 1 event, got %d", recorder.Count())
	}
	events := recorder.GetAll()
	if events[0].Type != "cert_renewed" {
		t.Errorf("expected event type cert_renewed, got %s", events[0].Type)
	}
}

func TestLogMonitor_CertRenewalFail(t *testing.T) {
	alerter := &mockErrorAlerter{}
	recorder := NewEventRecorder(t.TempDir())
	monitor := NewLogMonitor(LogMonitorConfig{}, alerter)
	monitor.SetEventRecorder(recorder)

	monitor.ProcessLine(`{"logger":"tls","level":"error","msg":"renewal failed","error":"connection refused","request":{"Host":"fail.com"}}`)

	if len(alerter.alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerter.alerts))
	}
	if alerter.alerts[0].Type != "cert_renewal_failed" {
		t.Errorf("expected alert type cert_renewal_failed, got %s", alerter.alerts[0].Type)
	}
	if recorder.Count() != 1 {
		t.Fatalf("expected 1 event, got %d", recorder.Count())
	}
}

func TestLogMonitor_ACMEDNSError(t *testing.T) {
	alerter := &mockErrorAlerter{}
	monitor := NewLogMonitor(LogMonitorConfig{}, alerter)

	monitor.ProcessLine(`{"level":"error","msg":"challenge","error":"dns challenge failed for example.com"}`)

	if len(alerter.alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerter.alerts))
	}
	if alerter.alerts[0].Type != "acme_dns_error" {
		t.Errorf("expected alert type acme_dns_error, got %s", alerter.alerts[0].Type)
	}
}

func TestLogMonitor_ACMETimeout(t *testing.T) {
	alerter := &mockErrorAlerter{}
	monitor := NewLogMonitor(LogMonitorConfig{}, alerter)

	monitor.ProcessLine(`{"level":"error","msg":"connect","error":"connection timeout to Let's Encrypt"}`)

	if len(alerter.alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerter.alerts))
	}
	if alerter.alerts[0].Type != "acme_timeout" {
		t.Errorf("expected alert type acme_timeout, got %s", alerter.alerts[0].Type)
	}
}

func TestLogMonitor_CertSuccessClearsRateLimit(t *testing.T) {
	tracker := NewRateLimitTracker(t.TempDir())
	recorder := NewEventRecorder(t.TempDir())
	alerter := &mockErrorAlerter{}
	monitor := NewLogMonitor(LogMonitorConfig{}, alerter)
	monitor.SetRateLimitTracker(tracker)
	monitor.SetEventRecorder(recorder)

	// Mark as rate-limited
	tracker.MarkRateLimited("clear.com")
	if !tracker.IsRateLimited("clear.com") {
		t.Fatal("expected domain to be rate-limited")
	}

	// Cert success should clear rate limit
	monitor.ProcessLine(`{"logger":"tls","msg":"obtained certificate","request":{"Host":"clear.com"}}`)

	if tracker.IsRateLimited("clear.com") {
		t.Error("expected rate limit to be cleared after cert success")
	}
}
