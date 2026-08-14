package preflight

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

func checkSystemd() CheckResult {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return CheckResult{
			Name:           "systemd",
			DisplayName:    "Systemd",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        "systemctl not found in PATH — systemd does not appear to be installed",
			FixInstruction: "The agent requires systemd. Install systemd for your distribution (e.g., apt-get install systemd).",
		}
	}

	out, err := exec.Command("systemctl", "is-system-running").Output()
	if err != nil {
		return CheckResult{
			Name:           "systemd",
			DisplayName:    "Systemd",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        fmt.Sprintf("systemctl is-system-running failed: %v", strings.TrimSpace(string(out))),
			FixInstruction: "Ensure systemd is running. Run: sudo systemctl default",
		}
	}

	state := strings.TrimSpace(string(out))
	switch state {
	case "running":
		return CheckResult{
			Name:        "systemd",
			DisplayName: "Systemd",
			Status:      StatusPass,
			Severity:    SeverityBlocking,
			Message:     "systemd is running",
		}
	case "degraded":
		return CheckResult{
			Name:        "systemd",
			DisplayName: "Systemd",
			Status:      StatusPass,
			Severity:    SeverityBlocking,
			Message:     "systemd is running (degraded — some services have failed)",
		}
	case "initializing":
		return CheckResult{
			Name:           "systemd",
			DisplayName:    "Systemd",
			Status:         StatusWarn,
			Severity:       SeverityWarning,
			Message:        "systemd is still initializing — this should resolve shortly",
			FixInstruction: "Wait a moment and try again. If this persists, check systemd service status.",
		}
	case "starting":
		return CheckResult{
			Name:           "systemd",
			DisplayName:    "Systemd",
			Status:         StatusWarn,
			Severity:       SeverityWarning,
			Message:        "systemd is starting",
			FixInstruction: "Wait a moment and try again.",
		}
	case "stopping":
		return CheckResult{
			Name:           "systemd",
			DisplayName:    "Systemd",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        "systemd is stopping — the system may be shutting down",
			FixInstruction: "Try again after the system has finished its current operation.",
		}
	case "offline":
		return CheckResult{
			Name:           "systemd",
			DisplayName:    "Systemd",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        "systemd is not running — the system is in an offline/rescue state",
			FixInstruction: "Boot the system normally and ensure systemd starts on boot.",
		}
	default:
		return CheckResult{
			Name:           "systemd",
			DisplayName:    "Systemd",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        fmt.Sprintf("systemd is in an unexpected state: '%s'", state),
			FixInstruction: "Check: sudo systemctl is-system-running. If the state is unusual, consult your distribution's documentation.",
		}
	}
}

func checkDirectories() CheckResult {
	dirs := []string{
		"/etc/yourplatform",
		"/var/lib/yourplatform",
		"/var/log/yourplatform",
		"/tmp",
	}

	var created []string
	var failed []string

	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err == nil {
			if !info.IsDir() {
				failed = append(failed, dir+" exists but is not a directory")
				continue
			}
			tmpFile := filepath.Join(dir, ".yourplatform_write_test")
			if err := os.WriteFile(tmpFile, []byte("test"), 0644); err != nil {
				failed = append(failed, dir+" exists but is not writable")
			} else {
				os.Remove(tmpFile)
			}
			continue
		}

		if err := os.MkdirAll(dir, 0755); err != nil {
			failed = append(failed, fmt.Sprintf("%s could not be created: %v", dir, err))
		} else {
			created = append(created, dir)
		}
	}

	if len(failed) > 0 {
		return CheckResult{
			Name:           "directories",
			DisplayName:    "Required Directories",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        fmt.Sprintf("Directory issues: %s", strings.Join(failed, "; ")),
			FixInstruction: "Ensure the agent has permission to create and write to /etc/yourplatform/, /var/lib/yourplatform/, /var/log/yourplatform/. Run install as root or with sudo.",
		}
	}

	msg := "all required directories exist and are writable"
	status := StatusPass
	autoFixed := false
	if len(created) > 0 {
		msg = fmt.Sprintf("created missing directories: %s", strings.Join(created, ", "))
		status = StatusFixed
		autoFixed = true
	}

	return CheckResult{
		Name:        "directories",
		DisplayName: "Required Directories",
		Status:      status,
		Severity:    SeverityBlocking,
		Message:     msg,
		AutoFixed:   autoFixed,
	}
}

