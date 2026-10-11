package runner

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/blaxstar/citric/internal/brand"
	"github.com/blaxstar/citric/internal/config"
	critgit "github.com/blaxstar/citric/internal/git"
	"github.com/blaxstar/citric/internal/model"
	"github.com/blaxstar/citric/internal/store"
)

// The node network (docs/nodes.md). While no build node is online, builds run
// on the server exactly as they always have. Once one is, every ready tool
// goes through a central queue that two kinds of executor claim from: the
// server's own worker slots ("node zero") and remote nodes, which long-poll
// for work. An executor only takes a job whose runs_on labels it has.

const (
	// nodeOnlineWindow is how recently a node must have checked in to count as
	// online for dispatch.
	nodeOnlineWindow = 90 * time.Second
	// nodeLostAfter is how long a node may go without reporting on a job it
	// holds before the job fails as [node lost]. Nodes report at least every
	// few seconds while a job runs.
	nodeLostAfter = 60 * time.Second
	// remoteGrace is how far past its timeout a remote job may run before the
	// server gives up on it (the node enforces the timeout itself).
	remoteGrace = 2 * time.Minute
)

// RemoteJob is what a node receives when it claims work: everything needed to
// run one tool for one patchset, already resolved against the repo config.
type RemoteJob struct {
	BuildID        int64             `json:"build_id"`
	Repo           string            `json:"repo"`
	Ref            string            `json:"ref"`
	CommitSHA      string            `json:"commit_sha"`
	Tool           string            `json:"tool"`
	Command        string            `json:"command"`
	Shell          string            `json:"shell,omitempty"`
	TimeoutSeconds int64             `json:"timeout_seconds"`
	ContextEnv     []string          `json:"context_env"`
	Env            map[string]string `json:"env,omitempty"`
}

type job struct {
	remote RemoteJob
	cl     *model.CL
	ps     *model.Patchset
	tool   config.BuildTool
	runsOn []string

	node      string // who claimed it; "" while pending
	cancelled bool
	lastSeen  time.Time
	deadline  time.Time
	logFile   *os.File
	lw        *lineWriter
	done      chan model.BuildStatus // buffered 1
}

type dispatcher struct {
	mu      sync.Mutex
	pending []*job
	running map[int64]*job
	wake    chan struct{} // closed and replaced whenever pending changes

	onlineMu    sync.Mutex
	onlineAt    time.Time
	onlineCache bool
}

func newDispatcher() *dispatcher {
	return &dispatcher{running: map[int64]*job{}, wake: make(chan struct{})}
}

// changed wakes every executor waiting for work. Caller holds d.mu.
func (d *dispatcher) changed() {
	close(d.wake)
	d.wake = make(chan struct{})
}

// LabelsMatch reports whether an executor offering have satisfies runsOn.
func LabelsMatch(runsOn, have []string) bool {
	set := make(map[string]bool, len(have))
	for _, l := range have {
		set[strings.ToLower(l)] = true
	}
	for _, want := range runsOn {
		if !set[strings.ToLower(want)] {
			return false
		}
	}
	return true
}

// claim takes the oldest pending job the executor can run, waiting up to
// wait for one to appear (wait < 0: forever, until stop is closed).
func (d *dispatcher) claim(labels []string, executor string, wait time.Duration, stop <-chan struct{}) *job {
	var timeout <-chan time.Time
	if wait >= 0 {
		t := time.NewTimer(wait)
		defer t.Stop()
		timeout = t.C
	}
	for {
		d.mu.Lock()
		for i, j := range d.pending {
			if LabelsMatch(j.runsOn, labels) {
				d.pending = append(d.pending[:i], d.pending[i+1:]...)
				j.node = executor
				j.lastSeen = time.Now()
				d.running[j.remote.BuildID] = j
				d.mu.Unlock()
				return j
			}
		}
		wake := d.wake
		d.mu.Unlock()
		select {
		case <-wake:
		case <-timeout:
			return nil
		case <-stop:
			return nil
		}
	}
}

