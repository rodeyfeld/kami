package k8s

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// ErrWorkloadNotFound is returned when no Deployment or StatefulSet matches the requested name
var ErrWorkloadNotFound = errors.New("workload not found")

const (
	pingTimeout  = 3 * time.Second
	pingCacheTTL = 10 * time.Second
)

// PingResult is the outcome of checking a workload through the probe declared in its pod spec
type PingResult struct {
	Name       string    `json:"name"`
	OK         bool      `json:"ok"`
	Probe      string    `json:"probe"` // "http", "tcp", or "pod-ready" when there is no network probe to call
	Path       string    `json:"path,omitempty"`
	StatusCode int       `json:"status_code,omitempty"`
	LatencyMs  int64     `json:"latency_ms"`
	Error      string    `json:"error,omitempty"`
	CheckedAt  time.Time `json:"checked_at"`
}

// Pinger calls a workload's readiness probe (falling back to its liveness probe) against one of its ready pods.
// Targets come from the cluster, never from the caller, so it can't be used to reach arbitrary hosts.
type Pinger struct {
	client *Client
	http   *http.Client

	mu    sync.Mutex
	cache map[string]*PingResult
}

func NewPinger(c *Client) *Pinger {
	return &Pinger{
		client: c,
		http: &http.Client{
			Timeout: pingTimeout,
			// Probes with scheme HTTPS skip verification, same as the kubelet
			Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
			// Probes treat redirects as success, so don't follow them
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		cache: make(map[string]*PingResult),
	}
}

// Ping checks a single Deployment or StatefulSet by name, reusing a recent result if there is one
func (p *Pinger) Ping(ctx context.Context, name string) (*PingResult, error) {
	if r := p.cached(name); r != nil {
		return r, nil
	}

	selector, err := p.selectorFor(ctx, name)
	if err != nil {
		return nil, err
	}

	r := p.ping(ctx, name, selector)
	p.store(r)
	return r, nil
}

// PingAll checks every Deployment and StatefulSet in the namespace concurrently
func (p *Pinger) PingAll(ctx context.Context) ([]*PingResult, error) {
	selectors, err := p.allSelectors(ctx)
	if err != nil {
		return nil, err
	}

	results := make([]*PingResult, len(selectors))
	var wg sync.WaitGroup
	for i, w := range selectors {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if r := p.cached(w.name); r != nil {
				results[i] = r
				return
			}
			results[i] = p.ping(ctx, w.name, w.selector)
			p.store(results[i])
		}()
	}
	wg.Wait()

	return results, nil
}

type workloadSelector struct {
	name     string
	selector labels.Selector
}

func (p *Pinger) selectorFor(ctx context.Context, name string) (labels.Selector, error) {
	apps := p.client.Clientset.AppsV1()

	dep, err := apps.Deployments(p.client.Namespace).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		return metav1.LabelSelectorAsSelector(dep.Spec.Selector)
	}
	if !apierrors.IsNotFound(err) {
		return nil, err
	}

	sts, err := apps.StatefulSets(p.client.Namespace).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		return metav1.LabelSelectorAsSelector(sts.Spec.Selector)
	}
	if apierrors.IsNotFound(err) {
		return nil, ErrWorkloadNotFound
	}
	return nil, err
}

func (p *Pinger) allSelectors(ctx context.Context) ([]workloadSelector, error) {
	apps := p.client.Clientset.AppsV1()
	selectors := make([]workloadSelector, 0)

	deployments, err := apps.Deployments(p.client.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list deployments: %w", err)
	}
	for _, dep := range deployments.Items {
		if s, err := metav1.LabelSelectorAsSelector(dep.Spec.Selector); err == nil {
			selectors = append(selectors, workloadSelector{dep.Name, s})
		}
	}

	statefulSets, err := apps.StatefulSets(p.client.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list statefulsets: %w", err)
	}
	for _, sts := range statefulSets.Items {
		if s, err := metav1.LabelSelectorAsSelector(sts.Spec.Selector); err == nil {
			selectors = append(selectors, workloadSelector{sts.Name, s})
		}
	}

	return selectors, nil
}

