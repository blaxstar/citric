package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/blaxstar/citric/internal/brand"
	"github.com/blaxstar/citric/internal/config"
	critgit "github.com/blaxstar/citric/internal/git"
	"github.com/blaxstar/citric/internal/model"
	"github.com/blaxstar/citric/internal/proc"
	"github.com/blaxstar/citric/internal/store"
)

type Runner struct {
	db       *store.DB
	cfg      *config.Config
	stream   *Broadcaster
	events   *EventHub
	logsDir  string
	sem      chan struct{} // limits concurrent running builds
	queueSem chan struct{} // size 1; acquired for a whole CL's build set when Server.QueueMode is on
	cancels  sync.Map      // buildID int64 → context.CancelFunc
	disp     *dispatcher   // node queue, see dispatch.go
	stop     chan struct{} // closed by Close

	// Shared, persistent build caches — see critcache.md. Unlike the per-build
	// worktree, these live under DataDir and are never removed, so they survive
	// across builds AND across CLs. Every concurrent tool invocation points at
	// the same directories: cargo's registry cache and sccache are both
	// designed for concurrent access (advisory locking / content-hash keys),
	// so sharing them doesn't reintroduce the contention this is meant to fix.
	cargoHome   string
	sccacheDir  string
	sccachePath string // absolute path to sccache on Server.BuildPath, or "" if not found

	// Go's equivalents. Without these, GOCACHE/GOMODCACHE derive from HOME,
	// which buildEnv pins to /tmp — tmpfs here, so every build re-downloaded
	// every module and recompiled the whole stdlib after each reboot.
	goCache    string
	goModCache string

	// Gradle's equivalent (Android/Kotlin repos, e.g. cxrcut_mobile). Without
	// this, GRADLE_USER_HOME derives from HOME (/tmp), so every build
	// re-downloads Gradle's own distribution plus the full dependency graph.
	// Gradle's caches under GRADLE_USER_HOME use file-based locking and are
	// documented as safe for concurrent builds to share, same as cargo's.
	gradleHome string
}

// New returns the server's runner, with its local executor slots and the
// remote-job reaper running in the background.
func New(db *store.DB, cfg *config.Config) *Runner {
	r := newRunner(db, cfg)
	if cfg.Server.RunsLocalBuilds() {
		for i := 0; i < cfg.Server.MaxConcurrent; i++ {
			go r.localWorker(r.stop)
		}
	}
	go r.reapRemote(r.stop)
	return r
}

// NewExecutor returns a runner used only for its build environment
// (ExecEnv) and caches, e.g. on a build node; nothing runs in the background.
func NewExecutor(cfg *config.Config) *Runner {
	return newRunner(nil, cfg)
}

func newRunner(db *store.DB, cfg *config.Config) *Runner {
	// Mirror config.Load's default: New is also called directly (tests build a
	// Config as a struct literal and never go through Load). Without this,
	// filepath.Join("", "cargo") is the *relative* path "cargo" and every run
	// scatters cache dirs into whatever the working directory happens to be.
	// If there is no sensible location at all, leave the caches unset rather
	// than guessing — buildEnv then omits them and each build falls back to the
	// toolchain's own default.
	cacheDir := cfg.Server.CacheDir
	if cacheDir == "" && cfg.Server.DataDir != "" {
		cacheDir = filepath.Join(cfg.Server.DataDir, "cache")
	}
	var cargoHome, sccacheDir, goCache, goModCache, gradleHome string
	if cacheDir != "" {
		cargoHome = filepath.Join(cacheDir, "cargo")
		sccacheDir = filepath.Join(cacheDir, "sccache")
		goCache = filepath.Join(cacheDir, "go-build")
		goModCache = filepath.Join(cacheDir, "gomod")
		gradleHome = filepath.Join(cacheDir, "gradle")
		for _, d := range []string{cargoHome, sccacheDir, goCache, goModCache, gradleHome} {
			if err := os.MkdirAll(d, 0755); err != nil {
				log.Printf("[runner] create shared cache dir %s: %v (builds will fall back to an uncached, per-worktree default)", d, err)
			}
		}
	}

	sccachePath, _ := findOnPath(cfg.Server.BuildPath, "sccache")
	if sccachePath == "" {
		log.Printf("[runner] sccache not found on build_path — Rust builds will use a shared CARGO_HOME but no compiler cache")
	}

	r := &Runner{
		disp:        newDispatcher(),
		stop:        make(chan struct{}),
		db:          db,
		cfg:         cfg,
		stream:      NewBroadcaster(),
		events:      NewEventHub(),
		logsDir:     filepath.Join(cfg.Server.DataDir, "logs"),
		sem:         make(chan struct{}, cfg.Server.MaxConcurrent),
		queueSem:    make(chan struct{}, 1),
		cargoHome:   cargoHome,
		sccacheDir:  sccacheDir,
		sccachePath: sccachePath,
		goCache:     goCache,
		goModCache:  goModCache,
		gradleHome:  gradleHome,
	}
	return r
}

