package git

import (
	"errors"
	"fmt"

	"github.com/go-git/go-git/v5/plumbing"
)

type MergeStrategy string

const (
	MergeStrategyFF         MergeStrategy = "fast-forward"
	MergeStrategyRebase     MergeStrategy = "rebase"
	MergeStrategyCherryPick MergeStrategy = "cherry-pick"
)

// Submit merges commitSHA into targetBranch using the given strategy.
// Returns the resulting merge commit SHA.
func (r *Repo) Submit(targetBranch, commitSHA string, strategy MergeStrategy) (string, error) {
	switch strategy {
	case MergeStrategyFF:
		return r.fastForward(targetBranch, commitSHA)
	case MergeStrategyCherryPick:
		// TODO: shell fallback — go-git doesn't support cherry-pick natively
		return "", fmt.Errorf("cherry-pick not yet implemented")
	case MergeStrategyRebase:
		// TODO: shell fallback — go-git rebase support is limited
		return "", fmt.Errorf("rebase not yet implemented")
	default:
		return "", fmt.Errorf("unknown merge strategy: %s", strategy)
	}
}

// ErrNotFastForward reports a submit that would move the branch anywhere other
// than strictly forward. Callers should surface this as a client-side conflict,
// not a server error — the CL needs rebasing, nothing is broken server-side.
var ErrNotFastForward = errors.New("not a fast-forward")

func (r *Repo) fastForward(branch, commitSHA string) (string, error) {
	refName := plumbing.NewBranchReferenceName(branch)
	newHash := plumbing.NewHash(commitSHA)

	old, err := r.r.Reference(refName, true)
	if err != nil {
		// Unborn branch: no tip to move forward from, so the first commit lands as-is.
		if err := r.r.Storer.SetReference(plumbing.NewHashReference(refName, newHash)); err != nil {
			return "", fmt.Errorf("create %s: %w", branch, err)
		}
		return commitSHA, nil
	}

	if old.Hash() == newHash {
		return commitSHA, nil // already there; submitting twice is a no-op, not an error
	}

	// A submit must only ever move the branch FORWARD.
	//
	// This used to be a bare SetReference, which is `git update-ref --force`:
	// it happily pointed the branch at a commit that was behind, or unrelated
	// to, the current tip — silently discarding every commit above it while
	// those CLs stayed marked merged. That is exactly how CL-80 was lost on
	// 2026-08-01: CL-80 (tip) merged, then CL-81 (its own parent commit) merged
	// 14s later and rewound main by one commit.
	//
	// The caller's stacking guard is not enough on its own — it only checks that
	// the CL's *base* is on the branch, which stays true for any CL whose base is
	// an ancestor of the tip. Two CLs cut from the same commit and submitted in
	// sequence pass that check and still clobber each other without this.
	tip, err := r.r.CommitObject(old.Hash())
	if err != nil {
		return "", fmt.Errorf("read branch %s: %w", branch, err)
	}
	next, err := r.r.CommitObject(newHash)
	if err != nil {
		return "", fmt.Errorf("read commit %s: %w", ShortSHA(commitSHA), err)
	}
	ff, err := tip.IsAncestor(next)
	if err != nil {
		return "", fmt.Errorf("ancestry check %s..%s: %w", ShortSHA(old.Hash().String()), ShortSHA(commitSHA), err)
	}
	if !ff {
		return "", fmt.Errorf("%w: %s is not a descendant of %s (%s) — rebase onto %s and push a new patchset",
			ErrNotFastForward, ShortSHA(commitSHA), ShortSHA(old.Hash().String()), branch, branch)
	}

	// Compare-and-swap against the tip we just validated. If a concurrent submit
	// moved the branch in between, this fails loudly instead of overwriting it.
	if err := r.r.Storer.CheckAndSetReference(plumbing.NewHashReference(refName, newHash), old); err != nil {
		return "", fmt.Errorf("fast-forward %s: %w", branch, err)
	}
	return commitSHA, nil
}
