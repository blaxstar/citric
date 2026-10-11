package api

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blaxstar/citric/internal/config"
	"github.com/blaxstar/citric/internal/model"
)

// needsRebaseRepo creates a repo at <reposDir>/<name>.git with two commits on
// main, returning their SHAs oldest first — the same layout repoPath expects
// for a real repo on disk.
func needsRebaseRepo(t *testing.T, reposDir, name string) (older, newer string) {
	t.Helper()
	dir := filepath.Join(reposDir, name+".git")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@e")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return strings.TrimSpace(string(out))
	}
	commit := func(msg string) string {
		if err := os.WriteFile(filepath.Join(dir, msg+".txt"), []byte(msg), 0o644); err != nil {
			t.Fatal(err)
		}
		git("add", "-A")
		git("commit", "-q", "-m", msg)
		return git("rev-parse", "HEAD")
	}
	git("init", "-q", "-b", "main")
	older = commit("first")
	newer = commit("second")
	return older, newer
}

// The core case: two open CLs on the same repo/branch, one based on the
// current tip, one based on the commit before it. Only the stale one should
// be flagged — this is exactly the state CL-287/CL-288 were silently in.
func TestMarkNeedsRebase_FlagsStaleBase(t *testing.T) {
	reposDir := t.TempDir()
	older, newer := needsRebaseRepo(t, reposDir, "ANAMI")

	cfg := &config.Config{}
	cfg.Server.ReposDir = reposDir
	s := &Server{cfg: cfg}

	cls := []*model.CL{
		{ID: 1, Repo: "ANAMI", TargetBranch: "main", Status: model.CLStatusOpen, LatestBaseSHA: older},
		{ID: 2, Repo: "ANAMI", TargetBranch: "main", Status: model.CLStatusOpen, LatestBaseSHA: newer},
	}
	s.markNeedsRebase(cls)

	if !cls[0].NeedsRebase {
		t.Error("CL based on the older commit should be flagged needs-rebase")
	}
	if cls[1].NeedsRebase {
		t.Error("CL based on the current tip should NOT be flagged")
	}
}

// Merged and abandoned CLs are done — a stale base there is just history,
// not something to nag about.
func TestMarkNeedsRebase_IgnoresMergedAndAbandoned(t *testing.T) {
	reposDir := t.TempDir()
	older, _ := needsRebaseRepo(t, reposDir, "ANAMI")

	cfg := &config.Config{}
	cfg.Server.ReposDir = reposDir
	s := &Server{cfg: cfg}

	cls := []*model.CL{
		{ID: 1, Repo: "ANAMI", TargetBranch: "main", Status: model.CLStatusMerged, LatestBaseSHA: older},
		{ID: 2, Repo: "ANAMI", TargetBranch: "main", Status: model.CLStatusAbandoned, LatestBaseSHA: older},
	}
	s.markNeedsRebase(cls)

	if cls[0].NeedsRebase || cls[1].NeedsRebase {
		t.Error("only open CLs should ever be flagged")
	}
}

// A repo/branch that can't be resolved (typo, deleted branch, whatever) must
// not be flagged — an unresolvable tip is "we don't know", not "it's stale".
func TestMarkNeedsRebase_UnknownRepoDoesNotFalsePositive(t *testing.T) {
	reposDir := t.TempDir()
	cfg := &config.Config{}
	cfg.Server.ReposDir = reposDir
	s := &Server{cfg: cfg}

	cls := []*model.CL{
		{ID: 1, Repo: "does-not-exist", TargetBranch: "main", Status: model.CLStatusOpen, LatestBaseSHA: "deadbeef"},
	}
	s.markNeedsRebase(cls)

	if cls[0].NeedsRebase {
		t.Error("an unresolvable repo/branch should not be flagged — no signal, not a false positive")
	}
}

// Same repo+branch shared by several CLs must resolve the tip once, not once
// per CL — the whole point of caching by (repo,branch) in markNeedsRebase.
func TestMarkNeedsRebase_SharesOneTipResolutionPerRepoBranch(t *testing.T) {
	reposDir := t.TempDir()
	older, newer := needsRebaseRepo(t, reposDir, "ANAMI")

	cfg := &config.Config{}
	cfg.Server.ReposDir = reposDir
	s := &Server{cfg: cfg}

	cls := []*model.CL{
		{ID: 1, Repo: "ANAMI", TargetBranch: "main", Status: model.CLStatusOpen, LatestBaseSHA: older},
		{ID: 2, Repo: "ANAMI", TargetBranch: "main", Status: model.CLStatusOpen, LatestBaseSHA: older},
		{ID: 3, Repo: "ANAMI", TargetBranch: "main", Status: model.CLStatusOpen, LatestBaseSHA: newer},
	}
	s.markNeedsRebase(cls)

	if !cls[0].NeedsRebase || !cls[1].NeedsRebase {
		t.Error("both CLs sharing the stale base should be flagged")
	}
	if cls[2].NeedsRebase {
		t.Error("the CL already on the tip should not be flagged")
	}
}
