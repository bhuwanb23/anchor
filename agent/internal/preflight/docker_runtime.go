package preflight

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

func checkDockerDaemon() CheckResult {
	out, err := exec.Command("docker", "version", "--format", "{{.Server.Version}}").Output()
	if err == nil {
		return CheckResult{
			Name:        "docker_daemon",
			DisplayName: "Docker Daemon",
			Status:      StatusPass,
			Severity:    SeverityBlocking,
			Message:     fmt.Sprintf("Docker daemon is running (version %s)", strings.TrimSpace(string(out))),
		}
	}

	slog.Info("Docker daemon is not running. Attempting to start it...")
	_ = exec.Command("systemctl", "start", "docker").Run()
	_ = exec.Command("systemctl", "enable", "docker").Run()

	for i := 0; i < 10; i++ {
		time.Sleep(1 * time.Second)
		out, err := exec.Command("docker", "version", "--format", "{{.Server.Version}}").Output()
		if err == nil {
			return CheckResult{
				Name:        "docker_daemon",
				DisplayName: "Docker Daemon",
				Status:      StatusFixed,
				Severity:    SeverityBlocking,
				Message:     fmt.Sprintf("Docker daemon was not running — agent started it successfully (version %s)", strings.TrimSpace(string(out))),
				AutoFixed:   true,
			}
		}
	}

	journalOut, _ := exec.Command("journalctl", "-u", "docker", "-n", "10", "--no-pager").Output()
	dockerLogs := ""
	if len(journalOut) > 0 {
		dockerLogs = "\n\nDocker daemon logs:\n" + string(journalOut)
	}

	return CheckResult{
		Name:           "docker_daemon",
		DisplayName:    "Docker Daemon",
		Status:         StatusFail,
		Severity:       SeverityBlocking,
		Message:        "Docker is installed but the Docker daemon is not running and could not be started." + dockerLogs,
		FixInstruction: "Try manually: sudo systemctl start docker. Check: sudo journalctl -u docker -n 50. Common causes: kernel too old, missing overlay module, or conflicting container runtime.",
	}
}

func checkDockerVersion() CheckResult {
	out, err := exec.Command("docker", "version", "--format", "{{.Server.Version}}").Output()
	if err != nil {
		return CheckResult{
			Name:           "docker_version",
			DisplayName:    "Docker Version",
			Status:         StatusFail,
			Severity:       SeverityWarning,
			Message:        "Could not determine Docker version",
			FixInstruction: "Run: docker version",
		}
	}

	version := strings.TrimSpace(string(out))

	if !versionAtLeast(version, "20.10") {
		return CheckResult{
			Name:           "docker_version",
			DisplayName:    "Docker Version",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        fmt.Sprintf("Docker version %s is too old. Version 20.10 or newer is required.", version),
			FixInstruction: "Upgrade Docker: follow the upgrade instructions for your OS at https://docs.docker.com/engine/install/",
		}
	}

	if !versionAtLeast(version, "24") {
		return CheckResult{
			Name:           "docker_version",
			DisplayName:    "Docker Version",
			Status:         StatusWarn,
			Severity:       SeverityWarning,
			Message:        fmt.Sprintf("Docker version %s is installed. Version 24+ is recommended for latest security patches.", version),
			FixInstruction: "To upgrade: follow the upgrade instructions at https://docs.docker.com/engine/install/",
		}
	}

	return CheckResult{
		Name:        "docker_version",
		DisplayName: "Docker Version",
		Status:      StatusPass,
		Severity:    SeverityBlocking,
		Message:     fmt.Sprintf("Docker version %s", version),
	}
}

func checkDockerSocket() CheckResult {
	socketPath := "/var/run/docker.sock"

	info, err := os.Stat(socketPath)
	if err != nil {
		altPaths := []string{
			"/run/user/1000/docker.sock",
			"$HOME/.docker/run/docker.sock",
		}
		for _, p := range altPaths {
			p = os.ExpandEnv(p)
			if info, err = os.Stat(p); err == nil {
				socketPath = p
				break
			}
		}
		if err != nil {
			return CheckResult{
				Name:           "docker_socket",
				DisplayName:    "Docker Socket",
				Status:         StatusFail,
				Severity:       SeverityBlocking,
				Message:        "Docker socket not found at /var/run/docker.sock. Docker may be in rootless mode.",
				FixInstruction:  "If running Docker rootless, set DOCKER_HOST=unix:///run/user/1000/docker.sock. Otherwise: sudo systemctl start docker",
			}
		}
	}

	dockerClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
		Timeout: 5 * time.Second,
	}

	resp, err := dockerClient.Get("http://localhost/info")
	if err != nil {
		mode := info.Mode()
		return CheckResult{
			Name:           "docker_socket",
			DisplayName:    "Docker Socket",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        fmt.Sprintf("The agent cannot communicate with Docker via the socket. Socket: %s, Permissions: %#o", socketPath, mode.Perm()),
			FixInstruction:  "Fix socket permissions: sudo chmod 660 " + socketPath + ". If running Docker rootless, see: https://docs.yourplatform.com/docker-rootless",
		}
	}
	defer resp.Body.Close()

	return CheckResult{
		Name:        "docker_socket",
		DisplayName: "Docker Socket",
		Status:      StatusPass,
		Severity:    SeverityBlocking,
		Message:     "agent can communicate with Docker via the socket",
	}
}

func checkDockerPull() CheckResult {
	slog.Info("Verifying Docker can pull images (this may take a moment)...")

	out, err := exec.Command("docker", "pull", "hello-world").CombinedOutput()
	if err != nil {
		return CheckResult{
			Name:           "docker_pull",
			DisplayName:    "Docker Pull",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        fmt.Sprintf("Docker is running but cannot pull images from Docker Hub. Error: %s", strings.TrimSpace(string(out))),
			FixInstruction: "Test manually: sudo docker pull hello-world. Common causes: firewall blocking registry-1.docker.io, Docker Hub rate limit, or incorrect proxy settings.",
		}
	}

	_ = exec.Command("docker", "rmi", "hello-world").Run()

	return CheckResult{
		Name:        "docker_pull",
		DisplayName: "Docker Pull",
		Status:      StatusPass,
		Severity:    SeverityBlocking,
		Message:     "Docker can pull images from Docker Hub",
	}
}
