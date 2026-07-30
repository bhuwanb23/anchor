package preflight

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func checkInternet() CheckResult {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Get("https://1.1.1.1")
	if err != nil {
		return CheckResult{
			Name:           "internet",
			DisplayName:    "Internet Connectivity",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        "Your server cannot reach the internet. This is required to pull app images and connect to the YourPlatform control plane.",
			FixInstruction: "Check your hosting provider's firewall settings and ensure outbound traffic is allowed. Try: curl -I https://1.1.1.1",
		}
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	return CheckResult{
		Name:        "internet",
		DisplayName: "Internet Connectivity",
		Status:      StatusPass,
		Severity:    SeverityBlocking,
		Message:     "server can reach the internet",
	}
}

func checkDNS() CheckResult {
	_, err := net.LookupHost("google.com")
	if err != nil {
		return CheckResult{
			Name:           "dns",
			DisplayName:    "DNS Resolution",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        "Your server cannot resolve domain names.",
			FixInstruction: "Check /etc/resolv.conf and ensure a nameserver is configured. Try: cat /etc/resolv.conf",
		}
	}

	_, err = net.LookupHost("api.yourplatform.com")
	if err != nil {
		return CheckResult{
			Name:           "dns",
			DisplayName:    "DNS Resolution",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        "Your server can reach the internet but cannot resolve api.yourplatform.com.",
			FixInstruction: "This may be a temporary DNS issue. Wait 60 seconds and try again. If this persists, contact your hosting provider about DNS filtering.",
		}
	}

	return CheckResult{
		Name:        "dns",
		DisplayName: "DNS Resolution",
		Status:      StatusPass,
		Severity:    SeverityBlocking,
		Message:     "DNS is working correctly",
	}
}

// searchProcNetTCP reads /proc/net/tcp or /proc/net/tcp6 to find socket inode for the given port.
func searchProcNetTCP(path string, portHex string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		addrParts := strings.Split(fields[1], ":")
		if len(addrParts) == 2 && addrParts[1] == portHex {
			if len(fields[3]) >= 2 && fields[3] == "0A" {
				return fields[9]
			}
		}
	}
	return ""
}

// findProcessOnPort reads /proc/net/tcp and /proc/net/tcp6 to find which process
// is listening on the given port. Handles both IPv4 and IPv6.
func findProcessOnPort(port int) (int, string) {
	portHex := fmt.Sprintf("%04X", port)

	targetInode := searchProcNetTCP("/proc/net/tcp", portHex)
	if targetInode == "" {
		targetInode = searchProcNetTCP("/proc/net/tcp6", portHex)
	}
	if targetInode == "" {
		return 0, ""
	}

	procEntries, _ := filepath.Glob("/proc/[0-9]*/fd/*")
	for _, fdPath := range procEntries {
		link, err := os.Readlink(fdPath)
		if err != nil {
			continue
		}
		if strings.HasPrefix(link, "socket:[") && strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]") == targetInode {
			parts := strings.Split(fdPath, "/")
			if len(parts) >= 3 {
				pid, _ := strconv.Atoi(parts[2])
				name := readProcessName(pid)
				return pid, name
			}
		}
	}

	return 0, ""
}

func readProcessName(pid int) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return "unknown"
	}
	parts := strings.SplitN(string(data), "\x00", 2)
	if len(parts) > 0 && parts[0] != "" {
		return filepath.Base(parts[0])
	}
	return "unknown"
}

// hasActiveSites checks if apache2 or nginx has active sites configured.
func hasActiveSites(name string) bool {
	switch name {
	case "apache2":
		dir := "/etc/apache2/sites-enabled"
		entries, err := filepath.Glob(filepath.Join(dir, "*"))
		if err != nil || len(entries) == 0 {
			return false
		}
		for _, e := range entries {
			base := filepath.Base(e)
			if !strings.HasPrefix(base, "000-") && !strings.HasPrefix(base, "default") {
				return true
			}
		}
		return false
	case "httpd":
		dir := "/etc/httpd/conf.d"
		entries, err := filepath.Glob(filepath.Join(dir, "*.conf"))
		if err != nil || len(entries) == 0 {
			return false
		}
		for _, e := range entries {
			base := filepath.Base(e)
			if base != "welcome.conf" && !strings.Contains(base, "ssl") && !strings.HasPrefix(base, "README") {
				return true
			}
		}
		return false
	case "nginx":
		dir := "/etc/nginx/sites-enabled"
		entries, err := filepath.Glob(filepath.Join(dir, "*"))
		if err != nil || len(entries) == 0 {
			return false
		}
		for _, e := range entries {
			base := filepath.Base(e)
			if !strings.HasPrefix(base, "default") {
				return true
			}
		}
		return false
	}
	return false
}

