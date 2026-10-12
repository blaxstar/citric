package api

import (
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	gitpkg "github.com/blaxstar/citric/internal/git"
	"github.com/blaxstar/citric/internal/model"
)

// Readiness says whether a CL can be merged right now and, if not, why. It
// applies the same checks as submit, plus the "at least one LGTM" rule the
// UI requires, and words them for people.
type Readiness struct {
	Ready bool `json:"ready"`
	// Waiting is true when the only blockers are builds still running, so the
	// CL may become ready on its own.
	Waiting bool     `json:"waiting"`
	Reasons []string `json:"reasons"`
	// BaseCL is the open CL this one sits on, when its base isn't on the
	// target branch yet (merging that one first unblocks this one).
	BaseCL int64 `json:"base_cl,omitempty"`
}

// GET /cl/{id}/readiness
func (s *Server) handleCLReadiness(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	cl, err := s.db.GetCL(id)
	if err != nil || cl == nil {
		http.Error(w, "cl not found", http.StatusNotFound)
		return
	}
	rd, err := s.clReadiness(cl, s.currentUser(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, rd)
}

func (s *Server) clReadiness(cl *model.CL, username string) (*Readiness, error) {
	rd := &Readiness{Reasons: []string{}}
	block := func(format string, args ...any) { rd.Reasons = append(rd.Reasons, fmt.Sprintf(format, args...)) }

	if cl.Status != model.CLStatusOpen {
		block("already %s", cl.Status)
		return rd, nil
	}
	ps, err := s.db.LatestPatchset(cl.ID)
	if err != nil || ps == nil {
		block("has no patchset")
		return rd, nil
	}
	if u, _ := s.db.GetUser(username); u == nil || (u.Role != model.RoleAdmin && u.Role != model.RoleSubmitter) {
		block("you don't have permission to merge")
	}

	repoPath := filepath.Join(s.cfg.Server.ReposDir, cl.Repo+".git")
	requireOther := s.requiresOtherLGTM(repoPath)

	reviews, _ := s.db.ListReviews(cl.ID, ps.ID)
	var needsWork []string
	authorLGTM := false
	for _, rev := range reviews {
		if rev.PSID != ps.ID {
			continue
		}
		switch rev.Verdict {
		case model.VerdictLGTM:
			authorLGTM = authorLGTM || rev.Reviewer == cl.Author
		case model.VerdictNeedsWork:
			needsWork = append(needsWork, rev.Reviewer)
		}
	}
	lgtmers := countedLGTMers(reviews, ps.ID, cl.Author, requireOther)
	if len(needsWork) > 0 {
		sort.Strings(needsWork)
		block("needs work (%s)", joinNames(needsWork))
	}
	if len(lgtmers) == 0 {
		if requireOther && authorLGTM {
			block("needs an LGTM from someone other than the author")
		} else {
			block("no LGTM yet")
		}
	}

	repo, err := gitpkg.Open(repoPath)
	if err != nil {
		return nil, fmt.Errorf("repo unavailable")
	}
	changed, _ := repo.ChangedFiles(ps.BaseSHA, ps.CommitSHA)
	if !checkOwnersApproval(changed, lgtmers, repoPath, ps.CommitSHA) {
		block("needs an OWNERS approval")
	}

	builds, _ := s.db.ListBuilds(cl.ID, ps.ID)
	latest := map[string]*model.Build{}
	for _, b := range builds {
		if cur, ok := latest[b.ToolName]; !ok || b.ID > cur.ID {
			latest[b.ToolName] = b
		}
	}
	running := 0
	for _, t := range s.cfg.BuildTools {
		if !t.Required {
			continue
		}
		b, ok := latest[t.Name]
		if !ok || b.Status == model.BuildStatusPass {
			continue // no record = waived, as on submit
		}
		if b.Status == model.BuildStatusFail {
			block("%s failed", t.Name)
		} else {
			running++
			block("%s is still running", t.Name)
		}
	}

	if ps.BaseSHA != ps.CommitSHA {
		if onBranch, err := repo.IsOnBranch(ps.BaseSHA, cl.TargetBranch); err == nil && !onBranch {
			if parent, _ := s.db.GetCLByCommitSHA(ps.BaseSHA); parent != nil && parent.Status == model.CLStatusOpen {
				rd.BaseCL = parent.ID
				block("sits on CL-%d, which isn't merged yet", parent.ID)
			} else {
				block("sits on a commit that isn't on %s; rebase it", cl.TargetBranch)
			}
		}
	}

	rd.Ready = len(rd.Reasons) == 0
	rd.Waiting = running > 0 && len(rd.Reasons) == running
	return rd, nil
}

func joinNames(names []string) string { return strings.Join(names, ", ") }
