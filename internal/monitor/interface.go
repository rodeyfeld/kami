package monitor

import (
	"context"
	"errors"
	"kami/internal/k8s"
	"time"
)

// ErrPingUnsupported is returned by monitors that can't reach workloads over the network
var ErrPingUnsupported = errors.New("health pings require kubernetes mode")

type Monitor interface {
	GetStatus(ctx context.Context) (*Status, error)
	GetMode() string
	Ping(ctx context.Context, name string) (*k8s.PingResult, error)
	PingAll(ctx context.Context) ([]*k8s.PingResult, error)
}

type Status struct {
	Resources      []Resource
	TotalCount     int
	HealthyCount   int
	DegradedCount  int
	UnhealthyCount int
	UpdatedAt      time.Time
}

type Resource struct {
	Name    string
	Type    string
	State   string
	Status  string
	Current int32
	Desired int32
	Labels  map[string]string
	Ping    *k8s.PingResult // nil when the resource isn't pingable (jobs, docker mode)
}
