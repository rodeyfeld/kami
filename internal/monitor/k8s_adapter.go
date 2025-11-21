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

// isPublicService filters out sensitive internal infrastructure
func isPublicService(name string) bool {
	// List of public-facing services that are safe to display
	publicServices := []string{
		"luna",      // Frontend
		"augur",     // API
		"doppler",   // Blog
		"kami",      // This monitoring service
		"dreamflow", // Airflow (has its own auth)
	}

	// Check if it's in the public list
	for _, public := range publicServices {
		if strings.HasPrefix(name, public) {
			return true
		}
	}

	// Hide internal infrastructure
	hideKeywords := []string{
		"redis", "postgres", "atlas", "garage",
		"worker", "scheduler", "flower", "beat",
		"db", "database", "cache", "queue",
	}

	nameLower := strings.ToLower(name)
	for _, keyword := range hideKeywords {
		if strings.Contains(nameLower, keyword) {
			return false
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

	// Filter and count only public services
	for _, s := range k8s.Services {
		if !isPublicService(s.Name) {
			continue
		}

		resource := Resource{
			Name:    s.Name,
			Type:    s.Type,
			State:   s.Status,
			Status:  s.Message,
			Current: s.ReadyReplicas,
			Desired: s.DesiredReplicas,
			Labels:  map[string]string{"namespace": m.client.Namespace},
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