// nodesOnline reports whether any active node has checked in recently. The
// answer is cached for a few seconds: it's asked once per tool launch.
func (r *Runner) nodesOnline() bool {
	d := r.disp
	d.onlineMu.Lock()
	defer d.onlineMu.Unlock()
	if time.Since(d.onlineAt) < 5*time.Second {
		return d.onlineCache
	}
	d.onlineAt = time.Now()
	d.onlineCache = false
	if r.db == nil {
		return false
	}
	nodes, err := r.db.ListNodes()
	if err != nil {
		return false
	}
	for _, n := range nodes {
		if n.Status == store.NodeActive && n.LastSeen != nil && time.Since(*n.LastSeen) < nodeOnlineWindow {
			d.onlineCache = true
			break
		}
	}
	return d.onlineCache
}

// localLabels are the runs_on labels the server itself offers.
func (r *Runner) localLabels() []string {
	return append([]string{runtime.GOOS, runtime.GOARCH}, r.cfg.Server.LocalLabels...)
}

// useDispatch decides whether a tool goes through the node queue rather than
// straight onto the server's own slots.
func (r *Runner) useDispatch(runsOn []string) bool {
	return !r.cfg.Server.RunsLocalBuilds() || !LabelsMatch(runsOn, r.localLabels()) || r.nodesOnline()
}

