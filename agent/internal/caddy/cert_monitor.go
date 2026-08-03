package caddy

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

const (
	defaultCheckInterval = 24 * time.Hour
	warningThreshold     = 14 * 24 * time.Hour // 14 days
	criticalThreshold    = 7 * 24 * time.Hour  // 7 days
)

// AlertLevel represents the severity of a certificate alert.
type AlertLevel string

const (
	AlertWarning  AlertLevel = "warning"
	AlertCritical AlertLevel = "critical"
	AlertExpired  AlertLevel = "expired"
)

// CertificateAlert represents a certificate alert to send.
type CertificateAlert struct {
	Level   AlertLevel `json:"level"`
	Domain  string     `json:"domain"`
	Message string     `json:"message"`
	Expiry  time.Time  `json:"expiry"`
}

// AlertReporter sends certificate alerts to the control plane.
type AlertReporter interface {
	SendCertificateAlert(alert CertificateAlert) error
}

// CertStateUpdater updates certificate state tracking.
type CertStateUpdater interface {
	SetCertificate(domain, expiry, issuer, status string) error
}

// CertMonitor periodically checks certificate expiry and sends alerts.
type CertMonitor struct {
	certDir   string
	interval  time.Duration
	stateMgr  CertStateUpdater
	reporter  AlertReporter
}

// NewCertMonitor creates a new certificate monitor.
func NewCertMonitor(certDir string, stateMgr CertStateUpdater, reporter AlertReporter) *CertMonitor {
	return &CertMonitor{
		certDir:  certDir,
		interval: defaultCheckInterval,
		stateMgr: stateMgr,
		reporter: reporter,
	}
}

// Run starts the certificate monitoring loop. It checks every interval.
func (cm *CertMonitor) Run(ctx context.Context) {
	slog.Info("certificate monitor started", "interval", cm.interval, "cert_dir", cm.certDir)

	ticker := time.NewTicker(cm.interval)
	defer ticker.Stop()

	// Run initial check immediately
	cm.check(ctx)

	for {
		select {
		case <-ctx.Done():
			slog.Info("certificate monitor stopped")
			return
		case <-ticker.C:
			cm.check(ctx)
		}
	}
}

func (cm *CertMonitor) check(ctx context.Context) {
	certs, err := ScanCertificates(cm.certDir)
	if err != nil {
		slog.Error("certificate monitor: scan failed", "error", err)
		return
	}

	if len(certs) == 0 {
		slog.Debug("certificate monitor: no certificates found")
		return
	}

	now := time.Now()
	for _, cert := range certs {
		remaining := cert.Expiry.Sub(now)

		var status string
		var level AlertLevel

		switch {
		case remaining <= 0:
			status = "expired"
			level = AlertExpired
		case remaining <= criticalThreshold:
			status = "expiring_soon"
			level = AlertCritical
		case remaining <= warningThreshold:
			status = "expiring_soon"
			level = AlertWarning
		default:
			status = "valid"
		}

		// Update state
		if cm.stateMgr != nil {
			_ = cm.stateMgr.SetCertificate(
				cert.Domain,
				cert.Expiry.Format(time.RFC3339),
				cert.Issuer,
				status,
			)
		}

		// Send alert if not valid
		if status != "valid" {
			daysLeft := int(remaining.Hours() / 24)
			if daysLeft < 0 {
				daysLeft = 0
			}

			alert := CertificateAlert{
				Level:  level,
				Domain: cert.Domain,
				Message: fmt.Sprintf(
					"HTTPS certificate for %s expires in %d days and automatic renewal appears to have failed. "+
						"This means your site will show certificate errors. "+
						"Common causes: domain no longer points to this server, port 80 is blocked (needed for renewal verification). "+
						"Please check your DNS settings and ensure port 80 is accessible.",
					cert.Domain, daysLeft,
				),
				Expiry: cert.Expiry,
			}

			if cm.reporter != nil {
				if err := cm.reporter.SendCertificateAlert(alert); err != nil {
					slog.Error("certificate monitor: failed to send alert", "domain", cert.Domain, "error", err)
				} else {
					slog.Warn("certificate alert sent", "domain", cert.Domain, "level", level, "days_left", daysLeft)
				}
			}
		} else {
			slog.Debug("certificate monitor: certificate valid", "domain", cert.Domain, "expiry", cert.Expiry)
		}
	}
}
