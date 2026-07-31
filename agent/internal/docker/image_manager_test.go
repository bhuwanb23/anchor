package docker

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// ImageRef parsing
// ---------------------------------------------------------------------------

func TestParseImageRef_SimpleName(t *testing.T) {
	r := ParseImageRef("nginx")
	if r.Name != "nginx" {
		t.Errorf("expected name 'nginx', got '%s'", r.Name)
	}
	if r.Tag != "latest" {
		t.Errorf("expected tag 'latest', got '%s'", r.Tag)
	}
	if r.Registry != "" {
		t.Errorf("expected empty registry, got '%s'", r.Registry)
	}
	if r.Full != "nginx" {
		t.Errorf("expected full 'nginx', got '%s'", r.Full)
	}
}

func TestParseImageRef_WithTag(t *testing.T) {
	r := ParseImageRef("nginx:1.25")
	if r.Name != "nginx" {
		t.Errorf("expected name 'nginx', got '%s'", r.Name)
	}
	if r.Tag != "1.25" {
		t.Errorf("expected tag '1.25', got '%s'", r.Tag)
	}
}

func TestParseImageRef_LatestTag(t *testing.T) {
	r := ParseImageRef("nginx:latest")
	if r.Name != "nginx" {
		t.Errorf("expected name 'nginx', got '%s'", r.Name)
	}
	if r.Tag != "latest" {
		t.Errorf("expected tag 'latest', got '%s'", r.Tag)
	}
	if !r.IsLatest() {
		t.Error("expected IsLatest()=true")
	}
}

func TestParseImageRef_WithRegistry(t *testing.T) {
	r := ParseImageRef("ghcr.io/owner/myapp:v1.0")
	if r.Registry != "ghcr.io" {
		t.Errorf("expected registry 'ghcr.io', got '%s'", r.Registry)
	}
	if r.Name != "owner/myapp" {
		t.Errorf("expected name 'owner/myapp', got '%s'", r.Name)
	}
	if r.Tag != "v1.0" {
		t.Errorf("expected tag 'v1.0', got '%s'", r.Tag)
	}
	if !r.IsRegistrySpecific() {
		t.Error("expected IsRegistrySpecific()=true")
	}
}

func TestParseImageRef_Empty(t *testing.T) {
	r := ParseImageRef("")
	if r.Name != "" {
		t.Errorf("expected empty name, got '%s'", r.Name)
	}
	if r.Tag != "latest" {
		t.Errorf("expected tag 'latest', got '%s'", r.Tag)
	}
}

func TestParseImageRef_DockerIO(t *testing.T) {
	r := ParseImageRef("docker.io/library/nginx:latest")
	if r.Registry != "docker.io" {
		t.Errorf("expected registry 'docker.io', got '%s'", r.Registry)
	}
	if r.Name != "library/nginx" {
		t.Errorf("expected name 'library/nginx', got '%s'", r.Name)
	}
	if r.Tag != "latest" {
		t.Errorf("expected tag 'latest', got '%s'", r.Tag)
	}
}

func TestParseImageRef_LibraryPath(t *testing.T) {
	r := ParseImageRef("library/nginx")
	if r.Name != "library/nginx" {
		t.Errorf("expected name 'library/nginx', got '%s'", r.Name)
	}
	if r.Tag != "latest" {
		t.Errorf("expected tag 'latest', got '%s'", r.Tag)
	}
}

func TestImageRef_Normalized(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"nginx", "nginx:latest"},
		{"nginx:1.25", "nginx:1.25"},
		{"postgres:16", "postgres:16"},
		{"ghcr.io/owner/myapp:v1.0", "ghcr.io/owner/myapp:v1.0"},
	}
	for _, tt := range tests {
		r := ParseImageRef(tt.input)
		normalized := r.Normalized()
		if normalized != tt.expected {
			t.Errorf("ParseImageRef(%q).Normalized() = %q, want %q", tt.input, normalized, tt.expected)
		}
	}
}

