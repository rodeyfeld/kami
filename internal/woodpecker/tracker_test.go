package woodpecker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 10, 23, 0, 0, 0, time.UTC)

func unix(d time.Duration) int64 { return t0.Add(d).Unix() }

// fakeServer serves repos and pipelines the way Woodpecker does: private repos only to requests with its token
type fakeServer struct {
	mu        sync.Mutex
	repos     map[int64]Repo
	pipelines map[int64][]Pipeline
	logs      map[int64][]string
	events    chan Event
}

const fakeToken = "kami-read-token"

func newFakeServer() *fakeServer {
	return &fakeServer{
		repos:     make(map[int64]Repo),
		pipelines: make(map[int64][]Pipeline),
		logs:      make(map[int64][]string),
		events:    make(chan Event, 4),
	}
}

func (f *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var repoID, number, stepID int64
	switch {
	case r.URL.Path == "/api/stream/events":
		f.mu.Unlock()
		defer f.mu.Lock()
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, ": ping\n\n")
		w.(http.Flusher).Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case e := <-f.events:
				b, _ := json.Marshal(e)
				fmt.Fprintf(w, "data: %s\n\n", b)
				w.(http.Flusher).Flush()
			}
		}
	case scan(r.URL.Path, "/api/repos/%d/logs/%d/%d", &repoID, &number, &stepID):
		entries := make([]logEntry, 0)
		for _, line := range f.logs[stepID] {
			entries = append(entries, logEntry{Data: []byte(line + "\n")})
		}
		json.NewEncoder(w).Encode(entries)
	case scan(r.URL.Path, "/api/repos/%d/pipelines/%d", &repoID, &number):
		for _, p := range f.pipelines[repoID] {
			if p.Number == number {
				json.NewEncoder(w).Encode(p)
				return
			}
		}
		http.NotFound(w, r)
	case scan(r.URL.Path, "/api/repos/%d/pipelines", &repoID):
		list := make([]Pipeline, 0)
		for _, p := range f.pipelines[repoID] {
			p.Workflows = nil // the list endpoint leaves out steps
			list = append(list, p)
		}
		json.NewEncoder(w).Encode(list)
	case scan(r.URL.Path, "/api/repos/%d", &repoID):
		repo, ok := f.repos[repoID]
		if !ok || (repo.Private && r.Header.Get("Authorization") != "Bearer "+fakeToken) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(repo)
	default:
		http.NotFound(w, r)
	}
}

// scan matches a path exactly against a format of %d segments
func scan(path, format string, ids ...*int64) bool {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	n, err := fmt.Sscanf(path, format, args...)
	if err != nil || n != len(ids) {
		return false
	}
	vals := make([]any, len(ids))
	for i, id := range ids {
		vals[i] = *id
	}
	return fmt.Sprintf(format, vals...) == path
}

func pipeline(number int64, status string, created time.Duration, steps ...Step) Pipeline {
	return Pipeline{
		Number: number, Status: status, Event: "push", Branch: "main",
		Commit: "b2346949d1323a2f50f11892210b01474b03599b", Message: "Fix the thing\n\nLonger body", Author: "rodeyfeld",
		Created: unix(created), Started: unix(created), Finished: unix(created + 3*time.Minute),
		Workflows: []Workflow{{Name: "woodpecker", State: status, Children: steps}},
	}
}

func step(id int64, name, state string, started, finished time.Duration) Step {
	return Step{ID: id, Name: name, State: state, Type: "commands", Started: unix(started), Finished: unix(finished)}
}

func setup(t *testing.T) (*fakeServer, *Tracker) {
	return setupWithToken(t, "")
}

func setupWithToken(t *testing.T, token string) (*fakeServer, *Tracker) {
	f := newFakeServer()
	f.repos[1] = Repo{ID: 1, Name: "kami", FullName: "rodeyfeld/kami", Active: true}
	f.repos[2] = Repo{ID: 2, Name: "mirage", FullName: "rodeyfeld/mirage", Active: true}
	f.repos[3] = Repo{ID: 3, Name: "mycelia", FullName: "rodeyfeld/mycelia", Active: true, Private: true}
	f.repos[4] = Repo{ID: 4, Name: "luna", FullName: "rodeyfeld/luna", Active: true}

	f.pipelines[1] = []Pipeline{pipeline(25, "success", 0,
		step(11, "build", "success", 10*time.Second, 160*time.Second),
		step(12, "deploy", "success", 160*time.Second, 163*time.Second),
	)}
	f.pipelines[2] = []Pipeline{pipeline(90, "success", time.Hour,
		step(21, "apply-all", "success", time.Hour+10*time.Second, time.Hour+20*time.Second),
		step(22, "restart-deployments", "success", time.Hour+20*time.Second, time.Hour+50*time.Second),
	)}
	f.pipelines[4] = []Pipeline{pipeline(7, "failure", -time.Hour,
		step(41, "build", "failure", -time.Hour, -time.Hour+time.Minute),
		step(42, "deploy", "skipped", 0, 0),
	)}
	f.pipelines[4][0].Workflows[0].Children[0].ExitCode = 1
	f.logs[41] = []string{"#5 [build 2/4] RUN go build", "#5 ERROR: exit code 1", "ERROR: failed to solve"}

	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	tr := NewTracker(NewClient(srv.URL, token), "https://ouroboros.pinwheel.fan")
	if err := tr.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	return f, tr
}

