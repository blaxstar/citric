package model

import "time"

type Verdict string

const (
	VerdictLGTM      Verdict = "lgtm"
	VerdictNeedsWork Verdict = "needs_work"
	VerdictComment   Verdict = "comment"
)

type Review struct {
	ID        int64     `json:"id"`
	CLID      int64     `json:"cl_id"`
	PSID      int64     `json:"ps_id"`
	Reviewer  string    `json:"reviewer"`
	Verdict   Verdict   `json:"verdict"`
	CreatedAt time.Time `json:"created_at"`
}

type Comment struct {
	ID        int64     `json:"id"`
	CLID      int64     `json:"cl_id"`
	PSID      int64     `json:"ps_id"`
	Author    string    `json:"author"`
	FilePath  string    `json:"file_path"`
	Line      *int      `json:"line,omitempty"`
	Body      string    `json:"body"`
	ParentID  *int64    `json:"parent_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
