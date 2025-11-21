# Kami Setup Guide

## 🎯 What is Kami?

Kami (神 - "god/deity" in Japanese) is your all-seeing Kubernetes health monitoring dashboard for the Universe ecosystem. It monitors deployments, statefulsets, and jobs in the `galaxy` namespace with real-time updates every 10 seconds.

## 📦 Complete Stack

- **Backend**: Go 1.25 + Echo web framework
- **Templates**: Templ (type-safe Go templates)
- **Styling**: Tailwind CSS + DaisyUI (forest theme)
- **Interactivity**: HTMX for auto-refreshing status
- **Build**: Bun for JS/CSS, Air for hot reload
- **Monitoring**: Kubernetes client-go official library

## 🚀 Quick Start

### Local Development

```bash
cd ~/universe/kami
cp example.env .env
docker compose up
```

Then visit: **http://localhost:8080**

### Production Deployment

```bash
# 1. Build and push Docker image
docker build -t edrodefeld/kami .
docker push edrodefeld/kami:latest

# 2. Apply RBAC configuration (first time only)
kubectl apply -f ../mirage/deployments/kami-rbac.yml

# 3. Deploy to Kubernetes
kubectl apply -f ../mirage/deployments/kami.yml
kubectl apply -f ../mirage/services/kami-service.yml

# 4. Update ingress (already done)
kubectl apply -f ../mirage/ingress.yml

# 5. Wait for rollout
kubectl rollout status deployment/kami -n galaxy

# 6. Check logs
kubectl logs -f deployment/kami -n galaxy
```

Visit: **https://kami.pinwheel.fan**

## 📁 Project Structure

```
kami/
├── cmd/
│   └── main.go                           # Entry point
├── internal/
│   ├── server/
│   │   ├── server.go                     # Server setup
│   │   ├── handlers/
│   │   │   └── health.go                 # Health dashboard handler
│   │   └── routes/
│   │       └── routes.go                 # Route definitions
│   ├── k8s/
│   │   ├── client.go                     # K8s client initialization
│   │   └── monitor.go                    # Cluster monitoring logic
│   └── components/
│       ├── layout/
│       │   └── base.templ                # Base HTML layout
│       └── health/
│           └── dashboard.templ           # Health dashboard UI
├── static/
│   ├── js/
│   │   └── index.js                      # HTMX bundle
│   └── css/
│       └── input.css                     # Tailwind input
├── app.go                                # Application startup
├── go.mod                                # Go dependencies
├── Dockerfile                            # Multi-stage build
├── compose.yml                           # Docker Compose config
├── .air.toml                             # Hot reload config
├── start-dev.sh                          # Dev startup script
├── package.json                          # Bun dependencies
├── tailwind.config.js                    # Tailwind config
├── example.env                           # Environment template
└── README.md                             # Documentation
```

## 🔧 What Gets Monitored

### Deployments
- augur
- luna  
- doppler
- kami (self-monitoring!)
- dreamflow-webserver
- dreamflow-scheduler
- dreamflow-worker
- garage

### StatefulSets
- atlas (PostgreSQL)
- dreamflow-redis

### Jobs
- oracle
- augur-db-migrate
- dreamflow-db-init

## 🔐 Security (RBAC)

Kami uses a dedicated ServiceAccount with read-only permissions:

- **ServiceAccount**: `kami-monitor`
- **Namespace**: `galaxy` only
- **Permissions**: `get`, `list`, `watch` on:
  - deployments
  - statefulsets
  - jobs
  - pods
  - services
  - events

**No write/delete permissions** - completely safe monitoring!

## 🎨 Features

- ✅ Real-time health status with color coding
- ✅ Replica count tracking with progress bars
- ✅ Auto-refresh every 10 seconds (HTMX)
- ✅ Responsive design (mobile-friendly)
- ✅ Animated cards and transitions
- ✅ Standalone mode fallback (no K8s cluster)
- ✅ Self-monitoring (Kami monitors itself!)

## 🔄 Development Workflow

When you run `docker compose up` or `air`, the build pipeline automatically:

1. Runs `templ generate` → Generates `*_templ.go` files
2. Runs `bun run build:js` → Bundles HTMX into `static/dist/index.js`
3. Runs `bun run build:css` → Compiles Tailwind to `static/css/output.css`
4. Builds and starts the Go application
5. Watches for changes and repeats on file changes

**You never need to manually build assets!**

## 🌐 Network Architecture

### Local Development
```
Browser → localhost:8080 → Kami
                           ↓
                    ~/.kube/config → Your K8s cluster
```

### Production (Kubernetes)
```
Internet → Traefik (HTTPS) → kami.pinwheel.fan
                              ↓
                       kami-service.galaxy.svc.cluster.local:8080
                              ↓
                       Kami Pod (in-cluster K8s client)
                              ↓
                       Kubernetes API (monitors galaxy namespace)
```

## 📊 Status Indicators

- **🟢 Healthy**: All replicas ready and available
- **🟡 Degraded**: Some replicas ready, but not all
- **🔴 Unhealthy**: No replicas ready
- **🔵 Running**: Job is currently active
- **⚪ Unknown**: Status cannot be determined

## 🐛 Troubleshooting

### "Standalone Mode" warning
**Cause**: No Kubernetes cluster available  
**Fix**: 
- Local: Ensure `~/.kube/config` exists and points to a valid cluster
- K8s: Check RBAC permissions with `kubectl get sa kami-monitor -n galaxy`

### "Failed to list deployments"
**Cause**: Insufficient RBAC permissions  
**Fix**: 
```bash
kubectl apply -f ../mirage/deployments/kami-rbac.yml
kubectl rollout restart deployment/kami -n galaxy
```

### Empty dashboard
**Cause**: No resources in `galaxy` namespace  
**Fix**: This is normal if your cluster is empty. Deploy other services first!

## 📝 Environment Variables

- `KAMI_NAMESPACE` - Kubernetes namespace to monitor (default: `galaxy`)

## 🎯 Next Steps

1. Test locally with `docker compose up`
2. Deploy to Kubernetes following the production steps above
3. Visit https://kami.pinwheel.fan to see your cluster health!

## 💡 Pro Tips

- Kami monitors itself! You'll see the `kami` deployment in the dashboard
- The dashboard auto-refreshes every 10 seconds - no manual refresh needed
- Click on service cards to see detailed information (future enhancement)
- Works great on mobile - check your cluster health on the go!

---

**Built with ❤️ for the Universe ecosystem**

