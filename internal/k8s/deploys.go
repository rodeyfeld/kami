package k8s

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	deployPollInterval = 5 * time.Second
	maxRollouts        = 30
	maxSteps           = 40
	// Woodpecker deletes step pods shortly after they finish, so kami keeps its own record of them
	stepRetention = 6 * time.Hour

	revisionAnnotation  = "deployment.kubernetes.io/revision"
	restartedAnnotation = "kubectl.kubernetes.io/restartedAt"
	stepLabel           = "woodpecker-ci.org/step"
	repoLabel           = "woodpecker-ci.org/repo-name"
	taskLabel           = "woodpecker-ci.org/task-uuid"
)

// Rollout states
const (
	RolloutRolling    = "rolling"
	RolloutStuck      = "stuck"
	RolloutDone       = "done"
	RolloutSuperseded = "superseded" // an older revision, kept for history
	RolloutWaiting    = "waiting"    // an OnDelete StatefulSet whose pods keep the old revision until someone deletes them
)

// Rollout is one revision of a workload: each `rollout restart` or spec change makes a new one
type Rollout struct {
	Workload  string     `json:"workload"`
	Kind      string     `json:"kind"`
	Revision  int64      `json:"revision,omitempty"`
	State     string     `json:"state"`
	Reason    string     `json:"reason,omitempty"`  // why a rollout is stuck, e.g. CreateContainerConfigError: secret "x" not found
	Trigger   string     `json:"trigger,omitempty"` // "restart", "spec change" or "created"
	StartedAt time.Time  `json:"started_at"`
	ReadyAt   *time.Time `json:"ready_at,omitempty"` // nil when kami didn't see it become ready
	Ready     int32      `json:"ready"`
	Desired   int32      `json:"desired"`

	// Image digests running in this revision and the one before. Every image is pushed as :latest,
	// so these are the only way to tell whether a restart picked up a new build.
	Digest     string `json:"digest,omitempty"`
	PrevDigest string `json:"prev_digest,omitempty"`
}

// ImageChanged is nil when kami doesn't know both digests
func (r *Rollout) ImageChanged() *bool {
	if r.Digest == "" || r.PrevDigest == "" {
		return nil
	}
	changed := r.Digest != r.PrevDigest
	return &changed
}

func (r *Rollout) InProgress() bool {
	return r.State == RolloutRolling || r.State == RolloutStuck
}

// priority keeps rows that need attention above history. Waiting rows only know their pods' age,
// which can be months old, so sorting them by time would push them past maxRollouts.
func (r *Rollout) priority() int {
	switch {
	case r.InProgress():
		return 0
	case r.State == RolloutWaiting:
		return 1
	default:
		return 2
	}
}

