package api

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/blaxstar/citric/internal/model"
	"github.com/blaxstar/citric/internal/store"
)

// bottomCLWithSelfLGTM files a stack and returns the bottom CL (the only one
// submittable) after its author LGTMs it.
func bottomCLWithSelfLGTM(t *testing.T) (*Server, *store.DB, *model.CL, string) {
	t.Helper()
	s, db, stack, repoName := stackRepo(t)
	if err := db.CreateUser(&model.User{Username: "tester", Email: "t@e", Role: model.RoleAdmin}); err != nil {
		t.Fatal(err)
	}
	if out := postHook(s, repoName, stack[len(stack)-1]).Body.String(); strings.Contains(out, "error:") {
		t.Fatalf("hook error: %s", out)
	}
	cls, _ := db.ListCLs(store.CLFilter{Status: "open"})
	var bottom *model.CL
	for _, cl := range cls {
		ps, _ := db.ListPatchsets(cl.ID)
		if ps[0].CommitSHA == stack[0] {
			bottom = cl
		}
	}
	if bottom == nil {
		t.Fatal("bottom CL not found")
	}
	lgtm(t, db, bottom, bottom.Author)
	return s, db, bottom, filepath.Join(s.cfg.Server.ReposDir, repoName+".git")
}

func lgtm(t *testing.T, db *store.DB, cl *model.CL, who string) {
	t.Helper()
	ps, _ := db.LatestPatchset(cl.ID)
	if err := db.UpsertReview(&model.Review{CLID: cl.ID, PSID: ps.ID, Reviewer: who, Verdict: model.VerdictLGTM}); err != nil {
		t.Fatal(err)
	}
}

func readinessHas(t *testing.T, s *Server, cl *model.CL, reason string) bool {
	t.Helper()
	rd, err := s.clReadiness(cl, "tester")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rd.Reasons {
		if strings.Contains(r, reason) {
			return true
		}
	}
	return false
}

const otherLGTMReason = "someone other than the author"

// Off by default: the author's own LGTM is enough, as before.
func TestRequireOtherLGTM_OffByDefault(t *testing.T) {
	s, _, cl, _ := bottomCLWithSelfLGTM(t)
	if readinessHas(t, s, cl, otherLGTMReason) || readinessHas(t, s, cl, "no LGTM") {
		t.Fatal("a self-LGTM should count when the rule is off")
	}
	if w := submitReq(t, s, cl.ID, "tester"); w.Code != 200 {
		t.Fatalf("submit with a self-LGTM and the rule off: got %d: %s", w.Code, w.Body.String())
	}
}

func TestRequireOtherLGTM_ServerWide(t *testing.T) {
	s, db, cl, _ := bottomCLWithSelfLGTM(t)
	if err := db.SetSetting(settingRequireOtherLGTM, "true"); err != nil {
		t.Fatal(err)
	}
	if !readinessHas(t, s, cl, otherLGTMReason) {
		t.Error("readiness should say the CL needs another reviewer")
	}
	if w := submitReq(t, s, cl.ID, "tester"); w.Code != 403 || !strings.Contains(w.Body.String(), otherLGTMReason) {
		t.Fatalf("submit with only a self-LGTM: want 403 naming the rule, got %d: %s", w.Code, w.Body.String())
	}

	lgtm(t, db, cl, "someone-else")
	if readinessHas(t, s, cl, otherLGTMReason) {
		t.Error("another reviewer's LGTM should clear the blocker")
	}
	if w := submitReq(t, s, cl.ID, "tester"); w.Code != 200 {
		t.Fatalf("submit with another reviewer's LGTM: got %d: %s", w.Code, w.Body.String())
	}
}

func TestRequireOtherLGTM_PerRepo(t *testing.T) {
	s, _, cl, repoPath := bottomCLWithSelfLGTM(t)
	if s.requiresOtherLGTM(repoPath) {
		t.Fatal("repo flag should be off by default")
	}
	if out, err := s.gitC(repoPath, "config", "--bool", gitConfigRequireOtherLGTM, "true").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if !s.requiresOtherLGTM(repoPath) {
		t.Fatal("repo flag not picked up")
	}
	if w := submitReq(t, s, cl.ID, "tester"); w.Code != 403 {
		t.Fatalf("submit with only a self-LGTM in a repo that requires another reviewer: got %d: %s", w.Code, w.Body.String())
	}
}
