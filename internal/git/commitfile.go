package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// CommitFile writes a single file's content to a branch in a bare repository and
// commits it, using git plumbing (no working tree). It builds a new tree from the
// branch tip's tree with the one file added/replaced, then commit-tree +
// update-ref with a compare-and-swap on the old tip.
//
// repoDir is the bare repo path (used as GIT_DIR); dataDir locates citric's
// safe.directory gitconfig. The commit is attributed to author{Name,Email}.
// If the branch has no commits yet, an initial commit is created.
func CommitFile(dataDir, repoDir, branch, filePath, content, authorName, authorEmail, message string) error {
	base := append(os.Environ(), "GIT_DIR="+repoDir)
	base = append(base, Env(dataDir)...)

	run := func(env []string, stdin string, args ...string) (string, error) {
		cmd := exec.Command("git", args...)
		cmd.Env = env
		if stdin != "" {
			cmd.Stdin = strings.NewReader(stdin)
		}
		var out, errb bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &errb
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
		}
		return strings.TrimSpace(out.String()), nil
	}

	ref := "refs/heads/" + branch
	parent, _ := run(base, "", "rev-parse", "--verify", "--quiet", ref) // "" if unborn

	blob, err := run(base, content, "hash-object", "-w", "--stdin")
	if err != nil {
		return err
	}

	// Build the new tree in a throwaway index so the live repo is untouched.
	idx, err := os.CreateTemp("", "citric-index-*")
	if err != nil {
		return err
	}
	idxPath := idx.Name()
	idx.Close()
	os.Remove(idxPath) // git wants to create it itself
	defer os.Remove(idxPath)

	idxEnv := append(append([]string{}, base...), "GIT_INDEX_FILE="+idxPath)
	if parent != "" {
		if _, err := run(idxEnv, "", "read-tree", parent); err != nil {
			return err
		}
	}
	if _, err := run(idxEnv, "", "update-index", "--add", "--cacheinfo", "100644,"+blob+","+filePath); err != nil {
		return err
	}
	tree, err := run(idxEnv, "", "write-tree")
	if err != nil {
		return err
	}

	commitEnv := append(append([]string{}, base...),
		"GIT_AUTHOR_NAME="+authorName, "GIT_AUTHOR_EMAIL="+authorEmail,
		"GIT_COMMITTER_NAME="+authorName, "GIT_COMMITTER_EMAIL="+authorEmail,
	)
	commitArgs := []string{"commit-tree", tree, "-m", message}
	if parent != "" {
		commitArgs = []string{"commit-tree", tree, "-p", parent, "-m", message}
	}
	commit, err := run(commitEnv, "", commitArgs...)
	if err != nil {
		return err
	}

	if parent != "" {
		_, err = run(base, "", "update-ref", ref, commit, parent)
	} else {
		_, err = run(base, "", "update-ref", ref, commit)
	}
	return err
}

// ShowFile returns the content of filePath at the tip of branch in a bare repo,
// or ("", nil) if the file does not exist there.
func ShowFile(dataDir, repoDir, branch, filePath string) (string, error) {
	base := append(os.Environ(), "GIT_DIR="+repoDir)
	base = append(base, Env(dataDir)...)
	cmd := exec.Command("git", "show", branch+":"+filePath)
	cmd.Env = base
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		// missing file / unborn branch → empty, not an error
		return "", nil
	}
	return out.String(), nil
}