// Close stops the runner's background workers.
func (r *Runner) Close() {
	select {
	case <-r.stop:
	default:
		close(r.stop)
	}
}

// findOnPath searches the ':'-separated pathList for an executable file named
// name. It mirrors exec.LookPath but against an arbitrary PATH string instead
// of the current process's environment, since build commands run with
// Server.BuildPath rather than the citric server's own PATH.
func findOnPath(pathList, name string) (string, bool) {
	for _, dir := range filepath.SplitList(pathList) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, name)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		if info.Mode()&0111 != 0 {
			return candidate, true
		}
	}
	return "", false
}

// Events returns the per-CL change hub for SSE subscribers.
func (r *Runner) Events() *EventHub { return r.events }

func (r *Runner) publish(clID int64) { r.events.Publish(clID) }

// entry holds per-tool state during a Run invocation.
type entry struct {
	id   int64
	tool config.BuildTool
	done chan struct{}
	pass bool // written before done is closed
}

// Run spawns build tools for the given patchset. It returns as soon as the
// tools are queued — it does not wait for them to finish.
// filter restricts which tools run; if empty all configured tools run.
// Tools listed in depends_on wait for those tools to pass before starting.
// Concurrent execution is bounded by Server.MaxConcurrent server-wide, and
// additionally by Server.MaxConcurrentPerCL among this CL's own tools — unless
// Server.QueueMode is on, in which case MaxConcurrent is ignored entirely and
// this CL's tools don't start until every tool from whichever CL is currently
// running has finished (MaxConcurrentPerCL still applies to the active CL).
func (r *Runner) Run(cl *model.CL, ps *model.Patchset, filter []string) error {
	if err := os.MkdirAll(r.logsDir, 0755); err != nil {
		return fmt.Errorf("create logs dir: %w", err)
	}

	// Determine which tools to run.
	tools := r.selectTools(filter)
	if len(tools) == 0 {
		return nil
	}

	entries := make(map[string]*entry, len(tools))
	for _, tool := range tools {
		buildID, err := r.db.CreateBuild(&model.Build{
			CLID:     cl.ID,
			PSID:     ps.ID,
			ToolName: tool.Name,
		})
		if err != nil {
			return fmt.Errorf("create build record for %s: %w", tool.Name, err)
		}
		t := tool
		entries[tool.Name] = &entry{id: buildID, tool: t, done: make(chan struct{})}
	}

	r.publish(cl.ID) // new build rows are visible (queued)

	// Scoped to this Run() call — every tool spawned below shares it, so it
	// naturally caps concurrency across this one patchset's own tools without
	// any cross-call bookkeeping or cleanup once they've all finished.
	clSem := make(chan struct{}, r.cfg.Server.MaxConcurrentPerCL)

	go r.runEntries(cl, ps, entries, clSem)
	return nil
}

