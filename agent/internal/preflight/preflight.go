package preflight

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
	"golang.org/x/sys/unix"
	"gopkg.in/yaml.v3"
)

// RunAll runs all pre-flight checks and returns the overall result.
func RunAll() *Result {
	result := NewResult()

	// Group A — System basics
	result.AddCheck(checkOS())
	result.AddCheck(checkArch())
	result.AddCheck(checkDisk())
	result.AddCheck(checkRAM())
	result.AddCheck(checkClock())

	// Group B — Network checks
	result.AddCheck(checkInternet())
	result.AddCheck(checkDNS())
	result.AddCheck(checkPort(80, "HTTP"))
	result.AddCheck(checkPort(443, "HTTPS"))
	result.AddCheck(checkControlPlaneConnect())

	// Group C — Docker checks
	c1 := checkDockerInstalled()
	result.AddCheck(c1)
	// Only run C2-C5 if Docker is installed (or was installed by auto-fix)
	if c1.Status == StatusPass || c1.Status == StatusFixed {
		result.AddCheck(checkDockerDaemon())
		result.AddCheck(checkDockerVersion())
		result.AddCheck(checkDockerSocket())
		result.AddCheck(checkDockerPull())
	}

	// Group D — Runtime environment
	result.AddCheck(checkSystemd())
	result.AddCheck(checkDirectories())
	result.AddCheck(checkConflictingAgent())
	result.AddCheck(checkConfig())

	// Collect system info directly
	result.SystemInfo = collectSystemInfo()

	result.Done()
	return result
}

func collectSystemInfo() SystemInfo {
	info := SystemInfo{
		OS:   runtime.GOOS,
		Arch: runtime.GOARCH,
	}

	// Read OS info from /etc/os-release
	if data, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "ID=") && !strings.HasPrefix(line, "ID_LIKE=") {
				info.OS = strings.Trim(strings.TrimPrefix(line, "ID="), "\"")
			}
			if strings.HasPrefix(line, "VERSION_ID=") {
				info.OSVersion = strings.Trim(strings.TrimPrefix(line, "VERSION_ID="), "\"")
			}
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				info.OSPretty = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
			}
		}
	}

	// Override Arch with actual uname -m result
	if out, err := exec.Command("uname", "-m").Output(); err == nil {
		info.Arch = strings.TrimSpace(string(out))
	}

	// Read RAM info
	if v, err := mem.VirtualMemory(); err == nil {
		info.RAMMB = int(v.Total / 1024 / 1024)
		info.RAMAvailableMB = int(v.Available / 1024 / 1024)
	}

	// Read disk info
	if usage, err := disk.Usage("/"); err == nil {
		info.DiskTotalGB = int(usage.Total / 1024 / 1024 / 1024)
		info.DiskAvailableGB = int(usage.Free / 1024 / 1024 / 1024)
		info.DiskUsedPercent = usage.UsedPercent
	}

	return info
}

type osRequirement struct {
	ID          string
	MinVersion  string
	Label       string
}

var supportedOS = []osRequirement{
	{"ubuntu", "20.04", "Ubuntu"},
	{"debian", "11", "Debian"},
	{"centos", "8", "CentOS"},
	{"rhel", "8", "RHEL"},
	{"fedora", "36", "Fedora"},
	{"rocky", "8", "Rocky Linux"},
	{"almalinux", "8", "AlmaLinux"},
}

// versionAtLeast compares two version strings (e.g. "22.04" >= "20.04").
func versionAtLeast(version, minVersion string) bool {
	vParts := strings.Split(version, ".")
	mParts := strings.Split(minVersion, ".")

	maxLen := len(vParts)
	if len(mParts) > maxLen {
		maxLen = len(mParts)
	}

	for i := 0; i < maxLen; i++ {
		var v, m int
		if i < len(vParts) {
			v, _ = strconv.Atoi(vParts[i])
		}
		if i < len(mParts) {
			m, _ = strconv.Atoi(mParts[i])
		}
		if v > m {
			return true
		}
		if v < m {
			return false
		}
	}
	return true // equal
}

