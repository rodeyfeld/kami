package k8s

import (
	"context"
	"strconv"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

var t0 = time.Date(2026, 10, 10, 18, 0, 0, 0, time.UTC)

func rolloutDeployment(name string, replicas int32) *appsv1.Deployment {
	d := deployment(name)
	d.UID = types.UID(name + "-uid")
	d.Generation = 2
	d.Spec.Replicas = &replicas
	d.Status = appsv1.DeploymentStatus{ObservedGeneration: 2, UpdatedReplicas: replicas}
	return d
}

func replicaSet(dep *appsv1.Deployment, revision, restartedAt string, created time.Time) *appsv1.ReplicaSet {
	controller := true
	return &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:              dep.Name + "-" + revision,
			Namespace:         ns,
			UID:               types.UID(dep.Name + "-rs-" + revision),
			CreationTimestamp: metav1.NewTime(created),
			Annotations:       map[string]string{revisionAnnotation: revision},
			OwnerReferences:   []metav1.OwnerReference{{Kind: "Deployment", Name: dep.Name, UID: dep.UID, Controller: &controller}},
		},
		Spec: appsv1.ReplicaSetSpec{Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{restartedAnnotation: restartedAt},
		}}},
	}
}

func ownedPod(rs *appsv1.ReplicaSet, ready bool, digest string, readyAt time.Time) *corev1.Pod {
	controller := true
	status := corev1.ConditionFalse
	if ready {
		status = corev1.ConditionTrue
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            rs.Name + "-pod",
			Namespace:       ns,
			OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID, Controller: &controller}},
		},
		Status: corev1.PodStatus{
			Conditions:        []corev1.PodCondition{{Type: corev1.PodReady, Status: status, LastTransitionTime: metav1.NewTime(readyAt)}},
			ContainerStatuses: []corev1.ContainerStatus{{Name: "app", Ready: ready, ImageID: "docker.io/edrodefeld/app@" + digest}},
		},
	}
}

func watcher(objects ...runtime.Object) (*DeployWatcher, *fake.Clientset) {
	cs := fake.NewSimpleClientset(objects...)
	w := NewDeployWatcher(&Client{Clientset: cs, Namespace: ns})
	// Fixtures are dated around t0, so the step retention window has to be measured from there too
	w.now = func() time.Time { return t0 }
	return w, cs
}

func findRollout(t *testing.T, d *Deploys, workload string, revision int64) Rollout {
	t.Helper()
	for _, r := range d.Rollouts {
		if r.Workload == workload && r.Revision == revision {
			return r
		}
	}
	t.Fatalf("no rollout for %s rev %d in %+v", workload, revision, d.Rollouts)
	return Rollout{}
}

func TestRolloutRemembersDigestsAcrossRevisions(t *testing.T) {
	ctx := context.Background()
	dep := rolloutDeployment("kami", 1)
	old := replicaSet(dep, "51", "2026-10-10T17:00:00Z", t0.Add(-time.Hour))
	w, cs := watcher(dep, old, ownedPod(old, true, "sha256:aaa", t0.Add(-time.Hour+20*time.Second)))

	d := w.Refresh(ctx)
	r := findRollout(t, d, "kami", 51)
	if r.State != RolloutDone || r.Digest != "sha256:aaa" || r.Trigger != "created" {
		t.Fatalf("first revision: %+v", r)
	}
	if r.ReadyAt == nil || r.ReadyAt.Sub(r.StartedAt) != 20*time.Second {
		t.Fatalf("ready time: %+v", r.ReadyAt)
	}

	// rollout restart: a new ReplicaSet whose pod pulls a new build, and the old pod goes away
	next := replicaSet(dep, "52", "2026-10-10T18:00:00Z", t0)
	if _, err := cs.AppsV1().ReplicaSets(ns).Create(ctx, next, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := cs.CoreV1().Pods(ns).Delete(ctx, old.Name+"-pod", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := cs.CoreV1().Pods(ns).Create(ctx, ownedPod(next, true, "sha256:bbb", t0.Add(30*time.Second)), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	d = w.Refresh(ctx)
	r = findRollout(t, d, "kami", 52)
	if r.State != RolloutDone || r.Trigger != "restart" {
		t.Fatalf("new revision: %+v", r)
	}
	if changed := r.ImageChanged(); changed == nil || !*changed {
		t.Fatalf("expected a new build, got digest %q prev %q", r.Digest, r.PrevDigest)
	}
	if prev := findRollout(t, d, "kami", 51); prev.State != RolloutSuperseded || prev.Digest != "sha256:aaa" {
		t.Fatalf("old revision lost its memory: %+v", prev)
	}
}

func TestRolloutStuckOnContainerConfigError(t *testing.T) {
	dep := rolloutDeployment("mycelia", 1)
	dep.Status.UpdatedReplicas = 1
	rs := replicaSet(dep, "1", "", t0)
	p := ownedPod(rs, false, "", t0)
	p.Status.ContainerStatuses[0].State.Waiting = &corev1.ContainerStateWaiting{
		Reason:  "CreateContainerConfigError",
		Message: `secret "mycelia-env-secrets" not found`,
	}
	w, _ := watcher(dep, rs, p)

	d := w.Refresh(context.Background())
	r := findRollout(t, d, "mycelia", 1)
	if r.State != RolloutStuck || r.Reason != `CreateContainerConfigError: secret "mycelia-env-secrets" not found` {
		t.Fatalf("got %+v", r)
	}
	if !d.Rollouts[0].InProgress() {
		t.Fatal("in-progress rollouts should sort first")
	}
}

func TestRolloutRollingWhileContainerCreating(t *testing.T) {
	dep := rolloutDeployment("luna", 1)
	rs := replicaSet(dep, "3", "", t0)
	p := ownedPod(rs, false, "", t0)
	p.Status.ContainerStatuses[0].State.Waiting = &corev1.ContainerStateWaiting{Reason: "ContainerCreating"}
	w, _ := watcher(dep, rs, p)

	if r := findRollout(t, w.Refresh(context.Background()), "luna", 3); r.State != RolloutRolling {
		t.Fatalf("got %+v", r)
	}
}

func TestStatefulSetPendingIsReported(t *testing.T) {
	replicas := int32(1)
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "mycelia-postgres", Namespace: ns, UID: "sts-uid", CreationTimestamp: metav1.NewTime(t0)},
		Spec:       appsv1.StatefulSetSpec{Replicas: &replicas},
	}
	controller := true
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mycelia-postgres-0", Namespace: ns,
			OwnerReferences: []metav1.OwnerReference{{Kind: "StatefulSet", UID: sts.UID, Controller: &controller}},
		},
		Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{
			Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: corev1.PodReasonUnschedulable,
			Message: "pod has unbound immediate PersistentVolumeClaims",
		}}},
	}
	w, _ := watcher(sts, p)

	r := findRollout(t, w.Refresh(context.Background()), "mycelia-postgres", 0)
	if r.State != RolloutStuck || r.Reason != "Unschedulable: pod has unbound immediate PersistentVolumeClaims" {
		t.Fatalf("got %+v", r)
	}
}

