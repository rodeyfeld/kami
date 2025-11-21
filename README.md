# Kami (神)

Kubernetes health monitoring dashboard for the Universe ecosystem. Monitors deployments, statefulsets, and jobs in the `galaxy` namespace.

## Running it

```bash
cp example.env .env
docker compose up
```

Then hit http://localhost:8080

**Note:** For local development, Kami will attempt to connect to your local Kubernetes cluster using `~/.kube/config`. If no cluster is available, it runs in standalone mode.

## Deploying

Build and push:
```bash
docker build -t edrodefeld/kami .
docker push edrodefeld/kami:latest
```

Deploy to k8s:
```bash
kubectl apply -f ../mirage/deployments/kami-rbac.yml
kubectl apply -f ../mirage/deployments/kami.yml
kubectl apply -f ../mirage/services/kami-service.yml
kubectl rollout restart deployment/kami -n galaxy
```

## What's in it

Go + Echo for the backend, Templ for templates, Tailwind + DaisyUI for styling. HTMX makes it interactive with auto-refreshing status updates every 10 seconds.

Uses the official Kubernetes Go client (`client-go`) to monitor:
- **Deployments** - Check replica health for augur, luna, doppler, dreamflow components, etc.
- **StatefulSets** - Monitor atlas (PostgreSQL), dreamflow-redis
- **Jobs** - Track oracle and migration job statuses

Air watches for changes and automatically rebuilds everything - runs `templ generate` for templates, `bun run build:js` for JavaScript, and `bun run build:css` for Tailwind. Then restarts the Go app.

## Kubernetes Permissions

Kami requires read-only access to the cluster via RBAC:
- ServiceAccount: `kami-monitor`
- Permissions: `get`, `list`, `watch` on deployments, statefulsets, jobs, pods, and services
- Namespace: `galaxy`

See `../mirage/deployments/kami-rbac.yml` for the full RBAC configuration.