func checkOS() CheckResult {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return CheckResult{
			Name:           "os",
			DisplayName:    "Operating System",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        "could not read /etc/os-release \u2014 unable to determine operating system",
			FixInstruction: "This agent requires a supported Linux distribution. Ensure /etc/os-release is present and readable.",
		}
	}

	var osID, osVersion string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "ID=") && !strings.HasPrefix(line, "ID_LIKE=") {
			osID = strings.Trim(strings.TrimPrefix(line, "ID="), "\"")
		}
		if strings.HasPrefix(line, "VERSION_ID=") {
			osVersion = strings.Trim(strings.TrimPrefix(line, "VERSION_ID="), "\"")
		}
	}

	if osID == "" {
		return CheckResult{
			Name:           "os",
			DisplayName:    "Operating System",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        "could not determine OS distribution from /etc/os-release",
			FixInstruction: "This agent requires a supported Linux distribution. Contact support if you believe this is an error.",
		}
	}

	var requirement *osRequirement
	for i, req := range supportedOS {
		if req.ID == osID {
			requirement = &supportedOS[i]
			break
		}
	}

	if requirement == nil {
	supported := make([]string, len(supportedOS))
	for i, req := range supportedOS {
		supported[i] = fmt.Sprintf("%s %s+", req.Label, req.MinVersion)
	}
	return CheckResult{
		Name:           "os",
		DisplayName:    "Operating System",
		Status:         StatusFail,
		Severity:       SeverityBlocking,
		Message:        fmt.Sprintf("'%s' is not a supported operating system", osID),
		FixInstruction: fmt.Sprintf("Supported systems: %s", strings.Join(supported, ", ")),
	}
	}

	if osVersion == "" {
		return CheckResult{
			Name:           "os",
			DisplayName:    "Operating System",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        fmt.Sprintf("detected %s but could not determine version", requirement.Label),
			FixInstruction: fmt.Sprintf("Minimum supported version is %s. Check /etc/os-release for VERSION_ID.", requirement.MinVersion),
		}
	}

	if !versionAtLeast(osVersion, requirement.MinVersion) {
		return CheckResult{
			Name:           "os",
			DisplayName:    "Operating System",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        fmt.Sprintf("Your server is running %s %s, which is too old. Minimum supported version is %s.", requirement.Label, osVersion, requirement.MinVersion),
			FixInstruction: fmt.Sprintf("Upgrade to %s or newer. On Ubuntu: sudo do-release-upgrade", requirement.MinVersion),
		}
	}

	return CheckResult{
		Name:        "os",
		DisplayName: "Operating System",
		Status:      StatusPass,
		Severity:    SeverityBlocking,
		Message:     fmt.Sprintf("detected %s %s", osID, osVersion),
	}
}

func checkArch() CheckResult {
	out, err := exec.Command("uname", "-m").Output()
	if err != nil {
		return CheckResult{
			Name:           "arch",
			DisplayName:    "Architecture",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        "could not determine system architecture",
			FixInstruction: "Run 'uname -m' manually to check. The agent requires a 64-bit system (x86_64 or aarch64).",
		}
	}

	rawArch := strings.TrimSpace(string(out))

	switch rawArch {
	case "x86_64":
		return CheckResult{
			Name:        "arch",
			DisplayName: "Architecture",
			Status:      StatusPass,
			Severity:    SeverityBlocking,
			Message:     fmt.Sprintf("detected %s (amd64)", rawArch),
		}
	case "aarch64":
		return CheckResult{
			Name:        "arch",
			DisplayName: "Architecture",
			Status:      StatusPass,
			Severity:    SeverityBlocking,
			Message:     fmt.Sprintf("detected %s (arm64)", rawArch),
		}
	case "i386", "i686":
		return CheckResult{
			Name:           "arch",
			DisplayName:    "Architecture",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        fmt.Sprintf("Your server is running 32-bit %s, which is not supported.", rawArch),
			FixInstruction: "YourPlatform requires a 64-bit server (x86_64 or aarch64). Reinstall with a 64-bit OS.",
		}
	case "armv7l", "armv6l":
		return CheckResult{
			Name:           "arch",
			DisplayName:    "Architecture",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        fmt.Sprintf("Your server is running 32-bit ARM (%s), which is not supported.", rawArch),
			FixInstruction: "If this is a Raspberry Pi, please flash a 64-bit OS image (e.g., Raspberry Pi OS 64-bit).",
		}
	default:
		return CheckResult{
			Name:           "arch",
			DisplayName:    "Architecture",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        fmt.Sprintf("unsupported architecture '%s'", rawArch),
			FixInstruction: "YourPlatform requires a 64-bit server (x86_64 or aarch64).",
		}
	}
}

