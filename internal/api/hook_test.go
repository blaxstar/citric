package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/blaxstar/citric/internal/config"
	critgit "github.com/blaxstar/citric/internal/git"
	"github.com/blaxstar/citric/internal/model"
	"github.com/blaxstar/citric/internal/store"
)

// makeCommit creates a real git repo at repoPath with a single root commit whose
// message carries the given Change-Id footer, and returns the commit SHA.
func makeCommit(t *testing.T, repoPath, changeID string) string {
	t.Helper()
	repo, err := gogit.PlainInit(repoPath, false)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, "f.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if _, err := wt.Add("f.txt"); err != nil {
		t.Fatalf("add: %v", err)
	}
	h, err := wt.Commit("test change\n\nChange-Id: "+changeID+"\n", &gogit.CommitOptions{
		Author: &object.Signature{Name: "Tester", Email: "t@example.com", When: time.Now()},
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	return h.String()
}

// setupHook builds a Server backed by a temp DB + git repo, with one CL at the
// given status keyed to the pushed commit's Change-Id. Returns everything needed
// to drive handleHook.
func setupHook(t *testing.T, status model.CLStatus) (s *Server, db *store.DB, clID int64, sha, repoName string) {
	t.Helper()
	dir := t.TempDir()
	reposDir := filepath.Join(dir, "repos")
	repoName = "demo"
	repoPath := filepath.Join(reposDir, repoName+".git")
	if err := os.MkdirAll(reposDir, 0o755); err != nil {
		t.Fatalf("mkdir repos: %v", err)
	}

	const changeID = "Iabc123def4567890abc123def4567890abc123d"
	sha = makeCommit(t, repoPath, changeID)

	db, err := store.Open(filepath.Join(dir, "citric.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	clID, err = db.CreateCL(&model.CL{
		ChangeID: changeID, Repo: repoName, TargetBranch: "main",
		Author: "tester", Title: "demo",
	})
	if err != nil {
		t.Fatalf("create cl: %v", err)
	}
	if status != model.CLStatusOpen {
		if err := db.UpdateCLStatus(clID, status); err != nil {
			t.Fatalf("set status %s: %v", status, err)
		}
	}

	cfg := &config.Config{}
	cfg.Server.ReposDir = reposDir
	cfg.Server.DataDir = filepath.Join(filepath.Dir(reposDir), "data")
	s = &Server{cfg: cfg, db: db}
	return
}

func postHook(s *Server, repoName, sha string) *httptest.ResponseRecorder {
	token, err := critgit.HookToken(s.cfg.Server.DataDir)
	if err != nil {
		panic(err)
	}
	return postHookWithToken(s, repoName, sha, token)
}

func postHookWithToken(s *Server, repoName, sha, token string) *httptest.ResponseRecorder {
	body := strings.NewReader("0000000000000000000000000000000000000000 " + sha + " refs/for/main\n")
	req := httptest.NewRequest("POST", "/internal/hook/post-receive?repo="+repoName, body)
	if token != "" {
		req.Header.Set("X-Citric-Hook-Token", token)
	}
	w := httptest.NewRecorder()
	s.handleHook(w, req)
	return w
}

// A push whose Change-Id matches an already-merged CL must be rejected and must
// NOT create a new patchset. Regression guard for the CL-26 incident.
func TestHandleHook_RejectsPatchsetOnMergedCL(t *testing.T) {
	s, db, clID, sha, repoName := setupHook(t, model.CLStatusMerged)

	out := postHook(s, repoName, sha).Body.String()
	if !strings.Contains(out, "already merged") {
		t.Fatalf("expected rejection mentioning 'already merged', got: %q", out)
	}

	ps, err := db.ListPatchsets(clID)
	if err != nil {
		t.Fatalf("list patchsets: %v", err)
	}
	if len(ps) != 0 {
		t.Fatalf("expected no patchset on merged CL, got %d", len(ps))
	}
}

// Abandoned CLs are likewise closed to new patchsets.
func TestHandleHook_RejectsPatchsetOnAbandonedCL(t *testing.T) {
	s, db, clID, sha, repoName := setupHook(t, model.CLStatusAbandoned)

	out := postHook(s, repoName, sha).Body.String()
	if !strings.Contains(out, "already abandoned") {
		t.Fatalf("expected rejection mentioning 'already abandoned', got: %q", out)
	}
	if ps, _ := db.ListPatchsets(clID); len(ps) != 0 {
		t.Fatalf("expected no patchset on abandoned CL, got %d", len(ps))
	}
}

// An open CL still accepts a new patchset — the guard must not break the happy path.
func TestHandleHook_AcceptsPatchsetOnOpenCL(t *testing.T) {
	s, db, clID, sha, repoName := setupHook(t, model.CLStatusOpen)

	out := postHook(s, repoName, sha).Body.String()
	if !strings.Contains(out, "ok: CL-") {
		t.Fatalf("expected ok for open CL, got: %q", out)
	}
	ps, err := db.ListPatchsets(clID)
	if err != nil {
		t.Fatalf("list patchsets: %v", err)
	}
	if len(ps) != 1 {
		t.Fatalf("expected 1 patchset on open CL, got %d", len(ps))
	}
}

// The hook endpoint is reachable by anything that can reach the server; only
// the installed post-receive hook knows the token. Without it, an outsider
// could open CLs as any user and start builds.
func TestHandleHook_RejectsMissingOrWrongToken(t *testing.T) {
	s, db, clID, sha, repoName := setupHook(t, model.CLStatusOpen)
	for _, token := range []string{"", "not-the-token"} {
		w := postHookWithToken(s, repoName, sha, token)
		if w.Code != http.StatusForbidden {
			t.Errorf("token %q: got %d, want 403", token, w.Code)
		}
	}
	if ps, _ := db.ListPatchsets(clID); len(ps) != 0 {
		t.Errorf("a rejected hook call must not add patchsets, got %d", len(ps))
	}
}

func TestIsGitPush(t *testing.T) {
	cases := []struct {
		sub, service string
		want         bool
	}{
		{"/info/refs", "git-receive-pack", true},
		{"/git-receive-pack", "", true},
		{"/info/refs", "git-upload-pack", false},
		{"/git-upload-pack", "", false},
	}
	for _, c := range cases {
		if got := isGitPush(c.sub, c.service); got != c.want {
			t.Errorf("isGitPush(%q, %q) = %v, want %v", c.sub, c.service, got, c.want)
		}
	}
}