// Step is one Woodpecker pipeline step, which runs as a pod in the namespace
type Step struct {
	Pod        string     `json:"pod"`
	Repo       string     `json:"repo"`
	Name       string     `json:"name"`
	Task       string     `json:"task,omitempty"`
	Phase      string     `json:"phase"` // Pending, Running, Succeeded, Failed
	Reason     string     `json:"reason,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

func (s *Step) Done() bool {
	return s.Phase == string(corev1.PodSucceeded) || s.Phase == string(corev1.PodFailed)
}

type Deploys struct {
	Rollouts  []Rollout `json:"rollouts"` // in-progress first, then newest first
	Steps     []Step    `json:"steps"`    // newest first
	Error     string    `json:"error,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// DeployWatcher polls the namespace for rollouts and pipeline steps. It runs in the background
// because some of what it reports (old digests, when a revision became ready, finished steps)
// is gone from the cluster by the time anyone looks, so it has to be remembered as it happens.
type DeployWatcher struct {
	client *Client

	mu       sync.Mutex
	revs     map[types.UID]*revisionMemory
	steps    map[string]*Step
	snapshot *Deploys
}

type revisionMemory struct {
	digest  string
	readyAt *time.Time
}

func NewDeployWatcher(c *Client) *DeployWatcher {
	return &DeployWatcher{
		client: c,
		revs:   make(map[types.UID]*revisionMemory),
		steps:  make(map[string]*Step),
	}
}

// Start polls until ctx is cancelled
func (w *DeployWatcher) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(deployPollInterval)
		defer ticker.Stop()
		for {
			w.Refresh(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// Snapshot returns the latest poll, refreshing first if there hasn't been one yet
func (w *DeployWatcher) Snapshot(ctx context.Context) *Deploys {
	w.mu.Lock()
	s := w.snapshot
	w.mu.Unlock()
	if s == nil {
		return w.Refresh(ctx)
	}
	return s
}

func (w *DeployWatcher) Refresh(ctx context.Context) *Deploys {
	d := &Deploys{Rollouts: make([]Rollout, 0), Steps: make([]Step, 0), UpdatedAt: time.Now().UTC()}
	ns := w.client.Namespace
	cs := w.client.Clientset

	pods, err := cs.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		d.Error = fmt.Sprintf("failed to list pods: %v", err)
		return w.store(d)
	}

	w.mu.Lock()
	w.recordSteps(pods.Items, d.UpdatedAt)
	d.Steps = w.recentSteps()
	w.mu.Unlock()

	deployments, err := cs.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		d.Error = fmt.Sprintf("failed to list deployments: %v", err)
		return w.store(d)
	}
	replicaSets, err := cs.AppsV1().ReplicaSets(ns).List(ctx, metav1.ListOptions{})
	if apierrors.IsForbidden(err) {
		d.Error = "kami can't list replicasets yet; apply mirage/apps/kami/rbac.yml"
	} else if err != nil {
		d.Error = fmt.Sprintf("failed to list replicasets: %v", err)
	}
	statefulSets, stsErr := cs.AppsV1().StatefulSets(ns).List(ctx, metav1.ListOptions{})
	if stsErr != nil && d.Error == "" {
		d.Error = fmt.Sprintf("failed to list statefulsets: %v", stsErr)
	}

	w.mu.Lock()
	if err == nil {
		d.Rollouts = append(d.Rollouts, w.deploymentRollouts(deployments.Items, replicaSets.Items, pods.Items)...)
	}
	w.mu.Unlock()
	if stsErr == nil {
		d.Rollouts = append(d.Rollouts, statefulSetRollouts(statefulSets.Items, pods.Items)...)
	}

	sort.SliceStable(d.Rollouts, func(i, j int) bool {
		a, b := d.Rollouts[i], d.Rollouts[j]
		if a.priority() != b.priority() {
			return a.priority() < b.priority()
		}
		return a.StartedAt.After(b.StartedAt)
	})
	if len(d.Rollouts) > maxRollouts {
		d.Rollouts = d.Rollouts[:maxRollouts]
	}

	return w.store(d)
}

func (w *DeployWatcher) store(d *Deploys) *Deploys {
	if d.Error != "" {
		log.Printf("deploy watcher: %s", d.Error)
	}
	w.mu.Lock()
	w.snapshot = d
	w.mu.Unlock()
	return d
}

// deploymentRollouts turns each Deployment's ReplicaSets into rollouts. Callers hold w.mu.
func (w *DeployWatcher) deploymentRollouts(deployments []appsv1.Deployment, replicaSets []appsv1.ReplicaSet, pods []corev1.Pod) []Rollout {
	byDeployment := make(map[types.UID][]appsv1.ReplicaSet)
	live := make(map[types.UID]bool, len(replicaSets))
	for _, rs := range replicaSets {
		live[rs.UID] = true
		if ref := metav1.GetControllerOf(&rs); ref != nil && ref.Kind == "Deployment" {
			byDeployment[ref.UID] = append(byDeployment[ref.UID], rs)
		}
	}

	rollouts := make([]Rollout, 0)
	for _, dep := range deployments {
		sets := byDeployment[dep.UID]
		sort.Slice(sets, func(i, j int) bool { return revisionOf(sets[i]) < revisionOf(sets[j]) })

		desired := getInt32(dep.Spec.Replicas, 1)
		var prev *appsv1.ReplicaSet
		for i := range sets {
			rs := &sets[i]
			latest := i == len(sets)-1
			owned := podsOwnedBy(pods, rs.UID)
			mem := w.remember(rs.UID, owned)

			r := Rollout{
				Workload:  dep.Name,
				Kind:      "Deployment",
				Revision:  revisionOf(*rs),
				State:     RolloutSuperseded,
				Trigger:   trigger(rs, prev),
				StartedAt: rs.CreationTimestamp.UTC(),
				ReadyAt:   mem.readyAt,
				Ready:     countReady(owned),
				Desired:   getInt32(rs.Spec.Replicas, 0),
				Digest:    mem.digest,
			}
			if prev != nil {
				if m, ok := w.revs[prev.UID]; ok {
					r.PrevDigest = m.digest
				}
			}

			if latest {
				// Scaled-to-zero workloads have nothing rolling; their history is still worth showing
				r.Desired = desired
				r.State, r.Reason = deploymentState(&dep, r.Ready, owned)
				if desired == 0 {
					r.State = RolloutDone
				}
			}

			rollouts = append(rollouts, r)
			prev = rs
		}
	}

	// Forget revisions Kubernetes has garbage collected
	for uid := range w.revs {
		if !live[uid] {
			delete(w.revs, uid)
		}
	}
	return rollouts
}

// remember records the digest and first-ready time of a revision while its pods still exist. Callers hold w.mu.
func (w *DeployWatcher) remember(uid types.UID, pods []corev1.Pod) *revisionMemory {
	mem, ok := w.revs[uid]
	if !ok {
		mem = &revisionMemory{}
		w.revs[uid] = mem
	}
	for _, pod := range pods {
		if !isReady(pod) {
			continue
		}
		if mem.digest == "" {
			mem.digest = podDigest(pod)
		}
		if mem.readyAt == nil {
			mem.readyAt = readySince(pod)
		}
	}
	return mem
}

func deploymentState(dep *appsv1.Deployment, ready int32, pods []corev1.Pod) (string, string) {
	desired := getInt32(dep.Spec.Replicas, 1)
	if dep.Status.ObservedGeneration >= dep.Generation && dep.Status.UpdatedReplicas >= desired && ready >= desired {
		return RolloutDone, ""
	}
	for _, cond := range dep.Status.Conditions {
		if cond.Type == appsv1.DeploymentProgressing && cond.Reason == "ProgressDeadlineExceeded" {
			if reason := stuckReason(pods); reason != "" {
				return RolloutStuck, reason
			}
			return RolloutStuck, cond.Message
		}
	}
	if reason := stuckReason(pods); reason != "" {
		return RolloutStuck, reason
	}
	return RolloutRolling, ""
}

// statefulSetRollouts only reports StatefulSets that are mid-rollout; their history lives in
// ControllerRevisions, which kami doesn't read
func statefulSetRollouts(statefulSets []appsv1.StatefulSet, pods []corev1.Pod) []Rollout {
	rollouts := make([]Rollout, 0)
	for _, sts := range statefulSets {
		desired := getInt32(sts.Spec.Replicas, 1)
		owned := podsOwnedBy(pods, sts.UID)
		ready := countReady(owned)
		updating := sts.Status.UpdateRevision != "" && sts.Status.CurrentRevision != sts.Status.UpdateRevision
		if desired == 0 || (!updating && ready >= desired) {
			continue
		}

		r := Rollout{
			Workload:  sts.Name,
			Kind:      "StatefulSet",
			State:     RolloutRolling,
			StartedAt: newestPodStart(owned, sts.CreationTimestamp.UTC()),
			Ready:     ready,
			Desired:   desired,
		}
		if reason := stuckReason(owned); reason != "" {
			r.State, r.Reason = RolloutStuck, reason
		} else if sts.Spec.UpdateStrategy.Type == appsv1.OnDeleteStatefulSetStrategyType && ready >= desired {
			// Nothing will happen on its own, so this isn't in progress, e.g. mirage's restart-everything step
			r.State = RolloutWaiting
			r.Reason = "OnDelete update strategy: pods keep the old revision until they're deleted"
		}
		rollouts = append(rollouts, r)
	}
	return rollouts
}

// stuckReason finds the first pod problem that won't fix itself by waiting
func stuckReason(pods []corev1.Pod) string {
	for _, pod := range pods {
		if pod.DeletionTimestamp != nil {
			continue
		}
		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionFalse && cond.Reason == corev1.PodReasonUnschedulable {
				return "Unschedulable: " + cond.Message
			}
		}
		statuses := append(append([]corev1.ContainerStatus{}, pod.Status.InitContainerStatuses...), pod.Status.ContainerStatuses...)
		for _, cs := range statuses {
			if waiting := cs.State.Waiting; waiting != nil {
				switch waiting.Reason {
				case "", "ContainerCreating", "PodInitializing":
				default:
					return joinReason(waiting.Reason, waiting.Message)
				}
			}
			if term := cs.LastTerminationState.Terminated; term != nil && cs.RestartCount > 0 && !cs.Ready {
				return joinReason(fmt.Sprintf("Restarted %d times, last exit %d", cs.RestartCount, term.ExitCode), term.Reason)
			}
		}
	}
	return ""
}

