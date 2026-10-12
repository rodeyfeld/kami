package monitor

import (
	"context"
	"errors"
	"kami/internal/k8s"
	"kami/internal/uptime"
	"time"
)

// ErrPingUnsupported is returned by monitors that can't reach workloads over the network
var ErrPingUnsupported = errors.New("health pings require kubernetes mode")

// ErrDeploysUnsupported is returned by monitors that can't see rollouts
var ErrDeploysUnsupported = errors.New("deploy tracking requires kubernetes mode")

type Monitor interface {
	GetStatus(ctx context.Context) (*Status, error)
	GetMode() string
	Ping(ctx context.Context, name string) (*k8s.PingResult, error)
	PingAll(ctx context.Context) ([]*k8s.PingResult, error)
	Deploys(ctx context.Context) (*k8s.Deploys, error)
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

// Level maps a health state onto uptime; scaled-to-zero resources aren't counted at all
func Level(state string) (uptime.Level, bool) {
	switch state {
	case StateOff:
		return 0, false
	case StateUp, StateSucceeded, StateRunning:
		return uptime.Up, true
	case StateDegraded, StatePending:
		return uptime.Degraded, true
	default:
		return uptime.Down, true
	}
}

// Samples turns the status into one uptime sample per component and one per project, where a
// project is as healthy as its worst service
func (s *Status) Samples() []uptime.Sample {
	samples := make([]uptime.Sample, 0, len(s.Resources))
	groups := make(map[string]uptime.Level)
	for _, r := range s.Resources {
		level, counted := Level(r.State)
		if !counted {
			continue
		}
		samples = append(samples, uptime.Sample{Key: r.Name, Level: level})
		// A project's uptime is its services'; a failed job run doesn't take it down
		if r.Type == "Job" {
			continue
		}
		if worst, seen := groups[r.Group]; !seen || level > worst {
			groups[r.Group] = level
		}
	}
	for group, level := range groups {
		samples = append(samples, uptime.Sample{Key: uptime.GroupKey(group), Level: level})
	}
	return samples
}
