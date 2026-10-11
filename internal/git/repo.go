package git

import (
	"fmt"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

type Repo struct {
	r    *gogit.Repository
	path string
}

// CommitInfo holds the fields we extract from a commit object for CL creation.
type CommitInfo struct {
	SHA        string
	Subject    string // first line of message
	Body       string // everything after the blank line, footers stripped
	Message    string // full raw message
	Author     string
	Email      string
	ParentSHAs []string
}

func Open(path string) (*Repo, error) {
	r, err := gogit.PlainOpen(path)
	if err != nil {
		return nil, fmt.Errorf("open repo %s: %w", path, err)
	}
	return &Repo{r: r, path: path}, nil
}

func (r *Repo) CommitInfo(sha string) (*CommitInfo, error) {
	c, err := r.r.CommitObject(plumbing.NewHash(sha))
	if err != nil {
		return nil, fmt.Errorf("commit %s: %w", sha, err)
	}

	info := &CommitInfo{
		SHA:     sha,
		Message: c.Message,
		Author:  c.Author.Name,
		Email:   c.Author.Email,
	}

	lines := strings.SplitN(strings.TrimRight(c.Message, "\n"), "\n", 2)
	info.Subject = lines[0]
	if len(lines) > 1 {
		info.Body = strings.TrimSpace(stripFooters(lines[1]))
	}

	for _, p := range c.ParentHashes {
		info.ParentSHAs = append(info.ParentSHAs, p.String())
	}
	return info, nil
}

// ChangedFiles returns the set of file paths modified between baseSHA and commitSHA.
// Used by the OWNERS resolver to determine which approver sets apply.
func (r *Repo) ChangedFiles(baseSHA, commitSHA string) ([]string, error) {
	diffs, err := r.Diff(baseSHA, commitSHA)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var paths []string
	for _, d := range diffs {
		for _, p := range []string{d.OldPath, d.NewPath} {
			if p != "" && !seen[p] {
				seen[p] = true
				paths = append(paths, p)
			}
		}
	}
	return paths, nil
}

// CreateRef creates a ref pointing at the given commit SHA.
// Used to store patchsets under refs/changes/XX/<cl_id>/<ps_number>.
func (r *Repo) CreateRef(name, commitSHA string) error {
	hash := plumbing.NewHash(commitSHA)
	ref := plumbing.NewHashReference(plumbing.ReferenceName(name), hash)
	return r.r.Storer.SetReference(ref)
}

// ResolveRef returns the commit SHA a ref points to.
func (r *Repo) ResolveRef(name string) (string, error) {
	ref, err := r.r.Reference(plumbing.ReferenceName(name), true)
	if err != nil {
		return "", fmt.Errorf("resolve ref %s: %w", name, err)
	}
	return ref.Hash().String(), nil
}

// PatchsetRef returns the canonical ref name for a given CL ID and patchset number.
// Format mirrors Gerrit: refs/changes/XX/<cl_id>/<ps_number>
func PatchsetRef(clID int64, psNumber int) string {
	return fmt.Sprintf("refs/changes/%02d/%d/%d", clID%100, clID, psNumber)
}

// stripFooters removes the git commit message footer block (lines like "Change-Id: ...",
// "Signed-off-by: ...") so the description stored in the CL is clean prose.
func stripFooters(body string) string {
	lines := strings.Split(body, "\n")
	cut := len(lines)
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			break
		}
		if strings.Contains(line, ": ") {
			cut = i
		} else {
			break
		}
	}
	return strings.TrimSpace(strings.Join(lines[:cut], "\n"))
}
