package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// TreeEntry represents one entry returned by git ls-tree.
type TreeEntry struct {
	Name       string `json:"name"`
	Type       string `json:"type"` // "blob" or "tree"
	SHA        string `json:"sha"`
	Size       int64  `json:"size,omitempty"`
	Mode       string `json:"mode"`
	ModifiedAt string `json:"modified_at,omitempty"`
}

// CommitSummary is a lightweight commit record for the log view.
type CommitSummary struct {
	SHA     string `json:"sha"`
	Subject string `json:"subject"`
	Author  string `json:"author"`
	Email   string `json:"email"`
	Time    string `json:"time"`
}

// ListTree returns the directory entries at path inside ref.
// path should be "" for the root.
func ListTree(dataDir, repoDir, ref, path string) ([]TreeEntry, error) {
	base := append(os.Environ(), "GIT_DIR="+repoDir)
	base = append(base, Env(dataDir)...)

	treeSpec := ref
	if path != "" {
		treeSpec = ref + ":" + path
	}

	cmd := exec.Command("git", "ls-tree", treeSpec)
	cmd.Env = base
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ls-tree %s: %v: %s", treeSpec, err, errb.String())
	}

	entries := []TreeEntry{}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		// format: <mode> SP <type> SP <sha> TAB <name>
		tabIdx := strings.Index(line, "\t")
		if tabIdx < 0 {
			continue
		}
		meta := strings.Fields(line[:tabIdx])
		if len(meta) != 3 {
			continue
		}
		entries = append(entries, TreeEntry{
			Mode: meta[0],
			Type: meta[1],
			SHA:  meta[2],
			Name: UnquotePath(line[tabIdx+1:]),
		})
	}
	return entries, nil
}

// FillModifiedAt populates ModifiedAt for each entry by running git log -1
// against the full path (basePath/name). Errors per-entry are silently ignored.
func FillModifiedAt(dataDir, repoDir, ref, basePath string, entries []TreeEntry) {
	base := append(os.Environ(), "GIT_DIR="+repoDir)
	base = append(base, Env(dataDir)...)

	for i, e := range entries {
		fullPath := e.Name
		if basePath != "" {
			fullPath = basePath + "/" + e.Name
		}
		cmd := exec.Command("git", "log", ref, "-1", "--format=%aI", "--", fullPath)
		cmd.Env = base
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err == nil {
			if t := strings.TrimSpace(out.String()); t != "" {
				entries[i].ModifiedAt = t
			}
		}
	}
}

// FillSizes populates the Size field for blob entries via git cat-file -s.
// Errors per-entry are silently ignored.
func FillSizes(dataDir, repoDir string, entries []TreeEntry) {
	base := append(os.Environ(), "GIT_DIR="+repoDir)
	base = append(base, Env(dataDir)...)

	for i, e := range entries {
		if e.Type != "blob" {
			continue
		}
		cmd := exec.Command("git", "cat-file", "-s", e.SHA)
		cmd.Env = base
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err == nil {
			if n, err := strconv.ParseInt(strings.TrimSpace(out.String()), 10, 64); err == nil {
				entries[i].Size = n
			}
		}
	}
}

// ListBranches returns branch names and the default branch name from HEAD.
func ListBranches(dataDir, repoDir string) (branches []string, defaultBranch string, err error) {
	base := append(os.Environ(), "GIT_DIR="+repoDir)
	base = append(base, Env(dataDir)...)

	// Always return a non-nil slice so it marshals to [] not null in JSON.
	branches = []string{}

	// List branches that actually have commits.
	var out bytes.Buffer
	list := exec.Command("git", "for-each-ref", "refs/heads", "--format=%(refname:short)")
	list.Env = base
	list.Stdout = &out
	if err := list.Run(); err != nil {
		return nil, "", fmt.Errorf("list branches: %w", err)
	}
	for _, b := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if b != "" {
			branches = append(branches, b)
		}
	}

	// Only use symbolic-ref as default if that branch actually has commits.
	// An unborn branch (no pushes yet) appears in symbolic-ref but not in for-each-ref.
	out.Reset()
	symref := exec.Command("git", "symbolic-ref", "--short", "HEAD")
	symref.Env = base
	symref.Stdout = &out
	if symref.Run() == nil {
		sym := strings.TrimSpace(out.String())
		for _, b := range branches {
			if b == sym {
				defaultBranch = sym
				break
			}
		}
	}
	// Fall back to first available branch if HEAD is unborn.
	if defaultBranch == "" && len(branches) > 0 {
		defaultBranch = branches[0]
	}

	return branches, defaultBranch, nil
}

// ListCommits returns up to n commits on ref as summaries.
func ListCommits(dataDir, repoDir, ref string, n int) ([]CommitSummary, error) {
	base := append(os.Environ(), "GIT_DIR="+repoDir)
	base = append(base, Env(dataDir)...)

	// Use git's %x1f escape (ASCII unit separator) so the format string itself
	// contains no null bytes — passing \x00 in an exec argument causes EINVAL.
	cmd := exec.Command("git", "log", ref, fmt.Sprintf("-n%d", n),
		"--format=%H%x1f%an%x1f%ae%x1f%aI%x1f%s")
	cmd.Env = base
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git log: %v: %s", err, errb.String())
	}

	commits := []CommitSummary{}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\x1f", 5)
		if len(parts) < 5 {
			continue
		}
		commits = append(commits, CommitSummary{
			SHA:     parts[0],
			Author:  parts[1],
			Email:   parts[2],
			Time:    parts[3],
			Subject: parts[4],
		})
	}
	return commits, nil
}

// RefSHA returns the commit SHA a ref points to. refName may be a branch name
// (resolved as refs/heads/<name>) or a full ref.
func RefSHA(dataDir, repoDir, refName string) (string, error) {
	base := append(os.Environ(), "GIT_DIR="+repoDir)
	base = append(base, Env(dataDir)...)

	// Resolve short branch name to full ref
	if !strings.HasPrefix(refName, "refs/") {
		refName = "refs/heads/" + refName
	}
	cmd := exec.Command("git", "rev-parse", refName)
	cmd.Env = base
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("rev-parse %s: %v: %s", refName, err, errb.String())
	}
	return strings.TrimSpace(out.String()), nil
}

// SetRef creates or updates a ref to point at sha.
func SetRef(dataDir, repoDir, refName, sha string) error {
	base := append(os.Environ(), "GIT_DIR="+repoDir)
	base = append(base, Env(dataDir)...)
	cmd := exec.Command("git", "update-ref", refName, sha)
	cmd.Env = base
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("set-ref %s: %v: %s", refName, err, errb.String())
	}
	return nil
}

// DeleteRef removes a ref.
func DeleteRef(dataDir, repoDir, refName string) error {
	base := append(os.Environ(), "GIT_DIR="+repoDir)
	base = append(base, Env(dataDir)...)
	cmd := exec.Command("git", "update-ref", "-d", refName)
	cmd.Env = base
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("delete-ref %s: %v: %s", refName, err, errb.String())
	}
	return nil
}

// ReadFile returns the content of a file at ref:path.
// Returns content, isBinary, error.
func ReadFile(dataDir, repoDir, ref, path string) (string, bool, error) {
	content, err := ShowFile(dataDir, repoDir, ref, path)
	if err != nil {
		return "", false, err
	}
	// Binary heuristic: null byte in first 8 KB
	check := content
	if len(check) > 8192 {
		check = check[:8192]
	}
	return content, strings.ContainsRune(check, '\x00'), nil
}
