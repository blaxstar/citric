package git

import (
	"fmt"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// NewCommits returns the commits a push introduces to targetBranch — everything
// reachable from newSHA that the branch does not already contain — ordered
// OLDEST FIRST.
//
// This is what makes a multi-commit push reviewable. A push of N commits arrives
// as a single post-receive line carrying only the tip, so reading just that tip
// (as citric did until this existed) files one CL for the last commit and leaves
// the other N-1 in the object store with no CL, no build, and no reviewer —
// while remaining ancestors of the tip, so submitting that one CL fast-forwards
// the branch over all of them.
//
// Oldest-first matters: each commit's first parent is the one before it, so
// filing them in this order makes every CL's base the previous CL's tip, which
// is exactly the chain the submit guard (IsOnBranch) then enforces.
//
// A merge commit in the range is refused rather than filed. citric's review unit
// is a single commit against a single base, and submit fast-forwards; a merge
// has no such base to diff against.
func (r *Repo) NewCommits(newSHA, targetBranch string, limit int) ([]string, error) {
	// One walk of the target branch up front, rather than an ancestry query per
	// candidate commit: the negative case (a genuinely new commit) is the common
	// one and costs a full history traversal every time it is asked.
	contained := map[plumbing.Hash]bool{}
	if ref, err := r.r.Reference(plumbing.ReferenceName("refs/heads/"+targetBranch), true); err == nil {
		if tip, err := r.r.CommitObject(ref.Hash()); err == nil {
			iter := object.NewCommitPreorderIter(tip, nil, nil)
			_ = iter.ForEach(func(c *object.Commit) error {
				contained[c.Hash] = true
				return nil
			})
		}
	}
	// An unborn target branch (new repo, first push) leaves the set empty, so
	// every commit reachable from newSHA counts as new — which is correct.

	var chain []string
	sha := newSHA
	for {
		hash := plumbing.NewHash(sha)
		if contained[hash] {
			break // reached the branch — everything below is already merged
		}
		c, err := r.r.CommitObject(hash)
		if err != nil {
			return nil, fmt.Errorf("read commit %s: %w", ShortSHA(sha), err)
		}
		if len(c.ParentHashes) > 1 {
			return nil, fmt.Errorf(
				"commit %s is a merge; citric reviews a linear chain of commits.\n"+
					"Rebase onto %s instead of merging into your branch, then push again",
				ShortSHA(sha), targetBranch)
		}
		chain = append(chain, sha)
		if len(chain) > limit {
			return nil, fmt.Errorf(
				"push introduces more than %d commits.\n"+
					"Squash the branch, or use 'citric import' to replay a long history as\n"+
					"successive patchsets of one CL", limit)
		}
		if len(c.ParentHashes) == 0 {
			break // root commit
		}
		sha = c.ParentHashes[0].String()
	}

	// Collected tip-first; reverse so each CL is filed after the one it sits on.
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain, nil
}

// IsOnBranch reports whether sha is already contained in branch (a commit is
// considered contained in itself, matching `git merge-base --is-ancestor`).
//
// Submit uses this to refuse a CL still sitting on unmerged commits: merging is
// a fast-forward, so a CL whose base is not yet on the branch would carry every
// commit beneath it along unreviewed.
func (r *Repo) IsOnBranch(sha, branch string) (bool, error) {
	ref, err := r.r.Reference(plumbing.ReferenceName("refs/heads/"+branch), true)
	if err != nil {
		// Unborn branch contains nothing — the first CL on it has no base to wait for.
		return false, nil
	}
	tip, err := r.r.CommitObject(ref.Hash())
	if err != nil {
		return false, fmt.Errorf("read branch %s: %w", branch, err)
	}
	c, err := r.r.CommitObject(plumbing.NewHash(sha))
	if err != nil {
		return false, fmt.Errorf("read commit %s: %w", ShortSHA(sha), err)
	}
	return c.IsAncestor(tip)
}

// ShortSHA abbreviates a SHA for human-facing messages.
func ShortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
