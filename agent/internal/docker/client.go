package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"os"
	"strings"
	"sync"
	"time"

	"errors"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/docker/go-connections/nat"
)

// DockerInfo holds cached information from the Docker engine info API.
type DockerInfo struct {
	Version       string `json:"version"`
	APIVersion    string `json:"api_version"`
	StorageDriver string `json:"storage_driver"`
	OSType        string `json:"os_type"`
	Architecture  string `json:"architecture"`
	KernelVersion string `json:"kernel_version"`
}

// Client wraps the Docker SDK client with connection resilience,
// thread safety, and a cached info object.
type Client struct {
	socket    string
	cli       *client.Client
	info      *DockerInfo
	connected bool
	mu        sync.RWMutex
}

// NewClient creates a new Docker client, checks the socket, and
// attempts a test connection. If the socket is missing or unreadable,
// an error is returned immediately. If the test connection fails
// (e.g., Docker daemon is restarting), the client is still returned
// but IsConnected() will return false — operations will attempt to
// reconnect with backoff.
func NewClient(socket string) (*Client, error) {
	if err := checkSocket(socket); err != nil {
		return nil, err
	}

	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithHost(socket),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}

	c := &Client{
		socket: socket,
		cli:    cli,
	}

	// Test connection to get Docker info
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if info, err := c.fetchInfo(ctx); err == nil {
		c.info = info
		c.connected = true
		slog.Info("docker connected",
			"version", info.Version,
			"api_version", info.APIVersion,
			"storage_driver", info.StorageDriver,
		)
	} else {
		slog.Warn("docker test connection failed (daemon may be restarting)", "error", err)
		// Don't return an error — the agent should survive Docker restarts.
		// The first operation will trigger a reconnect attempt.
	}

	return c, nil
}

// ---------------------------------------------------------------------------
// Socket checks
// ---------------------------------------------------------------------------

// SocketPath returns the Docker socket path being used.
func (c *Client) SocketPath() string {
	return c.socket
}

func checkSocket(socket string) error {
	// Strip unix:// prefix for file checks
	path := socket
	if len(path) > 7 && path[:7] == "unix://" {
		path = path[7:]
	}

	stat, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("docker socket not found at %s: %w", path, err)
		}
		return fmt.Errorf("docker socket stat error at %s: %w", path, err)
	}

	// Socket must be a socket file (not a regular file)
	if stat.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("docker socket path %s exists but is not a socket file", path)
	}

	// Check readability
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("docker socket at %s is not readable: %w\nTry: sudo chmod 660 %s", path, err, path)
	}
	f.Close()

	return nil
}

// CheckSocket performs a runtime check on the Docker socket.
// Returns nil if the socket is still present and readable.
func (c *Client) CheckSocket() error {
	return checkSocket(c.socket)
}

// ---------------------------------------------------------------------------
// Connection testing
// ---------------------------------------------------------------------------

// DockerInfo returns the cached Docker engine info, or nil if
// the test connection has not yet succeeded.
func (c *Client) DockerInfo() *DockerInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.info
}

// IsConnected returns whether the client currently believes it has
// a working connection to the Docker daemon.
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// TestConnection performs a fresh GET /info call and updates the
// cached DockerInfo. Returns the info if successful.
func (c *Client) TestConnection(ctx context.Context) (*DockerInfo, error) {
	info, err := c.fetchInfo(ctx)
	if err != nil {
		c.mu.Lock()
		c.connected = false
		c.mu.Unlock()
		return nil, err
	}

	c.mu.Lock()
	c.info = info
	c.connected = true
	c.mu.Unlock()

	return info, nil
}

func (c *Client) fetchInfo(ctx context.Context) (*DockerInfo, error) {
	dockerInfo, err := c.cli.Info(ctx)
	if err != nil {
		return nil, fmt.Errorf("docker info call failed: %w", err)
	}

	// Get server version separately (Info doesn't include it on older SDKs)
	ver := c.cli.ClientVersion()

	return &DockerInfo{
		Version:       dockerInfo.ServerVersion,
		APIVersion:    ver,
		StorageDriver: dockerInfo.Driver,
		OSType:        dockerInfo.OSType,
		Architecture:  dockerInfo.Architecture,
		KernelVersion: dockerInfo.KernelVersion,
	}, nil
}

// ---------------------------------------------------------------------------
// Reconnection with exponential backoff
// ---------------------------------------------------------------------------

