package preflight

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type osRequirement struct {
	ID         string
	MinVersion string
	Label      string
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

func checkOS() CheckResult {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return CheckResult{
			Name:           "os",
			DisplayName:    "Operating System",
			Status:         StatusFail,
			Severity:       SeverityBlocking,
			Message:        "could not read /etc/os-release — unable to determine operating system",
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
