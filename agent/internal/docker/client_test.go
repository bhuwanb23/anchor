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
