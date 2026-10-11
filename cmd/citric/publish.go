package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/blaxstar/citric/internal/brand"
)

// ---------------------------------------------------------------------------
// publish — mirror the current branch to an external git server (open source)
// ---------------------------------------------------------------------------
//
// citric's normal push targets refs/for/<branch> on the citric server, a review
// namespace no public host understands. `citric publish` is the one-way door out:
// it rebuilds the current history with the Change-Id trailers (that citric's
// commit-msg hook stamps on every commit) stripped, and pushes that clean copy
// to a plain external remote — GitHub, GitLab, wherever the repo gets open
// sourced.
//
// Stripping the trailers means the published commits get NEW SHAs — this is a
// mirror, not a shared branch. Re-publishing is deterministic (same input →
// same rewritten SHA, because author/committer identity and dates are
// preserved), so subsequent publishes fast-forward the external branch.
//
// Auth to the external host is whatever plain git already uses for it (an SSH
// key, a credential helper). citric's session token is NOT involved: setupGitAuth
// scopes it to http.<citric-server>.extraHeader, so it is never sent to another
// host.

var cmdPublish = &cobra.Command{
	Use:   "publish",
	Short: "Mirror the current branch to an external git server, Change-Ids stripped",
	Long: `Publish the current branch to a public git remote for open sourcing.

Reads the mirror target from .citric.toml:

  public_remote = "git@github.com:you/project.git"
  public_branch = "main"   # optional, defaults to "main"

It rewrites the history with citric's Change-Id trailers removed and .citric.toml
stripped from every tree — that file configures this citric server and means
nothing outside it — then pushes the result to the external remote. Commits left
empty by the removal are dropped. The published commits get new SHAs: this is a
one-way mirror, not a shared branch. Your local branch and its commits are left
completely untouched.

Authentication to the external host uses your normal git credentials for that
host; your citric token is never sent to it.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		remoteName, _ := cmd.Flags().GetString("remote")
		branchOverride, _ := cmd.Flags().GetString("branch")
		force, _ := cmd.Flags().GetBool("force")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		cfg, err := loadRepoPublishConfig()
		if err != nil {
			return err
		}
		if cfg.PublicRemote == "" {
			return fmt.Errorf("no public_remote set in .citric.toml — add e.g.\n\n" +
				"  public_remote = \"git@github.com:you/project.git\"\n" +
				"  public_branch = \"main\"\n")
		}
		destBranch := cfg.PublicBranch
		if branchOverride != "" {
			destBranch = branchOverride
		}
		if destBranch == "" {
			destBranch = "main"
		}

		// Resolve the tip we're mirroring, for reporting.
		srcBranch, err := gitOut("rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			return fmt.Errorf("resolve HEAD: %w", err)
		}
		tip, err := gitOut("rev-parse", "HEAD")
		if err != nil {
			return fmt.Errorf("resolve HEAD sha: %w", err)
		}

		// Make sure a git remote points at the configured URL, adding or
		// re-pointing it as needed so a plain `git push <remote>` works too.
		if err := ensureRemote(remoteName, cfg.PublicRemote); err != nil {
			return err
		}

		opts, err := publishOptionsFrom(cfg)
		if err != nil {
			return err
		}
		fmt.Printf("Rewriting %s (%s) without Change-Id trailers or %s...\n",
			srcBranch, shortSHA(tip), strings.Join(opts.exclude, ", "))
		newTip, kept, dropped, err := rewriteForPublish(tip, opts)
		if err != nil {
			return fmt.Errorf("rewrite history: %w", err)
		}
		fmt.Printf("✓ rebuilt %d commit(s) → %s\n", kept, shortSHA(newTip))
		if dropped > 0 {
			fmt.Printf("  (%d commit(s) dropped — nothing left once excluded paths were removed)\n", dropped)
		}
		if opts.forbid != nil {
			// With publish_author set, every published identity is that one,
			// chosen on purpose, so only contents and messages are checked.
			if hits, err := forbiddenInPublish(newTip, opts.forbid, opts.authorName != ""); err != nil {
				return err
			} else if len(hits) > 0 {
				fmt.Fprintln(os.Stderr, "publish_forbid matched in what would be published:")
				for _, h := range hits {
					fmt.Fprintln(os.Stderr, "  "+h)
				}
				return fmt.Errorf("refusing to publish: %d match(es) for publish_forbid", len(hits))
			}
			fmt.Println("✓ publish_forbid: no matches")
		}

		refspec := newTip + ":refs/heads/" + destBranch
		pushArgs := []string{"push"}
		if force {
			pushArgs = append(pushArgs, "--force")
		}
		if dryRun {
			pushArgs = append(pushArgs, "--dry-run")
		}
		pushArgs = append(pushArgs, remoteName, refspec)

		fmt.Printf("Pushing → %s %s\n", cfg.PublicRemote, destBranch)
		push := exec.Command("git", pushArgs...)
		push.Stdout = os.Stdout
		var stderr bytes.Buffer
		push.Stderr = io.MultiWriter(os.Stderr, &stderr)
		if err := push.Run(); err != nil {
			if strings.Contains(stderr.String(), "non-fast-forward") || strings.Contains(stderr.String(), "rejected") {
				return fmt.Errorf("push rejected: %w\n\n"+
					"The external branch has commits your mirror doesn't. If you rewrote\n"+
					"history intentionally, re-run with --force to overwrite it.", err)
			}
			return fmt.Errorf("git push failed: %w", err)
		}
		if dryRun {
			fmt.Println("✓ dry run only — nothing was pushed")
		} else {
			fmt.Printf("✓ published %s → %s (%s)\n", srcBranch, destBranch, cfg.PublicRemote)
		}
		return nil
	},
}

// ensureRemote makes the named git remote exist and point at url, without
// disturbing any other remote the user has configured.
func ensureRemote(name, url string) error {
	if cur, err := gitOut("remote", "get-url", name); err == nil {
		if strings.TrimSpace(cur) == url {
			return nil
		}
		if out, err := exec.Command("git", "remote", "set-url", name, url).CombinedOutput(); err != nil {
			return fmt.Errorf("git remote set-url %s: %w\n%s", name, err, out)
		}
		return nil
	}
	if out, err := exec.Command("git", "remote", "add", name, url).CombinedOutput(); err != nil {
		return fmt.Errorf("git remote add %s: %w\n%s", name, err, out)
	}
	return nil
}

// publishExcludePaths are dropped from every published tree. The repo config
// (.citric.toml, or .crit.toml before the rename) configures this server —
// build commands, the review namespace, the mirror target itself — and means
// nothing outside it, so it has no business in the public mirror. It is
// stripped from the content the same way Change-Ids are stripped from the
// messages: the internal repo keeps it, the mirror never sees it.
var publishExcludePaths = []string{brand.RepoConfigFile, brand.LegacyRepoConfigFile}

// publishOptions is the resolved publish configuration.
type publishOptions struct {
	exclude     []string
	base        string // full SHA, or "" for the whole history
	authorName  string
	authorEmail string
	forbid      *regexp.Regexp
}

func publishOptionsFrom(cfg *RepoPublishConfig) (publishOptions, error) {
	opts := publishOptions{exclude: append(append([]string{}, publishExcludePaths...), cfg.PublishExclude...)}
	if cfg.PublishBase != "" {
		sha, err := gitOut("rev-parse", "--verify", cfg.PublishBase+"^{commit}")
		if err != nil {
			return opts, fmt.Errorf("publish_base %q is not a commit here: %w", cfg.PublishBase, err)
		}
		opts.base = strings.TrimSpace(sha)
	}
	if cfg.PublishAuthor != "" {
		name, email, ok := strings.Cut(cfg.PublishAuthor, "<")
		email = strings.TrimSuffix(strings.TrimSpace(email), ">")
		if !ok || strings.TrimSpace(name) == "" || email == "" {
			return opts, fmt.Errorf("publish_author must look like \"Name <email>\", got %q", cfg.PublishAuthor)
		}
		opts.authorName, opts.authorEmail = strings.TrimSpace(name), email
	}
	if cfg.PublishForbid != "" {
		re, err := regexp.Compile("(?i)" + cfg.PublishForbid)
		if err != nil {
			return opts, fmt.Errorf("publish_forbid: %w", err)
		}
		opts.forbid = re
	}
	return opts, nil
}

// forbiddenInPublish lists every place re matches in the history that would
// be published: file contents at each commit, commit messages, and (unless
// skipIdentity) author and committer identities.
func forbiddenInPublish(tip string, re *regexp.Regexp, skipIdentity bool) ([]string, error) {
	var hits []string
	shas, err := gitOut("rev-list", tip)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, sha := range strings.Fields(shas) {
		format := "--format=%an <%ae> / %cn <%ce>%n%B"
		if skipIdentity {
			format = "--format=%B"
		}
		meta, err := gitOut("show", "-s", format, sha)
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(meta, "\n") {
			if re.MatchString(line) {
				hits = append(hits, fmt.Sprintf("commit %s: %s", shortSHA(sha), strings.TrimSpace(line)))
			}
		}
		// git grep over the commit's tree; exit 1 just means no match.
		out, err := exec.Command("git", "grep", "-n", "-I", "-i", "-E", re.String()[len("(?i)"):], sha).Output()
		if err != nil {
			var ee *exec.ExitError
			if !errors.As(err, &ee) || ee.ExitCode() != 1 {
				return nil, fmt.Errorf("git grep %s: %w", shortSHA(sha), err)
			}
		}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line == "" {
				continue
			}
			// Report each file:line once, from the newest commit carrying it.
			key := line[strings.Index(line, ":")+1:]
			if !seen[key] {
				seen[key] = true
				hits = append(hits, line)
			}
		}
	}
	return hits, nil
}

// treeWithoutPath returns tree with one path removed, via a throwaway index so
// the caller's real index is untouched. Removing a path the tree does not have
// is a no-op that yields the same tree, which keeps the rewrite idempotent.
func treeWithoutPath(tree, path string) (string, error) {
	idx, err := os.CreateTemp("", "citric-publish-index-*")
	if err != nil {
		return "", fmt.Errorf("temp index: %w", err)
	}
	idxPath := idx.Name()
	idx.Close()
	defer os.Remove(idxPath)

	env := append(os.Environ(), "GIT_INDEX_FILE="+idxPath)
	if _, err := gitOutEnvStdin(env, "", "read-tree", tree); err != nil {
		return "", fmt.Errorf("read-tree %s: %w", shortSHA(tree), err)
	}
	// path may name a file or a directory: drop every index entry under it.
	listed, err := gitOutEnvStdin(env, "", "ls-files", "-z", "--", path)
	if err != nil {
		return "", fmt.Errorf("list %s: %w", path, err)
	}
	var files []string
	for _, f := range strings.Split(listed, "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	if len(files) > 0 {
		args := append([]string{"update-index", "--force-remove", "--"}, files...)
		if _, err := gitOutEnvStdin(env, "", args...); err != nil {
			return "", fmt.Errorf("drop %s: %w", path, err)
		}
	}
	newTree, err := gitOutEnvStdin(env, "", "write-tree")
	if err != nil {
		return "", fmt.Errorf("write-tree: %w", err)
	}
	return newTree, nil
}

// rewriteForPublish rebuilds every commit reachable from tip with Change-Id
// trailers stripped from the message and the excluded paths stripped from the
// tree. It returns the rewritten tip, the number of commits kept, and the number
// dropped. Only new objects are written — no refs are touched — so the working
// repository is left exactly as it was.
//
// The rewrite is deterministic: author/committer identity and dates are
// preserved, so the same source history always yields the same rewritten SHAs
// and mirror pushes fast-forward.
//
// A commit whose entire content was the excluded file rewrites to its parent's
// tree. Emitting it would put a commit in the public history that does nothing
// and explains nothing ("commit publish config in .citric.toml"), so it is dropped
// and its children are reparented onto its parent instead.
func rewriteForPublish(tip string, opts publishOptions) (newTip string, kept, dropped int, err error) {
	stripTree := func(tree, at string) (string, error) {
		for _, path := range opts.exclude {
			tree, err = treeWithoutPath(tree, path)
			if err != nil {
				return "", fmt.Errorf("strip %s from %s: %w", path, shortSHA(at), err)
			}
		}
		return tree, nil
	}
	identity := func(f []string) []string {
		if opts.authorName != "" {
			f[2], f[3], f[5], f[6] = opts.authorName, opts.authorEmail, opts.authorName, opts.authorEmail
		}
		return []string{
			"GIT_AUTHOR_NAME=" + f[2], "GIT_AUTHOR_EMAIL=" + f[3], "GIT_AUTHOR_DATE=" + f[4],
			"GIT_COMMITTER_NAME=" + f[5], "GIT_COMMITTER_EMAIL=" + f[6], "GIT_COMMITTER_DATE=" + f[7],
		}
	}
	readMeta := func(sha string) ([]string, error) {
		// %T tree, %P space-separated parents, then the author/committer identity.
		meta, err := gitOut("show", "-s", "--format=%T%n%P%n%an%n%ae%n%aI%n%cn%n%ce%n%cI", sha)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", shortSHA(sha), err)
		}
		f := strings.SplitN(meta, "\n", 8)
		if len(f) < 8 {
			return nil, fmt.Errorf("unexpected metadata for %s", shortSHA(sha))
		}
		return f, nil
	}

	remap := map[string]string{}  // old commit → rewritten commit
	treeOf := map[string]string{} // rewritten commit → its tree

	// Oldest-first so every commit's parents are rewritten before it is.
	rangeSpec := tip
	if opts.base != "" {
		if _, err := gitOut("merge-base", "--is-ancestor", opts.base, tip); err != nil {
			return "", 0, 0, fmt.Errorf("publish_base %s is not an ancestor of %s", shortSHA(opts.base), shortSHA(tip))
		}
		// The whole history up to base becomes one root commit, dated like
		// base so the result is the same every time.
		f, err := readMeta(opts.base)
		if err != nil {
			return "", 0, 0, err
		}
		tree, err := stripTree(f[0], opts.base)
		if err != nil {
			return "", 0, 0, err
		}
		root, err := gitOutEnvStdin(append(os.Environ(), identity(f)...), "Initial public release\n", "commit-tree", tree)
		if err != nil {
			return "", 0, 0, fmt.Errorf("commit-tree for the initial commit: %w", err)
		}
		remap[opts.base] = root
		treeOf[root] = tree
		kept++
		rangeSpec = opts.base + ".." + tip
	}
	out, err := gitOut("rev-list", "--reverse", "--topo-order", rangeSpec)
	if err != nil {
		return "", 0, 0, fmt.Errorf("rev-list: %w", err)
	}
	shas := strings.Fields(out)
	if len(shas) == 0 && opts.base == "" {
		return "", 0, 0, fmt.Errorf("no commits to publish")
	}

	for _, old := range shas {
		f, err := readMeta(old)
		if err != nil {
			return "", 0, 0, err
		}
		parents := strings.Fields(f[1])
		newTree, err := stripTree(f[0], old)
		if err != nil {
			return "", 0, 0, err
		}
		env := append(os.Environ(), identity(f)...)

		// The commit message, with Change-Id trailers removed and any blank line
		// left where the trailer block was trimmed off the end.
		msg, err := gitOut("show", "-s", "--format=%B", old)
		if err != nil {
			return "", 0, 0, fmt.Errorf("read message %s: %w", shortSHA(old), err)
		}
		msg = strings.TrimRight(stripChangeID(msg), "\n") + "\n"

		newParents := make([]string, 0, len(parents))
		for _, p := range parents {
			np, ok := remap[p]
			if !ok {
				// A parent outside the reachable set can't happen for a full
				// rev-list from the tip, but guard rather than emit a bad graph.
				return "", 0, 0, fmt.Errorf("parent %s of %s was not rewritten", shortSHA(p), shortSHA(old))
			}
			newParents = append(newParents, np)
		}

		// Nothing left once the excluded file is gone — reparent onto the parent
		// rather than publishing an empty commit. Merges are left alone: dropping
		// one would change the shape of the history, not just its content.
		if len(newParents) == 1 && treeOf[newParents[0]] == newTree {
			remap[old] = newParents[0]
			dropped++
			continue
		}

		args := []string{"commit-tree", newTree}
		for _, np := range newParents {
			args = append(args, "-p", np)
		}

		newSHA, err := gitOutEnvStdin(env, msg, args...)
		if err != nil {
			return "", 0, 0, fmt.Errorf("commit-tree for %s: %w", shortSHA(old), err)
		}
		remap[old] = newSHA
		treeOf[newSHA] = newTree
		kept++
	}

	last := opts.base
	if len(shas) > 0 {
		last = shas[len(shas)-1]
	}
	newTip = remap[last]
	if newTip == "" {
		return "", 0, 0, fmt.Errorf("nothing to publish: every commit was empty once excluded paths were removed")
	}
	return newTip, kept, dropped, nil
}
