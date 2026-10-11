package git

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// submitRepo builds:
//
//	A ── B ── C        (main at B; C is ahead of main)
//	 ╲
//	  ╲── X            (side branch off A — neither ancestor nor descendant of B)
//
// Returns the repo and a name→SHA map.
func submitRepo(t *testing.T) (*Repo, map[string]string) {
	t.Helper()
	dir := t.TempDir()
	shas := map[string]string{}

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
	commit := func(name string) {
		if err := os.WriteFile(filepath.Join(dir, name+".txt"), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
		git("add", "-A")
		git("commit", "-q", "-m", "commit "+name)
		shas[name] = git("rev-parse", "HEAD")
	}

	git("init", "-q", "-b", "main")
	commit("A")
	commit("B")
	git("checkout", "-q", "-b", "ahead")
	commit("C")
	git("checkout", "-q", "-b", "side", shas["A"])
	commit("X")
	git("checkout", "-q", "main")

	repo, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return repo, shas
}

func mainTip(t *testing.T, r *Repo) string {
	t.Helper()
	sha, err := r.ResolveRef("refs/heads/main")
	if err != nil {
		t.Fatalf("resolve main: %v", err)
	}
	return sha
}

// The normal case: a CL whose commit is ahead of the branch moves it forward.
func TestFastForward_AdvancesBranch(t *testing.T) {
	repo, shas := submitRepo(t)

	got, err := repo.Submit("main", shas["C"], MergeStrategyFF)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if got != shas["C"] {
		t.Errorf("merged sha = %s, want %s", got, shas["C"])
	}
	if tip := mainTip(t, repo); tip != shas["C"] {
		t.Errorf("main = %s, want %s", tip, shas["C"])
	}
}

// Regression for the CL-80 data loss (2026-08-01): CL-80's tip merged, then
// CL-81 — which was CL-80's own *parent* commit — merged 14 seconds later and
// rewound main, dropping CL-80's commit while it stayed marked merged.
// Submitting a commit that is behind the tip must be refused outright.
func TestFastForward_RefusesRewind(t *testing.T) {
	repo, shas := submitRepo(t)

	// main is at B; A is B's parent — the CL-81-after-CL-80 shape.
	_, err := repo.Submit("main", shas["A"], MergeStrategyFF)
	if err == nil {
		t.Fatal("submitting an ancestor succeeded; expected refusal")
	}
	if !errors.Is(err, ErrNotFastForward) {
		t.Errorf("error = %v, want ErrNotFastForward", err)
	}
	if tip := mainTip(t, repo); tip != shas["B"] {
		t.Errorf("main moved to %s, want it left at %s", tip, shas["B"])
	}
}

// Two CLs cut from the same base and submitted in sequence: the second one's
// base is still on the branch, so the caller's stacking guard passes it. Only
// the ancestry check stops it from clobbering the first.
func TestFastForward_RefusesDivergent(t *testing.T) {
	repo, shas := submitRepo(t)

	_, err := repo.Submit("main", shas["X"], MergeStrategyFF)
	if err == nil {
		t.Fatal("submitting a divergent commit succeeded; expected refusal")
	}
	if !errors.Is(err, ErrNotFastForward) {
		t.Errorf("error = %v, want ErrNotFastForward", err)
	}
	if tip := mainTip(t, repo); tip != shas["B"] {
		t.Errorf("main moved to %s, want it left at %s", tip, shas["B"])
	}
}

// Re-submitting the commit the branch already points at is a no-op, not an error.
func TestFastForward_SameCommitIsNoOp(t *testing.T) {
	repo, shas := submitRepo(t)

	got, err := repo.Submit("main", shas["B"], MergeStrategyFF)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if got != shas["B"] {
		t.Errorf("merged sha = %s, want %s", got, shas["B"])
	}
	if tip := mainTip(t, repo); tip != shas["B"] {
		t.Errorf("main = %s, want %s", tip, shas["B"])
	}
}
