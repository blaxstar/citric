// Package node is the build-node agent: it asks a citric server for work,
// runs each job in a throwaway worktree of a local mirror, and streams the
// output back. All traffic is node → server, so a node can sit behind NAT.
package node

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/blaxstar/citric/internal/config"
	critgit "github.com/blaxstar/citric/internal/git"
	"github.com/blaxstar/citric/internal/model"
	"github.com/blaxstar/citric/internal/proc"
	"github.com/blaxstar/citric/internal/runner"
)

// Config is a node's settings.
type Config struct {
	Server        string   // citric server base URL
	Name          string   // this node's registered name
	Credential    string   // bearer credential from joining
	Labels        []string // extra runs_on labels (os and arch are added)
	MaxConcurrent int      // jobs at once
	DataDir       string   // mirrors, worktrees
	CacheDir      string   // shared build caches
	BuildPath     string   // PATH for build commands
	Version       string
}

// ErrRejected means the server no longer accepts this node's credential
// (it was removed); retrying won't help.
var ErrRejected = errors.New("the server rejected this node's credential (was the node removed?)")

// Join exchanges a one-time join token for a node credential.
func Join(server, token, name string) (string, error) {
	body, _ := json.Marshal(map[string]string{"token": token, "name": name})
	resp, err := http.Post(strings.TrimRight(server, "/")+"/node/join", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("join: %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	var out struct {
		Credential string `json:"credential"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.Credential == "" {
		return "", fmt.Errorf("join: bad response")
	}
	return out.Credential, nil
}

type agent struct {
	cfg      Config
	client   *http.Client
	exec     *runner.Runner
	inFlight atomic.Int32
	mirrorMu sync.Map // repo -> *sync.Mutex
}

// Run serves jobs until ctx is cancelled or the server rejects the node.
func Run(ctx context.Context, cfg Config) error {
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 1
	}
	cfg.Server = strings.TrimRight(cfg.Server, "/")
	for _, d := range []string{cfg.DataDir, filepath.Join(cfg.DataDir, "mirrors"), filepath.Join(cfg.DataDir, "run")} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			return err
		}
	}
	scfg := &config.Config{}
	scfg.Server.DataDir = cfg.DataDir
	scfg.Server.CacheDir = cfg.CacheDir
	scfg.Server.BuildPath = cfg.BuildPath
	a := &agent{
		cfg:    cfg,
		client: &http.Client{Timeout: 60 * time.Second},
		exec:   runner.NewExecutor(scfg),
	}
	log.Printf("node %s serving %s with %d slot(s), labels %v", cfg.Name, cfg.Server, cfg.MaxConcurrent, a.labels())

	errc := make(chan error, cfg.MaxConcurrent)
	var wg sync.WaitGroup
	for i := 0; i < cfg.MaxConcurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errc <- a.worker(ctx)
		}()
	}
	wg.Wait()
	close(errc)
	for err := range errc {
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *agent) labels() []string {
	return append([]string{runtime.GOOS, runtime.GOARCH}, a.cfg.Labels...)
}

func (a *agent) worker(ctx context.Context) error {
	backoff := time.Second
	for ctx.Err() == nil {
		job, err := a.claim(ctx)
		switch {
		case errors.Is(err, ErrRejected):
			return err
		case err != nil:
			if ctx.Err() == nil {
				log.Printf("claim: %v (retrying in %s)", err, backoff)
				select {
				case <-time.After(backoff):
				case <-ctx.Done():
				}
				backoff = min(backoff*2, 30*time.Second)
			}
			continue
		}
		backoff = time.Second
		if job != nil {
			a.inFlight.Add(1)
			a.runJob(ctx, job)
			a.inFlight.Add(-1)
		}
	}
	return nil
}

func (a *agent) post(ctx context.Context, path, contentType string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.Server+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.Credential)
	req.Header.Set("Content-Type", contentType)
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		return nil, ErrRejected
	}
	return resp, nil
}

// claim long-polls the server for one job; (nil, nil) means none came up.
func (a *agent) claim(ctx context.Context) (*runner.RemoteJob, error) {
	body, _ := json.Marshal(map[string]any{
		"os": runtime.GOOS, "arch": runtime.GOARCH, "labels": a.cfg.Labels,
		"version": a.cfg.Version, "max_concurrent": a.cfg.MaxConcurrent,
		"in_flight": a.inFlight.Load(),
	})
	resp, err := a.post(ctx, "/node/claim", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNoContent:
		return nil, nil
	case http.StatusOK:
		var job runner.RemoteJob
		if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
			return nil, fmt.Errorf("decode job: %w", err)
		}
		return &job, nil
	}
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return nil, fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(msg)))
}

func (a *agent) runJob(ctx context.Context, job *runner.RemoteJob) {
	out := newLogShipper(a, job.BuildID)
	timeout := time.Duration(job.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	jobCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out.start(jobCtx, cancel)

	status, errMsg := a.execute(jobCtx, job, out)
	switch {
	case out.wasCancelled():
		fmt.Fprintln(out, "[cancelled]")
		status = model.BuildStatusFail
	case errors.Is(jobCtx.Err(), context.DeadlineExceeded):
		fmt.Fprintln(out, "[timeout]")
		status = model.BuildStatusFail
	}
	out.stop()

	body, _ := json.Marshal(map[string]string{"status": string(status), "error": errMsg})
	for attempt := 0; attempt < 5; attempt++ {
		resp, err := a.post(context.Background(), "/node/jobs/"+strconv.FormatInt(job.BuildID, 10)+"/done",
			"application/json", bytes.NewReader(body))
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(time.Duration(attempt+1) * 2 * time.Second)
	}
	log.Printf("build %d: could not report result; the server will mark it lost", job.BuildID)
}

// execute checks the commit out and runs the command, returning the result.
func (a *agent) execute(ctx context.Context, job *runner.RemoteJob, out io.Writer) (model.BuildStatus, string) {
	mirror, err := a.syncMirror(ctx, job)
	if err != nil {
		return model.BuildStatusFail, "fetch: " + err.Error()
	}
	wt := filepath.Join(a.cfg.DataDir, "run", strconv.FormatInt(job.BuildID, 10))
	gitEnv := append(os.Environ(), "GIT_DIR="+mirror)
	gitEnv = append(gitEnv, critgit.Env(a.cfg.DataDir)...)
	os.RemoveAll(wt)
	add := exec.CommandContext(ctx, "git", "worktree", "add", "--detach", wt, job.CommitSHA)
	add.Env = gitEnv
	if b, err := add.CombinedOutput(); err != nil {
		return model.BuildStatusFail, fmt.Sprintf("worktree: %v\n%s", err, b)
	}
	defer func() {
		rm := exec.Command("git", "worktree", "remove", "--force", wt)
		rm.Env = gitEnv
		rm.Run()
		os.RemoveAll(wt)
	}()

	cmd, err := proc.Command(ctx, job.Shell, job.Command)
	if err != nil {
		return model.BuildStatusFail, err.Error()
	}
	cmd.Dir = wt
	cmd.Env = a.exec.ExecEnv(job.ContextEnv, job.Env)
	cmd.Stdout, cmd.Stderr = out, out
	if err := proc.Run(cmd); err != nil {
		if ctx.Err() != nil {
			return model.BuildStatusFail, "" // cancelled / timed out: runJob says which
		}
		return model.BuildStatusFail, err.Error()
	}
	return model.BuildStatusPass, ""
}

// syncMirror keeps a bare mirror per repo and fetches the job's patchset ref
// into it, authenticated with the node credential.
func (a *agent) syncMirror(ctx context.Context, job *runner.RemoteJob) (string, error) {
	muAny, _ := a.mirrorMu.LoadOrStore(job.Repo, &sync.Mutex{})
	mu := muAny.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	mirror := filepath.Join(a.cfg.DataDir, "mirrors", job.Repo+".git")
	env := append(os.Environ(), critgit.Env(a.cfg.DataDir)...)
	// Pass the credential through env-config, not argv, so it stays out of
	// the process list.
	env = append(env,
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http."+a.cfg.Server+"/.extraHeader",
		"GIT_CONFIG_VALUE_0=Authorization: Bearer "+a.cfg.Credential,
		"GIT_TERMINAL_PROMPT=0",
	)
	if _, err := os.Stat(mirror); err != nil {
		init := exec.CommandContext(ctx, "git", "init", "--bare", "--quiet", mirror)
		init.Env = env
		if b, err := init.CombinedOutput(); err != nil {
			return "", fmt.Errorf("%v: %s", err, b)
		}
	}
	url := a.cfg.Server + "/git/" + job.Repo + ".git"
	fetch := exec.CommandContext(ctx, "git", "--git-dir", mirror, "fetch", "--quiet", "--no-tags", "--force",
		url, "+"+job.Ref+":"+job.Ref)
	fetch.Env = env
	if b, err := fetch.CombinedOutput(); err != nil {
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(string(b)))
	}
	return mirror, nil
}
