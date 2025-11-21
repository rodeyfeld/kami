//go:build !dev

package monitor

import (
	"context"
	"errors"
	"kami/internal/docker"
)

type DockerMonitor struct{}

func NewDockerMonitor(*docker.Client) *DockerMonitor { return &DockerMonitor{} }
func (m *DockerMonitor) GetMode() string             { return "standalone" }
func (m *DockerMonitor) GetStatus(context.Context) (*Status, error) {
	return nil, errors.New("docker not available in production")
}
