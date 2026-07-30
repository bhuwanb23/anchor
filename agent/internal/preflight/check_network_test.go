package preflight

import (
	"net"
	"os"
	"testing"
)

func TestCheckInternetStructure(t *testing.T) {
	c := checkInternet()
	if c.Name != "internet" {
		t.Errorf("expected name 'internet', got '%s'", c.Name)
	}
	if c.Severity != SeverityBlocking {
		t.Errorf("expected blocking severity, got %s", c.Severity)
	}
	if c.Status != StatusPass && c.Status != StatusFail {
		t.Errorf("unexpected status: %s", c.Status)
	}
}

func TestCheckDNSStructure(t *testing.T) {
	c := checkDNS()
	if c.Name != "dns" {
		t.Errorf("expected name 'dns', got '%s'", c.Name)
	}
	if c.Severity != SeverityBlocking {
		t.Errorf("expected blocking severity, got %s", c.Severity)
	}
}

func TestCheckControlPlaneConnectStructure(t *testing.T) {
	c := checkControlPlaneConnect()
	if c.Name != "control_plane_connect" {
		t.Errorf("expected name 'control_plane_connect', got '%s'", c.Name)
	}
	if c.Severity != SeverityBlocking {
		t.Errorf("expected blocking severity, got %s", c.Severity)
	}
}

func TestCheckPortStructure(t *testing.T) {
	t.Run("port 80", func(t *testing.T) {
		c := checkPort(80, "HTTP")
		if c.Name != "port_80" {
			t.Errorf("expected name 'port_80', got '%s'", c.Name)
		}
		if c.Severity != SeverityBlocking {
			t.Errorf("expected blocking severity, got %s", c.Severity)
		}
	})

	t.Run("port 443", func(t *testing.T) {
		c := checkPort(443, "HTTPS")
		if c.Name != "port_443" {
			t.Errorf("expected name 'port_443', got '%s'", c.Name)
		}
		if c.Severity != SeverityBlocking {
			t.Errorf("expected blocking severity, got %s", c.Severity)
		}
	})
}

func TestReadProcessName(t *testing.T) {
	name := readProcessName(os.Getpid())
	if name == "" || name == "unknown" {
		t.Logf("current process name: %s (may be empty in test runner)", name)
	}
}

func TestFindProcessOnPort(t *testing.T) {
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Skipf("cannot start listener: %v", err)
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	pid, name := findProcessOnPort(port)
	t.Logf("port %d: pid=%d name=%s", port, pid, name)
}

func TestHasActiveSites(t *testing.T) {
	if hasActiveSites("apache2") {
		t.Log("apache2 has active sites (or directory not found)")
	}
	if hasActiveSites("nginx") {
		t.Log("nginx has active sites (or directory not found)")
	}
	if hasActiveSites("caddy") {
		t.Error("expected false for unknown service")
	}
}
