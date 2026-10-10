# Kami (神)

Kubernetes health monitoring dashboard for the Universe ecosystem. Monitors deployments, statefulsets, and jobs in the `galaxy` namespace.

## Running it

```bash
cp example.env .env
docker compose up
```

Then hit http://localhost:8080

**Note:** Dev builds (`-tags dev`, used by Air) monitor local Docker containers first, then fall back to the Kubernetes cluster in `~/.kube/config`. With neither, kami runs in standalone mode. Pings need pod IPs, so they only work when kami runs inside the cluster.

## Deploying

Build and push:
```bash
docker build -t edrodefeld/kami .
docker push edrodefeld/kami:latest
```

Woodpecker builds and pushes on every push to main. Manifests live in `../mirage/apps/kami/`:
```bash
kubectl apply -f ../mirage/apps/kami/
kubectl rollout restart deployment/kami -n galaxy
```

## Ping API

Other services can ask kami whether a workload in `galaxy` is actually answering, without needing in-cluster access themselves. Kami calls the workload's own readiness probe (or liveness probe if there is no readiness probe), as declared in its manifest, against one of its ready pods. Results are cached for 10s. CORS is open for GET.

```bash
curl https://kami.pinwheel.fan/api/ping/augur   # one Deployment or StatefulSet
curl https://kami.pinwheel.fan/api/ping         # all of them
```

```json
{"name":"augur","ok":true,"probe":"http","path":"/api/readyz","port":8000,"status_code":200,"latency_ms":4,"checked_at":"2026-10-10T18:00:00Z","restarts":0,"started_at":"2026-10-08T16:00:00Z"}
```

- `/api/ping/:name` returns 200 when `ok` and 503 when not, so the status code alone works for uptime checks. Unknown names return 404.
- `probe` is `http` or `tcp` when kami called the workload itself. `exec` (e.g. `pg_isready`, which only the kubelet can run) and `none` (no probe declared) mean `ok` only reflects the pod's Ready flag. Add an HTTP or TCP readinessProbe in mirage to make a workload verifiable.
- `restarts` and `started_at` come from the checked pod, or from a not-ready pod when none are ready, so crash loops show up.
- Targets come from the cluster, never from the request, so the endpoint can't be pointed at arbitrary hosts.

## Deploys

`/deploys` shows what's rolling out right now, recent rollouts, and the Woodpecker steps kami has seen. The same data is available as JSON:

```bash
curl https://kami.pinwheel.fan/api/deploys
```

- **In progress:** Deployments and StatefulSets whose new pods aren't all ready yet. A rollout counts as stuck when a pod hits a problem that waiting won't fix, like `CreateContainerConfigError`, `ImagePullBackOff`, a crash loop, or an unschedulable pod. The reason is shown with it.
- **Rollouts:** each Deployment revision (one ReplicaSet each), newest first, with what triggered it (`restart` or `spec change`) and how long it took to become ready.
- **Image:** every image is pushed as `:latest`, so kami compares the image digest each revision's pods pulled. "same build" means a restart picked up the old image, which usually means the build step didn't push.
- **Pipeline steps:** Woodpecker runs each step as a `wp-*` pod in `galaxy`. kami polls every 5 seconds and remembers steps for 6 hours after Woodpecker deletes their pods.

Digests, ready times and steps are remembered in memory, so they reset when kami restarts. Revisions from before then show without a digest. StatefulSets only appear while rolling, since their history lives in ControllerRevisions.

## What's in it

Go + Echo for the backend, Templ for templates, Tailwind + DaisyUI for styling. HTMX polls `/partials/status` every 10 seconds to refresh the summary and table.

The table groups workloads by project (the name up to the first dash, so `noctis-postgres` sits under `noctis`). Each row shows:
- **Health:** the rollout status combined with the ping. Ready pods whose health check fails show as "Not responding".
- **Check:** which probe kami called and what it returned, or why it couldn't call one.
- **Uptime:** pod age and restart count.

Uses the official Kubernetes Go client (`client-go`) to monitor:
- **Deployments** - Check replica health for augur, luna, doppler, noctis, ouroboros, etc.
- **StatefulSets** - Monitor atlas (PostgreSQL), garage, cuneiform
- **Jobs** - Track oracle and other scheduled job statuses (CronJobs show only their latest run)

Air watches for changes and automatically rebuilds everything - runs `templ generate` for templates, `bun run build:js` for JavaScript, and `bun run build:css` for Tailwind. Then restarts the Go app.

## Kubernetes Permissions

Kami requires read-only access to the cluster via RBAC:
- ServiceAccount: `kami-monitor`
- Permissions: `get`, `list`, `watch` on deployments, replicasets, statefulsets, jobs, pods, events, and services
- Namespace: `galaxy`

See `../mirage/apps/kami/rbac.yml` for the full RBAC configuration. The ping API only needs the existing `pods` read access, plus network access from the kami pod to other pods' IPs.

