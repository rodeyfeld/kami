package monitor

import (
	"context"
	"kami/internal/k8s"
	"strings"
)

type K8sMonitor struct {
	client *k8s.Client
}

func NewK8sMonitor(c *k8s.Client) *K8sMonitor {
	return &K8sMonitor{client: c}
}

func (m *K8sMonitor) GetMode() string { return "kubernetes" }

// getCategory classifies a service into "Public" or "Infrastructure"
func getCategory(name string) string {
	// Main user-facing applications
	publicServices := []string{
		"luna",      // Frontend
		"augur",     // API
		"doppler",   // Blog
		"kami",      // Monitoring
		"dreamflow", // Airflow
		"enchiridion", // Infisical (Secrets)
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
		"postgres",
		"redis",
		"rabbitmq",
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
	k8s, err := m.client.GetClusterStatus(ctx)
	if err != nil {
		return nil, err
	}

	status := &Status{
		Resources:      make([]Resource, 0),
		TotalCount:     0,
		HealthyCount:   0,
		UnhealthyCount: 0,
	}

	for _, s := range k8s.Services {
		category := getCategory(s.Name)
		
		// Determine label based on category
		if category == "Infrastructure" {
			// Optional: add specific labels if needed
		}

		resource := Resource{
			Name:    s.Name,
			Type:    s.Type,
			State:   s.Status,
			Status:  s.Message,
			Current: s.ReadyReplicas,
			Desired: s.DesiredReplicas,
			Labels:  map[string]string{
				"namespace": m.client.Namespace,
				"category": category,
			},
		}
		status.Resources = append(status.Resources, resource)

		// Count health status
		if s.Status == "Healthy" {
			status.HealthyCount++
		} else {
			status.UnhealthyCount++
		}
		status.TotalCount++
	}

	return status, nil
}
