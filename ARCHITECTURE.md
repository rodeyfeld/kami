# Kami Architecture - Clean Separation of Concerns

## 🎯 Design Philosophy

Kami uses the **Adapter Pattern** to keep Docker and Kubernetes monitoring completely isolated, joining them only at the interface level. This ensures:

- ✅ **Complete isolation** - Docker and K8s code never mix
- ✅ **Build-time selection** - Docker support only in dev builds  
- ✅ **Easy extensibility** - Add new monitoring backends without touching existing code
- ✅ **Clean interfaces** - Single unified API for all backends

## 📊 Architecture Diagram

```
┌──────────────────────────────────────────────────────┐
│                    Application Layer                  │
│                  (app.go, server.go)                 │
└────────────────────┬─────────────────────────────────┘
                     │
                     ▼
┌──────────────────────────────────────────────────────┐
│              Monitor Interface (Common)               │
│                                                       │
│  type Monitor interface {                            │
│    GetStatus(ctx) (*Status, error)                   │
│    GetMode() string                                   │
│  }                                                    │
└───────┬──────────────────────────────────┬───────────┘
        │                                  │
        ▼                                  ▼
┌──────────────────┐            ┌──────────────────┐
│  K8s Adapter     │            │  Docker Adapter  │
│  (Production)    │            │  (Dev Only)      │
│                  │            │                  │
│  ✓ Always built  │            │  ✓ dev build    │
│  ✓ No Docker     │            │  ✗ prod build   │
│    dependency    │            │                  │
└────────┬─────────┘            └────────┬─────────┘
         │                               │
         ▼                               ▼
┌──────────────────┐            ┌──────────────────┐
│   k8s package    │            │  docker package  │
│                  │            │                  │
│  client.go       │            │  client_dev.go   │
│  monitor.go      │            │  client_prod.go  │
└──────────────────┘            └──────────────────┘
```

## 📁 Package Structure

```
internal/
├── monitor/                    # Common interface layer
│   ├── interface.go           # Monitor interface + Status types
│   ├── k8s_adapter.go         # K8s → Monitor adapter
│   ├── docker_adapter_dev.go  # Docker → Monitor adapter (dev only)
│   └── docker_adapter_prod.go # Docker stub (production)
│
├── k8s/                       # Kubernetes implementation (isolated)
│   ├── client.go             # K8s client initialization
│   └── monitor.go            # K8s-specific monitoring logic
│
├── docker/                    # Docker implementation (isolated)
│   ├── types.go              # Docker types (always built)
│   ├── client_dev.go         # Docker client (dev only)
│   └── client_prod.go        # Docker stub (production)
│
├── server/
│   ├── server.go             # Holds Monitor interface (not concrete types)
│   └── handlers/
│       └── health.go         # Works with Monitor interface only
│
└── components/
    └── health/
        └── dashboard.templ   # Renders unified Status type
```

## 🔧 How Isolation Works

### 1. Common Interface (`internal/monitor/interface.go`)

```go
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
```

**This is the ONLY contract between backends and the application.**

### 2. Adapter Pattern

Each backend implements the Monitor interface through an adapter:

**K8s Adapter** (`internal/monitor/k8s_adapter.go`)
```go
type K8sMonitor struct {
    client *k8s.Client  // K8s-specific
}

func (m *K8sMonitor) GetStatus(ctx) (*Status, error) {
    k8sStatus := m.client.GetClusterStatus(ctx)
    // Convert k8s.ClusterStatus → monitor.Status
    return convertedStatus, nil
}
```

**Docker Adapter** (`internal/monitor/docker_adapter_dev.go`)
```go
//go:build dev  // ← Only compiled in dev builds!

type DockerMonitor struct {
    client *docker.Client  // Docker-specific
}

func (m *DockerMonitor) GetStatus(ctx) (*Status, error) {
    dockerStatus := m.client.GetContainerStatus(ctx)
    // Convert docker.ContainerStatus → monitor.Status
    return convertedStatus, nil
}
```

