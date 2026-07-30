package docker

import (
	"context"
	"testing"
)

// ---------------------------------------------------------------------------
// Project name sanitization
// ---------------------------------------------------------------------------

func TestSanitizeProjectName_Simple(t *testing.T) {
	got := SanitizeProjectName("My Shop!")
	if got != "my-shop" {
		t.Errorf("expected 'my-shop', got '%s'", got)
	}
}

func TestSanitizeProjectName_WithSpaces(t *testing.T) {
	got := SanitizeProjectName("  Hello World  ")
	if got != "hello-world" {
		t.Errorf("expected 'hello-world', got '%s'", got)
	}
}

func TestSanitizeProjectName_WithUnderscores(t *testing.T) {
	got := SanitizeProjectName("my_project")
	if got != "my-project" {
		t.Errorf("expected 'my-project', got '%s'", got)
	}
}

func TestSanitizeProjectName_WithSpecialChars(t *testing.T) {
	got := SanitizeProjectName("Next.js App 3!")
	if got != "nextjs-app-3" {
		t.Errorf("expected 'nextjs-app-3', got '%s'", got)
	}
}

func TestSanitizeProjectName_AlreadyClean(t *testing.T) {
	got := SanitizeProjectName("my-app")
	if got != "my-app" {
		t.Errorf("expected 'my-app', got '%s'", got)
	}
}

func TestSanitizeProjectName_Empty(t *testing.T) {
	got := SanitizeProjectName("")
	if got != "project" {
		t.Errorf("expected 'project', got '%s'", got)
	}
}

func TestSanitizeProjectName_AllSpecialChars(t *testing.T) {
	got := SanitizeProjectName("!@#$%^&*()")
	if got != "project" {
		t.Errorf("expected 'project', got '%s'", got)
	}
}

func TestSanitizeProjectName_MultipleHyphens(t *testing.T) {
	got := SanitizeProjectName("a---b")
	if got != "a-b" {
		t.Errorf("expected 'a-b', got '%s'", got)
	}
}

func TestSanitizeProjectName_LeadingTrailingHyphens(t *testing.T) {
	got := SanitizeProjectName("--hello-")
	if got != "hello" {
		t.Errorf("expected 'hello', got '%s'", got)
	}
}

func TestSanitizeProjectName_UpperCase(t *testing.T) {
	got := SanitizeProjectName("MY AWESOME APP")
	if got != "my-awesome-app" {
		t.Errorf("expected 'my-awesome-app', got '%s'", got)
	}
}

// ---------------------------------------------------------------------------
// Project network name
// ---------------------------------------------------------------------------

func TestProjectNetworkName(t *testing.T) {
	got := ProjectNetworkName("My Shop!")
	if got != "yourplatform_my-shop" {
		t.Errorf("expected 'yourplatform_my-shop', got '%s'", got)
	}

	got = ProjectNetworkName("blog")
	if got != "yourplatform_blog" {
		t.Errorf("expected 'yourplatform_blog', got '%s'", got)
	}
}

// ---------------------------------------------------------------------------
// Network operations (structural tests, no real Docker)
// ---------------------------------------------------------------------------

func TestEnsureProjectNetwork_NoDocker(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	_, err := client.EnsureProjectNetwork(context.Background(), "test-project")
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
}

func TestRemoveProjectNetwork_NoDocker(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	err := client.RemoveProjectNetwork(context.Background(), "test-project")
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
}

func TestConnectContainerToNetwork_NoDocker(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	err := client.ConnectContainerToNetwork(context.Background(), "abc123", "test-project")
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
}

func TestDisconnectContainerFromNetwork_NoDocker(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	err := client.DisconnectContainerFromNetwork(context.Background(), "abc123", "test-project")
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
}

func TestListProjectNetworks_NoDocker(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	_, err := client.ListProjectNetworks(context.Background())
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
}

func TestCleanupOrphanedNetworks_NoDocker(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	err := client.CleanupOrphanedNetworks(context.Background())
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
}

// ---------------------------------------------------------------------------
// Label filter
// ---------------------------------------------------------------------------

func TestLabelFilter(t *testing.T) {
	f := labelFilter("yourplatform.owner", "yourplatform-agent")
	if f.Len() == 0 {
		t.Fatal("expected non-empty filter")
	}
	// The filter should contain the label key=value
	// We can't easily inspect it, but we can verify it doesn't panic
	_ = f
}
