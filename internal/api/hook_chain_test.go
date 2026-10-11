package api

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blaxstar/citric/internal/config"
	"github.com/blaxstar/citric/internal/store"
)

// stackRepo builds a repo whose main is one commit behind a three-commit stack,
// mirroring a developer who committed three times before pushing. Returns the
// server, db, the stack SHAs oldest-first, and the repo name.
func stackRepo(t *testing.T) (*Server, *store.DB, []string, string) {
	t.Helper()
	dir := t.TempDir()
	reposDir := filepath.Join(dir, "repos")
	repoName := "demo"
	repoPath := filepath.Join(reposDir, repoName+".git")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}

	git := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoPath
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@e")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return strings.TrimSpace(string(out))
	}
	commit := func(name string) string {
		if err := os.WriteFile(filepath.Join(repoPath, name+".txt"), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
		git("add", "-A")
		git("commit", "-q", "-m", "commit "+name+"\n\nChange-Id: I"+strings.Repeat(name, 40)[:40])
		return git("rev-parse", "HEAD")
	}

	git("init", "-q", "-b", "main")
	commit("a") // this one lands on main
	git("checkout", "-q", "-b", "work")
	var stack []string
	for _, n := range []string{"b", "c", "d"} {
		stack = append(stack, commit(n))
	}

	db, err := store.Open(filepath.Join(dir, "citric.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	cfg := &config.Config{}
	cfg.Server.ReposDir = reposDir
	cfg.Server.DataDir = filepath.Join(filepath.Dir(reposDir), "data")
	return &Server{cfg: cfg, db: db}, db, stack, repoName
}

// The regression this exists for: git sends one post-receive line for a push of
// any size, so reading only the tip filed ONE CL and left the rest of the stack
// with no CL, no build and no reviewer — while still being ancestors of the tip,
// so submitting that one CL fast-forwarded the branch over all of them.
func TestHandleHook_MultiCommitPushFilesOneCLPerCommit(t *testing.T) {
	s, db, stack, repoName := stackRepo(t)

	out := postHook(s, repoName, stack[len(stack)-1]).Body.String()
	if strings.Contains(out, "error:") {
		t.Fatalf("hook reported an error: %q", out)
	}

	cls, err := db.ListCLs(store.CLFilter{Status: "open"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cls) != 3 {
		t.Fatalf("push carried 3 commits, want 3 CLs, got %d\nhook said: %s", len(cls), out)
	}

	// Each CL must cover exactly its own commit, and sit on the one before it —
	// that chain is what the submit guard relies on to enforce merge order.
	byCommit := map[string]int64{}
	baseOf := map[string]string{}
	for _, cl := range cls {
		ps, err := db.ListPatchsets(cl.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(ps) != 1 {
			t.Fatalf("CL-%d: want 1 patchset, got %d", cl.ID, len(ps))
		}
		byCommit[ps[0].CommitSHA] = cl.ID
		baseOf[ps[0].CommitSHA] = ps[0].BaseSHA
	}

	for i, sha := range stack {
		if _, ok := byCommit[sha]; !ok {
			t.Errorf("commit %s got no CL of its own", sha[:8])
			continue
		}
		if i == 0 {
			continue // its base is the pre-existing main tip
		}
		if got, want := baseOf[sha], stack[i-1]; got != want {
			t.Errorf("CL for %s has base %s, want the previous commit %s",
				sha[:8], got[:8], want[:8])
		}
	}
}

// A single-commit push must stay exactly as it was — one CL, no chain noise.
func TestHandleHook_SingleCommitPushUnchanged(t *testing.T) {
	s, db, stack, repoName := stackRepo(t)

	// Push only the first commit of the stack.
	out := postHook(s, repoName, stack[0]).Body.String()
	if !strings.Contains(out, "ok: CL-") {
		t.Fatalf("want a filed CL, got: %q", out)
	}
	if strings.Contains(out, "filing one CL per commit") {
		t.Errorf("single-commit push should not announce a chain: %q", out)
	}
	cls, _ := db.ListCLs(store.CLFilter{Status: "open"})
	if len(cls) != 1 {
		t.Fatalf("want 1 CL for a 1-commit push, got %d", len(cls))
	}
}