func TestStepsAreRememberedAfterPodIsDeleted(t *testing.T) {
	ctx := context.Background()
	finished := metav1.NewTime(t0.Add(-time.Minute))
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "wp-01abc", Namespace: ns, CreationTimestamp: metav1.NewTime(t0.Add(-2 * time.Minute)),
			Labels: map[string]string{stepLabel: "build", repoLabel: "kami", taskLabel: "250"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodFailed,
			ContainerStatuses: []corev1.ContainerStatus{{State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
				ExitCode: 1, Reason: "Error", FinishedAt: finished,
			}}}},
		},
	}
	w, cs := watcher(p)
	w.Refresh(ctx)

	if err := cs.CoreV1().Pods(ns).Delete(ctx, p.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	d := w.Refresh(ctx)
	if len(d.Steps) != 1 {
		t.Fatalf("expected the deleted step to be remembered, got %+v", d.Steps)
	}
	s := d.Steps[0]
	if s.Repo != "kami" || s.Name != "build" || s.Phase != "Failed" || s.Reason != "exit 1: Error" || s.FinishedAt == nil {
		t.Fatalf("got %+v", s)
	}
}

func TestMissingReplicaSetPermissionIsExplained(t *testing.T) {
	w, cs := watcher(rolloutDeployment("kami", 1))
	cs.PrependReactor("list", "replicasets", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "apps", Resource: "replicasets"}, "", nil)
	})

	d := w.Refresh(context.Background())
	if d.Error == "" || len(d.Rollouts) != 0 {
		t.Fatalf("got %+v", d)
	}
}

func TestOnDeleteStatefulSetOnlyShowsWhenStuck(t *testing.T) {
	replicas := int32(1)
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "noctis-postgres", Namespace: ns, UID: "sts-uid", CreationTimestamp: metav1.NewTime(t0)},
		Spec: appsv1.StatefulSetSpec{
			Replicas:       &replicas,
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{Type: appsv1.OnDeleteStatefulSetStrategyType},
		},
		Status: appsv1.StatefulSetStatus{CurrentRevision: "noctis-postgres-1", UpdateRevision: "noctis-postgres-2"},
	}
	controller := true
	pod := func(ready bool) *corev1.Pod {
		p := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "noctis-postgres-0", Namespace: ns,
				OwnerReferences: []metav1.OwnerReference{{Kind: "StatefulSet", UID: sts.UID, Controller: &controller}},
			},
		}
		if ready {
			p.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
		} else {
			p.Status.ContainerStatuses = []corev1.ContainerStatus{{
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
			}}
		}
		return p
	}

	// A pending revision with ready pods will never roll on its own, so it gets no row
	w, _ := watcher(sts, pod(true))
	for _, r := range w.Refresh(context.Background()).Rollouts {
		if r.Workload == "noctis-postgres" {
			t.Fatalf("healthy OnDelete StatefulSet got a row: %+v", r)
		}
	}

	// Enough newer history to overflow maxRollouts, which must not push the stuck row out
	objects := []runtime.Object{sts, pod(false)}
	dep := rolloutDeployment("kami", 1)
	objects = append(objects, dep)
	for i := 1; i <= maxRollouts+5; i++ {
		objects = append(objects, replicaSet(dep, strconv.Itoa(i), "", t0.Add(time.Duration(i)*time.Minute)))
	}
	w, _ = watcher(objects...)

	r := findRollout(t, w.Refresh(context.Background()), "noctis-postgres", 0)
	if r.State != RolloutStuck || r.Reason != "CrashLoopBackOff" {
		t.Fatalf("got %+v", r)
	}
}