func checkPort(portNum int, label string) CheckResult {
	name := fmt.Sprintf("port_%d", portNum)
	displayName := fmt.Sprintf("Port %d (%s)", portNum, label)

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", portNum))
	if err == nil {
		ln.Close()
		return CheckResult{
			Name:        name,
			DisplayName: displayName,
			Status:      StatusPass,
			Severity:    SeverityBlocking,
			Message:     fmt.Sprintf("port %d is available", portNum),
		}
	}

	pid, procName := findProcessOnPort(portNum)
	if pid == 0 {
		return CheckResult{
			Name:           name,
			DisplayName:    displayName,
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        fmt.Sprintf("Port %d is in use by an unknown process.", portNum),
			FixInstruction: fmt.Sprintf("Identify the process: sudo ss -tlnp | grep :%d", portNum),
		}
	}

	msg := fmt.Sprintf("Port %d is in use by %s (PID %d).", portNum, procName, pid)

	switch procName {
	case "apache2", "httpd":
		if !hasActiveSites(procName) {
			_ = exec.Command("systemctl", "stop", "apache2").Run()
			_ = exec.Command("systemctl", "disable", "apache2").Run()
			ln, err := net.Listen("tcp", fmt.Sprintf(":%d", portNum))
			if err == nil {
				ln.Close()
				return CheckResult{
					Name:      name,
					DisplayName: displayName,
					Status:    StatusFixed,
					Severity:  SeverityBlocking,
					Message:   fmt.Sprintf("Apache was using port %d but had no active sites — agent stopped it", portNum),
					AutoFixed: true,
				}
			}
			ln.Close()
		}
		return CheckResult{
			Name:           name,
			DisplayName:    displayName,
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        msg + " Apache appears to serve active sites, so the agent cannot stop it automatically.",
			FixInstruction: "Either move those sites to YourPlatform first, or configure Apache to proxy to YourPlatform. See docs for details.",
		}
	case "nginx":
		if !hasActiveSites(procName) {
			_ = exec.Command("systemctl", "stop", "nginx").Run()
			_ = exec.Command("systemctl", "disable", "nginx").Run()
			ln, err := net.Listen("tcp", fmt.Sprintf(":%d", portNum))
			if err == nil {
				ln.Close()
				return CheckResult{
					Name:      name,
					DisplayName: displayName,
					Status:    StatusFixed,
					Severity:  SeverityBlocking,
					Message:   fmt.Sprintf("Nginx was using port %d but had no active sites — agent stopped it", portNum),
					AutoFixed: true,
				}
			}
			ln.Close()
		}
		return CheckResult{
			Name:           name,
			DisplayName:    displayName,
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        msg + " Nginx appears to serve active sites, so the agent cannot stop it automatically.",
			FixInstruction: "Either move those sites to YourPlatform first, or configure Nginx to proxy to YourPlatform.",
		}
	default:
		return CheckResult{
			Name:           name,
			DisplayName:    displayName,
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        msg,
			FixInstruction: fmt.Sprintf("Stop the process: sudo systemctl stop %s (or kill -9 %d). Then run the installer again.", procName, pid),
		}
	}
}

func checkControlPlaneConnect() CheckResult {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
	}
	conn, err := dialer.Dial("tcp", "api.yourplatform.com:443")
	if err != nil {
		return CheckResult{
			Name:           "control_plane_connect",
			DisplayName:    "Control Plane Connectivity",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        "Your server cannot reach api.yourplatform.com on port 443. The agent needs this connection to receive deployment commands.",
			FixInstruction: "Check your hosting provider's firewall settings and ensure outbound TCP port 443 is allowed to all destinations. Common providers: AWS EC2 (Security Group outbound rules), Google Cloud (VPC firewall), Hetzner Cloud (Firewall rules).",
		}
	}
	conn.Close()

	return CheckResult{
		Name:        "control_plane_connect",
		DisplayName: "Control Plane Connectivity",
		Status:      StatusPass,
		Severity:    SeverityBlocking,
		Message:     "server can reach the control plane on port 443",
	}
}
