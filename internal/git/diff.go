package git

import (
	"fmt"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

type FileDiff struct {
	OldPath string `json:"old_path"`
	NewPath string `json:"new_path"`
	Unified string `json:"unified"`
}

// Diff returns per-file unified diffs between baseSHA and commitSHA.
// If baseSHA == commitSHA (initial commit with no parent), diffs against an
// empty tree so all files in the commit show as additions.
func (r *Repo) Diff(baseSHA, commitSHA string) ([]FileDiff, error) {
	head, err := r.commit(commitSHA)
	if err != nil {
		return nil, fmt.Errorf("resolve head: %w", err)
	}
	headTree, err := head.Tree()
	if err != nil {
		return nil, err
	}

	out := make([]FileDiff, 0)

	if baseSHA == commitSHA || baseSHA == "" {
		// Initial commit — diff against empty tree: every file is an addition.
		changes, err := object.DiffTree(nil, headTree)
		if err != nil {
			return nil, fmt.Errorf("diff from empty tree: %w", err)
		}
		for _, change := range changes {
			patch, err := change.Patch()
			if err != nil {
				return nil, fmt.Errorf("patch for %s: %w", change.To.Name, err)
			}
			out = append(out, FileDiff{
				OldPath: change.From.Name,
				NewPath: change.To.Name,
				Unified: patch.String(),
			})
		}
		return out, nil
	}

	base, err := r.commit(baseSHA)
	if err != nil {
		return nil, fmt.Errorf("resolve base: %w", err)
	}
	baseTree, err := base.Tree()
	if err != nil {
		return nil, err
	}

	changes, err := baseTree.Diff(headTree)
	if err != nil {
		return nil, fmt.Errorf("diff trees: %w", err)
	}

	for _, change := range changes {
		patch, err := change.Patch()
		if err != nil {
			return nil, fmt.Errorf("patch for %s: %w", change.To.Name, err)
		}
		out = append(out, FileDiff{
			OldPath: change.From.Name,
			NewPath: change.To.Name,
			Unified: patch.String(),
		})
	}
	return out, nil
}

func (r *Repo) commit(sha string) (*object.Commit, error) {
	return r.r.CommitObject(plumbing.NewHash(sha))
}
