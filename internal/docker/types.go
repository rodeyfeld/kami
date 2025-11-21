package docker

// ContainerStatus represents the overall Docker container status
type ContainerStatus struct {
	Containers   []ContainerInfo
	TotalCount   int
	RunningCount int
	StoppedCount int
}

// ContainerInfo represents information about a single container
type ContainerInfo struct {
	ID      string
	Name    string
	Image   string
	State   string // running, exited, paused, etc.
	Status  string // Status message
	Project string // Compose project name
}
