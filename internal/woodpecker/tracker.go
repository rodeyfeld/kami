package woodpecker

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	perRepo       = 10
	maxRepoScan   = 200
	repoScanGap   = 5 // private repos show up as gaps in the ids, so tolerate a few before stopping
	logTailLines  = 20
	queuedWarning = 2 * time.Minute

	// A rollout belongs to a pipeline when its ReplicaSet was created while one of the pipeline's
	// steps was running. Woodpecker records seconds and the restart lands at the end of the step.
	matchBefore = 5 * time.Second
	matchAfter  = 10 * time.Second
)

// Status says whether kami can currently see Woodpecker
type Status struct {
	Connected bool   `json:"connected"` // the event stream is open
	Error     string `json:"error,omitempty"`
	Repos     int    `json:"repos"`
}

// PipelineView is a pipeline as the deploys page shows it
type PipelineView struct {
	Repo      string     `json:"repo"`
	Number    int64      `json:"number"`
	Status    string     `json:"status"`
	Event     string     `json:"event"`
	Branch    string     `json:"branch"`
	Commit    string     `json:"commit"`
	Message   string     `json:"message"` // first line only
	Author    string     `json:"author"`
	URL       string     `json:"url"`
	CommitURL string     `json:"commit_url,omitempty"`
	Created   time.Time  `json:"created"`
	Finished  *time.Time `json:"finished,omitempty"`
	Steps     []StepView `json:"steps"`
	// Last lines of the first failed step's log
	FailedStep string   `json:"failed_step,omitempty"`
	LogTail    []string `json:"log_tail,omitempty"`
}

type StepView struct {
	Name     string         `json:"name"`
	State    string         `json:"state"`
	ExitCode int            `json:"exit_code,omitempty"`
	Error    string         `json:"error,omitempty"`
	Duration *time.Duration `json:"duration,omitempty"`
}

func (p *PipelineView) Running() bool {
	return isActive(p.Status)
}

func isActive(status string) bool {
	return status == "running" || status == "pending" || status == "created"
}

func (p *PipelineView) Failed() bool {
	return p.Status == "failure" || p.Status == "error" || p.Status == "killed"
}

// Queued is true when a pipeline has waited long enough to start that something is likely wrong,
// e.g. the agent can't create pods (as when the k3s certs expired)
func (p *PipelineView) Queued() bool {
	return (p.Status == "pending" || p.Status == "created") && time.Since(p.Created) > queuedWarning
}

// Tracker keeps recent pipelines for every repo the client can see, kept current by Woodpecker's event stream
type Tracker struct {
	client    *Client
	publicURL string

	mu        sync.Mutex
	repos     map[int64]Repo
	pipelines map[int64][]*Pipeline // per repo, newest first
	logs      map[int64][]string    // log tails by step id
	connected bool
	loaded    bool
	err       string
}

func NewTracker(client *Client, publicURL string) *Tracker {
	return &Tracker{
		client:    client,
		publicURL: strings.TrimSuffix(publicURL, "/"),
		repos:     make(map[int64]Repo),
		pipelines: make(map[int64][]*Pipeline),
		logs:      make(map[int64][]string),
	}
}

// Start loads recent pipelines, then follows the event stream, reloading after every reconnect
// so changes missed while disconnected still show up
func (t *Tracker) Start(ctx context.Context) {
	go func() {
		backoff := 5 * time.Second
		for ctx.Err() == nil {
			if err := t.Sync(ctx); err != nil {
				t.setError(err)
			} else {
				started := time.Now()
				t.setConnected(true)
				err := t.client.Stream(ctx, func(e Event) { t.handle(ctx, e) })
				t.setConnected(false)
				if ctx.Err() != nil {
					return
				}
				t.setError(fmt.Errorf("event stream dropped: %w", err))
				if time.Since(started) > time.Minute {
					backoff = 5 * time.Second
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, time.Minute)
		}
	}()
}

// Sync finds the visible repos and loads their recent pipelines
func (t *Tracker) Sync(ctx context.Context) error {
	repos, err := t.discover(ctx)
	if err != nil {
		return err
	}
	if len(repos) == 0 {
		return errors.New("no repos visible")
	}

	var wg sync.WaitGroup
	errs := make(chan error, len(repos))
	for _, repo := range repos {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := t.loadRepo(ctx, repo); err != nil {
				errs <- fmt.Errorf("%s: %w", repo.FullName, err)
			}
		}()
	}
	wg.Wait()
	close(errs)

	t.mu.Lock()
	defer t.mu.Unlock()
	t.loaded = true
	if err := <-errs; err != nil {
		return err
	}
	t.err = ""
	return nil
}

