package runner

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blaxstar/citric/internal/config"
	"github.com/blaxstar/citric/internal/model"
)

// newTestRunner builds a Runner with only the fields toolIsDefined reads.
func newTestRunner(t *testing.T, reposDir string) *Runner {
	t.Helper()
	return &Runner{cfg: &config.Config{
		Server: config.Server{
			ReposDir: reposDir,
			DataDir:  t.TempDir(),
		},
	}}
}

// initBareRepo creates <dir>/<name>.git with one commit and returns its SHA.
// critToml, when non-empty, is committed as .citric.toml.
func initBareRepo(t *testing.T, dir, name, critToml string) string {
	t.Helper()
	work := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(work, 0755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = work
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return string(out)
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(work, "README"), []byte("x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if critToml != "" {
		if err := os.WriteFile(filepath.Join(work, ".citric.toml"), []byte(critToml), 0644); err != nil {
			t.Fatal(err)
		}
	}
	run("add", "-A")
	run("commit", "-q", "-m", "init")
	sha := run("rev-parse", "HEAD")
	sha = sha[:40]

	bare := filepath.Join(dir, name+".git")
	if out, err := exec.Command("git", "clone", "-q", "--bare", work, bare).CombinedOutput(); err != nil {
		t.Fatalf("clone bare: %v: %s", err, out)
	}
	return sha
}

// A tool the repo never configured has nothing to run — waiving it is correct.
func TestToolIsDefined_AbsentConfigWaives(t *testing.T) {
	reposDir := t.TempDir()
	sha := initBareRepo(t, reposDir, "norepo", "") // no .citric.toml at all
	r := newTestRunner(t, reposDir)

	got := r.toolIsDefined(
		&model.CL{Repo: "norepo"},
		&model.Patchset{CommitSHA: sha},
		config.BuildTool{Name: "lint"},
	)
	if got {
		t.Fatal("tool with no .citric.toml should not be reported as defined")
	}
}

func TestToolIsDefined_ConfiguredToolIsDefined(t *testing.T) {
	reposDir := t.TempDir()
	sha := initBareRepo(t, reposDir, "cfgrepo", "[[tool]]\nname = \"lint\"\ncommand = \"echo hi\"\n")
	r := newTestRunner(t, reposDir)

	cl := &model.CL{Repo: "cfgrepo"}
	ps := &model.Patchset{CommitSHA: sha}

	if !r.toolIsDefined(cl, ps, config.BuildTool{Name: "lint"}) {
		t.Error("lint is configured in .citric.toml, want defined")
	}
	if r.toolIsDefined(cl, ps, config.BuildTool{Name: "test"}) {
		t.Error("test is absent from .citric.toml, want not defined")
	}
}

// The regression this guards: git reports a missing COMMIT with the same
// "does not exist in <sha>" message as a missing PATH. Reading that as "tool not
// configured" marks every tool pass without running it — a CL goes fully green
// having built nothing. Server-side faults must fail loud, never waive.
func TestToolIsDefined_OperationalFailuresDoNotWaive(t *testing.T) {
	reposDir := t.TempDir()
	initBareRepo(t, reposDir, "okrepo", "[[tool]]\nname = \"lint\"\ncommand = \"echo hi\"\n")

	cases := []struct {
		name string
		repo string
		sha  string
	}{
		{
			name: "commit missing from repo",
			repo: "okrepo",
			sha:  "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		},
		{
			name: "repo directory does not exist",
			repo: "nosuchrepo",
			sha:  "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newTestRunner(t, reposDir)
			got := r.toolIsDefined(
				&model.CL{Repo: tc.repo},
				&model.Patchset{CommitSHA: tc.sha},
				config.BuildTool{Name: "lint"},
			)
			if !got {
				t.Fatal("operational git failure must assume the tool IS defined " +
					"so runTool surfaces it; waiving here reports a false green build")
			}
		})
	}
}

// A command in the server registry short-circuits: no git access needed.
func TestToolIsDefined_ServerDefaultCommand(t *testing.T) {
	r := newTestRunner(t, t.TempDir())
	got := r.toolIsDefined(
		&model.CL{Repo: "nonexistent"},
		&model.Patchset{CommitSHA: "irrelevant"},
		config.BuildTool{Name: "lint", Command: "make lint"},
	)
	if !got {
		t.Fatal("server registry supplies a command, want defined without touching git")
	}
}

// buildEnv's git-config overrides (safe.directory=* via GIT_CONFIG_GLOBAL)
// must reach the tool command itself, not just the worktree-setup git calls
// that use a separately-built env. Without this, a build tool that shells
// out to git on its own — go build's VCS stamping is exactly such a call —
// hits "detected dubious ownership" the moment the worktree's owning UID
// differs from the service user's, surfacing as an opaque build failure
// with nothing pointing back at git at all ("error obtaining VCS status:
// exit status 128"). Regression test for exactly that gap.
func TestBuildEnv_IncludesGitSafeDirectoryConfig(t *testing.T) {
	r := newTestRunner(t, t.TempDir())
	env := r.buildEnv(
		&model.CL{Repo: "example"},
		&model.Patchset{CommitSHA: "deadbeef"},
		config.BuildTool{Name: "integration"},
		nil,
	)
	hasGlobal := false
	for _, kv := range env {
		if strings.HasPrefix(kv, "GIT_CONFIG_GLOBAL=") {
			hasGlobal = true
		}
	}
	if !hasGlobal {
		t.Fatal("buildEnv must set GIT_CONFIG_GLOBAL so a tool command's own " +
			"git invocations get the same safe.directory=* exemption the " +
			"worktree-setup commands already have")
	}
}
