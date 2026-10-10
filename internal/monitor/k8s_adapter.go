package monitor

import (
	"context"
	"kami/internal/k8s"
	"strings"
	"time"
)

type K8sMonitor struct {
	client *k8s.Client
	pinger *k8s.Pinger
}

func NewK8sMonitor(c *k8s.Client) *K8sMonitor {
	return &K8sMonitor{client: c, pinger: k8s.NewPinger(c)}
}

func (m *K8sMonitor) GetMode() string { return "kubernetes" }

func (m *K8sMonitor) Ping(ctx context.Context, name string) (*k8s.PingResult, error) {
	return m.pinger.Ping(ctx, name)
}

func (m *K8sMonitor) PingAll(ctx context.Context) ([]*k8s.PingResult, error) {
	return m.pinger.PingAll(ctx)
}

// getCategory classifies a service into "Public" or "Infrastructure"
func getCategory(name string) string {
	// Main user-facing applications
	publicServices := []string{
		"luna",      // Frontend
		"augur",     // API
		"doppler",   // Blog
		"kami",      // Monitoring
		"noctis",    // Prefect workflows
		"oracle",    // Scheduled jobs
		"ouroboros", // Woodpecker CI
	}

	for _, public := range publicServices {
		if strings.HasPrefix(name, public) && !isInfrastructureComponent(name) {
			return "Service"
		}
	}

	// Infrastructure components (DBs, Queues, Storage)
	if isInfrastructureComponent(name) {
		return "Infrastructure"
	}

	return "Other"
}

func isInfrastructureComponent(name string) bool {
	nameLower := strings.ToLower(name)

	// Explicit infrastructure services
	infrastructureServices := []string{
		"atlas",
		"garage",
		"cuneiform",
		"postgres",
		"redis",
		"rabbitmq",
		"prefect-server",
	}

	for _, infra := range infrastructureServices {
		if strings.Contains(nameLower, infra) {
			return true
		}
	}

	// Helper components often associated with main apps
	helperKeywords := []string{
		"worker", "scheduler", "flower", "beat", "db", "cache", "queue",
	}

	for _, keyword := range helperKeywords {
		if strings.Contains(nameLower, keyword) {
			return true
		}
	}

	return false
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
		resource := Resource{
			Name:    s.Name,
			Type:    s.Type,
			State:   s.Status,
			Status:  s.Message,
			Current: s.ReadyReplicas,
			Desired: s.DesiredReplicas,
			Labels: map[string]string{
				"namespace": m.client.Namespace,
				"category":  getCategory(s.Name),
			},
		}
		if s.Type != "Job" {
			resource.Ping = pings[s.Name]
		}
		status.Resources = append(status.Resources, resource)

		switch s.Status {
		case "Healthy", "Running":
			status.HealthyCount++
		case "Degraded", "Unknown":
			status.DegradedCount++
		default:
			status.UnhealthyCount++
		}
		status.TotalCount++
	}

	return status, nil
}