func (t *Tracker) discover(ctx context.Context) ([]Repo, error) {
	repos := make([]Repo, 0)
	gap := 0
	for id := int64(1); id <= maxRepoScan && gap < repoScanGap; id++ {
		repo, err := t.client.Repo(ctx, id)
		if errors.Is(err, ErrNotVisible) {
			gap++
			continue
		}
		if err != nil {
			return nil, err
		}
		gap = 0
		// Woodpecker already hides repos the client can't read, so private ones here are visible to it
		if repo.Active {
			repos = append(repos, *repo)
		}
	}

	t.mu.Lock()
	for _, r := range repos {
		t.repos[r.ID] = r
	}
	t.mu.Unlock()
	return repos, nil
}

func (t *Tracker) loadRepo(ctx context.Context, repo Repo) error {
	list, err := t.client.Pipelines(ctx, repo.ID, perRepo)
	if err != nil {
		return err
	}
	for _, p := range list {
		if t.isCurrent(repo.ID, p) {
			continue
		}
		if err := t.refresh(ctx, repo.ID, p.Number); err != nil {
			return err
		}
	}
	return nil
}

// isCurrent skips refetching finished pipelines kami already has
func (t *Tracker) isCurrent(repoID int64, p Pipeline) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, known := range t.pipelines[repoID] {
		if known.Number == p.Number {
			return known.Status == p.Status && p.Finished != 0
		}
	}
	return false
}

func (t *Tracker) handle(ctx context.Context, e Event) {
	if e.Pipeline.Number == 0 {
		return
	}
	t.mu.Lock()
	if _, ok := t.repos[e.Repo.ID]; !ok {
		t.repos[e.Repo.ID] = e.Repo
	}
	t.mu.Unlock()

	// Events carry the pipeline but not always its steps, so fetch the full pipeline
	if err := t.refresh(ctx, e.Repo.ID, e.Pipeline.Number); err != nil {
		log.Printf("woodpecker: refreshing %s #%d: %v", e.Repo.FullName, e.Pipeline.Number, err)
	}
}

func (t *Tracker) refresh(ctx context.Context, repoID, number int64) error {
	p, err := t.client.Pipeline(ctx, repoID, number)
	if err != nil {
		return err
	}

	if failed := firstFailedStep(p); failed != nil && p.Finished != 0 {
		t.mu.Lock()
		_, have := t.logs[failed.ID]
		t.mu.Unlock()
		if !have {
			if lines, err := t.client.LogTail(ctx, repoID, number, failed.ID, logTailLines); err == nil {
				t.mu.Lock()
				t.logs[failed.ID] = lines
				t.mu.Unlock()
			}
		}
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	list := t.pipelines[repoID]
	replaced := false
	for i, known := range list {
		if known.Number == p.Number {
			list[i] = p
			replaced = true
		}
	}
	if !replaced {
		list = append(list, p)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Number > list[j].Number })
	if len(list) > perRepo {
		for _, dropped := range list[perRepo:] {
			for _, s := range steps(dropped) {
				delete(t.logs, s.ID)
			}
		}
		list = list[:perRepo]
	}
	t.pipelines[repoID] = list
	return nil
}

func (t *Tracker) Status() Status {
	t.mu.Lock()
	defer t.mu.Unlock()
	return Status{Connected: t.connected, Error: t.err, Repos: len(t.repos)}
}

// Recent returns the newest pipelines across every repo
func (t *Tracker) Recent(n int) []PipelineView {
	t.mu.Lock()
	defer t.mu.Unlock()

	views := make([]PipelineView, 0)
	for repoID, list := range t.pipelines {
		for _, p := range list {
			views = append(views, t.view(repoID, p))
		}
	}
	sort.Slice(views, func(i, j int) bool { return views[i].Created.After(views[j].Created) })
	if len(views) > n {
		views = views[:n]
	}
	return views
}

