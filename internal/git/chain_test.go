package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// chainRepo builds a repo with commits A,B on main and C,D on a feature branch
// that main does not contain. Returns the repo and a name→SHA map.
func chainRepo(t *testing.T) (*Repo, map[string]string) {
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
	git("checkout", "-q", "-b", "feature")
	commit("C")
	commit("D")

	repo, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return repo, shas
}

// The core of multi-commit push: a tip whose branch is two commits behind must
// expand to BOTH new commits, oldest first, so each CL's base is the previous
// CL's tip. Returning only the tip is what silently hid N-1 commits from review.
func TestNewCommits_ExpandsChainOldestFirst(t *testing.T) {
	repo, shas := chainRepo(t)

	got, err := repo.NewCommits(shas["D"], "main", 50)
	if err != nil {
		t.Fatalf("NewCommits: %v", err)
	}
	want := []string{shas["C"], shas["D"]}
	if len(got) != len(want) {
		t.Fatalf("got %d commits, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("position %d: got %s, want %s", i, ShortSHA(got[i]), ShortSHA(want[i]))
		}
	}
}

// Commits already on the branch are history, not part of the push.
func TestNewCommits_ExcludesCommitsAlreadyOnBranch(t *testing.T) {
	repo, shas := chainRepo(t)

	got, err := repo.NewCommits(shas["B"], "main", 50)
	if err != nil {
		t.Fatalf("NewCommits: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("tip is already on main; want 0 new commits, got %d", len(got))
	}
}

// An unborn target branch means everything is new — the first push to a fresh repo.
func TestNewCommits_UnbornBranchTakesWholeHistory(t *testing.T) {
	repo, shas := chainRepo(t)

	got, err := repo.NewCommits(shas["D"], "does-not-exist", 50)
	if err != nil {
		t.Fatalf("NewCommits: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("want all 4 commits for an unborn branch, got %d", len(got))
	}
	if got[0] != shas["A"] {
		t.Errorf("want oldest first (A), got %s", ShortSHA(got[0]))
	}
}

// The cap exists so a mistaken push of a long branch cannot open hundreds of CLs.
func TestNewCommits_RefusesOverLimit(t *testing.T) {
	repo, shas := chainRepo(t)

	_, err := repo.NewCommits(shas["D"], "main", 1)
	if err == nil {
		t.Fatal("want an error when the chain exceeds the limit")
	}
	if !strings.Contains(err.Error(), "citric import") {
		t.Errorf("error should point at the alternative, got: %v", err)
	}
}

// A merge has no single base to diff against and cannot be a CL; say so clearly
// rather than filing something unreviewable.
func TestNewCommits_RefusesMergeCommit(t *testing.T) {
	repo, shas := chainRepo(t)
	dir := repo.path

	git := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@e")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	// Diverge main, then merge it into feature so the tip is a merge commit.
	git("checkout", "-q", "main")
	if err := os.WriteFile(filepath.Join(dir, "E.txt"), []byte("E"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-q", "-m", "commit E")
	git("checkout", "-q", "feature")
	git("merge", "-q", "--no-ff", "-m", "merge main", "main")

	tip := func() string {
		cmd := exec.Command("git", "rev-parse", "HEAD")
		cmd.Dir = dir
		out, _ := cmd.Output()
		return strings.TrimSpace(string(out))
	}()
	_ = shas

	_, err := repo.NewCommits(tip, "main", 50)
	if err == nil {
		t.Fatal("want an error for a merge commit in the pushed range")
	}
	if !strings.Contains(err.Error(), "merge") || !strings.Contains(err.Error(), "Rebase") {
		t.Errorf("error should name the problem and the fix, got: %v", err)
	}
}

// IsOnBranch backs the submit guard: only a CL whose base is already merged may
// fast-forward, otherwise the commits beneath it ride along unreviewed.
func TestIsOnBranch(t *testing.T) {
	repo, shas := chainRepo(t)

	for _, tc := range []struct {
		name string
		sha  string
		want bool
	}{
		{"branch tip counts as contained", shas["B"], true},
		{"older commit on branch", shas["A"], true},
		{"commit only on feature", shas["C"], false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := repo.IsOnBranch(tc.sha, "main")
			if err != nil {
				t.Fatalf("IsOnBranch: %v", err)
			}
			if got != tc.want {
				t.Errorf("IsOnBranch(%s, main) = %v, want %v", ShortSHA(tc.sha), got, tc.want)
			}
		})
	}
}
