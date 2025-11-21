//go:build !dev

package startup

import (
	"kami/internal/k8s"
	"kami/internal/monitor"
	"log"
)

// InitMonitor only tries K8s in production builds
func InitMonitor() monitor.Monitor {
	if k8s, err := k8s.NewClient(); err == nil {
		log.Println("✓ Kubernetes monitoring active")
		return monitor.NewK8sMonitor(k8s)
	}
	
	log.Println("⚠ Standalone mode - no monitoring")
	return nil
}

