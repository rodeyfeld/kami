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
	Resources []Resource
	// Scaled-to-zero workloads are left out of every count
	TotalCount    int
	UpCount       int
	DegradedCount int
	DownCount     int
	// Running workloads whose only health signal is the pod's Ready flag (exec probe or none)
	UnverifiedCount int
	UpdatedAt       time.Time
}

// Health states shared by every monitor
const (
	StateUp           = "Up"
	StateDegraded     = "Degraded"
	StateDown         = "Down"
	StateUnresponsive = "Unresponsive" // pods are Ready but the health check fails
	StateOff          = "Off"
	StateSucceeded    = "Succeeded"
	StateRunning      = "Running"
	StateFailed       = "Failed"
	StatePending      = "Pending"
)

type Resource struct {
	Name    string
	Group   string // project the resource belongs to, e.g. "noctis" for noctis-postgres
	Type    string
	State   string
	Status  string
	Current int32
	Desired int32
	Labels  map[string]string
	Ping    *k8s.PingResult // nil when the resource isn't pingable (jobs, docker mode)
}

func (s *Status) count(state string, ping *k8s.PingResult) {
	switch state {
	case StateOff:
		return
	case StateUp, StateSucceeded, StateRunning:
		s.UpCount++
		if ping != nil && !ping.Verified() {
			s.UnverifiedCount++
		}
	case StateDegraded, StatePending:
		s.DegradedCount++
	default:
		s.DownCount++
	}
	s.TotalCount++
}
