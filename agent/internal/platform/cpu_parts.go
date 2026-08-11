package platform

import "strings"

// armCPUPartCodes maps the CPU part hex field from /proc/cpuinfo to
// a human-readable microarchitecture name. The codes are sourced from
// the Arm Architecture Reference Manual and public kernel headers.
var armCPUPartCodes = map[string]string{
	"0xd0c": "Neoverse N1",
	"0xd49": "Neoverse N2",
	"0xd40": "Neoverse V1",
	"0xd4f": "Neoverse V2",
	"0xd05": "Cortex-A55",
	"0xd06": "Cortex-A65",
	"0xd07": "Cortex-A57",
	"0xd08": "Cortex-A72",
	"0xd09": "Cortex-A73",
	"0xd0a": "Cortex-A75",
	"0xd0b": "Cortex-A76",
	"0xd0d": "Cortex-A77",
	"0xd41": "Cortex-A78",
	"0xd42": "Cortex-A78C",
	"0xd43": "Cortex-A65AE",
	"0xd44": "Cortex-X1",
	"0xd46": "Cortex-A510",
	"0xd47": "Cortex-A710",
	"0xd48": "Cortex-X2",
	"0xd4b": "Cortex-A78AE",
	"0xd4c": "Cortex-X3",
	"0xd4d": "Cortex-A715",
	"0xd4e": "Cortex-X4",
	"0xd80": "Cortex-A520",
	"0xd81": "Cortex-A720",
	"0xd82": "Cortex-X4",
	"0xd84": "Cortex-X925",
}

// cloudProviderHints attempts to identify the cloud provider from DMI
// and hostname patterns. Returns a human-readable hint or empty string.
func cloudProviderHints(boardVendor, chassisVendor, hostname string) string {
	h := strings.ToLower(hostname)
	bv := strings.ToLower(boardVendor)
	cv := strings.ToLower(chassisVendor)

	// AWS Graviton
	if strings.Contains(bv, "amazon") || strings.Contains(cv, "amazon") {
		if strings.Contains(h, "graviton4") || strings.Contains(h, "g4") {
			return "AWS Graviton4"
		}
		if strings.Contains(h, "graviton3") || strings.Contains(h, "g3") {
			return "AWS Graviton3"
		}
		if strings.Contains(h, "graviton2") || strings.Contains(h, "g2") {
			return "AWS Graviton2"
		}
		if strings.Contains(h, "graviton") {
			return "AWS Graviton"
		}
		return "AWS (unknown generation)"
	}

	// Google Cloud Axion
	if strings.Contains(bv, "google") || strings.Contains(cv, "google") {
		if strings.Contains(h, "axion") {
			return "GCP Axion"
		}
		return "GCP (unknown)"
	}

	// Microsoft Azure Cobalt
	if strings.Contains(bv, "microsoft") || strings.Contains(cv, "microsoft") {
		if strings.Contains(h, "cobalt") {
			return "Azure Cobalt"
		}
		return "Azure (unknown)"
	}

	// Oracle Cloud
	if strings.Contains(bv, "oracle") || strings.Contains(cv, "oracle") {
		return "Oracle Cloud"
	}

	// Ampere
	if strings.Contains(bv, "ampere") || strings.Contains(cv, "ampere") {
		return "Ampere (bare metal)"
	}

	// Hostname-only hints (lower confidence)
	if strings.Contains(h, "graviton") {
		return "AWS Graviton (hostname hint)"
	}
	if strings.Contains(h, "axion") {
		return "GCP Axion (hostname hint)"
	}
	if strings.Contains(h, "cobalt") {
		return "Azure Cobalt (hostname hint)"
	}

	return ""
}
