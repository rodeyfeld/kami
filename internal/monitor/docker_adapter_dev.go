//go:build dev

package monitor

import (
	"context"
	"kami/internal/docker"
)

type DockerMonitor struct {
	client *docker.Client
}

func NewDockerMonitor(c *docker.Client) *DockerMonitor {
	return &DockerMonitor{client: c}
}

func (m *DockerMonitor) GetMode() string { return "docker" }

func (m *DockerMonitor) GetStatus(ctx context.Context) (*Status, error) {
	d, err := m.client.GetContainerStatus(ctx)
	if err != nil {
		return nil, err
	}

	status := &Status{
		Resources:      make([]Resource, 0, len(d.Containers)),
		TotalCount:     d.TotalCount,
		HealthyCount:   d.RunningCount,
		UnhealthyCount: d.StoppedCount,
	}

	for _, c := range d.Containers {
		state := "unhealthy"
		if c.State == "running" {
			state = "healthy"
		}

		status.Resources = append(status.Resources, Resource{
			Name:    c.Name,
			Type:    "container",
			State:   state,
			Status:  c.Status,
			Current: 1,
			Desired: 1,
			Labels: map[string]string{
				"image":   c.Image,
				"project": c.Project,
				"id":      c.ID,
			},
		})
	}

	return status, nil
}
