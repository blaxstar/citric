package gate

import (
	"errors"

	"github.com/blaxstar/citric/internal/model"
)

var (
	ErrNeedsWork       = errors.New("CL has outstanding needs_work verdicts")
	ErrOwnersNotMet    = errors.New("OWNERS approval requirements not satisfied")
	ErrBuildsNotPassed = errors.New("required builds have not passed")
	ErrNotSubmitter    = errors.New("user does not have submit permission")
)

type CheckInput struct {
	CL            *model.CL
	Patchset      *model.Patchset
	Reviews       []*model.Review
	Builds        []*model.Build
	RequiredTools []string // names of tools that must pass; empty = no build gate
	OwnersOK      bool     // pre-evaluated by owners.Resolver
	Submitter     *model.User
	ForceSubmit   bool // admin only — bypasses build gate
}

// Check evaluates all submit gate conditions and returns the first violation,
// or nil if the CL may proceed to submit.
func Check(in CheckInput) error {
	if in.Submitter.Role != model.RoleAdmin && in.Submitter.Role != model.RoleSubmitter {
		return ErrNotSubmitter
	}
	for _, r := range in.Reviews {
		if r.PSID == in.Patchset.ID && r.Verdict == model.VerdictNeedsWork {
			return ErrNeedsWork
		}
	}
	if !in.OwnersOK {
		return ErrOwnersNotMet
	}
	if !in.ForceSubmit && len(in.RequiredTools) > 0 {
		// Index builds for this patchset by tool name.
		byTool := make(map[string]*model.Build, len(in.Builds))
		for _, b := range in.Builds {
			if b.PSID == in.Patchset.ID {
				byTool[b.ToolName] = b
			}
		}
		// A required tool blocks submission only when a build record exists and
		// is not passing. No record = tool waived (no command in repo .citric.toml).
		for _, name := range in.RequiredTools {
			if b, exists := byTool[name]; exists && b.Status != model.BuildStatusPass {
				return ErrBuildsNotPassed
			}
		}
	}
	return nil
}
