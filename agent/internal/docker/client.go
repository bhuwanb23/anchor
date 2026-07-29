package docker

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/docker/go-connections/nat"
)

type Client struct {
	cli *client.Client
}

func NewClient(dockerSocket string) (*Client, error) {
	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithHost(dockerSocket),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, err
	}

	return &Client{cli: cli}, nil
}

func (c *Client) PullImage(ctx context.Context, ref string) error {
	slog.Info("pulling image", "image", ref)

	reader, err := c.cli.ImagePull(ctx, ref, types.ImagePullOptions{})
	if err != nil {
		return err
	}
	defer reader.Close()

	var progress jsonmessage.JSONMessage
	decoder := json.NewDecoder(reader)
	for decoder.More() {
		if err := decoder.Decode(&progress); err != nil {
			slog.Warn("image pull progress decode error", "error", err)
			continue
		}
		if progress.Status != "" {
			slog.Info("pull progress", "image", ref, "status", progress.Status)
		}
	}

	return nil
}

func (c *Client) CreateContainer(ctx context.Context, opts CreateContainerOpts) (string, error) {
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

	resp, err := c.cli.ContainerCreate(ctx, config, hostConfig, nil, nil, opts.Name)
	if err != nil {
		return "", err
	}

	return resp.ID, nil
}

func (c *Client) StartContainer(ctx context.Context, id string) error {
	slog.Info("starting container", "id", id)
	return c.cli.ContainerStart(ctx, id, types.ContainerStartOptions{})
}

func (c *Client) StopContainer(ctx context.Context, id string) error {
	slog.Info("stopping container", "id", id)
	timeout := 10 * time.Second
	return c.cli.ContainerStop(ctx, id, &timeout)
}

func (c *Client) RemoveContainer(ctx context.Context, id string) error {
	slog.Info("removing container", "id", id)
	return c.cli.ContainerRemove(ctx, id, types.ContainerRemoveOptions{})
}

func (c *Client) ListContainers(ctx context.Context) ([]types.Container, error) {
	return c.cli.ContainerList(ctx, types.ContainerListOptions{All: true})
}

func (c *Client) GetContainerLogs(ctx context.Context, id string) (io.ReadCloser, error) {
	return c.cli.ContainerLogs(ctx, id, types.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: true,
	})
}

type CreateContainerOpts struct {
	Name         string
	Image        string
	PortBindings nat.PortMap
	ExposedPorts nat.PortSet
	Env          []string
}
