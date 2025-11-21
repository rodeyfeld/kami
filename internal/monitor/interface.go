package monitor

import "context"

type Monitor interface {
	GetStatus(ctx context.Context) (*Status, error)
	GetMode() string
}

type Status struct {
	Resources      []Resource
	TotalCount     int
	HealthyCount   int
	UnhealthyCount int
}

type Resource struct {
	Name    string
	Type    string
	State   string
	Status  string
	Current int32
	Desired int32
	Labels  map[string]string
}
