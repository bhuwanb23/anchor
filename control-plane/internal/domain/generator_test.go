package domain

import (
	"testing"
)

func TestSanitizeSubdomain(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"simple", "myshop", "myshop", false},
		{"with spaces", "My Shop", "my-shop", false},
		{"special chars", "My_Shop!@#", "myshop", false},
		{"collapsing hyphens", "a---b", "a-b", false},
		{"trim hyphens", "-hello-", "hello", false},
		{"numbers", "app123", "app123", false},
		{"mixed", "My Cool App v2!", "my-cool-app-v2", false},
		{"all special", "!@#$%", "", true},
		{"empty after sanitize", "---", "", true},
		{"unicode stripped", "café", "caf", false},
		{"long name truncated", "abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz0123456789", "abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz0", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SanitizeSubdomain(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("SanitizeSubdomain(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("SanitizeSubdomain(%q) = %q, want %q", tt.input, got, tt.want)
			}
			if !tt.wantErr && len(got) > 63 {
				t.Errorf("subdomain too long: %d chars (max 63)", len(got))
			}
		})
	}
}

func TestGenerateSubdomain(t *testing.T) {
	tests := []struct {
		name     string
		appName  string
		serverID string
		want     string
		wantErr  bool
	}{
		{"basic", "myshop", "a1b2c3d4e5f6", "myshop.srv-a1b2c3d4", false},
		{"spaces", "My Shop", "a1b2c3d4e5f6", "my-shop.srv-a1b2c3d4", false},
		{"short server ID", "app", "abc", "", true},
		{"special chars", "My App!", "a1b2c3d4e5f6", "my-app.srv-a1b2c3d4", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateSubdomain(tt.appName, tt.serverID)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateSubdomain(%q, %q) error = %v, wantErr %v", tt.appName, tt.serverID, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("GenerateSubdomain(%q, %q) = %q, want %q", tt.appName, tt.serverID, got, tt.want)
			}
		})
	}
}

func TestGenerateDomain(t *testing.T) {
	tests := []struct {
		name       string
		appName    string
		serverID   string
		baseDomain string
		want       string
		wantErr    bool
	}{
		{"basic", "myshop", "a1b2c3d4e5f6", "yourplatform.app", "myshop.srv-a1b2c3d4.yourplatform.app", false},
		{"spaces", "My Shop", "a1b2c3d4e5f6", "yourplatform.app", "my-shop.srv-a1b2c3d4.yourplatform.app", false},
		{"custom domain", "myshop", "a1b2c3d4e5f6", "example.com", "myshop.srv-a1b2c3d4.example.com", false},
		{"short server ID", "app", "abc", "yourplatform.app", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateDomain(tt.appName, tt.serverID, tt.baseDomain)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateDomain(%q, %q, %q) error = %v, wantErr %v", tt.appName, tt.serverID, tt.baseDomain, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("GenerateDomain(%q, %q, %q) = %q, want %q", tt.appName, tt.serverID, tt.baseDomain, got, tt.want)
			}
		})
	}
}

func TestGenerateSubdomain_Idempotent(t *testing.T) {
	// Running twice should produce the same result
	got1, err := GenerateSubdomain("My Shop", "a1b2c3d4e5f6")
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	got2, err := GenerateSubdomain("My Shop", "a1b2c3d4e5f6")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if got1 != got2 {
		t.Errorf("not idempotent: %q != %q", got1, got2)
	}
}

func TestGenerateDomain_FullFormat(t *testing.T) {
	domain, err := GenerateDomain("My Cool App", "a1b2c3d4e5f6", "yourplatform.app")
	if err != nil {
		t.Fatalf("GenerateDomain: %v", err)
	}
	expected := "my-cool-app.srv-a1b2c3d4.yourplatform.app"
	if domain != expected {
		t.Errorf("got %q, want %q", domain, expected)
	}
}
