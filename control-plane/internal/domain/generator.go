package domain

import (
	"fmt"
	"regexp"
	"strings"
)

// SanitizeSubdomain converts an app name into a DNS-safe subdomain label.
// Rules:
//   - Lowercase
//   - Replace spaces with hyphens
//   - Strip non-alphanumeric characters (except hyphens)
//   - Collapse multiple hyphens
//   - Trim leading/trailing hyphens
//   - Must be 1-63 characters, not empty
func SanitizeSubdomain(name string) (string, error) {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, " ", "-")
	s = regexp.MustCompile(`[^a-z0-9-]`).ReplaceAllString(s, "")
	s = regexp.MustCompile(`-{2,}`).ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")

	if s == "" {
		return "", fmt.Errorf("name %q produces empty subdomain", name)
	}
	if len(s) > 63 {
		s = s[:63]
		s = strings.TrimRight(s, "-")
	}
	return s, nil
}

// GenerateSubdomain builds a full subdomain from app name and server ID.
// Format: {app-name}.srv-{serverID[:8]}
func GenerateSubdomain(appName, serverID string) (string, error) {
	sanitized, err := SanitizeSubdomain(appName)
	if err != nil {
		return "", fmt.Errorf("sanitize app name: %w", err)
	}

	if len(serverID) < 8 {
		return "", fmt.Errorf("server ID too short: %q (need at least 8 chars)", serverID)
	}
	shortID := serverID[:8]

	return fmt.Sprintf("%s.srv-%s", sanitized, shortID), nil
}

// GenerateDomain builds the full domain from app name, server ID, and base domain.
// Returns e.g. "myshop.srv-a1b2c3d4.yourplatform.app"
func GenerateDomain(appName, serverID, baseDomain string) (string, error) {
	subdomain, err := GenerateSubdomain(appName, serverID)
	if err != nil {
		return "", err
	}
	return subdomain + "." + baseDomain, nil
}
