//go:build !dev

package monitor

import (
	"context"
	"errors"
	"kami/internal/docker"
	"kami/internal/k8s"
)

type DockerMonitor struct{}

func NewDockerMonitor(*docker.Client) *DockerMonitor { return &DockerMonitor{} }
func (m *DockerMonitor) GetMode() string             { return "standalone" }
func (m *DockerMonitor) GetStatus(context.Context) (*Status, error) {
	return nil, errors.New("docker not available in production")
}
func (m *DockerMonitor) Ping(context.Context, string) (*k8s.PingResult, error) {
	return nil, ErrPingUnsupported
}
func (m *DockerMonitor) PingAll(context.Context) ([]*k8s.PingResult, error) {
	return nil, ErrPingUnsupported
}
func (m *DockerMonitor) Deploys(context.Context) (*k8s.Deploys, error) {
	return nil, ErrDeploysUnsupported
}
