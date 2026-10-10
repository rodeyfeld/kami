package k8s

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
)

const ns = "galaxy"

func deployment(name string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec:       appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"name": name}}},
	}
}

func pod(name string, ready bool, container corev1.Container) *corev1.Pod {
	status := corev1.ConditionFalse
	if ready {
		status = corev1.ConditionTrue
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name + "-abc", Namespace: ns, Labels: map[string]string{"name": name}},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{container}},
		Status: corev1.PodStatus{
			PodIP:      "127.0.0.1",
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: status}},
		},
	}
}

func crashLooping(p *corev1.Pod) *corev1.Pod {
	p.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: p.Spec.Containers[0].Name, RestartCount: 14}}
	return p
}

func httpProbe(path string, port intstr.IntOrString) *corev1.Probe {
	return &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: path, Port: port}}}
}

func withHost(p *corev1.Probe, host string) *corev1.Probe {
	p.HTTPGet.HTTPHeaders = []corev1.HTTPHeader{{Name: "Host", Value: host}}
	return p
}

func serverPort(t *testing.T, s *httptest.Server) int {
	u, _ := url.Parse(s.URL)
	port, _ := strconv.Atoi(u.Port())
	return port
}

func newPinger(objs ...runtime.Object) *Pinger {
	return NewPinger(&Client{Clientset: fake.NewSimpleClientset(objs...), Namespace: ns})
}

func TestPingHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "doppler-service" && r.URL.Path == "/readyz" {
			w.WriteHeader(http.StatusBadRequest) // like Django rejecting a Host outside ALLOWED_HOSTS
			return
		}
		if r.URL.Path == "/readyz" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	port := serverPort(t, srv)

	p := newPinger(
		deployment("doppler"),
		pod("doppler", true, corev1.Container{Name: "doppler", ReadinessProbe: withHost(httpProbe("/readyz", intstr.FromInt(port)), "doppler-service")}),
		deployment("augur"),
		pod("augur", true, corev1.Container{
			Name:           "augur",
			Ports:          []corev1.ContainerPort{{Name: "http", ContainerPort: int32(port)}},
			ReadinessProbe: httpProbe("/broken", intstr.FromString("http")),
		}),
	)

	r, err := p.Ping(context.Background(), "doppler")
	if err != nil {
		t.Fatal(err)
	}
	if !r.OK || r.Probe != "http" || r.StatusCode != 200 || r.Path != "/readyz" || r.Port != port {
		t.Errorf("doppler: got %+v", r)
	}

	r, err = p.Ping(context.Background(), "augur")
	if err != nil {
		t.Fatal(err)
	}
	if r.OK || r.StatusCode != 500 || r.Error == "" {
		t.Errorf("augur: expected failing 500 via named port, got %+v", r)
	}
}

func TestPingTCPAndExec(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	tcp := &corev1.Probe{ProbeHandler: corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt(port)}}}
	exec := &corev1.Probe{ProbeHandler: corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: []string{"pg_isready"}}}}

	p := newPinger(
		deployment("garage"), pod("garage", true, corev1.Container{Name: "garage", LivenessProbe: tcp}),
		deployment("atlas"), pod("atlas", true, corev1.Container{Name: "atlas", ReadinessProbe: exec}),
		deployment("luna"), pod("luna", true, corev1.Container{Name: "luna"}),
		deployment("noctis"), crashLooping(pod("noctis", false, corev1.Container{Name: "noctis", ReadinessProbe: exec})),
	)

	if r, _ := p.Ping(context.Background(), "garage"); !r.OK || r.Probe != "tcp" {
		t.Errorf("garage: got %+v", r)
	}
	if r, _ := p.Ping(context.Background(), "atlas"); !r.OK || r.Probe != "exec" || r.Verified() {
		t.Errorf("atlas: got %+v", r)
	}
	if r, _ := p.Ping(context.Background(), "luna"); !r.OK || r.Probe != "none" {
		t.Errorf("luna: got %+v", r)
	}
	if r, _ := p.Ping(context.Background(), "noctis"); r.OK || r.Error != "no ready pods" || r.Restarts != 14 || r.Probe != "exec" {
		t.Errorf("noctis: expected crash loop with restarts, got %+v", r)
	}

	all, err := p.PingAll(context.Background())
	if err != nil || len(all) != 4 {
		t.Fatalf("PingAll: got %d results, err %v", len(all), err)
	}
}

func TestPingNotFound(t *testing.T) {
	_, err := newPinger().Ping(context.Background(), "nope")
	if !errors.Is(err, ErrWorkloadNotFound) {
		t.Errorf("expected ErrWorkloadNotFound, got %v", err)
	}
}

func TestLatestJobsCollapsesCronJobRuns(t *testing.T) {
	now := time.Now()
	job := func(name, owner string, age time.Duration, failed int32) batchv1.Job {
		j := batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: name, CreationTimestamp: metav1.NewTime(now.Add(-age))}}
		if owner != "" {
			j.OwnerReferences = []metav1.OwnerReference{{Kind: "CronJob", Name: owner}}
		}
		j.Status.Failed = failed
		return j
	}

	jobs := latestJobs([]batchv1.Job{
		job("oracle-1", "oracle", 3*time.Hour, 1),
		job("oracle-3", "oracle", time.Hour, 0),
		job("oracle-2", "oracle", 2*time.Hour, 1),
		job("augur-db-migrate", "", 48*time.Hour, 0),
	})

	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}
	if jobs[0].Name != "oracle-3" || jobDisplayName(jobs[0]) != "oracle" {
		t.Errorf("expected newest oracle run shown as oracle, got %s", jobs[0].Name)
	}
	if jobDisplayName(jobs[1]) != "augur-db-migrate" {
		t.Errorf("standalone job renamed: %s", jobDisplayName(jobs[1]))
	}
}
