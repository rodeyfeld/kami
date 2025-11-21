package k8s

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ServiceStatus represents the health status of a Kubernetes service
type ServiceStatus struct {
	Name             string
	Type             string // "Deployment", "StatefulSet", "Job"
	Status           string // "Healthy", "Degraded", "Unhealthy", "Unknown"
	ReadyReplicas    int32
	DesiredReplicas  int32
	AvailableReplicas int32
	Message          string
}

// ClusterStatus represents the overall cluster health
type ClusterStatus struct {
	Services       []ServiceStatus
	TotalServices  int
	HealthyCount   int
	DegradedCount  int
	UnhealthyCount int
}

// GetClusterStatus retrieves the status of all services in the namespace
func (c *Client) GetClusterStatus(ctx context.Context) (*ClusterStatus, error) {
	status := &ClusterStatus{
		Services: make([]ServiceStatus, 0),
	}

	// Get Deployments
	deployments, err := c.Clientset.AppsV1().Deployments(c.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list deployments: %w", err)
	}

	for _, dep := range deployments.Items {
		svcStatus := ServiceStatus{
			Name:              dep.Name,
			Type:              "Deployment",
			DesiredReplicas:   *dep.Spec.Replicas,
			ReadyReplicas:     dep.Status.ReadyReplicas,
			AvailableReplicas: dep.Status.AvailableReplicas,
		}

		// Determine health status
		if dep.Status.ReadyReplicas == *dep.Spec.Replicas && dep.Status.AvailableReplicas == *dep.Spec.Replicas {
			svcStatus.Status = "Healthy"
			svcStatus.Message = "All replicas ready"
			status.HealthyCount++
		} else if dep.Status.ReadyReplicas > 0 {
			svcStatus.Status = "Degraded"
			svcStatus.Message = fmt.Sprintf("%d/%d replicas ready", dep.Status.ReadyReplicas, *dep.Spec.Replicas)
			status.DegradedCount++
		} else {
			svcStatus.Status = "Unhealthy"
			svcStatus.Message = "No replicas ready"
			status.UnhealthyCount++
		}

		status.Services = append(status.Services, svcStatus)
	}

	// Get StatefulSets
	statefulSets, err := c.Clientset.AppsV1().StatefulSets(c.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list statefulsets: %w", err)
	}

	for _, sts := range statefulSets.Items {
		svcStatus := ServiceStatus{
			Name:              sts.Name,
			Type:              "StatefulSet",
			DesiredReplicas:   *sts.Spec.Replicas,
			ReadyReplicas:     sts.Status.ReadyReplicas,
			AvailableReplicas: sts.Status.ReadyReplicas, // StatefulSets don't have AvailableReplicas
		}

		if sts.Status.ReadyReplicas == *sts.Spec.Replicas {
			svcStatus.Status = "Healthy"
			svcStatus.Message = "All replicas ready"
			status.HealthyCount++
		} else if sts.Status.ReadyReplicas > 0 {
			svcStatus.Status = "Degraded"
			svcStatus.Message = fmt.Sprintf("%d/%d replicas ready", sts.Status.ReadyReplicas, *sts.Spec.Replicas)
			status.DegradedCount++
		} else {
			svcStatus.Status = "Unhealthy"
			svcStatus.Message = "No replicas ready"
			status.UnhealthyCount++
		}

		status.Services = append(status.Services, svcStatus)
	}

	// Get Jobs (recent completions/failures)
	jobs, err := c.Clientset.BatchV1().Jobs(c.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list jobs: %w", err)
	}

	for _, job := range jobs.Items {
		svcStatus := ServiceStatus{
			Name:            job.Name,
			Type:            "Job",
			DesiredReplicas: *job.Spec.Completions,
		}

		if job.Status.Succeeded > 0 {
			svcStatus.Status = "Healthy"
			svcStatus.Message = fmt.Sprintf("Completed %d/%d", job.Status.Succeeded, *job.Spec.Completions)
			svcStatus.ReadyReplicas = job.Status.Succeeded
			status.HealthyCount++
		} else if job.Status.Active > 0 {
			svcStatus.Status = "Running"
			svcStatus.Message = fmt.Sprintf("Active: %d", job.Status.Active)
			svcStatus.ReadyReplicas = job.Status.Active
			status.DegradedCount++
		} else if job.Status.Failed > 0 {
			svcStatus.Status = "Unhealthy"
			svcStatus.Message = fmt.Sprintf("Failed: %d", job.Status.Failed)
			svcStatus.ReadyReplicas = 0
			status.UnhealthyCount++
		} else {
			svcStatus.Status = "Unknown"
			svcStatus.Message = "Pending"
			svcStatus.ReadyReplicas = 0
		}

		status.Services = append(status.Services, svcStatus)
	}

	status.TotalServices = len(status.Services)

	return status, nil
}

