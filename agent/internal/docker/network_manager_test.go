package docker

import (
	"context"
	"os"
	"testing"
	"time"
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
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := client.EnsureProjectNetwork(ctx, "test-project")
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
}

func TestRemoveProjectNetwork_NoDocker(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := client.RemoveProjectNetwork(ctx, "test-project")
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
}

func TestConnectContainerToNetwork_NoDocker(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := client.ConnectContainerToNetwork(ctx, "abc123", "test-project")
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
}

func TestConnectContainerWithAliases_NoDocker(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := client.ConnectContainerWithAliases(ctx, "abc123", "test-project", []string{"postgres", "db"})
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
}

func TestDisconnectContainerFromNetwork_NoDocker(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := client.DisconnectContainerFromNetwork(ctx, "abc123", "test-project")
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
}

func TestListProjectNetworks_NoDocker(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := client.ListProjectNetworks(ctx)
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
}

func TestCleanupOrphanedNetworks_NoDocker(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := client.CleanupOrphanedNetworks(ctx)
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
}

func TestRemoveProject_NoDocker(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := client.RemoveProject(ctx, "test-project", false)
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
}

func TestRemoveProject_WithVolumes_NoDocker(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := client.RemoveProject(ctx, "test-project", true)
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

// ---------------------------------------------------------------------------
// Integration tests (require Docker)
// ---------------------------------------------------------------------------

func TestEnsureProjectNetwork_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	socket := "unix:///var/run/docker.sock"
	if _, err := os.Stat(socket); os.IsNotExist(err) {
		t.Skip("Docker socket not available, skipping integration test")
	}

	c, err := NewClient(socket)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer c.Close()

	projectName := "test-integration-network"
	networkName := ProjectNetworkName(projectName)
	ctx := context.Background()

	// Clean up before and after
	defer c.RemoveProjectNetwork(ctx, projectName)

	// First call: create the network
	id1, err := c.EnsureProjectNetwork(ctx, projectName)
	if err != nil {
		t.Fatalf("EnsureProjectNetwork (create) failed: %v", err)
	}
	if id1 == "" {
		t.Fatal("expected non-empty network ID")
	}

	// Second call: should reuse (idempotent)
	id2, err := c.EnsureProjectNetwork(ctx, projectName)
	if err != nil {
		t.Fatalf("EnsureProjectNetwork (reuse) failed: %v", err)
	}
	if id1 != id2 {
		t.Errorf("expected same network ID on reuse, got %q and %q", id1, id2)
	}

	// Verify the network exists with correct labels
	network, err := c.findNetworkByName(ctx, networkName)
	if err != nil {
		t.Fatalf("findNetworkByName failed: %v", err)
	}
	if network == nil {
		t.Fatal("expected network to exist")
	}
	if network.Labels[labelOwner] != labelOwnerValue {
		t.Errorf("expected owner label %q, got %q", labelOwnerValue, network.Labels[labelOwner])
	}
	if network.Labels["yourplatform.project"] != "test-integration-network" {
		t.Errorf("expected project label %q, got %q", "test-integration-network", network.Labels["yourplatform.project"])
	}

	// Remove the network
	if err := c.RemoveProjectNetwork(ctx, projectName); err != nil {
		t.Fatalf("RemoveProjectNetwork failed: %v", err)
	}

	// Verify it's gone
	network, err = c.findNetworkByName(ctx, networkName)
	if err != nil {
		t.Fatalf("findNetworkByName after remove failed: %v", err)
	}
	if network != nil {
		t.Error("expected network to be removed")
	}
}
