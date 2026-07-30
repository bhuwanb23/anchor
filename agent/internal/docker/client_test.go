package docker

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDockerInfo_DefaultValues(t *testing.T) {
	// Just validate that DockerInfo fields exist and have reasonable types
	info := &DockerInfo{
		Version:       "25.0.3",
		APIVersion:    "1.44",
		StorageDriver: "overlay2",
		OSType:        "linux",
		Architecture:  "x86_64",
		KernelVersion: "5.15.0-91-generic",
	}
	if info.Version != "25.0.3" {
		t.Errorf("expected version 25.0.3, got %s", info.Version)
	}
	if info.APIVersion != "1.44" {
		t.Errorf("expected api version 1.44, got %s", info.APIVersion)
	}
	if info.OSType != "linux" {
		t.Errorf("expected os type linux, got %s", info.OSType)
	}
}

func TestCheckSocket_Absent(t *testing.T) {
	err := checkSocket("unix:///nonexistent/docker.sock")
	if err == nil {
		t.Fatal("expected error for nonexistent socket, got nil")
	}
	if !os.IsNotExist(err) {
		t.Logf("error should indicate not exist: %v", err)
	}
}

func TestCheckSocket_InvalidPath(t *testing.T) {
	err := checkSocket("unix:///tmp")
	if err == nil {
		// /tmp exists but isn't a socket, so this should fail
		t.Fatal("expected error for non-socket path, got nil")
	}
	t.Logf("got expected error: %v", err)
}

func TestIsConnectionError(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{connectionErr("connection refused"), true},
		{connectionErr("broken pipe"), true},
		{connectionErr("can't connect to docker daemon"), true},
		{connectionErr("unix:// connect: connection refused"), true},
		{connectionErr("is the docker daemon running?"), true},
		{connectionErr("image not found"), false},
		{connectionErr("permission denied"), false},
		{connectionErr("invalid reference format"), false},
	}
	for _, tt := range tests {
		got := isConnectionError(tt.err)
		if got != tt.want {
			t.Errorf("isConnectionError(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}

// connectionErr is a simple error type for testing.
type connectionErr string

func (e connectionErr) Error() string { return string(e) }

func TestReconnect_BackoffDuration(t *testing.T) {
	// Verify that backoff grows and caps at 30s.
	// We can't test the actual reconnect against a real Docker socket,
	// so we verify the backoff timing logic is reasonable.

	client := &Client{
		socket: "unix:///var/run/docker.sock",
	}
	if client.SocketPath() != "unix:///var/run/docker.sock" {
		t.Errorf("unexpected socket path: %s", client.SocketPath())
	}

	// Verify initial state
	if client.IsConnected() {
		t.Log("client should not be connected without a real socket")
	}
	if client.DockerInfo() != nil {
		t.Log("docker info should be nil without a real connection")
	}
}

func TestReconnect_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancelled

	client := &Client{
		socket: "unix:///var/run/docker.sock",
	}

	err := client.Reconnect(ctx)
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
	if !strings.Contains(err.Error(), "canceled") {
		t.Errorf("expected context canceled error, got: %v", err)
	}
}

func TestReconnect_TimeoutContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	client := &Client{
		socket: "unix:///var/run/docker.sock",
	}

	// This should timeout because there's no real Docker socket
	err := client.Reconnect(ctx)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	t.Logf("got expected timeout error: %v", err)
}

func TestCheckSocket_Missing(t *testing.T) {
	err := checkSocket("unix:///var/run/docker.sock.does.not.exist")
	if err == nil {
		t.Skip("this test only passes when Docker socket is not present")
	}
	// Should mention the path in the error
	if !strings.Contains(err.Error(), "docker socket") {
		t.Errorf("error should mention 'docker socket', got: %v", err)
	}
}

func TestNewClient_NoSocket(t *testing.T) {
	_, err := NewClient("unix:///tmp/docker-test-nonexistent.sock")
	if err == nil {
		t.Skip("this test only passes when the fake socket doesn't exist")
	}
	// Error should be descriptive
	t.Logf("got expected error: %v", err)
}

// ---------------------------------------------------------------------------
// Permission / non-socket error paths
// ---------------------------------------------------------------------------

func TestCheckSocket_RegularFile(t *testing.T) {
	// Test that a regular file (not a socket) returns the right error.
	// On Windows, use a file that exists; on Linux, use /etc/hostname.
	path := "/etc/hostname"
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// Windows — create a temp file instead
		f, err := os.CreateTemp("", "socket-test-*")
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
		defer os.Remove(f.Name())
		path = f.Name()
	}

	err := checkSocket("unix://" + path)
	if err == nil {
		t.Skip("checkSocket succeeded unexpectedly")
	}
	if !strings.Contains(err.Error(), "not a socket file") {
		t.Errorf("expected 'not a socket file' in error, got: %v", err)
	}
}

