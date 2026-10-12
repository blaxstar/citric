package api

import (
	"strings"

	"github.com/blaxstar/citric/internal/model"
)

// "Require another reviewer": when on, an author's LGTM on their own CL
// doesn't count — submit needs an LGTM from someone else, and OWNERS approval
// ignores the author. Off by default. Admins turn it on server-wide (the
// require_other_lgtm setting) or per repo (the repo's git config, so it moves
// with the repo); either one enables it.
const (
	settingRequireOtherLGTM   = "require_other_lgtm"
	gitConfigRequireOtherLGTM = "citric.requireOtherLGTM"
)

func (s *Server) requireOtherLGTMGlobal() bool {
	v, _ := s.db.GetSetting(settingRequireOtherLGTM)
	return v == "true"
}

func (s *Server) repoRequiresOtherLGTM(repoPath string) bool {
	out, err := s.gitC(repoPath, "config", "--bool", "--get", gitConfigRequireOtherLGTM).Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// requiresOtherLGTM reports whether the rule applies to the repo at repoPath.
func (s *Server) requiresOtherLGTM(repoPath string) bool {
	return s.requireOtherLGTMGlobal() || s.repoRequiresOtherLGTM(repoPath)
}

// countedLGTMers returns who LGTM'd patchset psID, minus the author when the
// rule is on.
func countedLGTMers(reviews []*model.Review, psID int64, author string, requireOther bool) map[string]bool {
	out := map[string]bool{}
	for _, rev := range reviews {
		if rev.PSID != psID || rev.Verdict != model.VerdictLGTM {
			continue
		}
		if requireOther && rev.Reviewer == author {
			continue
		}
		out[rev.Reviewer] = true
	}
	return out
}