// Reconnect attempts to re-establish the connection to Docker with
// exponential backoff. Returns nil on success. The caller should
// check ctx.Done() to abort retries.
func (c *Client) Reconnect(ctx context.Context) error {
	backoff := 1 * time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		slog.Info("attempting to reconnect to docker daemon")
		if err := c.CheckSocket(); err != nil {
			slog.Warn("docker socket not available during reconnect", "error", err)
			goto wait
		}

		// Recreate the SDK client (handles daemon restarts that change
		// the in-memory connection state)
		cli, err := client.NewClientWithOpts(
			client.FromEnv,
			client.WithHost(c.socket),
			client.WithAPIVersionNegotiation(),
		)
		if err != nil {
			slog.Warn("failed to recreate docker client", "error", err)
			goto wait
		}

		c.mu.Lock()
		c.cli = cli
		c.mu.Unlock()

		// Test the new connection
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		info, err := c.fetchInfo(checkCtx)
		cancel()

		if err == nil {
			c.mu.Lock()
			c.info = info
			c.connected = true
			c.mu.Unlock()

			slog.Info("reconnected to docker daemon",
				"version", info.Version,
				"api_version", info.APIVersion,
			)
			return nil
		}

		slog.Warn("docker reconnect test failed", "error", err)

	wait:
		c.mu.Lock()
		c.connected = false
		c.mu.Unlock()

		// Exponential backoff with jitter
		jitter := time.Duration(rand.Int63n(int64(backoff / 2)))
		waitTime := backoff + jitter
		if waitTime > maxBackoff {
			waitTime = maxBackoff
		}

		slog.Info("waiting before next docker reconnect attempt",
			"wait_seconds", waitTime.Seconds(),
		)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitTime):
		}

		backoff = backoff * 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// ensureConnected checks if the client is connected and attempts a
// reconnect if not. It should be called at the start of every public
// method that makes a Docker API call.
func (c *Client) ensureConnected(ctx context.Context) error {
	c.mu.RLock()
	if c.connected {
		c.mu.RUnlock()
		return nil
	}
	c.mu.RUnlock()

	return c.Reconnect(ctx)
}

// ---------------------------------------------------------------------------
// Internal helper — get the SDK client (ensuring we hold the lock)
// ---------------------------------------------------------------------------

// cli returns the wrapped Docker SDK client. The caller must NOT
// store the returned reference across API calls that might trigger
// a reconnect, because reconnect replaces c.cli.
func (c *Client) cliUnsafe() *client.Client {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cli
}

// ---------------------------------------------------------------------------
// Image management
// ---------------------------------------------------------------------------

func (c *Client) PullImage(ctx context.Context, ref string, progressFn PullProgressFunc) (*ImageSummary, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, fmt.Errorf("docker unavailable: %w", err)
	}

	slog.Info("pulling image", "image", ref)

	reader, err := c.cliUnsafe().ImagePull(ctx, ref, types.ImagePullOptions{})
	if err != nil {
		return nil, classifyPullError(ref, err)
	}
	defer reader.Close()

	var msg jsonmessage.JSONMessage
	decoder := json.NewDecoder(reader)
	for decoder.More() {
		if err := decoder.Decode(&msg); err != nil {
			slog.Warn("image pull progress decode error", "error", err)
			continue
		}

		if progressFn != nil {
			pp := PullProgress{
				ID:     msg.ID,
				Status: msg.Status,
				Stream: msg.Stream,
			}
			if msg.Progress != nil {
				pp.Current = msg.Progress.Current
				pp.Total = msg.Progress.Total
			}
			if err := progressFn(pp); err != nil {
				return nil, fmt.Errorf("pull aborted by caller: %w", err)
			}
		}

		if msg.Status != "" {
			slog.Debug("pull progress", "image", ref, "id", msg.ID, "status", msg.Status)
		}
	}

	// Verify the image was pulled successfully
	summary, err := c.VerifyImage(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("image pulled but verification failed: %w", err)
	}

	slog.Info("image pulled successfully",
		"image", ref,
		"size", summary.HumanSize(),
	)

	return summary, nil
}

