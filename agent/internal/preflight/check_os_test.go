package preflight

import (
	"testing"
)

func TestVersionAtLeast(t *testing.T) {
	tests := []struct {
		version    string
		minVersion string
		expected   bool
	}{
		{"22.04", "20.04", true},
		{"20.04", "20.04", true},
		{"20.04", "22.04", false},
		{"18.04", "20.04", false},
		{"11", "11", true},
		{"12", "11", true},
		{"10", "11", false},
		{"22.04.3", "20.04", true},
		{"20.04", "20.04.3", false},
		{"36", "36", true},
		{"40", "36", true},
		{"35", "36", false},
		{"8", "8", true},
		{"9", "8", true},
		{"7", "8", false},
	}

	for _, tt := range tests {
		t.Run(tt.version+">="+tt.minVersion, func(t *testing.T) {
			result := versionAtLeast(tt.version, tt.minVersion)
			if result != tt.expected {
				t.Errorf("versionAtLeast(%q, %q) = %v, want %v", tt.version, tt.minVersion, result, tt.expected)
			}
		})
	}
}

func TestGetOSInfo(t *testing.T) {
	id, ver := getOSInfo()
	_ = id
	_ = ver
}

func TestGetOSVersionCodename(t *testing.T) {
	codename := getOSVersionCodename()
	switch codename {
	case "jammy", "noble", "focal", "bionic", "xenial", "bullseye", "bookworm", "buster", "":
		// valid
	default:
		t.Errorf("unexpected codename: %s", codename)
	}
}