// ─────────────────────────────────────────────
// Docker Auto-Install Helpers
// ─────────────────────────────────────────────

// getOSInfo reads /etc/os-release and returns the OS ID and version.
func getOSInfo() (id, version string) {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "", ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "ID=") && !strings.HasPrefix(line, "ID_LIKE=") {
			id = strings.Trim(strings.TrimPrefix(line, "ID="), "\"")
		}
		if strings.HasPrefix(line, "VERSION_ID=") {
			version = strings.Trim(strings.TrimPrefix(line, "VERSION_ID="), "\"")
		}
	}
	return
}

func runAndLog(name string, args ...string) error {
	slog.Info("docker-install", "step", name)
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error("docker-install", "step", name, "error", string(output))
		return fmt.Errorf("%s failed: %s", name, strings.TrimSpace(string(output)))
	}
	return nil
}

func installDockerApt() error {
	slog.Info("Docker is not installed. Installing Docker automatically...")
	if err := runAndLog("apt-get", "update"); err != nil {
		return err
	}
	if err := runAndLog("apt-get", "install", "-y", "apt-transport-https", "ca-certificates", "curl", "gnupg"); err != nil {
		return err
	}
	slog.Info("Adding Docker GPG key...")
	osID := getOSID()
	osCodename := getOSVersionCodename()
	if osCodename == "" {
		return fmt.Errorf("cannot determine OS version codename for Docker repository")
	}
	// Ensure keyrings directory exists
	_ = runAndLog("sh", "-c", "mkdir -p /usr/share/keyrings")
	// Download and save GPG key
	if err := runAndLog("curl", "-fsSL", "https://download.docker.com/linux/"+osID+"/gpg", "-o", "/usr/share/keyrings/docker.asc"); err != nil {
		// Fallback: pipe through gpg
		if err := runAndLog("sh", "-c", "curl -fsSL https://download.docker.com/linux/"+osID+"/gpg | gpg --dearmor -o /usr/share/keyrings/docker.gpg"); err != nil {
			return fmt.Errorf("failed to add Docker GPG key: %w", err)
		}
	}
	slog.Info("Adding Docker repository...")
	repoLine := "deb [arch=" + archForRepo() + " signed-by=/usr/share/keyrings/docker.asc] https://download.docker.com/linux/" + osID + " " + osCodename + " stable"
	if err := runAndLog("sh", "-c", "echo '"+repoLine+"' > /etc/apt/sources.list.d/docker.list"); err != nil {
		return err
	}
	if err := runAndLog("apt-get", "update"); err != nil {
		return err
	}
	slog.Info("Installing Docker packages (this may take a minute)...")
	if err := runAndLog("apt-get", "install", "-y", "docker-ce", "docker-ce-cli", "containerd.io"); err != nil {
		return err
	}
	// Start and enable Docker daemon
	_ = exec.Command("systemctl", "start", "docker").Run()
	_ = exec.Command("systemctl", "enable", "docker").Run()
	return nil
}

func installDockerYum() error {
	slog.Info("Docker is not installed. Installing Docker automatically...")
	if err := runAndLog("yum", "install", "-y", "yum-utils"); err != nil {
		return err
	}
	slog.Info("Adding Docker repository...")
	if err := runAndLog("yum-config-manager", "--add-repo", "https://download.docker.com/linux/"+getOSID()+"/docker-ce.repo"); err != nil {
		return err
	}
	slog.Info("Installing Docker packages (this may take a minute)...")
	if err := runAndLog("yum", "install", "-y", "docker-ce", "docker-ce-cli", "containerd.io"); err != nil {
		return err
	}
	// Start and enable Docker daemon
	_ = exec.Command("systemctl", "start", "docker").Run()
	_ = exec.Command("systemctl", "enable", "docker").Run()
	return nil
}