func TestSyncSkipsPrivateReposAndLoadsSteps(t *testing.T) {
	_, tr := setup(t)

	if s := tr.Status(); s.Repos != 3 || s.Error != "" {
		t.Fatalf("status: %+v", s)
	}
	recent := tr.Recent(10)
	if len(recent) != 3 || recent[0].Repo != "mirage" {
		t.Fatalf("recent: %+v", recent)
	}
	kami := recent[1]
	if kami.Commit != "b234694" || kami.Message != "Fix the thing" || len(kami.Steps) != 2 {
		t.Fatalf("kami pipeline: %+v", kami)
	}
	if kami.URL != "https://ouroboros.pinwheel.fan/repos/1/pipeline/25" {
		t.Fatalf("url: %s", kami.URL)
	}
	if d := kami.Steps[0].Duration; d == nil || *d != 150*time.Second {
		t.Fatalf("build duration: %v", d)
	}
}

func TestTokenSeesPrivateRepos(t *testing.T) {
	_, tr := setupWithToken(t, fakeToken)

	if s := tr.Status(); s.Repos != 4 || s.Error != "" {
		t.Fatalf("status: %+v", s)
	}
}

func TestFailedPipelineCarriesLogTail(t *testing.T) {
	_, tr := setup(t)

	var luna PipelineView
	for _, p := range tr.Recent(10) {
		if p.Repo == "luna" {
			luna = p
		}
	}
	if !luna.Failed() || luna.FailedStep != "build" || len(luna.LogTail) != 3 || luna.LogTail[2] != "ERROR: failed to solve" {
		t.Fatalf("got %+v", luna)
	}
}

func TestMatchPrefersOwnRepoThenMirage(t *testing.T) {
	_, tr := setup(t)

	// kami's deploy step restarted kami
	if p, _ := tr.Match("kami", t0.Add(163*time.Second)); p == nil || p.Repo != "kami" {
		t.Fatalf("kami deploy: %+v", p)
	}
	// mirage's restart-deployments step restarted luna, which has no pipeline of its own then
	if p, _ := tr.Match("luna", t0.Add(time.Hour+40*time.Second)); p == nil || p.Repo != "mirage" {
		t.Fatalf("mirage restart: %+v", p)
	}
	// nothing was running: a manual restart, inside the loaded history
	p, covered := tr.Match("luna", t0.Add(30*time.Minute))
	if p != nil || !covered {
		t.Fatalf("manual restart: %+v covered=%v", p, covered)
	}
}

func TestStreamEventRefreshesPipeline(t *testing.T) {
	f, tr := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	running := pipeline(26, "running", 2*time.Hour, step(13, "build", "running", 2*time.Hour, 0))
	running.Finished = 0
	running.Workflows[0].Children[0].Finished = 0
	f.mu.Lock()
	f.pipelines[1] = append([]Pipeline{running}, f.pipelines[1]...)
	f.mu.Unlock()

	go tr.client.Stream(ctx, func(e Event) { tr.handle(ctx, e) })
	f.events <- Event{Repo: f.repos[1], Pipeline: Pipeline{Number: 26, Status: "running"}}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if r := tr.Recent(1); len(r) == 1 && r[0].Number == 26 {
			if !r[0].Running() || r[0].Steps[0].Name != "build" {
				t.Fatalf("got %+v", r[0])
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("event never refreshed the pipeline")
}

func TestQueuedAfterTwoMinutes(t *testing.T) {
	p := PipelineView{Status: "pending", Created: time.Now().Add(-3 * time.Minute)}
	if !p.Queued() {
		t.Fatal("expected a pipeline pending for 3m to count as queued")
	}
	p.Created = time.Now()
	if p.Queued() {
		t.Fatal("a just-created pipeline isn't stuck yet")
	}
}

func TestScanMatchesExactPaths(t *testing.T) {
	var a, b int64
	if !scan("/api/repos/2/pipelines/25", "/api/repos/%d/pipelines/%d", &a, &b) || a != 2 || b != 25 {
		t.Fatal("expected a match")
	}
	if scan("/api/repos/2/pipelines/25", "/api/repos/%d", &a) {
		t.Fatal("prefix should not match")
	}
}

func TestKilledPipelineDoesNotMatchForever(t *testing.T) {
	f, _ := setup(t)
	killed := pipeline(50, "killed", -48*time.Hour,
		step(51, "build", "running", -48*time.Hour, 0),
	)
	killed.Workflows[0].Children[0].Finished = 0
	f.mu.Lock()
	f.pipelines[4] = append(f.pipelines[4], killed)
	f.mu.Unlock()

	srv := httptest.NewServer(f)
	defer srv.Close()
	tr := NewTracker(NewClient(srv.URL, ""), "")
	if err := tr.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}

	if p, _ := tr.Match("luna", t0.Add(30*time.Minute)); p != nil {
		t.Fatalf("a killed pipeline from two days ago matched: %+v", p)
	}
	for _, p := range tr.Recent(10) {
		if p.Number == 50 && p.Steps[0].State != "killed" {
			t.Fatalf("step of a killed pipeline still shows %q", p.Steps[0].State)
		}
	}
}
