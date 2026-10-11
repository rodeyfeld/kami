// Package woodpecker reads pipelines from ouroboros, the Woodpecker CI server. It only makes GET
// requests: anonymously, which Woodpecker allows for public repos, or with the token of a user that
// has read-only access to the private ones (the forge's permissions carry over to Woodpecker).
package woodpecker

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrNotVisible is returned for repos that don't exist or are private, which Woodpecker doesn't distinguish for anonymous requests
var ErrNotVisible = errors.New("repo not visible")

type Repo struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	Private  bool   `json:"private"`
	Active   bool   `json:"active"`
}

type Pipeline struct {
	ID        int64      `json:"id"`
	Number    int64      `json:"number"`
	Status    string     `json:"status"` // created, pending, running, success, failure, killed, error, blocked, declined, skipped
	Event     string     `json:"event"`
	Branch    string     `json:"branch"`
	Commit    string     `json:"commit"`
	Message   string     `json:"message"`
	Author    string     `json:"author"`
	ForgeURL  string     `json:"forge_url"`
	Created   int64      `json:"created"`
	Started   int64      `json:"started"`
	Finished  int64      `json:"finished"`
	Workflows []Workflow `json:"workflows"`
}

type Workflow struct {
	Name     string `json:"name"`
	State    string `json:"state"`
	Children []Step `json:"children"`
}

type Step struct {
	ID       int64  `json:"id"`
	PID      int64  `json:"pid"` // the step's number in the pipeline, as its page URL uses it
	Name     string `json:"name"`
	State    string `json:"state"` // pending, running, success, failure, killed, skipped, ...
	Type     string `json:"type"`
	ExitCode int    `json:"exit_code"`
	Error    string `json:"error"`
	Started  int64  `json:"started"`
	Finished int64  `json:"finished"`
}

type logEntry struct {
	Data []byte `json:"data"`
}

// Event is what the server streams whenever a pipeline changes
type Event struct {
	Repo     Repo     `json:"repo"`
	Pipeline Pipeline `json:"pipeline"`
}

type Client struct {
	base   string
	token  string // empty for anonymous requests
	http   *http.Client
	stream *http.Client
}

func NewClient(baseURL, token string) *Client {
	return &Client{
		base:   strings.TrimSuffix(baseURL, "/"),
		token:  token,
		http:   &http.Client{Timeout: 10 * time.Second},
		stream: &http.Client{}, // the event stream stays open, so no overall timeout
	}
}

func (c *Client) Repo(ctx context.Context, id int64) (*Repo, error) {
	var r Repo
	return &r, c.get(ctx, fmt.Sprintf("/api/repos/%d", id), &r)
}

func (c *Client) Pipelines(ctx context.Context, repoID int64, perPage int) ([]Pipeline, error) {
	var p []Pipeline
	return p, c.get(ctx, fmt.Sprintf("/api/repos/%d/pipelines?perPage=%d", repoID, perPage), &p)
}

// Pipeline fetches one pipeline including its workflows and steps, which the list endpoint leaves out
func (c *Client) Pipeline(ctx context.Context, repoID, number int64) (*Pipeline, error) {
	var p Pipeline
	return &p, c.get(ctx, fmt.Sprintf("/api/repos/%d/pipelines/%d", repoID, number), &p)
}

// LogTail returns the last n lines a step printed
func (c *Client) LogTail(ctx context.Context, repoID, number, stepID int64, n int) ([]string, error) {
	var entries []logEntry
	if err := c.get(ctx, fmt.Sprintf("/api/repos/%d/logs/%d/%d", repoID, number, stepID), &entries); err != nil {
		return nil, err
	}
	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		lines = append(lines, strings.TrimRight(string(e.Data), "\r\n"))
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}

// Stream calls onEvent for each pipeline change until ctx is cancelled or the connection drops
func (c *Client) Stream(ctx context.Context, onEvent func(Event)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/stream/events", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	c.authorize(req)
	resp, err := c.stream.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("event stream: %s", resp.Status)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		// Lines starting with ":" are keepalive pings; events are single "data:" lines
		data, ok := strings.CutPrefix(scanner.Text(), "data:")
		if !ok {
			continue
		}
		var e Event
		if err := json.Unmarshal([]byte(strings.TrimSpace(data)), &e); err != nil {
			continue
		}
		onEvent(e)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return io.EOF
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	c.authorize(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusNotFound:
		return ErrNotVisible
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("GET %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) authorize(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}
