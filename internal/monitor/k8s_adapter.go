package monitor

import (
	"context"
	"kami/internal/k8s"
)

type K8sMonitor struct {
	client *k8s.Client
}

func NewK8sMonitor(c *k8s.Client) *K8sMonitor {
	return &K8sMonitor{client: c}
}

func (m *K8sMonitor) GetMode() string { return "kubernetes" }

func (m *K8sMonitor) GetStatus(ctx context.Context) (*Status, error) {
	k8s, err := m.client.GetClusterStatus(ctx)
	if err != nil {
		return nil, err
	}

	status := &Status{
		Resources:      make([]Resource, 0, len(k8s.Services)),
		TotalCount:     k8s.TotalServices,
		HealthyCount:   k8s.HealthyCount,
		UnhealthyCount: k8s.UnhealthyCount + k8s.DegradedCount,
	}

	for _, s := range k8s.Services {
		status.Resources = append(status.Resources, Resource{
			Name:    s.Name,
			Type:    s.Type,
			State:   s.Status,
			Status:  s.Message,
			Current: s.ReadyReplicas,
			Desired: s.DesiredReplicas,
			Labels:  map[string]string{"namespace": m.client.Namespace},
		})
	}

	return status, nil
}