func (p *Pinger) ping(ctx context.Context, name string, selector labels.Selector) *PingResult {
	r := &PingResult{Name: name, Probe: "pod-ready", CheckedAt: time.Now().UTC()}

	// Results are cached and shared between callers, so one caller hanging up shouldn't fail the check for everyone
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*pingTimeout)
	defer cancel()

	pods, err := p.client.Clientset.CoreV1().Pods(p.client.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		r.Error = fmt.Sprintf("failed to list pods: %v", err)
		return r
	}

	pod := firstReadyPod(pods.Items)
	if pod == nil {
		r.Error = "no ready pods"
		return r
	}

	container, probe := findProbe(pod)
	switch {
	case probe != nil && probe.HTTPGet != nil:
		p.pingHTTP(ctx, r, pod, container, probe.HTTPGet)
	case probe != nil && probe.TCPSocket != nil:
		p.pingTCP(ctx, r, pod, container, probe.TCPSocket)
	default:
		// exec probes and workloads without probes can't be called over the network, so readiness is all we know
		r.OK = true
	}

	return r
}

func (p *Pinger) pingHTTP(ctx context.Context, r *PingResult, pod *corev1.Pod, container *corev1.Container, action *corev1.HTTPGetAction) {
	r.Probe = "http"
	r.Path = action.Path

	port, err := resolvePort(action.Port, container)
	if err != nil {
		r.Error = err.Error()
		return
	}

	scheme := "http"
	if action.Scheme == corev1.URISchemeHTTPS {
		scheme = "https"
	}
	url := fmt.Sprintf("%s://%s%s", scheme, net.JoinHostPort(pod.Status.PodIP, strconv.Itoa(port)), action.Path)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		r.Error = err.Error()
		return
	}
	for _, h := range action.HTTPHeaders {
		req.Header.Set(h.Name, h.Value)
	}

	start := time.Now()
	resp, err := p.http.Do(req)
	r.LatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		r.Error = err.Error()
		return
	}
	resp.Body.Close()

	r.StatusCode = resp.StatusCode
	// Same success range the kubelet uses for httpGet probes
	r.OK = resp.StatusCode >= 200 && resp.StatusCode < 400
	if !r.OK {
		r.Error = resp.Status
	}
}

func (p *Pinger) pingTCP(ctx context.Context, r *PingResult, pod *corev1.Pod, container *corev1.Container, action *corev1.TCPSocketAction) {
	r.Probe = "tcp"

	port, err := resolvePort(action.Port, container)
	if err != nil {
		r.Error = err.Error()
		return
	}

	dialer := net.Dialer{Timeout: pingTimeout}
	start := time.Now()
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(pod.Status.PodIP, strconv.Itoa(port)))
	r.LatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		r.Error = err.Error()
		return
	}
	conn.Close()
	r.OK = true
}

func firstReadyPod(pods []corev1.Pod) *corev1.Pod {
	for i := range pods {
		pod := &pods[i]
		if pod.DeletionTimestamp != nil || pod.Status.PodIP == "" {
			continue
		}
		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
				return pod
			}
		}
	}
	return nil
}

// findProbe returns the first container's readiness probe, or its liveness probe if it has no readiness probe
func findProbe(pod *corev1.Pod) (*corev1.Container, *corev1.Probe) {
	for i := range pod.Spec.Containers {
		c := &pod.Spec.Containers[i]
		if c.ReadinessProbe != nil {
			return c, c.ReadinessProbe
		}
		if c.LivenessProbe != nil {
			return c, c.LivenessProbe
		}
	}
	return nil, nil
}

func resolvePort(port intstr.IntOrString, container *corev1.Container) (int, error) {
	if port.Type == intstr.Int {
		return port.IntValue(), nil
	}
	for _, cp := range container.Ports {
		if cp.Name == port.StrVal {
			return int(cp.ContainerPort), nil
		}
	}
	return 0, fmt.Errorf("named port %q not found on container %s", port.StrVal, container.Name)
}

func (p *Pinger) cached(name string) *PingResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	if r, ok := p.cache[name]; ok && time.Since(r.CheckedAt) < pingCacheTTL {
		return r
	}
	return nil
}

func (p *Pinger) store(r *PingResult) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cache[r.Name] = r
}
