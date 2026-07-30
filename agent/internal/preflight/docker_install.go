package preflight

import (
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
)

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

func getOSID() string {
	id, _ := getOSInfo()
	return id
}

func getOSVersionCodename() string {
	_, ver := getOSInfo()
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
		return ""
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
	_ = runAndLog("sh", "-c", "mkdir -p /usr/share/keyrings")
	if err := runAndLog("curl", "-fsSL", "https://download.docker.com/linux/"+osID+"/gpg", "-o", "/usr/share/keyrings/docker.asc"); err != nil {
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
	_ = exec.Command("systemctl", "start", "docker").Run()
	_ = exec.Command("systemctl", "enable", "docker").Run()
	return nil
}

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

	versionOut, _ := exec.Command("docker", "version", "--format", "{{.Server.Version}}").Output()
	versionMsg := "Docker installed successfully"
	if len(versionOut) > 0 {
		versionMsg = fmt.Sprintf("Docker installed successfully (version %s)", strings.TrimSpace(string(versionOut)))
	}

	return CheckResult{
		Name:        "docker_installed",
		DisplayName: "Docker Installation",
		Status:      StatusFixed,
		Severity:    SeverityBlocking,
		Message:     versionMsg,
		AutoFixed:   true,
	}
}
