package caddy

import (
	"strings"
	"testing"
	"time"
)

func TestAlert502Errors(t *testing.T) {
	alert := Alert502Errors("myshop.example.com", 5, 10)

	if alert.Level != "warning" {
		t.Errorf("expected level warning, got %s", alert.Level)
	}
	if alert.Type != "502_errors" {
		t.Errorf("expected type 502_errors, got %s", alert.Type)
	}
	if alert.Domain != "myshop.example.com" {
		t.Errorf("expected domain myshop.example.com, got %s", alert.Domain)
	}
	if !strings.Contains(alert.Message, "5 out of the last 10") {
		t.Errorf("expected message to contain failure counts, got: %s", alert.Message)
	}
	if !strings.Contains(alert.Message, "502 Bad Gateway") {
		t.Errorf("expected message to mention 502 Bad Gateway, got: %s", alert.Message)
	}
}

func TestAlertContainerDown(t *testing.T) {
	alert := AlertContainerDown("myshop.example.com", "myshop")

	if alert.Level != "critical" {
		t.Errorf("expected level critical, got %s", alert.Level)
	}
	if alert.Type != "container_down" {
		t.Errorf("expected type container_down, got %s", alert.Type)
	}
	if !strings.Contains(alert.Message, "myshop") {
		t.Errorf("expected message to mention project name, got: %s", alert.Message)
	}
}

func TestAlertContainerUpButFailing(t *testing.T) {
	alert := AlertContainerUpButFailing("myshop.example.com", "myshop")

	if alert.Level != "warning" {
		t.Errorf("expected level warning, got %s", alert.Level)
	}
	if alert.Type != "container_up_failing" {
		t.Errorf("expected type container_up_failing, got %s", alert.Type)
	}
}

func TestAlertRateLimit(t *testing.T) {
	resetDate := time.Now().Add(7 * 24 * time.Hour)
	alert := AlertRateLimit("shop.example.com", resetDate)

	if alert.Level != "warning" {
		t.Errorf("expected level warning, got %s", alert.Level)
	}
	if alert.Type != "rate_limit" {
		t.Errorf("expected type rate_limit, got %s", alert.Type)
	}
	if !strings.Contains(alert.Message, "Let's Encrypt rate limit") {
		t.Errorf("expected message to mention rate limit, got: %s", alert.Message)
	}
	if !strings.Contains(alert.Message, "days") {
		t.Errorf("expected message to mention days until reset, got: %s", alert.Message)
	}
}

func TestAlertCertFailed(t *testing.T) {
	alert := AlertCertFailed("shop.example.com", "DNS not pointing to server")

	if alert.Level != "critical" {
		t.Errorf("expected level critical, got %s", alert.Level)
	}
	if alert.Type != "cert_failed" {
		t.Errorf("expected type cert_failed, got %s", alert.Type)
	}
	if !strings.Contains(alert.Message, "DNS not pointing to server") {
		t.Errorf("expected message to mention reason, got: %s", alert.Message)
	}
}

func TestAlertPortMismatch(t *testing.T) {
	alert := AlertPortMismatch("myshop.example.com", 32847, 49152)

	if alert.Level != "warning" {
		t.Errorf("expected level warning, got %s", alert.Level)
	}
	if alert.Type != "port_mismatch" {
		t.Errorf("expected type port_mismatch, got %s", alert.Type)
	}
	if !strings.Contains(alert.Message, "32847") {
		t.Errorf("expected message to mention old port, got: %s", alert.Message)
	}
	if !strings.Contains(alert.Message, "49152") {
		t.Errorf("expected message to mention new port, got: %s", alert.Message)
	}
}