func installDockerDnf() error {
	slog.Info("Docker is not installed. Installing Docker automatically...")
	if err := runAndLog("dnf", "install", "-y", "dnf-plugins-core"); err != nil {
		return err
	}
	slog.Info("Adding Docker repository...")
	if err := runAndLog("dnf", "config-manager", "--add-repo", "https://download.docker.com/linux/"+getOSID()+"/docker-ce.repo"); err != nil {
		return err
	}
	slog.Info("Installing Docker packages (this may take a minute)...")
	if err := runAndLog("dnf", "install", "-y", "docker-ce", "docker-ce-cli", "containerd.io"); err != nil {
		return err
	}
	// Start and enable Docker daemon
	_ = exec.Command("systemctl", "start", "docker").Run()
	_ = exec.Command("systemctl", "enable", "docker").Run()
	return nil
}

func getOSID() string {
	id, _ := getOSInfo()
	return id
}

func getOSVersionCodename() string {
	_, ver := getOSInfo()
	// Map versions to codenames for Docker repo
	// Returns empty string for unknown versions so caller can detect failure
	switch ver {
	case "22.04":
		return "jammy"
	case "24.04":
		return "noble"
	case "20.04":
		return "focal"
	case "18.04":
		return "bionic"
	case "16.04":
		return "xenial"
	case "11":
		return "bullseye"
	case "12":
		return "bookworm"
	case "10":
		return "buster"
	default:
		return "" // unknown — caller should check and return error
	}
}

func archForRepo() string {
	out, err := exec.Command("uname", "-m").Output()
	if err != nil {
		return "amd64"
	}
	switch strings.TrimSpace(string(out)) {
	case "aarch64":
		return "arm64"
	case "x86_64":
		return "amd64"
	default:
		return "amd64"
	}
}

// ─────────────────────────────────────────────
// C1 — Docker Installed
// ─────────────────────────────────────────────

func checkDockerInstalled() CheckResult {
	_, err := exec.LookPath("docker")
	if err == nil {
		return CheckResult{
			Name:        "docker_installed",
			DisplayName: "Docker Installation",
			Status:      StatusPass,
			Severity:    SeverityBlocking,
			Message:     "Docker is installed",
		}
	}

	slog.Info("Docker is not installed. Attempting automatic installation...")

	osID, _ := getOSInfo()
	var installErr error

	switch osID {
	case "ubuntu", "debian":
		installErr = installDockerApt()
	case "centos", "rhel", "rocky", "almalinux":
		installErr = installDockerYum()
	case "fedora":
		installErr = installDockerDnf()
	default:
		return CheckResult{
			Name:           "docker_installed",
			DisplayName:    "Docker Installation",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        "Docker is not installed and automatic installation is not supported for this OS.",
			FixInstruction: "Install Docker manually: curl -fsSL https://get.docker.com | sh",
		}
	}

	if installErr != nil {
		return CheckResult{
			Name:           "docker_installed",
			DisplayName:    "Docker Installation",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        fmt.Sprintf("Docker is not installed and automatic installation failed. Error: %v", installErr),
			FixInstruction: "Install Docker manually: curl -fsSL https://get.docker.com | sh",
		}
	}

	// Verify installation succeeded
	if _, err := exec.LookPath("docker"); err != nil {
		return CheckResult{
			Name:           "docker_installed",
			DisplayName:    "Docker Installation",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        "Docker was installed but docker binary is not in PATH after installation.",
			FixInstruction: "Try: sudo apt-get install -y docker-ce (or sudo yum install -y docker-ce).",
		}
	}

	// Get the installed version
	versionOut, _ := exec.Command("docker", "version", "--format", "{{.Server.Version}}").Output()
	versionMsg := "Docker installed successfully"
	if len(versionOut) > 0 {
		versionMsg = fmt.Sprintf("Docker installed successfully (version %s)", strings.TrimSpace(string(versionOut)))
	}

	return CheckResult{
		Name:      "docker_installed",
		DisplayName: "Docker Installation",
		Status:    StatusFixed,
		Severity:  SeverityBlocking,
		Message:   versionMsg,
		AutoFixed: true,
	}
}