func checkConflictingAgent() CheckResult {
	currentPID := os.Getpid()

	procEntries, err := filepath.Glob("/proc/[0-9]*/cmdline")
	if err != nil {
		return CheckResult{
			Name:        "conflicting_agent",
			DisplayName: "Conflicting Agent",
			Status:      StatusPass,
			Severity:    SeverityBlocking,
			Message:     "could not scan running processes — skipping conflict check",
		}
	}

	for _, cmdlinePath := range procEntries {
		parts := strings.Split(cmdlinePath, "/")
		if len(parts) < 3 {
			continue
		}
		pid, err := strconv.Atoi(parts[2])
		if err != nil || pid == currentPID {
			continue
		}

		data, err := os.ReadFile(cmdlinePath)
		if err != nil {
			continue
		}

		cmdParts := strings.SplitN(string(data), "\x00", 2)
		if len(cmdParts) == 0 || cmdParts[0] == "" {
			continue
		}
		binary := filepath.Base(cmdParts[0])

		if binary == "yourplatform-agent" {
			return CheckResult{
				Name:           "conflicting_agent",
				DisplayName:    "Conflicting Agent",
				Status:         StatusFail,
				Severity:       SeverityBlocking,
				Message:        fmt.Sprintf("Another YourPlatform agent is already running (PID %d)", pid),
				FixInstruction: fmt.Sprintf("If this is an old agent: sudo systemctl stop yourplatform-agent && sudo kill %d. Then run the install command again.", pid),
			}
		}
	}

	return CheckResult{
		Name:        "conflicting_agent",
		DisplayName: "Conflicting Agent",
		Status:      StatusPass,
		Severity:    SeverityBlocking,
		Message:     "no conflicting agent processes found",
	}
}

func checkConfig() CheckResult {
	configPath := "/etc/yourplatform/config.yaml"

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return CheckResult{
			Name:        "config",
			DisplayName: "Agent Configuration",
			Status:      StatusPass,
			Severity:    SeverityBlocking,
			Message:     "no config file found at /etc/yourplatform/config.yaml — skipping (config will be created during install)",
		}
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return CheckResult{
			Name:           "config",
			DisplayName:    "Agent Configuration",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        fmt.Sprintf("config file at %s exists but is not readable: %v", configPath, err),
			FixInstruction: "Check file permissions: sudo chmod 600 " + configPath,
		}
	}

	raw := make(map[string]interface{})
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return CheckResult{
			Name:           "config",
			DisplayName:    "Agent Configuration",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        fmt.Sprintf("config file contains invalid YAML: %v", err),
			FixInstruction: "Restore the config file from backup or re-run the install command from your dashboard.",
		}
	}

	var missingFields []string

	if raw["control_plane_url"] == nil || raw["control_plane_url"] == "" {
		missingFields = append(missingFields, "control_plane_url")
	}

	hasToken := (raw["registration_token"] != nil && raw["registration_token"] != "") ||
		(raw["agent_token"] != nil && raw["agent_token"] != "")
	hasCredentials := (raw["agent_id"] != nil && raw["agent_id"] != "") && (raw["agent_secret"] != nil && raw["agent_secret"] != "")

	if !hasToken && !hasCredentials {
		missingFields = append(missingFields, "either registration_token (or agent_token) or agent_id+agent_secret")
	}

	if len(missingFields) > 0 {
		return CheckResult{
			Name:           "config",
			DisplayName:    "Agent Configuration",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        fmt.Sprintf("config file is missing required fields: %s", strings.Join(missingFields, ", ")),
			FixInstruction: "Reconnect this server by running a new install command from your dashboard. Your deployed apps will continue running — only management is affected.",
		}
	}

	return CheckResult{
		Name:        "config",
		DisplayName: "Agent Configuration",
		Status:      StatusPass,
		Severity:    SeverityBlocking,
		Message:     "config file is valid",
	}
}
