package api

import (
	"context"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/blaxstar/citric/internal/model"
	"github.com/blaxstar/citric/internal/store"
)

// submitReq drives handleCLSubmit for one CL as the given user.
func submitReq(t *testing.T, s *Server, clID int64, user string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/cl/"+strconv.FormatInt(clID, 10)+"/submit", strings.NewReader("{}"))

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.FormatInt(clID, 10))
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, ctxUser, user)

	w := httptest.NewRecorder()
	s.handleCLSubmit(w, req.WithContext(ctx))
	return w
}

// The whole point of filing a chain: submit is a fast-forward, so merging the TOP
// of a stack would carry every commit beneath it onto the branch having reviewed
// only the top commit's diff. A CL whose base is not yet merged must be refused,
// and the refusal must name the CL that has to go first.
func TestHandleCLSubmit_RefusesCLSittingOnUnmergedCommit(t *testing.T) {
	s, db, stack, repoName := stackRepo(t)
	if err := db.CreateUser(&model.User{Username: "tester", Email: "t@e", Role: model.RoleAdmin}); err != nil {
		t.Fatal(err)
	}

	// File the stack exactly as a real push would.
	if out := postHook(s, repoName, stack[len(stack)-1]).Body.String(); strings.Contains(out, "error:") {
		t.Fatalf("hook error: %s", out)
	}

	cls, _ := db.ListCLs(store.CLFilter{Status: "open"})
	if len(cls) != 3 {
		t.Fatalf("want 3 CLs, got %d", len(cls))
	}

	// Find the CL for the TOP commit and try to submit it first.
	var topCL int64
	var bottomCL int64
	for _, cl := range cls {
		ps, _ := db.ListPatchsets(cl.ID)
		switch ps[0].CommitSHA {
		case stack[len(stack)-1]:
			topCL = cl.ID
		case stack[0]:
			bottomCL = cl.ID
		}
	}
	if topCL == 0 || bottomCL == 0 {
		t.Fatal("could not locate the top and bottom CLs of the stack")
	}

	w := submitReq(t, s, topCL, "tester")
	if w.Code != 409 {
		t.Fatalf("submitting the top of an unmerged stack must be refused with 409, got %d: %s",
			w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "submit that one first") {
		t.Errorf("refusal should tell the author what to do, got: %q", body)
	}
	// Naming the blocking CL is what makes the message actionable.
	if !strings.Contains(body, "CL-") {
		t.Errorf("refusal should name the blocking CL, got: %q", body)
	}

	// And the CL must still be open — a refused submit must not half-apply.
	cl, _ := db.GetCL(topCL)
	if cl.Status != model.CLStatusOpen {
		t.Errorf("refused submit changed status to %s", cl.Status)
	}
}

// The guard must not block the commit at the bottom of the stack: its base is
// the existing branch tip, so it is legitimately ready to fast-forward. It gets
// past the ancestry check and is stopped later by the normal review gate, not by
// a 409 about ordering.
func TestHandleCLSubmit_BottomOfStackPassesTheChainGuard(t *testing.T) {
	s, db, stack, repoName := stackRepo(t)
	if err := db.CreateUser(&model.User{Username: "tester", Email: "t@e", Role: model.RoleAdmin}); err != nil {
		t.Fatal(err)
	}
	if out := postHook(s, repoName, stack[len(stack)-1]).Body.String(); strings.Contains(out, "error:") {
		t.Fatalf("hook error: %s", out)
	}

	cls, _ := db.ListCLs(store.CLFilter{Status: "open"})
	var bottomCL int64
	for _, cl := range cls {
		ps, _ := db.ListPatchsets(cl.ID)
		if ps[0].CommitSHA == stack[0] {
			bottomCL = cl.ID
		}
	}

	w := submitReq(t, s, bottomCL, "tester")
	if w.Code == 409 {
		t.Fatalf("bottom of the stack sits on the branch tip and must not be refused "+
			"for ordering, got 409: %s", w.Body.String())
	}
}