func joinReason(reason, message string) string {
	if message == "" {
		return reason
	}
	return reason + ": " + message
}

// recordSteps updates the remembered Woodpecker steps from the current pods. Callers hold w.mu.
func (w *DeployWatcher) recordSteps(pods []corev1.Pod, now time.Time) {
	for _, pod := range pods {
		name, ok := pod.Labels[stepLabel]
		if !ok {
			continue
		}
		step := &Step{
			Pod:       pod.Name,
			Repo:      pod.Labels[repoLabel],
			Name:      name,
			Task:      pod.Labels[taskLabel],
			Phase:     string(pod.Status.Phase),
			StartedAt: pod.CreationTimestamp.UTC(),
		}
		if pod.Status.StartTime != nil {
			step.StartedAt = pod.Status.StartTime.UTC()
		}
		for _, cs := range pod.Status.ContainerStatuses {
			if term := cs.State.Terminated; term != nil {
				t := term.FinishedAt.UTC()
				step.FinishedAt = &t
				if term.ExitCode != 0 {
					step.Reason = joinReason(fmt.Sprintf("exit %d", term.ExitCode), term.Reason)
				}
			}
		}
		if !step.Done() {
			if reason := stuckReason([]corev1.Pod{pod}); reason != "" {
				step.Reason = reason
			}
		}
		w.steps[pod.Name] = step
	}

	for name, step := range w.steps {
		if now.Sub(step.StartedAt) > stepRetention {
			delete(w.steps, name)
		}
	}
}