// runEntries launches every tool in entries and, when Server.QueueMode is on,
// blocks first until it's this CL's turn — so two different CLs' tools never
// run at once, no matter what MaxConcurrent says. Runs in its own goroutine
// (from Run) so Run itself keeps returning immediately either way; the queue
// wait, however long, never blocks the caller (an HTTP handler, the
// post-receive hook) that asked for the build.
func (r *Runner) runEntries(cl *model.CL, ps *model.Patchset, entries map[string]*entry, clSem chan struct{}) {
	if r.cfg.Server.QueueMode {
		r.queueSem <- struct{}{}
		defer func() { <-r.queueSem }()
	}

	for _, e := range entries {
		e := e
		go func() {
			status := r.runWithDeps(cl, ps, e, entries, clSem)
			e.pass = (status == model.BuildStatusPass)
			close(e.done)
		}()
	}

	// Hold the queue slot until every tool this CL launched has finished —
	// a no-op wait when queue mode is off, since nothing is blocked on it.
	for _, e := range entries {
		<-e.done
	}
}

// selectTools returns the server-configured tools matching filter (all if empty).
func (r *Runner) selectTools(filter []string) []config.BuildTool {
	if len(filter) == 0 {
		return r.cfg.BuildTools
	}
	want := make(map[string]bool, len(filter))
	for _, name := range filter {
		want[name] = true
	}
	out := make([]config.BuildTool, 0, len(filter))
	for _, t := range r.cfg.BuildTools {
		if want[t.Name] {
			out = append(out, t)
		}
	}
	return out
}

// runWithDeps waits for declared dependencies then runs the tool.
func (r *Runner) runWithDeps(cl *model.CL, ps *model.Patchset, e *entry, all map[string]*entry, clSem chan struct{}) model.BuildStatus {
	logPath := filepath.Join(r.logsDir, fmt.Sprintf("%d.log", e.id))

	// Waive BEFORE waiting on dependencies. A tool the repo never configured has
	// nothing to run and nothing to block on — if we checked deps first, a failed
	// dependency would mark it failed and runTool's waive path would never be
	// reached, so a tool absent from .citric.toml would show up red on the CL.
	if !r.toolIsDefined(cl, ps, e.tool) {
		msg := fmt.Sprintf("[skipped] no command defined for %q in .citric.toml or server config\n", e.tool.Name)
		// Only record the log path if the log is actually there — otherwise the
		// build row points at a file the UI can't open.
		if err := os.WriteFile(logPath, []byte(msg), 0644); err != nil {
			log.Printf("[runner] build %d: write waive log %s: %v", e.id, logPath, err)
		} else if err := r.db.SetBuildLogPath(e.id, logPath); err != nil {
			log.Printf("[runner] build %d: set log path: %v", e.id, err)
		}
		if err := r.db.UpdateBuildStatus(e.id, model.BuildStatusPass); err != nil {
			log.Printf("[runner] build %d: mark waived tool pass: %v", e.id, err)
		}
		r.stream.Close(e.id)
		r.publish(cl.ID)
		return model.BuildStatusPass
	}

	for _, dep := range e.tool.DependsOn {
		depEntry, ok := all[dep]
		if !ok {
			continue // dep not selected in this run — treat as satisfied
		}
		<-depEntry.done
		if !depEntry.pass {
			s := r.failWith(e.id, logPath, fmt.Sprintf("[skip] dependency %q failed or was cancelled\n", dep))
			r.publish(cl.ID)
			return s
		}
	}

	// Acquire the narrower per-CL slot before the global one, so a tool
	// waiting on its own CL's cap never sits holding a scarce global slot
	// hostage from every other CL in the meantime.
	clSem <- struct{}{}
	defer func() { <-clSem }()

	// With build nodes online (or when this tool needs labels the server
	// doesn't have, or local builds are off) the tool goes through the node
	// queue instead of straight onto this server's slots.
	if runsOn := r.runsOnFor(cl, ps, e.tool); r.useDispatch(runsOn) {
		status := r.dispatchAndWait(cl, ps, e.id, e.tool, runsOn)
		r.publish(cl.ID)
		return status
	}

	// In queue mode, runEntries already guarantees this is the only CL
	// building anything right now, so MaxConcurrent's global slot would add
	// nothing but is skipped explicitly rather than trusted to work out —
	// a misconfigured MaxConcurrentPerCL > MaxConcurrent shouldn't be able to
	// silently reintroduce the cross-CL contention queue mode exists to kill.
	if !r.cfg.Server.QueueMode {
		r.sem <- struct{}{}
		defer func() { <-r.sem }()
	}

	status := r.runTool(cl, ps, e.id, e.tool)
	r.publish(cl.ID) // final status (pass/fail/skipped)
	return status
}

