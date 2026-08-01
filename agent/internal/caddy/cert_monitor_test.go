package caddy

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// mockCertStateUpdater records SetCertificate calls.
type mockCertStateUpdater struct {
	calls []setCertCall
}

type setCertCall struct {
	domain, expiry, issuer, status string
}

func (m *mockCertStateUpdater) SetCertificate(domain, expiry, issuer, status string) error {
	m.calls = append(m.calls, setCertCall{domain, expiry, issuer, status})
	return nil
}

// mockAlertReporter records alerts sent.
type mockAlertReporter struct {
	alerts []CertificateAlert
}

func (m *mockAlertReporter) SendCertificateAlert(alert CertificateAlert) error {
	m.alerts = append(m.alerts, alert)
	return nil
}

func createMonitorCert(t *testing.T, dir, filename string, domain string, expiry time.Time) {
	t.Helper()
	certDir := filepath.Join(dir, "certificates")
	os.MkdirAll(certDir, 0755)
	createTestCert(t, certDir, filename, domain, expiry)
}

func TestCertMonitor_ValidCert(t *testing.T) {
	dir := t.TempDir()
	// Create a valid cert (30 days out)
	createMonitorCert(t, dir, "cert.pem", "valid.com", time.Now().Add(30*24*time.Hour))

	stateUpdater := &mockCertStateUpdater{}
	alertReporter := &mockAlertReporter{}
	cm := NewCertMonitor(dir, stateUpdater, alertReporter)
	cm.check(t.Context())

	if len(stateUpdater.calls) != 1 {
		t.Fatalf("expected 1 state update, got %d", len(stateUpdater.calls))
	}
	if stateUpdater.calls[0].status != "valid" {
		t.Errorf("expected status valid, got %s", stateUpdater.calls[0].status)
	}
	if len(alertReporter.alerts) != 0 {
		t.Errorf("expected 0 alerts for valid cert, got %d", len(alertReporter.alerts))
	}
}

func TestCertMonitor_ExpiredCert(t *testing.T) {
	dir := t.TempDir()
	createMonitorCert(t, dir, "expired.pem", "expired.com", time.Now().Add(-24*time.Hour))

	stateUpdater := &mockCertStateUpdater{}
	alertReporter := &mockAlertReporter{}
	cm := NewCertMonitor(dir, stateUpdater, alertReporter)
	cm.check(t.Context())

	if len(stateUpdater.calls) != 1 {
		t.Fatalf("expected 1 state update, got %d", len(stateUpdater.calls))
	}
	if stateUpdater.calls[0].status != "expired" {
		t.Errorf("expected status expired, got %s", stateUpdater.calls[0].status)
	}
	if len(alertReporter.alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alertReporter.alerts))
	}
	if alertReporter.alerts[0].Level != AlertExpired {
		t.Errorf("expected alert level expired, got %s", alertReporter.alerts[0].Level)
	}
	if alertReporter.alerts[0].Domain != "expired.com" {
		t.Errorf("expected domain expired.com, got %s", alertReporter.alerts[0].Domain)
	}
}

func TestCertMonitor_WarningCert(t *testing.T) {
	dir := t.TempDir()
	// 10 days out — within warning threshold (14 days) but not critical (7 days)
	createMonitorCert(t, dir, "warning.pem", "warning.com", time.Now().Add(10*24*time.Hour))

	stateUpdater := &mockCertStateUpdater{}
	alertReporter := &mockAlertReporter{}
	cm := NewCertMonitor(dir, stateUpdater, alertReporter)
	cm.check(t.Context())

	if len(stateUpdater.calls) != 1 {
		t.Fatalf("expected 1 state update, got %d", len(stateUpdater.calls))
	}
	if stateUpdater.calls[0].status != "expiring_soon" {
		t.Errorf("expected status expiring_soon, got %s", stateUpdater.calls[0].status)
	}
	if len(alertReporter.alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alertReporter.alerts))
	}
	if alertReporter.alerts[0].Level != AlertWarning {
		t.Errorf("expected alert level warning, got %s", alertReporter.alerts[0].Level)
	}
}

func TestCertMonitor_CriticalCert(t *testing.T) {
	dir := t.TempDir()
	// 5 days out — within critical threshold (7 days)
	createMonitorCert(t, dir, "critical.pem", "critical.com", time.Now().Add(5*24*time.Hour))

	stateUpdater := &mockCertStateUpdater{}
	alertReporter := &mockAlertReporter{}
	cm := NewCertMonitor(dir, stateUpdater, alertReporter)
	cm.check(t.Context())

	if len(stateUpdater.calls) != 1 {
		t.Fatalf("expected 1 state update, got %d", len(stateUpdater.calls))
	}
	if stateUpdater.calls[0].status != "expiring_soon" {
		t.Errorf("expected status expiring_soon, got %s", stateUpdater.calls[0].status)
	}
	if len(alertReporter.alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alertReporter.alerts))
	}
	if alertReporter.alerts[0].Level != AlertCritical {
		t.Errorf("expected alert level critical, got %s", alertReporter.alerts[0].Level)
	}
}

func TestCertMonitor_NoCerts(t *testing.T) {
	dir := t.TempDir()

	stateUpdater := &mockCertStateUpdater{}
	alertReporter := &mockAlertReporter{}
	cm := NewCertMonitor(dir, stateUpdater, alertReporter)
	cm.check(t.Context())

	if len(stateUpdater.calls) != 0 {
		t.Errorf("expected 0 state updates, got %d", len(stateUpdater.calls))
	}
	if len(alertReporter.alerts) != 0 {
		t.Errorf("expected 0 alerts, got %d", len(alertReporter.alerts))
	}
}

func TestCertMonitor_NilStateUpdater(t *testing.T) {
	dir := t.TempDir()
	createMonitorCert(t, dir, "cert.pem", "test.com", time.Now().Add(-24*time.Hour))

	alertReporter := &mockAlertReporter{}
	cm := NewCertMonitor(dir, nil, alertReporter)
	cm.check(t.Context()) // should not panic

	if len(alertReporter.alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alertReporter.alerts))
	}
}

func TestCertMonitor_NilReporter(t *testing.T) {
	dir := t.TempDir()
	createMonitorCert(t, dir, "cert.pem", "test.com", time.Now().Add(-24*time.Hour))

	stateUpdater := &mockCertStateUpdater{}
	cm := NewCertMonitor(dir, stateUpdater, nil)
	cm.check(t.Context()) // should not panic

	if len(stateUpdater.calls) != 1 {
		t.Errorf("expected 1 state update, got %d", len(stateUpdater.calls))
	}
}