// Match finds the pipeline whose step was running when a rollout started, preferring the
// workload's own repo over others (mirage restarts every workload). covered is false when
// kami's pipeline history doesn't reach back to that time, so a missing match means nothing.
func (t *Tracker) Match(repo string, at time.Time) (view *PipelineView, covered bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.loaded {
		return nil, false
	}

	var fallback *PipelineView
	for repoID, list := range t.pipelines {
		for _, p := range list {
			if !ranDuring(p, at) {
				continue
			}
			v := t.view(repoID, p)
			if v.Repo == repo {
				return &v, true
			}
			if fallback == nil {
				fallback = &v
			}
		}
	}
	if fallback != nil {
		return fallback, true
	}
	return nil, at.After(t.coverage())
}

// coverage is the time after which every repo's pipelines are all loaded. Callers hold t.mu.
func (t *Tracker) coverage() time.Time {
	var since time.Time
	for _, list := range t.pipelines {
		// Repos with fewer pipelines than the cap have their whole history loaded
		if len(list) < perRepo {
			continue
		}
		if oldest := time.Unix(list[len(list)-1].Created, 0); oldest.After(since) {
			since = oldest
		}
	}
	return since
}

func ranDuring(p *Pipeline, at time.Time) bool {
	for _, s := range steps(p) {
		if s.Type == "clone" || s.Started == 0 {
			continue
		}
		start := time.Unix(s.Started, 0).Add(-matchBefore)
		var end time.Time
		switch {
		case s.Finished != 0:
			end = time.Unix(s.Finished, 0).Add(matchAfter)
		case p.Finished != 0:
			// Killed pipelines can leave steps marked running with no finish time
			end = time.Unix(p.Finished, 0).Add(matchAfter)
		case isActive(p.Status):
			end = time.Now()
		default:
			continue
		}
		if !at.Before(start) && !at.After(end) {
			return true
		}
	}
	return false
}

// view builds what the page shows. Callers hold t.mu.
func (t *Tracker) view(repoID int64, p *Pipeline) PipelineView {
	repo := t.repos[repoID]
	message, _, _ := strings.Cut(strings.TrimSpace(p.Message), "\n")
	v := PipelineView{
		Repo:      repo.Name,
		Number:    p.Number,
		Status:    p.Status,
		Event:     p.Event,
		Branch:    p.Branch,
		Commit:    shortSHA(p.Commit),
		Message:   message,
		Author:    p.Author,
		URL:       fmt.Sprintf("%s/repos/%d/pipeline/%d", t.publicURL, repoID, p.Number),
		CommitURL: p.ForgeURL,
		Created:   time.Unix(p.Created, 0).UTC(),
		Steps:     make([]StepView, 0),
	}
	if p.Finished != 0 {
		f := time.Unix(p.Finished, 0).UTC()
		v.Finished = &f
	}
	for _, s := range steps(p) {
		sv := StepView{Name: s.Name, State: s.State, ExitCode: s.ExitCode, Error: s.Error}
		if !isActive(p.Status) && isActive(s.State) {
			// A finished pipeline can't have steps still going; they stopped when it was killed
			sv.State = "killed"
		}
		if s.Started != 0 && s.Finished != 0 {
			d := time.Duration(s.Finished-s.Started) * time.Second
			sv.Duration = &d
		}
		v.Steps = append(v.Steps, sv)
	}
	if failed := firstFailedStep(p); failed != nil {
		v.FailedStep = failed.Name
		v.LogTail = t.logs[failed.ID]
	}
	return v
}

func steps(p *Pipeline) []Step {
	all := make([]Step, 0)
	for _, w := range p.Workflows {
		all = append(all, w.Children...)
	}
	return all
}

func firstFailedStep(p *Pipeline) *Step {
	for _, s := range steps(p) {
		if s.State == "failure" || s.State == "error" || s.State == "killed" {
			return &s
		}
	}
	return nil
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func (t *Tracker) setConnected(connected bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.connected = connected
	if connected {
		t.err = ""
	}
}

func (t *Tracker) setError(err error) {
	log.Printf("woodpecker: %v", err)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.err = err.Error()
}