// readRepoConfig reads the repo config (.citric.toml, else .crit.toml) from
// the pushed commit. A commit with neither yields an empty config.
func (r *Runner) readRepoConfig(cl *model.CL, ps *model.Patchset) (*config.RepoBuildConfig, error) {
	repoDir := filepath.Join(r.cfg.Server.ReposDir, cl.Repo+".git")
	env := append(os.Environ(), "GIT_DIR="+repoDir)
	env = append(env, critgit.Env(r.cfg.Server.DataDir)...)
	for _, name := range brand.RepoConfigFiles() {
		show := exec.Command("git", "show", ps.CommitSHA+":"+name)
		show.Env = env
		out, err := show.Output()
		if err == nil {
			return config.ParseRepoBuildConfig(out)
		}
		var ee *exec.ExitError
		if !errors.As(err, &ee) || !strings.Contains(string(ee.Stderr), "does not exist in") {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
	}
	return &config.RepoBuildConfig{}, nil
}

// runsOnFor is the tool's executor requirement: the repo's runs_on for the
// tool, else the server's.
func (r *Runner) runsOnFor(cl *model.CL, ps *model.Patchset, tool config.BuildTool) []string {
	if cfg, err := r.readRepoConfig(cl, ps); err == nil {
		if rt, ok := cfg.ToolMap()[tool.Name]; ok && len(rt.RunsOn) > 0 {
			return rt.RunsOn
		}
	}
	return tool.RunsOn
}

// dispatchAndWait queues a tool for whichever executor claims it first and
// blocks until it finishes.
func (r *Runner) dispatchAndWait(cl *model.CL, ps *model.Patchset, buildID int64, tool config.BuildTool, runsOn []string) model.BuildStatus {
	logPath := filepath.Join(r.logsDir, fmt.Sprintf("%d.log", buildID))
	j := &job{cl: cl, ps: ps, tool: tool, runsOn: runsOn, done: make(chan model.BuildStatus, 1)}
	j.remote = RemoteJob{
		BuildID:    buildID,
		Repo:       cl.Repo,
		Ref:        critgit.PatchsetRef(cl.ID, ps.Number),
		CommitSHA:  ps.CommitSHA,
		Tool:       tool.Name,
		ContextEnv: ContextEnv(cl, ps),
	}

	// Resolve the command now, so a node needs nothing but the job.
	command, shell, timeout := tool.Command, tool.Shell, 10*time.Minute
	if d, err := time.ParseDuration(tool.Timeout); err == nil {
		timeout = d
	}
	env := map[string]string{}
	for k, v := range tool.Env {
		env[k] = v
	}
	repoCfg, err := r.readRepoConfig(cl, ps)
	if err != nil {
		return r.failWith(buildID, logPath, fmt.Sprintf("[error] %v\n", err))
	}
	if rt, ok := repoCfg.ToolMap()[tool.Name]; ok {
		if rt.Command != "" {
			command = rt.Command
		}
		if rt.Shell != "" {
			shell = rt.Shell
		}
		if d, err := time.ParseDuration(rt.Timeout); err == nil && rt.Timeout != "" {
			timeout = d
		}
		for k, v := range rt.Env {
			env[k] = v
		}
	}
	j.remote.Command, j.remote.Shell, j.remote.Env = command, shell, env
	j.remote.TimeoutSeconds = int64(timeout / time.Second)

	msg := "[queued] waiting for a build executor"
	if len(runsOn) > 0 {
		msg += fmt.Sprintf(" with runs_on = %v", runsOn)
	}
	if err := os.WriteFile(logPath, []byte(msg+"\n"), 0o644); err == nil {
		r.db.SetBuildLogPath(buildID, logPath)
	}
	r.stream.Broadcast(buildID, msg+"\n")

	r.disp.mu.Lock()
	r.disp.pending = append(r.disp.pending, j)
	r.disp.changed()
	r.disp.mu.Unlock()
	return <-j.done
}

// localWorker is one of the server's own executor slots in dispatch mode.
func (r *Runner) localWorker(stop <-chan struct{}) {
	for {
		j := r.disp.claim(r.localLabels(), "", -1, stop)
		if j == nil {
			return
		}
		if !r.cfg.Server.QueueMode {
			r.sem <- struct{}{}
		}
		status := r.runTool(j.cl, j.ps, j.remote.BuildID, j.tool)
		if !r.cfg.Server.QueueMode {
			<-r.sem
		}
		r.disp.mu.Lock()
		delete(r.disp.running, j.remote.BuildID)
		r.disp.mu.Unlock()
		j.done <- status
	}
}

// ClaimRemote hands node a job it can run, waiting up to wait for one. It
// returns nil when nothing turned up.
func (r *Runner) ClaimRemote(node string, labels []string, wait time.Duration, stop <-chan struct{}) *RemoteJob {
	j := r.disp.claim(labels, node, wait, stop)
	if j == nil {
		return nil
	}
	id := j.remote.BuildID
	logPath := filepath.Join(r.logsDir, fmt.Sprintf("%d.log", id))
	f, err := os.Create(logPath)
	if err != nil {
		r.finishRemote(j, model.BuildStatusFail, fmt.Sprintf("open log: %v", err))
		return nil
	}
	j.logFile = f
	j.lw = &lineWriter{buildID: id, w: f, b: r.stream}
	j.deadline = time.Now().Add(time.Duration(j.remote.TimeoutSeconds)*time.Second + remoteGrace)
	r.db.UpdateBuildStatus(id, model.BuildStatusRunning)
	r.db.SetBuildLogPath(id, logPath)
	r.db.SetBuildNode(id, node)
	fmt.Fprintf(j.lw, "[node: %s]\n[timeout: %s]\n", node, time.Duration(j.remote.TimeoutSeconds)*time.Second)
	r.publish(j.cl.ID)
	rj := j.remote
	return &rj
}

// ErrNotYourJob is returned when a node reports on a job it doesn't hold.
var ErrNotYourJob = errors.New("this node does not hold that build")

func (r *Runner) heldJob(node string, buildID int64) (*job, error) {
	r.disp.mu.Lock()
	defer r.disp.mu.Unlock()
	j, ok := r.disp.running[buildID]
	if !ok || j.node != node || node == "" {
		return nil, ErrNotYourJob
	}
	j.lastSeen = time.Now()
	return j, nil
}

// RemoteLog appends output a node streamed for a job it holds and reports
// whether the job has been cancelled. An empty chunk is a keep-alive.
func (r *Runner) RemoteLog(node string, buildID int64, chunk []byte) (cancel bool, err error) {
	j, err := r.heldJob(node, buildID)
	if err != nil {
		return false, err
	}
	if len(chunk) > 0 {
		j.lw.Write(chunk)
	}
	r.disp.mu.Lock()
	defer r.disp.mu.Unlock()
	return j.cancelled, nil
}

// RemoteDone records a node's final result for a job it holds.
func (r *Runner) RemoteDone(node string, buildID int64, status model.BuildStatus, errMsg string) error {
	j, err := r.heldJob(node, buildID)
	if err != nil {
		return err
	}
	if status != model.BuildStatusPass {
		status = model.BuildStatusFail
	}
	r.finishRemote(j, status, errMsg)
	return nil
}

// finishRemote closes out a remote job exactly once.
func (r *Runner) finishRemote(j *job, status model.BuildStatus, errMsg string) {
	r.disp.mu.Lock()
	if _, ok := r.disp.running[j.remote.BuildID]; !ok {
		r.disp.mu.Unlock()
		return
	}
	delete(r.disp.running, j.remote.BuildID)
	r.disp.mu.Unlock()

	id := j.remote.BuildID
	if j.lw != nil {
		if errMsg != "" {
			fmt.Fprintf(j.lw, "[error] %s\n", errMsg)
		}
		j.lw.flush()
	}
	if j.logFile != nil {
		j.logFile.Close()
	}
	r.db.UpdateBuildStatus(id, status)
	r.stream.Close(id)
	r.publish(j.cl.ID)
	j.done <- status
}

// cancelQueued handles CancelBuild for a build in the node queue: pending
// jobs end now; a remote one is flagged and ends when its node next reports.
func (r *Runner) cancelQueued(buildID int64) bool {
	r.disp.mu.Lock()
	for i, j := range r.disp.pending {
		if j.remote.BuildID == buildID {
			r.disp.pending = append(r.disp.pending[:i], r.disp.pending[i+1:]...)
			r.disp.mu.Unlock()
			logPath := filepath.Join(r.logsDir, fmt.Sprintf("%d.log", buildID))
			f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
			if err == nil {
				fmt.Fprintln(f, "[cancelled]")
				f.Close()
			}
			r.db.UpdateBuildStatus(buildID, model.BuildStatusFail)
			r.stream.Broadcast(buildID, "[cancelled]\n")
			r.stream.Close(buildID)
			r.publish(j.cl.ID)
			j.done <- model.BuildStatusFail
			return true
		}
	}
	if j, ok := r.disp.running[buildID]; ok && j.node != "" {
		j.cancelled = true
		r.disp.mu.Unlock()
		return true
	}
	r.disp.mu.Unlock()
	return false
}

// reapRemote fails remote jobs whose node went quiet or ran far past the
// timeout, so a lost node never leaves a build stuck "running".
func (r *Runner) reapRemote(stop <-chan struct{}) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		var lost, late []*job
		r.disp.mu.Lock()
		for _, j := range r.disp.running {
			if j.node == "" {
				continue // the server's own slot: its context enforces timeouts
			}
			switch {
			case time.Since(j.lastSeen) > nodeLostAfter:
				lost = append(lost, j)
			case !j.deadline.IsZero() && time.Now().After(j.deadline):
				late = append(late, j)
			}
		}
		r.disp.mu.Unlock()
		for _, j := range lost {
			log.Printf("[nodes] %s lost build %d", j.node, j.remote.BuildID)
			r.finishRemote(j, model.BuildStatusFail, fmt.Sprintf("[node lost] %s stopped reporting", j.node))
		}
		for _, j := range late {
			r.finishRemote(j, model.BuildStatusFail, fmt.Sprintf("[timeout] %s did not finish in time", j.node))
		}
	}
}