// PullImageWithRetry pulls an image, retrying with backoff on
// connection errors.
func (c *Client) PullImageWithRetry(ctx context.Context, ref string, maxRetries int, progressFn PullProgressFunc) (*ImageSummary, error) {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		if summary, err := c.PullImage(ctx, ref, progressFn); err != nil {
			lastErr = err
			if isConnectionError(err) || errors.Is(err, ErrNetwork) {
				slog.Warn("pull image connection error, reconnecting...", "attempt", i+1, "max", maxRetries)
				if reconnectErr := c.Reconnect(ctx); reconnectErr != nil {
					return nil, fmt.Errorf("pull image failed and reconnect failed: %v (original: %w)", reconnectErr, err)
				}
				continue
			}
			return nil, err
		} else {
			return summary, nil
		}
	}
	return nil, fmt.Errorf("pull image failed after %d retries: %w", maxRetries, lastErr)
}

// ---------------------------------------------------------------------------
// Container lifecycle
// ---------------------------------------------------------------------------

func (c *Client) CreateContainer(ctx context.Context, opts CreateContainerOpts) (string, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return "", fmt.Errorf("docker unavailable: %w", err)
	}

	config := &container.Config{
		Image:        opts.Image,
		ExposedPorts: opts.ExposedPorts,
		Env:          opts.Env,
		Tty:          false,
		OpenStdin:    false,
	}

	hostConfig := &container.HostConfig{
		PortBindings: opts.PortBindings,
		AutoRemove:   true,
	}

	resp, err := c.cliUnsafe().ContainerCreate(ctx, config, hostConfig, nil, nil, opts.Name)
	if err != nil {
		return "", err
	}

	return resp.ID, nil
}

func (c *Client) StartContainer(ctx context.Context, id string) error {
	if err := c.ensureConnected(ctx); err != nil {
		return fmt.Errorf("docker unavailable: %w", err)
	}

	slog.Info("starting container", "id", id)
	return c.cliUnsafe().ContainerStart(ctx, id, types.ContainerStartOptions{})
}

func (c *Client) StopContainer(ctx context.Context, id string) error {
	if err := c.ensureConnected(ctx); err != nil {
		return fmt.Errorf("docker unavailable: %w", err)
	}

	slog.Info("stopping container", "id", id)
	timeout := 10 * time.Second
	return c.cliUnsafe().ContainerStop(ctx, id, &timeout)
}

func (c *Client) RemoveContainer(ctx context.Context, id string) error {
	if err := c.ensureConnected(ctx); err != nil {
		return fmt.Errorf("docker unavailable: %w", err)
	}

	slog.Info("removing container", "id", id)
	return c.cliUnsafe().ContainerRemove(ctx, id, types.ContainerRemoveOptions{})
}

func (c *Client) ListContainers(ctx context.Context) ([]types.Container, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, fmt.Errorf("docker unavailable: %w", err)
	}

	return c.cliUnsafe().ContainerList(ctx, types.ContainerListOptions{All: true})
}

func (c *Client) GetContainerLogs(ctx context.Context, id string) (io.ReadCloser, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, fmt.Errorf("docker unavailable: %w", err)
	}

	return c.cliUnsafe().ContainerLogs(ctx, id, types.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: true,
	})
}

// ---------------------------------------------------------------------------
// Container inspection
// ---------------------------------------------------------------------------

// InspectContainer returns detailed info about a container.
func (c *Client) InspectContainer(ctx context.Context, id string) (types.ContainerJSON, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return types.ContainerJSON{}, fmt.Errorf("docker unavailable: %w", err)
	}

	resp, err := c.cliUnsafe().ContainerInspect(ctx, id)
	if err != nil {
		return types.ContainerJSON{}, err
	}
	return resp, nil
}

// ---------------------------------------------------------------------------
// Options
// ---------------------------------------------------------------------------

type CreateContainerOpts struct {
	Name         string
	Image        string
	PortBindings nat.PortMap
	ExposedPorts nat.PortSet
	Env          []string
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// isConnectionError returns true if the error is likely a Docker
// daemon connectivity issue (broken pipe, connection refused, etc.).
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// Common Docker connection error patterns
	connectionPatterns := []string{
		"connection refused",
		"broken pipe",
		"connection reset by peer",
		"can't connect",
		"no such host",
		"connect: connection refused",
		"connect: no such file or directory",
		"http: connection timed out",
		"unix:// connect: connection refused",
		"cannot connect to the Docker daemon",
		"is the docker daemon running",
	}
	for _, pattern := range connectionPatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}
	return false
}