// toolIsDefined reports whether tool has a command to run for this commit —
// either from the repo's .citric.toml or as a default in the server registry.
// Reads .citric.toml straight out of the bare repo so no worktree is needed.
//
// On any error reading or parsing the config we return true (assume defined) and
// let runTool surface the real failure with its full output, rather than silently
// waiving a tool that was meant to run.
func (r *Runner) toolIsDefined(cl *model.CL, ps *model.Patchset, tool config.BuildTool) bool {
	if tool.Command != "" {
		return true // server registry supplies a default command
	}

	repoDir := filepath.Join(r.cfg.Server.ReposDir, cl.Repo+".git")
	gitEnv := append(os.Environ(), "GIT_DIR="+repoDir)
	gitEnv = append(gitEnv, critgit.Env(r.cfg.Server.DataDir)...)

	// .citric.toml wins; .crit.toml is the pre-rename name, still honoured.
	var out []byte
	found := false
	for _, name := range brand.RepoConfigFiles() {
		show := exec.Command("git", "show", ps.CommitSHA+":"+name)
		show.Env = gitEnv
		b, err := show.Output()
		if err == nil {
			out, found = b, true
			break
		}
		// Waiving is the dangerous answer: it marks the tool pass without running
		// it, so a server-side fault would report every CL fully green having
		// built nothing. Only a genuinely absent repo config earns a false.
		//
		// "does not exist in <sha>" alone is NOT enough to conclude that — git
		// emits the identical message when the commit itself is missing from the
		// repo. Confirm the commit is present before trusting it.
		var ee *exec.ExitError
		if errors.As(err, &ee) && bytes.Contains(ee.Stderr, []byte("does not exist in")) {
			verify := exec.Command("git", "cat-file", "-e", ps.CommitSHA+"^{commit}")
			verify.Env = gitEnv
			if verify.Run() == nil {
				continue // commit is here, this file genuinely is not
			}
		}
		log.Printf("[runner] %s: reading %s at %s failed, assuming %q is defined: %v",
			cl.Repo, name, ps.CommitSHA, tool.Name, err)
		return true
	}
	if !found {
		return false
	}

	repoCfg, err := config.ParseRepoBuildConfig(out)
	if err != nil {
		return true // malformed repo config — let runTool report it properly
	}
	rt, ok := repoCfg.ToolMap()[tool.Name]
	return ok && rt.Command != ""
}

