//go:build dev

package startup

import (
	"kami/internal/docker"
	"kami/internal/k8s"
	"kami/internal/monitor"
	"log"
)

// InitMonitor tries Docker first in dev builds, then K8s as fallback
func InitMonitor() monitor.Monitor {
	// Try Docker first (dev priority)
	if d, err := docker.NewClient(); err == nil {
		log.Println("✓ Docker monitoring active (dev mode)")
		return monitor.NewDockerMonitor(d)
	}

	// Fall back to K8s
	if k8s, err := k8s.NewClient(); err == nil {
		log.Println("✓ Kubernetes monitoring active")
		return monitor.NewK8sMonitor(k8s)
	}

	log.Println("⚠ Standalone mode - no monitoring")
	return nil
}
