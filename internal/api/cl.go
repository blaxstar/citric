package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/blaxstar/citric/internal/brand"
	"github.com/blaxstar/citric/internal/gate"
	gitpkg "github.com/blaxstar/citric/internal/git"
	"github.com/blaxstar/citric/internal/model"
	"github.com/blaxstar/citric/internal/owners"
	"github.com/blaxstar/citric/internal/store"
)

func (s *Server) handleCLCreate(w http.ResponseWriter, r *http.Request) {
	// External CL creation — the hook path is preferred for normal pushes.
	// This endpoint exists for tooling that needs to open a CL without a git push.
	var body struct {
		Repo         string `json:"repo"`
		TargetBranch string `json:"target_branch"`
		CommitSHA    string `json:"commit_sha"`
		BaseSHA      string `json:"base_sha"`
		ChangeID     string `json:"change_id"`
		Title        string `json:"title"`
		Description  string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil ||
		body.Repo == "" || body.TargetBranch == "" || body.CommitSHA == "" || body.ChangeID == "" {
		http.Error(w, "bad request: repo, target_branch, commit_sha, change_id required", http.StatusBadRequest)
		return
	}

	clID, err := s.db.CreateCL(&model.CL{
		ChangeID:     body.ChangeID,
		Repo:         body.Repo,
		TargetBranch: body.TargetBranch,
		Author:       s.currentUser(r),
		Title:        body.Title,
		Description:  body.Description,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	repoPath := filepath.Join(s.cfg.Server.ReposDir, body.Repo+".git")
	repo, err := gitpkg.Open(repoPath)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	psRef := gitpkg.PatchsetRef(clID, 1)
	if err := repo.CreateRef(psRef, body.CommitSHA); err != nil {
		http.Error(w, "create ref: "+err.Error(), http.StatusInternalServerError)
		return
	}

	baseSHA := body.BaseSHA
	if baseSHA == "" {
		baseSHA = body.CommitSHA
	}
	psID, err := s.db.CreatePatchset(&model.Patchset{
		CLID:      clID,
		Number:    1,
		CommitSHA: body.CommitSHA,
		BaseSHA:   baseSHA,
		Ref:       psRef,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	cl, _ := s.db.GetCL(clID)
	if s.runner != nil && cl != nil {
		s.runner.Run(cl, &model.Patchset{
			ID: psID, CLID: clID, Number: 1,
			CommitSHA: body.CommitSHA, BaseSHA: baseSHA, Ref: psRef,
		}, nil)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]int64{"id": clID})
}

func (s *Server) handleCLList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.CLFilter{
		Status:   q.Get("status"),
		Author:   q.Get("author"),
		Reviewer: q.Get("reviewer"),
	}
	if q.Get("mine") == "1" {
		f.Author = s.currentUser(r)
	}
	if q.Get("assigned") == "1" {
		f.Reviewer = s.currentUser(r)
	}
	cls, err := s.db.ListCLs(f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Hide CLs in repos the caller can't view (memoize the decision per repo).
	username := s.currentUser(r)
	isAdmin := s.userIsAdmin(username)
	seen := map[string]bool{}
	visible := cls[:0]
	for _, cl := range cls {
		ok, cached := seen[cl.Repo]
		if !cached {
			ok = s.canView(username, isAdmin, cl.Repo)
			seen[cl.Repo] = ok
		}
		if ok {
			visible = append(visible, cl)
		}
	}

	s.markNeedsRebase(visible)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(visible)
}

// markNeedsRebase sets NeedsRebase on every open CL whose LatestBaseSHA is no
// longer the current tip of its target branch — same rot that let CL-287 and
// CL-288 (deploy/install.sh, deploy/publish-macos.sh) sit stacked on an
// already-superseded patchset with nothing to say so until a rebase or a
// build surfaced it as a conflict. One branch-tip resolution per distinct
// repo+branch, not per CL, since a repo's CLs overwhelmingly target the same
// branch.
func (s *Server) markNeedsRebase(cls []*model.CL) {
	type key struct{ repo, branch string }
	tips := map[key]string{}
	for _, cl := range cls {
		if cl.Status != model.CLStatusOpen || cl.LatestBaseSHA == "" {
			continue
		}
		k := key{cl.Repo, cl.TargetBranch}
		tip, cached := tips[k]
		if !cached {
			tip = s.branchTip(cl.Repo, cl.TargetBranch)
			tips[k] = tip
		}
		if tip != "" && tip != cl.LatestBaseSHA {
			cl.NeedsRebase = true
		}
	}
}

// branchTip resolves a repo's branch to its current commit SHA, or "" if the
// repo/branch/ref can't be resolved — callers treat that as "don't know,
// don't flag" rather than a false positive.
func (s *Server) branchTip(repo, branch string) string {
	path, ok := s.repoPath(repo)
	if !ok {
		return ""
	}
	r, err := gitpkg.Open(path)
	if err != nil {
		return ""
	}
	sha, err := r.ResolveRef("refs/heads/" + branch)
	if err != nil {
		return ""
	}
	return sha
}

func (s *Server) handleCLGet(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	cl, err := s.db.GetCL(id)
	if err != nil || cl == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cl)
}

func (s *Server) handleCLPatchsets(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	ps, err := s.db.ListPatchsets(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ps)
}

func (s *Server) handleCLDiff(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	s.serveDiff(w, r, id, 0)
}

func (s *Server) handleCLDiffPS(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	psNum, _ := strconv.Atoi(chi.URLParam(r, "ps"))
	s.serveDiff(w, r, id, psNum)
}

func (s *Server) serveDiff(w http.ResponseWriter, r *http.Request, clID int64, psNumber int) {
	cl, err := s.db.GetCL(clID)
	if err != nil || cl == nil {
		http.Error(w, "cl not found", http.StatusNotFound)
		return
	}

	var ps *model.Patchset
	if psNumber == 0 {
		ps, err = s.db.LatestPatchset(clID)
	} else {
		all, err2 := s.db.ListPatchsets(clID)
		if err2 != nil {
			http.Error(w, err2.Error(), http.StatusInternalServerError)
			return
		}
		for _, p := range all {
			if p.Number == psNumber {
				ps = p
				break
			}
		}
	}
	if err != nil || ps == nil {
		http.Error(w, "patchset not found", http.StatusNotFound)
		return
	}

	repoPath := filepath.Join(s.cfg.Server.ReposDir, cl.Repo+".git")
	repo, err := gitpkg.Open(repoPath)
	if err != nil {
		http.Error(w, "repo unavailable", http.StatusInternalServerError)
		return
	}

	diffs, err := repo.Diff(ps.BaseSHA, ps.CommitSHA)
	if err != nil {
		http.Error(w, "diff failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if strings.Contains(r.Header.Get("Accept"), "text/plain") {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		for _, d := range diffs {
			fmt.Fprintln(w, d.Unified)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(diffs)
}

// ── patchset summary ────────────────────────────────────────────────────────

type patchsetFileSummary struct {
	Path             string `json:"path"`
	OldPath          string `json:"old_path,omitempty"`
	Additions        int    `json:"additions"`
	Deletions        int    `json:"deletions"`
	Status           string `json:"status"` // added | deleted | renamed | modified
	ChangedSincePrev bool   `json:"changed_since_prev"`
}

type patchsetSummaryResp struct {
	Patchset       int                   `json:"patchset"`
	PrevPatchset   int                   `json:"prev_patchset"` // 0 if this is the first
	CommitSHA      string                `json:"commit_sha"`
	Files          []patchsetFileSummary `json:"files"`
	TotalAdditions int                   `json:"total_additions"`
	TotalDeletions int                   `json:"total_deletions"`
}

// handlePatchsetSummary returns a compact per-file summary for a patchset:
// add/del counts, change status, and whether each file differs from the
// previous patchset. GET /cl/{id}/patchsets/{ps}/summary
func (s *Server) handlePatchsetSummary(w http.ResponseWriter, r *http.Request) {
	clID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	psNum, _ := strconv.Atoi(chi.URLParam(r, "ps"))

	cl, err := s.db.GetCL(clID)
	if err != nil || cl == nil {
		http.Error(w, "cl not found", http.StatusNotFound)
		return
	}
	all, err := s.db.ListPatchsets(clID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var ps, prev *model.Patchset
	for _, p := range all {
		switch p.Number {
		case psNum:
			ps = p
		case psNum - 1:
			prev = p
		}
	}
	if ps == nil {
		http.Error(w, "patchset not found", http.StatusNotFound)
		return
	}

	repoPath := filepath.Join(s.cfg.Server.ReposDir, cl.Repo+".git")
	repo, err := gitpkg.Open(repoPath)
	if err != nil {
		http.Error(w, "repo unavailable", http.StatusInternalServerError)
		return
	}

	diffs, err := repo.Diff(ps.BaseSHA, ps.CommitSHA)
	if err != nil {
		http.Error(w, "diff failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Files that differ from the previous patchset (interdiff).
	changed := map[string]bool{}
	if prev != nil {
		if inter, err := repo.Diff(prev.CommitSHA, ps.CommitSHA); err == nil {
			for _, d := range inter {
				changed[diffName(d)] = true
			}
		}
	}

	resp := patchsetSummaryResp{Patchset: psNum, CommitSHA: ps.CommitSHA}
	if prev != nil {
		resp.PrevPatchset = prev.Number
	}
	for _, d := range diffs {
		add, del := countDiffLines(d.Unified)
		f := patchsetFileSummary{
			Path:             diffName(d),
			Additions:        add,
			Deletions:        del,
			Status:           diffStatus(d),
			ChangedSincePrev: prev != nil && changed[diffName(d)],
		}
		if d.OldPath != "" && d.OldPath != d.NewPath {
			f.OldPath = d.OldPath
		}
		resp.Files = append(resp.Files, f)
		resp.TotalAdditions += add
		resp.TotalDeletions += del
	}
	writeJSON(w, resp)
}

// diffName returns the canonical file path for a diff (new side, or old side
// for deletions). go-git uses "" for the missing side.
func diffName(d gitpkg.FileDiff) string {
	if d.NewPath != "" {
		return d.NewPath
	}
	return d.OldPath
}

func diffStatus(d gitpkg.FileDiff) string {
	switch {
	case d.OldPath == "":
		return "added"
	case d.NewPath == "":
		return "deleted"
	case d.OldPath != d.NewPath:
		return "renamed"
	default:
		return "modified"
	}
}

// countDiffLines counts added/deleted lines in a unified diff, ignoring the
// +++/--- file headers and @@ hunk markers.
func countDiffLines(unified string) (adds, dels int) {
	for _, line := range strings.Split(unified, "\n") {
		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
			continue
		case strings.HasPrefix(line, "+"):
			adds++
		case strings.HasPrefix(line, "-"):
			dels++
		}
	}
	return adds, dels
}

func (s *Server) handleCLReview(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var body struct {
		Verdict model.Verdict `json:"verdict"`
	}
	if !decodeBody(w, r, &body, `{"verdict": "lgtm" | "needs_work" | "comment"}`) {
		return
	}
	switch body.Verdict {
	case model.VerdictLGTM, model.VerdictNeedsWork, model.VerdictComment:
	default:
		http.Error(w, "invalid verdict: must be lgtm, needs_work, or comment", http.StatusBadRequest)
		return
	}

	ps, err := s.db.LatestPatchset(id)
	if err != nil || ps == nil {
		http.Error(w, "cl not found", http.StatusNotFound)
		return
	}

	if err := s.db.UpsertReview(&model.Review{
		CLID:     id,
		PSID:     ps.ID,
		Reviewer: s.currentUser(r),
		Verdict:  body.Verdict,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCLComment(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var body struct {
		FilePath string `json:"file_path"`
		Line     *int   `json:"line"`
		Body     string `json:"body"`
		ParentID *int64 `json:"parent_id"`
	}
	if !decodeBody(w, r, &body, `{"body": string (required), "file_path"?: string, "line"?: int, "parent_id"?: int}`) {
		return
	}
	if body.Body == "" {
		http.Error(w, `POST comment: "body" must be a non-empty string`, http.StatusBadRequest)
		return
	}

	ps, err := s.db.LatestPatchset(id)
	if err != nil || ps == nil {
		http.Error(w, "cl not found", http.StatusNotFound)
		return
	}

	author := s.currentUser(r)
	commentID, err := s.db.CreateComment(&model.Comment{
		CLID:     id,
		PSID:     ps.ID,
		Author:   author,
		FilePath: body.FilePath,
		Line:     body.Line,
		Body:     body.Body,
		ParentID: body.ParentID,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Typing "lgtm" (case-insensitive) as a comment records an LGTM review from
	// the comment's author on the latest patchset — a Gerrit-style shortcut.
	if strings.EqualFold(strings.TrimSpace(body.Body), "lgtm") {
		if err := s.db.UpsertReview(&model.Review{
			CLID:     id,
			PSID:     ps.ID,
			Reviewer: author,
			Verdict:  model.VerdictLGTM,
		}); err == nil {
			s.runner.Events().Publish(id)
		}
	}

	s.runner.Events().Publish(id)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]int64{"id": commentID})
}

func (s *Server) handleCLSubmit(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var body struct {
		Strategy    string `json:"strategy"`
		ForceSubmit bool   `json:"force_submit"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if body.Strategy == "" {
		body.Strategy = "fast-forward"
	}

	cl, err := s.db.GetCL(id)
	if err != nil || cl == nil {
		http.Error(w, "cl not found", http.StatusNotFound)
		return
	}
	if cl.Status != model.CLStatusOpen {
		http.Error(w, "cl is not open", http.StatusConflict)
		return
	}

	ps, err := s.db.LatestPatchset(id)
	if err != nil || ps == nil {
		http.Error(w, "no patchsets", http.StatusNotFound)
		return
	}

	submitter, err := s.db.GetUser(s.currentUser(r))
	if err != nil || submitter == nil {
		http.Error(w, "user not found", http.StatusUnauthorized)
		return
	}

	reviews, _ := s.db.ListReviews(id, ps.ID)
	builds, _ := s.db.ListBuilds(id, ps.ID)

	repoPath := filepath.Join(s.cfg.Server.ReposDir, cl.Repo+".git")
	repo, err := gitpkg.Open(repoPath)
	if err != nil {
		http.Error(w, "repo unavailable", http.StatusInternalServerError)
		return
	}

	// Submit is a fast-forward, so merging a CL also merges everything beneath it.
	// When a push files a chain of CLs, each sits on the one before it — submitting
	// the top one first would carry the whole stack onto the branch with only the
	// top commit's diff ever reviewed. Refuse until the base is on the branch.
	//
	// BaseSHA == CommitSHA only for a root commit, which has nothing beneath it.
	if ps.BaseSHA != ps.CommitSHA {
		onBranch, err := repo.IsOnBranch(ps.BaseSHA, cl.TargetBranch)
		if err != nil {
			http.Error(w, "check base ancestry: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if !onBranch {
			msg := fmt.Sprintf("CL-%d sits on commit %s, which is not on %s yet",
				cl.ID, gitpkg.ShortSHA(ps.BaseSHA), cl.TargetBranch)
			if parent, _ := s.db.GetCLByCommitSHA(ps.BaseSHA); parent != nil {
				msg = fmt.Sprintf("CL-%d sits on CL-%d (%s), which is %s — submit that one first",
					cl.ID, parent.ID, gitpkg.ShortSHA(ps.BaseSHA), parent.Status)
			}
			http.Error(w, msg, http.StatusConflict)
			return
		}
	}

	changedFiles, _ := repo.ChangedFiles(ps.BaseSHA, ps.CommitSHA)
	requireOther := s.requiresOtherLGTM(repoPath)
	lgtmers := countedLGTMers(reviews, ps.ID, cl.Author, requireOther)
	ownersOK := checkOwnersApproval(changedFiles, lgtmers, repoPath, ps.CommitSHA)

	requiredTools := make([]string, 0)
	for _, t := range s.cfg.BuildTools {
		if t.Required {
			requiredTools = append(requiredTools, t.Name)
		}
	}

	if gateErr := gate.Check(gate.CheckInput{
		CL:            cl,
		Patchset:      ps,
		Reviews:       reviews,
		Builds:        builds,
		RequiredTools: requiredTools,
		OwnersOK:      ownersOK,
		RequireOther:  requireOther,
		Submitter:     submitter,
		ForceSubmit:   body.ForceSubmit && submitter.Role == model.RoleAdmin,
	}); gateErr != nil {
		http.Error(w, gateErr.Error(), http.StatusForbidden)
		return
	}

	mergedSHA, err := repo.Submit(cl.TargetBranch, ps.CommitSHA, gitpkg.MergeStrategy(body.Strategy))
	if err != nil {
		// A non-fast-forward is the client's problem (the CL needs rebasing), not a
		// server fault — report it as a conflict so the UI can say something useful.
		if errors.Is(err, gitpkg.ErrNotFastForward) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, "submit failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := s.db.UpdateCLStatus(id, model.CLStatusMerged); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.db.WriteAudit(submitter.Username, "submit", fmt.Sprintf("cl/%d", id), mergedSHA)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"merged_sha": mergedSHA})
}

func (s *Server) handleCLAbandon(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	cl, err := s.db.GetCL(id)
	if err != nil || cl == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if cl.Status != model.CLStatusOpen {
		http.Error(w, "cl is not open", http.StatusConflict)
		return
	}

	username := s.currentUser(r)
	user, _ := s.db.GetUser(username)
	if user == nil || (user.Username != cl.Author && user.Role != model.RoleAdmin) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if err := s.db.UpdateCLStatus(id, model.CLStatusAbandoned); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.db.WriteAudit(username, "abandon", fmt.Sprintf("cl/%d", id), "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCLAssign(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var body struct {
		Username string `json:"username"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Username == "" {
		http.Error(w, "bad request: username required", http.StatusBadRequest)
		return
	}
	if err := s.db.AddReviewer(id, body.Username, model.ReviewerSourceManual); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleHook processes post-receive events from the git hook script.
// Called as: POST /internal/hook/post-receive?repo=<repo-name>
// Body: newline-separated "<old-sha> <new-sha> <ref>" lines from git's post-receive stdin.
func (s *Server) handleHook(w http.ResponseWriter, r *http.Request) {
	// Only the post-receive hook knows the token. Without this check anyone who
	// can reach the server could open CLs as any user and start builds.
	want, err := gitpkg.HookToken(s.cfg.Server.DataDir)
	got := r.Header.Get("X-Citric-Hook-Token")
	if err != nil || got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	repoName := r.URL.Query().Get("repo")
	if repoName == "" {
		http.Error(w, "missing repo param", http.StatusBadRequest)
		return
	}

	events, err := gitpkg.ParsePostReceive(r.Body)
	if err != nil {
		http.Error(w, "parse hook payload: "+err.Error(), http.StatusBadRequest)
		return
	}

	repoPath := filepath.Join(s.cfg.Server.ReposDir, repoName+".git")
	repo, err := gitpkg.Open(repoPath)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	for _, event := range events {
		if !strings.HasPrefix(event.Ref, "refs/for/") {
			continue
		}
		targetBranch := strings.TrimPrefix(event.Ref, "refs/for/")

		// git sends ONE post-receive line per ref no matter how many commits the
		// push carried, so the tip alone is not the push. Expand it into every
		// commit the branch does not already have and file each as its own CL,
		// oldest first — each one's base is then the previous one's tip.
		chain, err := repo.NewCommits(event.NewSHA, targetBranch, maxChainCommits)
		if err != nil {
			fmt.Fprintf(w, "error: %v\n", err)
			continue
		}
		if len(chain) == 0 {
			// Everything pushed is already on the branch. Nothing to review, but
			// say so — a silent success reads as a lost push.
			fmt.Fprintf(w, "note: nothing to review — %s is already on %s\n",
				gitpkg.ShortSHA(event.NewSHA), targetBranch)
			continue
		}
		if len(chain) > 1 {
			fmt.Fprintf(w, "note: push carries %d commits — filing one CL per commit\n", len(chain))
		}
		for _, sha := range chain {
			s.fileCommit(w, r, repo, repoName, targetBranch, sha)
		}
	}
}

// maxChainCommits caps how many CLs one push may open. A branch long enough to
// exceed this is almost always an import of existing history rather than a
// review request, and filing hundreds of CLs is not a recoverable mistake.
const maxChainCommits = 50

// fileCommit records a single pushed commit as a new CL, or as the next patchset
// of the CL that already owns its Change-Id, and kicks off its builds. Diagnostics
// go to w, which the pusher sees as remote output from git.
func (s *Server) fileCommit(w http.ResponseWriter, r *http.Request, repo *gitpkg.Repo, repoName, targetBranch, commitSHA string) {
	info, err := repo.CommitInfo(commitSHA)
	if err != nil {
		fmt.Fprintf(w, "error: cannot read commit %s: %v\n", commitSHA, err)
		return
	}

	changeID := gitpkg.ChangeIDFromCommit(info.Message)
	if changeID == "" {
		// No Change-Id footer — derive one from the commit SHA.
		// Amendments without a commit-msg hook will create a new CL each time
		// rather than adding a patchset to the existing one.
		changeID = "I" + commitSHA
	}

	cl, _ := s.db.GetCLByChangeID(changeID)

	var clID int64
	var psNumber int

	if cl == nil {
		// Attribution priority: authenticated pusher > commit-email match >
		// raw commit author name (last resort, may not be a citric user).
		authorUsername := info.Author
		if author, _ := s.db.GetUserByEmail(info.Email); author != nil {
			authorUsername = author.Username
		}
		if pusher := r.URL.Query().Get("pusher"); pusher != "" && !brand.IsServiceUser(pusher) {
			if pu, _ := s.db.GetUser(pusher); pu != nil {
				authorUsername = pu.Username
			}
		}

		clID, err = s.db.CreateCL(&model.CL{
			ChangeID:     changeID,
			Repo:         repoName,
			TargetBranch: targetBranch,
			Author:       authorUsername,
			Title:        info.Subject,
			Description:  info.Body,
		})
		if err != nil {
			fmt.Fprintf(w, "error: create CL: %v\n", err)
			return
		}
		psNumber = 1
	} else {
		// A closed CL must not accept new patchsets — the change is
		// already resolved. Reject and tell the pusher to open a new
		// change (fresh Change-Id) instead of amending onto this one.
		if cl.Status == model.CLStatusMerged || cl.Status == model.CLStatusAbandoned {
			fmt.Fprintf(w, "error: CL-%d is already %s; push a new change (new Change-Id) instead of a new patchset\n", cl.ID, cl.Status)
			return
		}
		clID = cl.ID
		existing, _ := s.db.ListPatchsets(clID)
		psNumber = len(existing) + 1
	}

	baseSHA := commitSHA
	if len(info.ParentSHAs) > 0 {
		baseSHA = info.ParentSHAs[0]
	}

	psRef := gitpkg.PatchsetRef(clID, psNumber)
	if err := repo.CreateRef(psRef, commitSHA); err != nil {
		fmt.Fprintf(w, "error: create patchset ref: %v\n", err)
		return
	}

	psID, err := s.db.CreatePatchset(&model.Patchset{
		CLID:      clID,
		Number:    psNumber,
		CommitSHA: commitSHA,
		BaseSHA:   baseSHA,
		Ref:       psRef,
	})
	if err != nil {
		fmt.Fprintf(w, "error: create patchset record: %v\n", err)
		return
	}

	if s.runner != nil {
		s.runner.Events().Publish(clID) // new patchset visible to open pages
	}

	if s.runner != nil {
		clObj, _ := s.db.GetCL(clID)
		s.runner.Run(clObj, &model.Patchset{
			ID:        psID,
			CLID:      clID,
			Number:    psNumber,
			CommitSHA: commitSHA,
			BaseSHA:   baseSHA,
			Ref:       psRef,
		}, nil)
	}

	fmt.Fprintf(w, "ok: CL-%d PS-%d → %s\n", clID, psNumber, psRef)
}

// checkOwnersApproval creates a temporary git worktree for the commit, runs the
// OWNERS resolver against every changed file, and returns true if all required
// approvals are satisfied by the set of lgtmers.
func checkOwnersApproval(changedFiles []string, lgtmers map[string]bool, repoPath, commitSHA string) bool {
	if len(changedFiles) == 0 {
		return true
	}

	tmpDir, err := os.MkdirTemp("", "citric-owners-*")
	if err != nil {
		return false
	}
	cmd := exec.Command("git", "worktree", "add", "--detach", tmpDir, commitSHA)
	cmd.Dir = repoPath
	if err := cmd.Run(); err != nil {
		os.RemoveAll(tmpDir)
		return true // can't evaluate — treat as satisfied rather than blocking indefinitely
	}
	defer func() {
		exec.Command("git", "-C", repoPath, "worktree", "remove", "--force", tmpDir).Run()
		os.RemoveAll(tmpDir)
	}()

	for _, file := range changedFiles {
		resolved, err := owners.Resolve(tmpDir, file)
		if err != nil || len(resolved.Required) == 0 {
			continue
		}
		lgtmCount := 0
		for _, o := range resolved.Required {
			if lgtmers[strings.TrimPrefix(o, "@")] {
				lgtmCount++
			}
		}
		if lgtmCount < resolved.RequiredLGTMs {
			return false
		}
	}
	return true
}
