//go:build dev

package docker

import (
	"context"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

type Client struct {
	cli *client.Client
}

// NewClient creates a new Docker client (dev mode only)
func NewClient() (*Client, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &Client{cli: cli}, nil
}

// IsAvailable returns true if Docker client is available (always true in dev mode)
func IsAvailable() bool {
	return true
}

// GetContainerStatus retrieves the status of all containers
func (c *Client) GetContainerStatus(ctx context.Context) (*ContainerStatus, error) {
	containers, err := c.cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, err
	}

	status := &ContainerStatus{
		Containers:   make([]ContainerInfo, 0),
		TotalCount:   len(containers),
		RunningCount: 0,
		StoppedCount: 0,
	}

	for _, container := range containers {
		info := ContainerInfo{
			ID:      container.ID[:12], // Short ID
			Name:    container.Names[0][1:], // Remove leading slash
			Image:   container.Image,
			State:   container.State,
			Status:  container.Status,
			Project: container.Labels["com.docker.compose.project"],
		}

		if container.State == "running" {
			status.RunningCount++
		} else {
			status.StoppedCount++
		}

		status.Containers = append(status.Containers, info)
	}

	return status, nil
}

