package monitor

import (
	"context"
	"kami/internal/k8s"
	"kami/internal/woodpecker"
	"os"
	"strings"
	"time"
)

type K8sMonitor struct {
	client  *k8s.Client
	pinger  *k8s.Pinger
	deploys *k8s.DeployWatcher
	ci      *woodpecker.Tracker // nil when KAMI_WOODPECKER_URL is "off"
}

const recentPipelines = 15

func NewK8sMonitor(c *k8s.Client) *K8sMonitor {
	deploys := k8s.NewDeployWatcher(c)
	deploys.Start(context.Background())
	m := &K8sMonitor{client: c, pinger: k8s.NewPinger(c), deploys: deploys}

	// ouroboros serves public repos to anonymous readers, so this needs no token
	if url := envOr("KAMI_WOODPECKER_URL", "http://ouroboros-service:8001"); url != "off" {
		public := envOr("KAMI_WOODPECKER_PUBLIC_URL", "https://ouroboros.pinwheel.fan")
		m.ci = woodpecker.NewTracker(woodpecker.NewClient(url), public)
		m.ci.Start(context.Background())
	}
	return m
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func (m *K8sMonitor) GetMode() string { return "kubernetes" }

func (m *K8sMonitor) Ping(ctx context.Context, name string) (*k8s.PingResult, error) {
	return m.pinger.Ping(ctx, name)
}

func (m *K8sMonitor) PingAll(ctx context.Context) ([]*k8s.PingResult, error) {
	return m.pinger.PingAll(ctx)
}

func (m *K8sMonitor) Deploys(ctx context.Context) (*k8s.Deploys, error) {
	snapshot := m.deploys.Snapshot(ctx)
	if m.ci == nil {
		return snapshot, nil
	}

	// The snapshot is shared between requests, so annotate a copy
	d := *snapshot
	d.Rollouts = append([]k8s.Rollout(nil), snapshot.Rollouts...)
	for i := range d.Rollouts {
		r := &d.Rollouts[i]
		// StatefulSet rows only know their pods' age, not when the revision was created
		if r.Kind != "Deployment" {
			continue
		}
		r.Pipeline, r.Unmatched = m.ci.Match(groupOf(r.Workload), r.StartedAt)
		r.Unmatched = r.Pipeline == nil && r.Unmatched
	}
	d.Pipelines = m.ci.Recent(recentPipelines)
	status := m.ci.Status()
	d.CI = &status
	return &d, nil
}

// groupOf treats the name up to the first dash as the project, so noctis-postgres groups under noctis
func groupOf(name string) string {
	group, _, _ := strings.Cut(name, "-")
	return group
}

// state combines the k8s rollout status with the ping, so a workload whose pods are Ready
// but whose health check fails isn't reported as up
func state(s k8s.ServiceStatus, ping *k8s.PingResult) string {
	switch s.Status {
	case "Healthy":
		if s.Type == "Job" {
			return StateSucceeded
		}
		if ping != nil && !ping.OK {
			return StateUnresponsive
		}
		return StateUp
	case "Degraded":
		return StateDegraded
	case "Running":
		return StateRunning
	case "Off":
		return StateOff
	case "Unknown":
		return StatePending
	default:
		if s.Type == "Job" {
			return StateFailed
		}
		return StateDown
	}
}

func (m *K8sMonitor) GetStatus(ctx context.Context) (*Status, error) {
	cluster, err := m.client.GetClusterStatus(ctx)
	if err != nil {
		return nil, err
	}

	pings := make(map[string]*k8s.PingResult)
	if results, err := m.pinger.PingAll(ctx); err == nil {
		for _, r := range results {
			pings[r.Name] = r
		}
	}

	status := &Status{
		Resources: make([]Resource, 0, len(cluster.Services)),
		UpdatedAt: time.Now().UTC(),
	}

	for _, s := range cluster.Services {
		var ping *k8s.PingResult
		if s.Type != "Job" && s.Status != "Off" {
			ping = pings[s.Name]
		}

		resource := Resource{
			Name:    s.Name,
			Group:   groupOf(s.Name),
			Type:    s.Type,
			State:   state(s, ping),
			Status:  s.Message,
			Current: s.ReadyReplicas,
			Desired: s.DesiredReplicas,
			Labels:  map[string]string{"namespace": m.client.Namespace},
			Ping:    ping,
		}
		status.Resources = append(status.Resources, resource)
		status.count(resource.State, ping)
	}

	return status, nil
}