func (r *Runner) runTool(cl *model.CL, ps *model.Patchset, buildID int64, tool config.BuildTool) model.BuildStatus {
	logPath := filepath.Join(r.logsDir, fmt.Sprintf("%d.log", buildID))
	// Worktrees live under data_dir (service-owned, in ReadWritePaths) rather
	// than a shared /tmp path, which can be pre-owned by another user and
	// reject subdir creation under our service user.
	wtDir := filepath.Join(r.cfg.Server.DataDir, "run", strconv.FormatInt(buildID, 10))

	r.db.UpdateBuildStatus(buildID, model.BuildStatusRunning)
	r.db.SetBuildLogPath(buildID, logPath)
	r.publish(cl.ID) // tool started running

	timeout := 10 * time.Minute
	if d, err := time.ParseDuration(tool.Timeout); err == nil {
		timeout = d
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	r.cancels.Store(buildID, cancel)
	defer func() {
		cancel()
		r.cancels.Delete(buildID)
	}()

	repoDir := filepath.Join(r.cfg.Server.ReposDir, cl.Repo+".git")

	// Base env for all worktree git commands:
	//   GIT_DIR    — name the bare repo explicitly (2.38+ safe.bareRepository).
	//   git.Env(…) — safe.directory=* so we can operate on repos owned by
	//                another user without a "dubious ownership" abort.
	gitEnv := append(os.Environ(), "GIT_DIR="+repoDir)
	gitEnv = append(gitEnv, critgit.Env(r.cfg.Server.DataDir)...)

	// Ensure the worktree parent dir exists — git worktree add won't create it.
	// 0775 so both the service user and the dev group retain write access.
	if err := os.MkdirAll(filepath.Dir(wtDir), 0775); err != nil {
		return r.failWith(buildID, logPath, fmt.Sprintf("[error] create worktree parent: %v\n", err))
	}

	// Prune stale worktree registrations from previous failed runs.
	prune := exec.CommandContext(ctx, "git", "worktree", "prune")
	prune.Env = gitEnv
	prune.Run()

	setupCmd := exec.CommandContext(ctx, "git", "worktree", "add", wtDir, ps.CommitSHA)
	setupCmd.Env = gitEnv
	if out, err := setupCmd.CombinedOutput(); err != nil {
		return r.failWith(buildID, logPath, fmt.Sprintf("[error] worktree setup: %v\n%s", err, out))
	}
	defer func() {
		rm := exec.Command("git", "worktree", "remove", "--force", wtDir)
		rm.Env = gitEnv
		rm.Run()
	}()

	// Load repo .citric.toml once — used for command, timeout, and env overrides.
	var repoEnv map[string]string
	command := tool.Command
	shell := tool.Shell
	repoCfg, _ := config.LoadRepoBuildConfig(brand.RepoConfigPath(wtDir))
	if repoCfg != nil {
		if rt, ok := repoCfg.ToolMap()[tool.Name]; ok {
			if rt.Command != "" {
				command = rt.Command
			}
			if rt.Shell != "" {
				shell = rt.Shell
			}
			if rt.Timeout != "" {
				if d, err := time.ParseDuration(rt.Timeout); err == nil {
					timeout = d
					cancel()
					ctx, cancel = context.WithTimeout(context.Background(), d)
					r.cancels.Store(buildID, cancel)
				}
			}
			if len(rt.Env) > 0 {
				repoEnv = rt.Env
			}
		}
	}

	if command == "" {
		msg := fmt.Sprintf("[skipped] no command defined for %q in .citric.toml or server config\n", tool.Name)
		os.WriteFile(logPath, []byte(msg), 0644)
		r.db.UpdateBuildStatus(buildID, model.BuildStatusPass)
		r.stream.Close(buildID)
		return model.BuildStatusPass
	}

	logFile, err := os.Create(logPath)
	if err != nil {
		return r.failWith(buildID, logPath, fmt.Sprintf("[error] open log: %v\n", err))
	}
	defer logFile.Close()

	lw := &lineWriter{buildID: buildID, w: logFile, b: r.stream}
	fmt.Fprintf(lw, "[timeout: %s]\n", timeout)

	cmd, err := proc.Command(ctx, shell, command)
	if err != nil {
		fmt.Fprintf(lw, "[error] %v\n", err)
		lw.flush()
		r.db.UpdateBuildStatus(buildID, model.BuildStatusFail)
		r.stream.Close(buildID)
		return model.BuildStatusFail
	}
	cmd.Dir = wtDir
	cmd.Env = r.buildEnv(cl, ps, tool, repoEnv)
	cmd.Stdout = lw
	cmd.Stderr = lw

	status := model.BuildStatusPass
	if err := proc.Run(cmd); err != nil {
		status = model.BuildStatusFail
		switch ctx.Err() {
		case context.DeadlineExceeded:
			fmt.Fprintln(lw, "[timeout]")
		case context.Canceled:
			fmt.Fprintln(lw, "[cancelled]")
		default:
			// Not our own context that ended the process — e.g. OOM-killed under
			// concurrent build contention. Without this, a signal-killed build
			// leaves a log with no explanation at all (see critcache.md).
			fmt.Fprintf(lw, "[error] %v\n", err)
		}
	}

	lw.flush()

	r.db.UpdateBuildStatus(buildID, status)
	r.stream.Close(buildID)
	return status
}

// failWith writes a message to the log, marks the build failed, and closes the stream.
func (r *Runner) failWith(buildID int64, logPath, msg string) model.BuildStatus {
	os.WriteFile(logPath, []byte(msg), 0644)
	r.db.UpdateBuildStatus(buildID, model.BuildStatusFail)
	r.stream.Close(buildID)
	return model.BuildStatusFail
}

// CancelBuild cancels a running build by its ID.
func (r *Runner) CancelBuild(buildID int64) bool {
	if v, ok := r.cancels.Load(buildID); ok {
		v.(context.CancelFunc)()
		return true
	}
	return r.cancelQueued(buildID)
}

// Stream returns the broadcaster so API handlers can subscribe to live build output.
func (r *Runner) Stream() *Broadcaster {
	return r.stream
}

// buildEnv returns a minimal, clean environment for a build command.
// It does NOT inherit os.Environ() — only an explicit allowlist of variables
// is passed, so secrets or sensitive process state can't leak to build code.
// Server config env, then repo env (from .citric.toml [tool.env]) are applied
// last so per-tool and per-repo overrides take precedence.
//
// CARGO_HOME, GOCACHE/GOMODCACHE, and GRADLE_USER_HOME (and, when sccache is
// available, RUSTC_WRAPPER/SCCACHE_DIR/CARGO_INCREMENTAL) point at persistent
// directories under Server.DataDir shared by every build — see critcache.md.
// The per-build worktree (and its target/ dir) is still torn down after each
// run; deliberately NOT sharing CARGO_TARGET_DIR too, since cargo serializes
// concurrent writers to one target dir via its own lock, which would
// parallelize worse than leaving target/ per-worktree and letting sccache
// absorb the redundant compilation instead.
func (r *Runner) buildEnv(cl *model.CL, ps *model.Patchset, tool config.BuildTool, repoEnv map[string]string) []string {
	return r.ExecEnv(ContextEnv(cl, ps), tool.Env, repoEnv)
}

// ContextEnv is the review context exposed to build commands, as CITRIC_* and
// the legacy CRIT_* names.
func ContextEnv(cl *model.CL, ps *model.Patchset) []string {
	var env []string
	for _, kv := range [][2]string{
		{"CL_ID", strconv.FormatInt(cl.ID, 10)},
		{"PS_NUMBER", strconv.Itoa(ps.Number)},
		{"COMMIT_SHA", ps.CommitSHA},
		{"REPO", cl.Repo},
		{"TARGET_BRANCH", cl.TargetBranch},
	} {
		env = append(env, brand.BuildEnv(kv[0], kv[1])...)
	}
	return env
}

// ExecEnv returns the clean environment a build command runs with on this
// runner (the server, or a node): PATH, the shared caches and git settings,
// then contextEnv, then each of envs in order so later ones override.
func (r *Runner) ExecEnv(contextEnv []string, envs ...map[string]string) []string {
	cfg := r.cfg
	env := []string{
		"PATH=" + cfg.Server.BuildPath,
		"NO_COLOR=1",
		"CI=true",
	}
	env = append(env, platformBuildEnv()...)
	env = append(env, contextEnv...)
	for _, kv := range [][2]string{
		{"CARGO_HOME", r.cargoHome},
		{"GOCACHE", r.goCache},
		{"GOMODCACHE", r.goModCache},
		{"GRADLE_USER_HOME", r.gradleHome},
	} {
		if kv[1] != "" {
			env = append(env, kv[0]+"="+kv[1])
		}
	}
	if r.sccachePath != "" && r.sccacheDir != "" {
		env = append(env,
			"RUSTC_WRAPPER="+r.sccachePath,
			"SCCACHE_DIR="+r.sccacheDir,
			// sccache refuses to cache anything built with incremental
			// compilation. Every build gets a fresh worktree and therefore a
			// fresh target/, so there is no prior incremental state to reuse
			// here anyway — incremental only costs us the cache.
			"CARGO_INCREMENTAL=0",
		)
	}
	// The worktree these commands run in was created via gitEnv's
	// safe.directory=* exemption (git worktree add), but that exemption lives
	// in an env var, not repo state — it does not follow the worktree to
	// whatever git invocations the tool command itself makes. `go build`'s
	// VCS stamping is exactly such a call: without this it aborts with
	// "detected dubious ownership" the moment the worktree's owning UID
	// differs from the service user's — "error obtaining VCS status: exit
	// status 128", indistinguishable at a glance from a real build failure.
	env = append(env, critgit.Env(cfg.Server.DataDir)...)
	for _, m := range envs {
		for k, v := range m {
			env = append(env, k+"="+v)
		}
	}
	return env
}