### 3. Build Tags

**Development Build** (with Docker support):
```bash
go build -tags dev -o kami ./cmd/main.go
```
- Compiles `docker/client_dev.go`
- Compiles `monitor/docker_adapter_dev.go`
- Docker SDK linked into binary

**Production Build** (without Docker support):
```bash
go build -o kami ./cmd/main.go
```
- Compiles `docker/client_prod.go` (stub that returns errors)
- Compiles `monitor/docker_adapter_prod.go` (stub)
- No Docker SDK in binary
- **Smaller binary size**
- **No Docker dependencies**

### 4. Application Layer

`app.go` tries backends in order and wraps them in adapters:

```go
// Try Kubernetes first
if k8sClient, err := k8s.NewClient(); err == nil {
    kamiServer.Monitor = monitor.NewK8sMonitor(k8sClient)  // Wrapped!
}

// Try Docker as fallback (dev only)
if docker.IsAvailable() {  // Returns false in prod builds
    if dockerClient, err := docker.NewClient(); err == nil {
        kamiServer.Monitor = monitor.NewDockerMonitor(dockerClient)  // Wrapped!
    }
}
```

**The server only knows about `monitor.Monitor`** - it has NO IDEA whether it's talking to K8s or Docker!

### 5. Handler Layer

```go
func (h *HealthHandler) GetStatus(c echo.Context) error {
    // Works with ANY Monitor implementation
    status, err := h.server.Monitor.GetStatus(ctx)
    
    // Render unified status
    return health.StatusCards(status, h.server.Monitor.GetMode())
}
```

**No if/else, no type switching, no backend-specific logic!**

## 🎨 Benefits of This Architecture

### 1. **Complete Isolation**
- K8s code never imports Docker packages
- Docker code never imports K8s packages
- They share NOTHING except the Monitor interface

### 2. **Build-Time Optimization**
- Production builds are smaller (no Docker SDK)
- Development builds get Docker support automatically
- No runtime overhead checking features

### 3. **Easy to Extend**
```go
// Want to add Nomad monitoring? Just implement Monitor!
type NomadMonitor struct {
    client *nomad.Client
}

func (m *NomadMonitor) GetStatus(ctx) (*Status, error) {
    // Convert Nomad-specific status to monitor.Status
}
```

### 4. **Testability**
```go
// Mock monitor for testing
type MockMonitor struct {
    status *monitor.Status
}

func (m *MockMonitor) GetStatus(ctx) (*Status, error) {
    return m.status, nil
}
```

### 5. **Single Responsibility**
- `k8s/` knows ONLY about Kubernetes
- `docker/` knows ONLY about Docker
- `monitor/` knows ONLY about the common interface
- `handlers/` knows ONLY about the Monitor interface

## 🚀 Adding a New Backend

Want to add support for Podman, Nomad, or Docker Swarm?

1. **Create the implementation**:
   ```
   internal/podman/
     ├── client.go       # Podman-specific client
     └── monitor.go      # Podman-specific logic
   ```

2. **Create the adapter**:
   ```go
   // internal/monitor/podman_adapter.go
   type PodmanMonitor struct {
       client *podman.Client
   }
   
   func (m *PodmanMonitor) GetStatus(ctx) (*Status, error) {
       // Convert Podman → monitor.Status
   }
   ```

3. **Add to app.go**:
   ```go
   if podmanClient, err := podman.NewClient(); err == nil {
       kamiServer.Monitor = monitor.NewPodmanMonitor(podmanClient)
   }
   ```

**That's it!** No changes to handlers, templates, or existing backends.

## 📝 Key Takeaways

1. **Isolation**: Backends never see each other
2. **Interface**: Single Monitor interface for all backends
3. **Adapter**: Each backend wraps into Monitor
4. **Build Tags**: Docker support only in dev builds
5. **Extensibility**: Add new backends without touching old code

---

**Clean Architecture = Happy Developers** ✨

