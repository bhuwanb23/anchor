package docker

import (
	"context"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Default health checks per container type
// ---------------------------------------------------------------------------

func TestDefaultHealthCheck_App(t *testing.T) {
	hc := DefaultHealthCheck(ContainerTypeApp, 3000)
	if hc == nil {
		t.Fatal("expected health check for app")
	}
	if len(hc.Test) != 2 {
		t.Fatalf("expected 2 test elements (CMD-SHELL + command), got %d", len(hc.Test))
	}
	if hc.Test[0] != "CMD-SHELL" {
		t.Errorf("expected CMD-SHELL, got %s", hc.Test[0])
	}
	if hc.Interval != 30*time.Second {
		t.Errorf("expected interval 30s, got %v", hc.Interval)
	}
	if hc.Timeout != 10*time.Second {
		t.Errorf("expected timeout 10s, got %v", hc.Timeout)
	}
	if hc.StartPeriod != 60*time.Second {
		t.Errorf("expected start period 60s, got %v", hc.StartPeriod)
	}
	if hc.Retries != 3 {
		t.Errorf("expected retries 3, got %d", hc.Retries)
	}
}

func TestDefaultHealthCheck_AppNoPort(t *testing.T) {
	hc := DefaultHealthCheck(ContainerTypeApp, 0)
	if hc != nil {
		t.Error("expected nil health check when port is 0")
	}
}

func TestDefaultHealthCheck_Postgres(t *testing.T) {
	hc := DefaultHealthCheck(ContainerTypePostgres, 0)
	if hc == nil {
		t.Fatal("expected health check for postgres")
	}
	if len(hc.Test) < 2 {
		t.Fatal("expected test command")
	}
}

func TestDefaultHealthCheck_MySQL(t *testing.T) {
	hc := DefaultHealthCheck(ContainerTypeMySQL, 0)
	if hc == nil {
		t.Fatal("expected health check for mysql")
	}
}

func TestDefaultHealthCheck_Redis(t *testing.T) {
	hc := DefaultHealthCheck(ContainerTypeRedis, 0)
	if hc == nil {
		t.Fatal("expected health check for redis")
	}
}

func TestDefaultHealthCheck_Unknown(t *testing.T) {
	hc := DefaultHealthCheck("unknown", 0)
	if hc != nil {
		t.Error("expected nil health check for unknown type")
	}
}

// ---------------------------------------------------------------------------
// Health check config defaults
// ---------------------------------------------------------------------------

func TestHealthCheckConfig_Defaults(t *testing.T) {
	hc := &HealthCheckConfig{
		Test: []string{"CMD-SHELL", "true"},
	}
	dc := hc.ToDockerConfig()
	if dc == nil {
		t.Fatal("expected non-nil Docker config")
	}
	if dc.Interval != 30*time.Second {
		t.Errorf("expected default interval 30s, got %v", dc.Interval)
	}
	if dc.Timeout != 10*time.Second {
		t.Errorf("expected default timeout 10s, got %v", dc.Timeout)
	}
	if dc.StartPeriod != 60*time.Second {
		t.Errorf("expected default start period 60s, got %v", dc.StartPeriod)
	}
	if dc.Retries != 3 {
		t.Errorf("expected default retries 3, got %d", dc.Retries)
	}
}

func TestHealthCheckConfig_CustomValues(t *testing.T) {
	hc := &HealthCheckConfig{
		Test:        []string{"CMD-SHELL", "custom-check"},
		Interval:    10 * time.Second,
		Timeout:     5 * time.Second,
		StartPeriod: 30 * time.Second,
		Retries:     1,
	}
	dc := hc.ToDockerConfig()
	if dc.Interval != 10*time.Second {
		t.Errorf("expected interval 10s, got %v", dc.Interval)
	}
	if dc.Timeout != 5*time.Second {
		t.Errorf("expected timeout 5s, got %v", dc.Timeout)
	}
	if dc.StartPeriod != 30*time.Second {
		t.Errorf("expected start period 30s, got %v", dc.StartPeriod)
	}
	if dc.Retries != 1 {
		t.Errorf("expected retries 1, got %d", dc.Retries)
	}
}

// ---------------------------------------------------------------------------
// Container name
// ---------------------------------------------------------------------------

func TestContainerName(t *testing.T) {
	tests := []struct {
		project string
		role    string
		want    string
	}{
		{"myshop", "app", "yourplatform_myshop_app"},
		{"My Blog", "postgres", "yourplatform_my-blog_postgres"},
		{"my_project", "redis", "yourplatform_my-project_redis"},
		{"", "app", "yourplatform_project_app"},
	}
	for _, tc := range tests {
		got := ContainerName(tc.project, tc.role)
		if got != tc.want {
			t.Errorf("ContainerName(%q, %q) = %q, want %q", tc.project, tc.role, got, tc.want)
		}
	}
}

func TestContainerRole(t *testing.T) {
	if ContainerRole(ContainerTypeApp) != "app" {
		t.Errorf("expected 'app', got %s", ContainerRole(ContainerTypeApp))
	}
	if ContainerRole(ContainerTypePostgres) != "postgres" {
		t.Errorf("expected 'postgres', got %s", ContainerRole(ContainerTypePostgres))
	}
	if ContainerRole(ContainerTypeRedis) != "redis" {
		t.Errorf("expected 'redis', got %s", ContainerRole(ContainerTypeRedis))
	}
}

// ---------------------------------------------------------------------------
// Container labels
// ---------------------------------------------------------------------------

func TestContainerLabels(t *testing.T) {
	labels := ContainerLabels("My App", ContainerTypePostgres)
	if labels[containerLabelOwner] != "yourplatform-agent" {
		t.Errorf("expected owner label 'yourplatform-agent'")
	}
	if labels[containerLabelProject] != "my-app" {
		t.Errorf("expected project 'my-app', got '%s'", labels[containerLabelProject])
	}
	if labels[containerLabelRole] != "postgres" {
		t.Errorf("expected role 'postgres', got '%s'", labels[containerLabelRole])
	}
	if labels[containerLabelManagedBy] != "yourplatform-agent" {
		t.Errorf("expected managed-by 'yourplatform-agent'")
	}
}

// ---------------------------------------------------------------------------
// StartContainerWithWait (structural — no real Docker)
// ---------------------------------------------------------------------------

func TestStartContainerWithWait_NoDocker(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	err := client.StartContainerWithWait(context.Background(), "abc123", 2*time.Second)
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
}

func TestStopContainerGraceful_NoDocker(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	err := client.StopContainerGraceful(context.Background(), "abc123")
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
}

func TestRemoveContainerSafe_NoDocker(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	err := client.RemoveContainerSafe(context.Background(), "abc123")
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
}

// ---------------------------------------------------------------------------
// Crash error
// ---------------------------------------------------------------------------

func TestCrashError(t *testing.T) {
	err := &CrashError{
		ContainerID: "abc123",
		ExitCode:    1,
		Logs:        "Error: something broke",
	}
	msg := err.Error()
	if msg == "" {
		t.Error("expected non-empty error message")
	}
}

func TestCrashError_NoLogs(t *testing.T) {
	err := &CrashError{
		ContainerID: "abc123",
		ExitCode:    137,
	}
	msg := err.Error()
	if msg == "" {
		t.Error("expected non-empty error message")
	}
}

func TestIsOOMKill(t *testing.T) {
	if !IsOOMKill(137) {
		t.Error("expected exit code 137 to be OOM kill")
	}
	if IsOOMKill(1) {
		t.Error("expected exit code 1 to not be OOM kill")
	}
	if IsOOMKill(0) {
		t.Error("expected exit code 0 to not be OOM kill")
	}
}

// ---------------------------------------------------------------------------
// GetContainerHealth (structural — no real Docker)
// ---------------------------------------------------------------------------

func TestGetContainerHealth_NoDocker(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := client.GetContainerHealth(ctx, "abc1234567890abcdef")
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
}

// ---------------------------------------------------------------------------
// DeployContainer (structural — no real Docker)
// ---------------------------------------------------------------------------

func TestDeployContainer_NoDocker(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := client.DeployContainer(ctx, CreateContainerOpts{
		Name:  "test-container",
		Image: "nginx:latest",
	})
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
}

// ---------------------------------------------------------------------------
// RestartContainer (structural — no real Docker)
// ---------------------------------------------------------------------------

func TestRestartContainer_NoDocker(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := client.RestartContainer(ctx, "abc1234567890abcdef")
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
}

// ---------------------------------------------------------------------------
// ReplaceExistingContainer (structural — no real Docker)
// ---------------------------------------------------------------------------

func TestReplaceExistingContainer_NoDocker(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := client.ReplaceExistingContainer(ctx, "test-container")
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
}

// ---------------------------------------------------------------------------
// ToDockerConfig — health check conversion
// ---------------------------------------------------------------------------

func TestToDockerConfig_App(t *testing.T) {
	hc := DefaultHealthCheck(ContainerTypeApp, 8080)
	dockerCfg := hc.ToDockerConfig()
	if dockerCfg == nil {
		t.Fatal("expected non-nil Docker config")
	}
	if dockerCfg.Interval != 30*time.Second {
		t.Errorf("expected 30s interval, got %v", dockerCfg.Interval)
	}
	if dockerCfg.Timeout != 10*time.Second {
		t.Errorf("expected 10s timeout, got %v", dockerCfg.Timeout)
	}
	if dockerCfg.StartPeriod != 60*time.Second {
		t.Errorf("expected 60s start period, got %v", dockerCfg.StartPeriod)
	}
	if dockerCfg.Retries != 3 {
		t.Errorf("expected 3 retries, got %d", dockerCfg.Retries)
	}
	if len(dockerCfg.Test) == 0 {
		t.Error("expected non-empty test command")
	}
}

func TestToDockerConfig_Postgres(t *testing.T) {
	hc := DefaultHealthCheck(ContainerTypePostgres, 5432)
	dockerCfg := hc.ToDockerConfig()
	if dockerCfg == nil {
		t.Fatal("expected non-nil Docker config")
	}
	if len(dockerCfg.Test) == 0 {
		t.Error("expected non-empty test command")
	}
}

// ---------------------------------------------------------------------------
// GetTotalRAMMB
// ---------------------------------------------------------------------------

func TestGetTotalRAMMB(t *testing.T) {
	// On Windows this returns 0 (stub), on Linux it reads /proc/meminfo
	ram := GetTotalRAMMB()
	// Just verify it doesn't panic; value may be 0 on Windows
	_ = ram
}