// ─────────────────────────────────────────────
// C2 — Docker Daemon Running
// ─────────────────────────────────────────────

func checkDockerDaemon() CheckResult {
	// Check if daemon is running
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

	// Wait up to 10 seconds for Docker to start
	for i := 0; i < 10; i++ {
		time.Sleep(1 * time.Second)
		out, err := exec.Command("docker", "version", "--format", "{{.Server.Version}}").Output()
		if err == nil {
			return CheckResult{
				Name:      "docker_daemon",
				DisplayName: "Docker Daemon",
				Status:    StatusFixed,
				Severity:  SeverityBlocking,
				Message:   fmt.Sprintf("Docker daemon was not running — agent started it successfully (version %s)", strings.TrimSpace(string(out))),
				AutoFixed: true,
			}
		}
	}

	// Read Docker daemon logs for diagnostics
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

// ─────────────────────────────────────────────
// C3 — Docker Version Acceptable
// ─────────────────────────────────────────────

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

	// Minimum: 20.10.0 (Docker 20.10+)
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

	// Warning if < 24.x (missing security patches)
	if !versionAtLeast(version, "24") {
		return CheckResult{
			Name:        "docker_version",
			DisplayName: "Docker Version",
			Status:      StatusWarn,
			Severity:    SeverityWarning,
			Message:     fmt.Sprintf("Docker version %s is installed. Version 24+ is recommended for latest security patches.", version),
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

// ─────────────────────────────────────────────
// C4 — Docker Socket Access
// ─────────────────────────────────────────────

func checkDockerSocket() CheckResult {
	socketPath := "/var/run/docker.sock"

	// Check if socket exists
	info, err := os.Stat(socketPath)
	if err != nil {
		// Try rootless socket
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
				FixInstruction: "If running Docker rootless, set DOCKER_HOST=unix:///run/user/1000/docker.sock. Otherwise: sudo systemctl start docker",
			}
		}
	}

	// Try to make a real Docker API call via the socket
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
			FixInstruction: "Fix socket permissions: sudo chmod 660 " + socketPath + ". If running Docker rootless, see: https://docs.yourplatform.com/docker-rootless",
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

// ─────────────────────────────────────────────
// C5 — Docker Can Pull from Internet
// ─────────────────────────────────────────────

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

	// Delete the pulled image immediately
	_ = exec.Command("docker", "rmi", "hello-world").Run()

	return CheckResult{
		Name:        "docker_pull",
		DisplayName: "Docker Pull",
		Status:      StatusPass,
		Severity:    SeverityBlocking,
		Message:     "Docker can pull images from Docker Hub",
	}
}

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
	// Consume body to reuse connection
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
	// Test public DNS first
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

	// Test control plane DNS specifically
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

	// Search /proc/*/fd for sockets with matching inode
	procEntries, _ := filepath.Glob("/proc/[0-9]*/fd/*")
	for _, fdPath := range procEntries {
		link, err := os.Readlink(fdPath)
		if err != nil {
			continue
		}
		// Socket links look like: socket:[12345]
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
	// cmdline uses null bytes as separators, take first part
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
		// Debian/Ubuntu: /etc/apache2/sites-enabled
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
		// RHEL/CentOS/Fedora: /etc/httpd/conf.d/*.conf
		dir := "/etc/httpd/conf.d"
		entries, err := filepath.Glob(filepath.Join(dir, "*.conf"))
		if err != nil || len(entries) == 0 {
			return false
		}
		for _, e := range entries {
			base := filepath.Base(e)
			// welcome.conf and ssl.conf are defaults, not real sites
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

	// Try to listen on the port
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

	// Find the occupying process
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

	// Auto-fix logic: stop apache2/nginx if no active sites
	switch procName {
	case "apache2", "httpd":
		if !hasActiveSites(procName) {
			_ = exec.Command("systemctl", "stop", "apache2").Run()
			_ = exec.Command("systemctl", "disable", "apache2").Run()
			// Re-check
			ln, err := net.Listen("tcp", fmt.Sprintf(":%d", portNum))
			if err == nil {
				ln.Close()
				return CheckResult{
					Name:        name,
					DisplayName: displayName,
					Status:      StatusFixed,
					Severity:    SeverityBlocking,
					Message:     fmt.Sprintf("Apache was using port %d but had no active sites — agent stopped it", portNum),
					AutoFixed:   true,
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
			FixInstruction: fmt.Sprintf("Either move those sites to YourPlatform first, or configure Apache to proxy to YourPlatform. See docs for details."),
		}
	case "nginx":
		if !hasActiveSites(procName) {
			_ = exec.Command("systemctl", "stop", "nginx").Run()
			_ = exec.Command("systemctl", "disable", "nginx").Run()
			ln, err := net.Listen("tcp", fmt.Sprintf(":%d", portNum))
			if err == nil {
				ln.Close()
				return CheckResult{
					Name:        name,
					DisplayName: displayName,
					Status:      StatusFixed,
					Severity:    SeverityBlocking,
					Message:     fmt.Sprintf("Nginx was using port %d but had no active sites — agent stopped it", portNum),
					AutoFixed:   true,
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

func checkDisk() CheckResult {
	getAvailableGB := func(path string) (int, error) {
		var stat unix.Statfs_t
		if err := unix.Statfs(path, &stat); err != nil {
			return 0, err
		}
		available := int(stat.Bavail * uint64(stat.Bsize) / (1024 * 1024 * 1024))
		return available, nil
	}

	freeGB, err := getAvailableGB("/")
	if err != nil {
		return CheckResult{
			Name:           "disk_space",
			DisplayName:    "Disk Space",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        fmt.Sprintf("could not read disk usage: %v", err),
			FixInstruction: "Check that the root filesystem is accessible and not corrupted.",
		}
	}

	// Also check /var if it's a separate mount point
	var varFreeGB int
	if varFree, err := getAvailableGB("/var"); err == nil {
		varFreeGB = varFree
		if varFree < freeGB {
			freeGB = varFree
		}
	}

	// Also check /var/lib/docker if it exists
	if dockerFree, err := getAvailableGB("/var/lib/docker"); err == nil {
		if dockerFree < freeGB {
			freeGB = dockerFree
		}
	}

	if freeGB < 2 {
		msg := fmt.Sprintf("Your server only has %d GB of free disk space.", freeGB)
		if varFreeGB > 0 && varFreeGB < freeGB {
			msg = fmt.Sprintf("Your /var partition only has %d GB of free disk space.", freeGB)
		}
		return CheckResult{
			Name:           "disk_space",
			DisplayName:    "Disk Space",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        msg + " YourPlatform needs at least 2 GB free to pull Docker images and store app data.",
			FixInstruction: "To free up space: remove unused Docker images (docker image prune -a), check usage (du -sh /* 2>/dev/null | sort -rh | head -20), or expand your server's disk.",
		}
	}

	if freeGB < 5 {
		msg := fmt.Sprintf("Your server has %d GB of free disk space.", freeGB)
		if varFreeGB > 0 && varFreeGB < freeGB {
			msg = fmt.Sprintf("Your /var partition has %d GB of free disk space.", freeGB)
		}
		return CheckResult{
			Name:           "disk_space",
			DisplayName:    "Disk Space",
			Status:         StatusWarn,
			Severity:       SeverityWarning,
			Message:        msg + " This is enough to get started, but apps and their data will fill this quickly. Consider expanding your disk soon.",
			FixInstruction: "Free up disk space or expand your disk to avoid running out.",
		}
	}

	return CheckResult{
		Name:        "disk_space",
		DisplayName: "Disk Space",
		Status:      StatusPass,
		Severity:    SeverityBlocking,
		Message:     fmt.Sprintf("%d GB free disk space", freeGB),
	}
}

func checkRAM() CheckResult {
	v, err := mem.VirtualMemory()
	if err != nil {
		return CheckResult{
			Name:           "memory",
			DisplayName:    "Memory",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        fmt.Sprintf("could not read memory info: %v", err),
			FixInstruction: "Check that /proc/meminfo is accessible.",
		}
	}

	totalMB := int(v.Total / 1024 / 1024)
	availableMB := int(v.Available / 1024 / 1024)

	if totalMB < 512 {
		return CheckResult{
			Name:           "memory",
			DisplayName:    "Memory",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        fmt.Sprintf("Your server has %d MB of RAM, which is below the 512 MB minimum. Running Docker and your apps requires at least 512 MB.", totalMB),
			FixInstruction: "Upgrade to a larger server plan (usually $2-5/month more). Your current hosting provider likely offers a RAM upgrade.",
		}
	}

	if totalMB < 1024 {
		return CheckResult{
			Name:           "memory",
			DisplayName:    "Memory",
			Status:         StatusWarn,
			Severity:       SeverityWarning,
			Message:        fmt.Sprintf("Your server has %d MB of RAM (%d MB available). This is enough to run but may be tight for multiple apps.", totalMB, availableMB),
			FixInstruction: "Consider upgrading to at least 1 GB RAM for running multiple apps comfortably.",
		}
	}

	return CheckResult{
		Name:        "memory",
		DisplayName: "Memory",
		Status:      StatusPass,
		Severity:    SeverityBlocking,
		Message:     fmt.Sprintf("%d MB RAM (%d MB available)", totalMB, availableMB),
	}
}

func checkClock() CheckResult {
	// Check if systemd-timesyncd is active
	out, err := exec.Command("timedatectl", "show", "--property=NTPSynchronized", "--value").Output()
	if err == nil && strings.TrimSpace(string(out)) == "yes" {
		return CheckResult{
			Name:        "system_clock",
			DisplayName: "System Clock",
			Status:      StatusPass,
			Severity:    SeverityBlocking,
			Message:     "system clock is synchronized via NTP",
		}
	}

	// Check if systemd-timesyncd service exists and try to start it
	if err := exec.Command("systemctl", "cat", "systemd-timesyncd").Run(); err == nil {
		_ = exec.Command("systemctl", "start", "systemd-timesyncd").Run()
		_ = exec.Command("systemctl", "enable", "systemd-timesyncd").Run()

		// Re-check after auto-fix
		out, err := exec.Command("timedatectl", "show", "--property=NTPSynchronized", "--value").Output()
		if err == nil && strings.TrimSpace(string(out)) == "yes" {
			return CheckResult{
				Name:        "system_clock",
				DisplayName: "System Clock",
				Status:      StatusFixed,
				Severity:    SeverityBlocking,
				Message:     "systemd-timesyncd was not running — agent started it successfully",
				AutoFixed:   true,
			}
		}

		// Check if timedatectl shows NTP service is active but not yet synchronized
		out, err = exec.Command("timedatectl", "show", "--property=NTP", "--value").Output()
		if err == nil && strings.TrimSpace(string(out)) == "yes" {
			return CheckResult{
				Name:        "system_clock",
				DisplayName: "System Clock",
				Status:      StatusWarn,
				Severity:    SeverityWarning,
				Message:     "NTP is enabled but clock may not yet be synchronized — this should resolve within a few minutes",
			}
		}

		return CheckResult{
			Name:           "system_clock",
			DisplayName:    "System Clock",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        "systemd-timesyncd is installed but could not be started",
			FixInstruction: "Run: sudo timedatectl set-ntp true && sudo systemctl start systemd-timesyncd",
		}
	}

	// Check for ntpd as fallback
	if err := exec.Command("systemctl", "is-active", "ntpd").Run(); err == nil {
		return CheckResult{
			Name:        "system_clock",
			DisplayName: "System Clock",
			Status:      StatusPass,
			Severity:    SeverityBlocking,
			Message:     "system clock is synchronized via ntpd",
		}
	}

	// Check for chronyd as another fallback
	if err := exec.Command("systemctl", "is-active", "chronyd").Run(); err == nil {
		return CheckResult{
			Name:        "system_clock",
			DisplayName: "System Clock",
			Status:      StatusPass,
			Severity:    SeverityBlocking,
			Message:     "system clock is synchronized via chronyd",
		}
	}

	// Check if chrony is available but not running
	if err := exec.Command("systemctl", "cat", "chronyd").Run(); err == nil {
		_ = exec.Command("systemctl", "start", "chronyd").Run()
		_ = exec.Command("systemctl", "enable", "chronyd").Run()

		if err := exec.Command("systemctl", "is-active", "chronyd").Run(); err == nil {
			return CheckResult{
				Name:        "system_clock",
				DisplayName: "System Clock",
				Status:      StatusFixed,
				Severity:    SeverityBlocking,
				Message:     "chronyd was not running — agent started it successfully",
				AutoFixed:   true,
			}
		}
	}

	return CheckResult{
		Name:           "system_clock",
		DisplayName:    "System Clock",
		Status:         StatusFail,
		Severity:       SeverityBlocking,
		Message:        "no time synchronization service is running",
		FixInstruction: "Enable NTP: sudo timedatectl set-ntp true (requires systemd) or install ntp/chrony for your distribution.",
	}
}

// ─────────────────────────────────────────────
// D1 — Systemd Available and Functional
// ─────────────────────────────────────────────

func checkSystemd() CheckResult {
	// First check if systemctl exists
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

	// Check if systemd daemon is accessible
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
		// Degraded is acceptable — some services may have failed but systemd is functional
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

// ─────────────────────────────────────────────
// D2 — Required Directories Exist and Are Writable
// ─────────────────────────────────────────────

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
			// Directory exists — check writable
			if !info.IsDir() {
				failed = append(failed, dir+" exists but is not a directory")
				continue
			}
			// Check write permission by trying to create a temp file
			tmpFile := filepath.Join(dir, ".yourplatform_write_test")
			if err := os.WriteFile(tmpFile, []byte("test"), 0644); err != nil {
				failed = append(failed, dir+" exists but is not writable")
			} else {
				os.Remove(tmpFile)
			}
			continue
		}

		// Directory doesn't exist — try to create it
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
	if len(created) > 0 {
		msg = fmt.Sprintf("created missing directories: %s", strings.Join(created, ", "))
	}

	status := StatusPass
	autoFixed := false
	if len(created) > 0 {
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

// ─────────────────────────────────────────────
// D3 — No Conflicting Agent Already Running
// ─────────────────────────────────────────────

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

		// cmdline uses null bytes as separators, take the binary path (first segment)
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

// ─────────────────────────────────────────────
// D4 — Config File Readable and Valid
// ─────────────────────────────────────────────

func checkConfig() CheckResult {
	configPath := "/etc/yourplatform/config.yaml"

	// Config may not exist during install flow — that's OK, skip check
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return CheckResult{
			Name:        "config",
			DisplayName: "Agent Configuration",
			Status:      StatusPass,
			Severity:    SeverityBlocking,
			Message:     "no config file found at /etc/yourplatform/config.yaml — skipping (config will be created during install)",
		}
	}

	// Read and parse the config file
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

	// Parse as YAML
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

	// Validate required fields
	var missingFields []string

	if raw["control_plane_url"] == nil || raw["control_plane_url"] == "" {
		missingFields = append(missingFields, "control_plane_url")
	}

	hasToken := raw["registration_token"] != nil && raw["registration_token"] != ""
	hasCredentials := (raw["agent_id"] != nil && raw["agent_id"] != "") && (raw["agent_secret"] != nil && raw["agent_secret"] != "")

	if !hasToken && !hasCredentials {
		missingFields = append(missingFields, "either registration_token or agent_id+agent_secret")
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

// PreflightLog logs the pre-flight results using slog.
func PreflightLog(result *Result) {
	for _, r := range result.Checks {
		switch r.Status {
		case StatusPass, StatusFixed:
			slog.Info("preflight", "check", r.DisplayName, "status", string(r.Status), "message", r.Message)
		case StatusWarn:
			slog.Warn("preflight", "check", r.DisplayName, "status", string(r.Status), "message", r.Message)
		case StatusFail:
			slog.Error("preflight", "check", r.DisplayName, "status", string(r.Status), "message", r.Message)
		}
	}
}

// HasErrors returns true if any blocking checks failed.
// Deprecated: Use Result.HasBlockingFailures() instead.
func HasErrors(result *Result) bool {
	return result.HasBlockingFailures()
}