func TestImageRef_IsLatest(t *testing.T) {
	if !ParseImageRef("nginx:latest").IsLatest() {
		t.Error("expected IsLatest for 'nginx:latest'")
	}
	if !ParseImageRef("nginx").IsLatest() {
		t.Error("expected IsLatest for 'nginx' (no tag)")
	}
	if ParseImageRef("nginx:1.25").IsLatest() {
		t.Error("expected !IsLatest for 'nginx:1.25'")
	}
}

// ---------------------------------------------------------------------------
// PullProgress
// ---------------------------------------------------------------------------

func TestPullProgress_DefaultValues(t *testing.T) {
	pp := PullProgress{
		ID:     "abc123",
		Status: "Downloading",
		Current: 1024,
		Total:   4096,
	}
	if pp.ID != "abc123" {
		t.Errorf("expected ID 'abc123', got '%s'", pp.ID)
	}
	if pp.Status != "Downloading" {
		t.Errorf("expected status 'Downloading', got '%s'", pp.Status)
	}
}

// ---------------------------------------------------------------------------
// ImageSummary
// ---------------------------------------------------------------------------

func TestImageSummary_HumanSize(t *testing.T) {
	tests := []struct {
		size int64
		want string
	}{
		{500, "500B"},
		{2048, "2.0KB"},
		{1048576, "1.0MB"},
		{1073741824, "1.0GB"},
	}
	for _, tt := range tests {
		s := &ImageSummary{SizeBytes: tt.size}
		got := s.HumanSize()
		if got != tt.want {
			t.Errorf("HumanSize(%d) = %q, want %q", tt.size, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Pull error classification
// ---------------------------------------------------------------------------

func TestClassifyPullError_ImageNotFound(t *testing.T) {
	original := errors.New("manifest for nginx:invalid not found: manifest unknown")
	pe := classifyPullError("nginx:invalid", original)
	if pe == nil {
		t.Fatal("expected PullError, got nil")
	}
	if !errors.Is(pe, ErrImageNotFound) {
		t.Errorf("expected ErrImageNotFound, got %v", pe.Err)
	}
	if !strings.Contains(pe.Message, "nginx:invalid") {
		t.Errorf("message should mention image name: %s", pe.Message)
	}
}

func TestClassifyPullError_RegistryAuth(t *testing.T) {
	original := errors.New("unauthorized: authentication required")
	pe := classifyPullError("ghcr.io/private/app:latest", original)
	if pe == nil {
		t.Fatal("expected PullError, got nil")
	}
	if !errors.Is(pe, ErrRegistryAuth) {
		t.Errorf("expected ErrRegistryAuth, got %v", pe.Err)
	}
	if !strings.Contains(pe.Message, "registry authentication") {
		t.Errorf("message should mention auth: %s", pe.Message)
	}
}

func TestClassifyPullError_Network(t *testing.T) {
	original := errors.New("i/o timeout")
	pe := classifyPullError("nginx:latest", original)
	if pe == nil {
		t.Fatal("expected PullError, got nil")
	}
	if !errors.Is(pe, ErrNetwork) {
		t.Errorf("expected ErrNetwork, got %v", pe.Err)
	}
}

func TestClassifyPullError_DiskFull(t *testing.T) {
	original := errors.New("write /var/lib/docker/overlay2: no space left on device")
	pe := classifyPullError("postgres:16", original)
	if pe == nil {
		t.Fatal("expected PullError, got nil")
	}
	if !errors.Is(pe, ErrDiskFull) {
		t.Errorf("expected ErrDiskFull, got %v", pe.Err)
	}
	if !strings.Contains(pe.Message, "disk space") {
		t.Errorf("message should mention disk: %s", pe.Message)
	}
}

func TestClassifyPullError_Timeout(t *testing.T) {
	original := errors.New("context deadline exceeded")
	pe := classifyPullError("myapp:latest", original)
	if pe == nil {
		t.Fatal("expected PullError, got nil")
	}
	if !errors.Is(pe, ErrPullTimeout) {
		t.Errorf("expected ErrPullTimeout, got %v", pe.Err)
	}
}

func TestClassifyPullError_Denied(t *testing.T) {
	original := errors.New("denied: requested access to the resource is denied")
	pe := classifyPullError("private/repo:latest", original)
	if pe == nil {
		t.Fatal("expected PullError, got nil")
	}
	if !errors.Is(pe, ErrRegistryAuth) {
		t.Errorf("expected ErrRegistryAuth, got %v", pe.Err)
	}
}

func TestClassifyPullError_NoBasicAuth(t *testing.T) {
	original := errors.New("no basic auth credentials")
	pe := classifyPullError("ghcr.io/owner/app:latest", original)
	if pe == nil {
		t.Fatal("expected PullError, got nil")
	}
	if !errors.Is(pe, ErrRegistryAuth) {
		t.Errorf("expected ErrRegistryAuth, got %v", pe.Err)
	}
}

func TestClassifyPullError_Unknown(t *testing.T) {
	original := errors.New("some weird error we haven't seen before")
	pe := classifyPullError("test:latest", original)
	if pe == nil {
		t.Fatal("expected PullError, got nil")
	}
	if !errors.Is(pe, ErrUnknownPullFailure) {
		t.Errorf("expected ErrUnknownPullFailure, got %v", pe.Err)
	}
}

func TestClassifyPullError_NilInput(t *testing.T) {
	pe := classifyPullError("test:latest", nil)
	if pe != nil {
		t.Fatal("expected nil for nil original error")
	}
}

// ---------------------------------------------------------------------------
// Progress callback
// ---------------------------------------------------------------------------

func TestPullProgressFunc(t *testing.T) {
	// Verify the callback type works as expected
	progresses := make([]PullProgress, 0)
	fn := PullProgressFunc(func(p PullProgress) error {
		progresses = append(progresses, p)
		return nil
	})

	if err := fn(PullProgress{ID: "layer1", Status: "Pulling fs layer"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := fn(PullProgress{ID: "layer2", Status: "Downloading", Current: 1024, Total: 4096}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(progresses) != 2 {
		t.Fatalf("expected 2 progress updates, got %d", len(progresses))
	}
	if progresses[0].ID != "layer1" {
		t.Errorf("expected first progress ID 'layer1', got '%s'", progresses[0].ID)
	}
}

func TestPullProgressFunc_Abort(t *testing.T) {
	// Verify callback can abort the pull by returning an error
	fn := PullProgressFunc(func(p PullProgress) error {
		if p.ID == "layer3" {
			return errors.New("stop")
		}
		return nil
	})

	if err := fn(PullProgress{ID: "layer1"}); err != nil {
		t.Fatalf("unexpected error on layer1: %v", err)
	}
	if err := fn(PullProgress{ID: "layer3"}); err == nil {
		t.Fatal("expected error on layer3, got nil")
	}
}

// ---------------------------------------------------------------------------
// ImageExistsLocally behaviour (structural test, no real Docker)
// ---------------------------------------------------------------------------

func TestImageExistsLocally_NoDocker(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	_, exists, err := client.ImageExistsLocally(context.Background(), "nginx:latest")
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
	if exists {
		t.Error("expected exists=false without Docker")
	}
}

// ---------------------------------------------------------------------------
// PullImageIfNeeded behaviour (structural test)
// ---------------------------------------------------------------------------

func TestPullImageIfNeeded_NoDocker(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	_, pulled, err := client.PullImageIfNeeded(context.Background(), "nginx:latest", nil, nil)
	if err == nil {
		t.Skip("expected error without real Docker socket")
	}
	if pulled {
		t.Error("expected pulled=false without Docker")
	}
}

func TestPullImageIfNeeded_CallsEnsureConnected(t *testing.T) {
	client := &Client{
		socket:    "unix:///var/run/docker.sock",
		connected: false,
	}
	_, _, err := client.PullImageIfNeeded(context.Background(), "nonexistent:test", nil, nil)
	if err == nil {
		t.Skip("expected error — no Docker socket")
	}
	// Error should mention "docker unavailable"
	if !strings.Contains(err.Error(), "docker unavailable") {
		t.Errorf("expected 'docker unavailable' in error, got: %v", err)
	}
}