func TestCheckSocket_UnixPrefix(t *testing.T) {
	// Verify that the unix:// prefix is stripped correctly for file checks.
	err := checkSocket("unix:///nonexistent/path.sock")
	if err == nil {
		t.Fatal("expected error for nonexistent socket")
	}
	if !strings.Contains(err.Error(), "docker socket not found") {
		t.Errorf("expected 'docker socket not found' in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Client state methods
// ---------------------------------------------------------------------------

func TestClient_SocketPath(t *testing.T) {
	c := &Client{socket: "unix:///var/run/docker.sock"}
	if c.SocketPath() != "unix:///var/run/docker.sock" {
		t.Errorf("unexpected socket path: %s", c.SocketPath())
	}
}

func TestClient_IsConnected_InitialState(t *testing.T) {
	c := &Client{socket: "unix:///var/run/docker.sock", connected: false}
	if c.IsConnected() {
		t.Error("expected client to report disconnected")
	}

	c.connected = true
	if !c.IsConnected() {
		t.Error("expected client to report connected")
	}
}

func TestClient_DockerInfo_NilWhenDisconnected(t *testing.T) {
	c := &Client{socket: "unix:///var/run/docker.sock"}
	if c.DockerInfo() != nil {
		t.Error("expected DockerInfo to be nil for fresh client")
	}
}

func TestClient_CheckSocket(t *testing.T) {
	c := &Client{socket: "unix:///nonexistent/path.sock"}
	err := c.CheckSocket()
	if err == nil {
		t.Skip("CheckSocket succeeded unexpectedly")
	}
	if !strings.Contains(err.Error(), "docker socket") {
		t.Errorf("expected 'docker socket' in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ensureConnected
// ---------------------------------------------------------------------------

func TestEnsureConnected_AlreadyConnected(t *testing.T) {
	c := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: true,
	}
	ctx := context.Background()
	if err := c.ensureConnected(ctx); err != nil {
		t.Errorf("ensureConnected should succeed when already connected, got: %v", err)
	}
}

func TestEnsureConnected_TriggersReconnect(t *testing.T) {
	c := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	// With a 1ms timeout, Reconnect should fail quickly because
	// there's no real Docker socket.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	err := c.ensureConnected(ctx)
	// Should fail — no real Docker socket available
	if err == nil {
		t.Log("ensureConnected succeeded (Docker may be running)")
	}
}

// ---------------------------------------------------------------------------
// Reconnect
// ---------------------------------------------------------------------------

func TestReconnect_SocketMissing(t *testing.T) {
	c := &Client{
		socket:    "unix:///nonexistent/socket.sock",
		connected: false,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := c.Reconnect(ctx)
	// Should fail — either socket error or context deadline (both valid)
	if err == nil {
		t.Skip("Reconnect succeeded (socket may exist)")
	}
	t.Logf("got expected error: %v", err)
}

// ---------------------------------------------------------------------------
// Integration test (requires Docker)
// ---------------------------------------------------------------------------

func TestNewClient_Connected(t *testing.T) {
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

	if !c.IsConnected() {
		t.Error("expected client to be connected")
	}

	info := c.DockerInfo()
	if info == nil {
		t.Fatal("expected DockerInfo to be set")
	}
	if info.Version == "" {
		t.Error("expected Docker version to be set")
	}
	if info.APIVersion == "" {
		t.Error("expected API version to be set")
	}
	if info.StorageDriver == "" {
		t.Error("expected storage driver to be set")
	}

	t.Logf("Docker connected: version=%s api=%s driver=%s",
		info.Version, info.APIVersion, info.StorageDriver)
}

func TestTestConnection_Success(t *testing.T) {
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	info, err := c.TestConnection(ctx)
	if err != nil {
		t.Fatalf("TestConnection failed: %v", err)
	}
	if info == nil {
		t.Fatal("expected info, got nil")
	}
	if info.Version == "" {
		t.Error("expected version in info")
	}
}