// recentSteps returns remembered steps, newest first. Callers hold w.mu.
func (w *DeployWatcher) recentSteps() []Step {
	steps := make([]Step, 0, len(w.steps))
	for _, s := range w.steps {
		steps = append(steps, *s)
	}
	sort.Slice(steps, func(i, j int) bool { return steps[i].StartedAt.After(steps[j].StartedAt) })
	if len(steps) > maxSteps {
		steps = steps[:maxSteps]
	}
	return steps
}

func revisionOf(rs appsv1.ReplicaSet) int64 {
	n, _ := strconv.ParseInt(rs.Annotations[revisionAnnotation], 10, 64)
	return n
}

func trigger(rs, prev *appsv1.ReplicaSet) string {
	if prev == nil {
		return "created"
	}
	restarted := rs.Spec.Template.Annotations[restartedAnnotation]
	if restarted != "" && restarted != prev.Spec.Template.Annotations[restartedAnnotation] {
		return "restart"
	}
	return "spec change"
}

func podsOwnedBy(pods []corev1.Pod, owner types.UID) []corev1.Pod {
	owned := make([]corev1.Pod, 0)
	for _, pod := range pods {
		if ref := metav1.GetControllerOf(&pod); ref != nil && ref.UID == owner {
			owned = append(owned, pod)
		}
	}
	return owned
}

func countReady(pods []corev1.Pod) int32 {
	var n int32
	for _, pod := range pods {
		if isReady(pod) {
			n++
		}
	}
	return n
}

func isReady(pod corev1.Pod) bool {
	if pod.DeletionTimestamp != nil {
		return false
	}
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady {
			return cond.Status == corev1.ConditionTrue
		}
	}
	return false
}

func readySince(pod corev1.Pod) *time.Time {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			t := cond.LastTransitionTime.UTC()
			return &t
		}
	}
	return nil
}

// podDigest returns the first container's image digest, e.g. sha256:e8375f69…
func podDigest(pod corev1.Pod) string {
	for _, cs := range pod.Status.ContainerStatuses {
		if _, digest, ok := strings.Cut(cs.ImageID, "@"); ok {
			return digest
		}
	}
	return ""
}

func newestPodStart(pods []corev1.Pod, fallback time.Time) time.Time {
	newest := fallback
	for _, pod := range pods {
		if t := pod.CreationTimestamp.UTC(); t.After(newest) {
			newest = t
		}
	}
	return newest
}
