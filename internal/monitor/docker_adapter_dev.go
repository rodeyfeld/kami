//go:build dev

package monitor

import (
	"context"
	"kami/internal/docker"
	"kami/internal/k8s"
	"time"
)

type DockerMonitor struct {
	client *docker.Client
}

func NewDockerMonitor(c *docker.Client) *DockerMonitor {
	return &DockerMonitor{client: c}
}

func (m *DockerMonitor) GetMode() string { return "docker" }

func (m *DockerMonitor) Ping(context.Context, string) (*k8s.PingResult, error) {
	return nil, ErrPingUnsupported
}

func (m *DockerMonitor) PingAll(context.Context) ([]*k8s.PingResult, error) {
	return nil, ErrPingUnsupported
}

func (m *DockerMonitor) GetStatus(ctx context.Context) (*Status, error) {
	d, err := m.client.GetContainerStatus(ctx)
	if err != nil {
		return nil, err
	}

	status := &Status{
		Resources: make([]Resource, 0, len(d.Containers)),
		UpdatedAt: time.Now().UTC(),
	}

	for _, c := range d.Containers {
		state := StateDown
		if c.State == "running" {
			state = StateUp
		}
		group := c.Project
		if group == "" {
			group = "standalone"
		}

		status.count(state, nil)
		status.Resources = append(status.Resources, Resource{
			Name:    c.Name,
			Group:   group,
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
