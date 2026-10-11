package model

import "time"

type CLStatus string

const (
	CLStatusOpen      CLStatus = "open"
	CLStatusMerged    CLStatus = "merged"
	CLStatusAbandoned CLStatus = "abandoned"
)

type ReviewerSource string

const (
	ReviewerSourceManual        ReviewerSource = "manual"
	ReviewerSourceAutoOwners    ReviewerSource = "auto_owners"
	ReviewerSourceAutoHierarchy ReviewerSource = "auto_hierarchy"
)

type CL struct {
	ID           int64     `json:"id"`
	ChangeID     string    `json:"change_id"`
	Repo         string    `json:"repo"`
	TargetBranch string    `json:"target_branch"`
	Author       string    `json:"author"`
	Title        string    `json:"title"`
	Description  string    `json:"description,omitempty"`
	Status       CLStatus  `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`

	// LatestCommitSHA/LatestBaseSHA are the tip and base commit of this CL's
	// latest patchset. Only ListCLs populates them (a single query with every
	// other CL, so clients can group CLs pushed together into a stack — one
	// CL's base matching another open CL's tip — without an extra request per
	// CL). Empty on every other lookup (GetCL, GetCLByChangeID, ...).
	LatestCommitSHA string `json:"latest_commit_sha,omitempty"`
	LatestBaseSHA   string `json:"latest_base_sha,omitempty"`

	// NeedsRebase reports whether LatestBaseSHA is stale — the target branch's
	// tip has moved past it since this patchset was pushed. Only ListCLs
	// computes this (it has to open the repo to resolve the branch tip, once
	// per distinct repo+branch rather than per CL). Always false on every
	// other lookup, same as LatestCommitSHA/LatestBaseSHA.
	NeedsRebase bool `json:"needs_rebase,omitempty"`

	// Reviewers is populated only by ListCLs (via a batched cl_reviewers
	// lookup) — the usernames assigned to review this CL, manually or via
	// OWNERS auto-assignment. Empty on every other lookup.
	Reviewers []string `json:"reviewers,omitempty"`
}

type Patchset struct {
	ID        int64     `json:"id"`
	CLID      int64     `json:"cl_id"`
	Number    int       `json:"number"`
	CommitSHA string    `json:"commit_sha"`
	BaseSHA   string    `json:"base_sha"`
	Ref       string    `json:"ref"`
	Message   string    `json:"message,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type CLReviewer struct {
	CLID     int64          `json:"cl_id"`
	Username string         `json:"username"`
	Source   ReviewerSource `json:"source"`
}
