package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/blaxstar/citric/internal/model"
)

// rewind is `git reflog show` and `git reset --hard` in one place: list the
// recent positions of HEAD, then jump back to one of them by reflog selector,
// SHA, Change-Id, or CL number.
//
// The CL/Change-Id forms are the reason this exists rather than telling people
// to use git directly — going back to "where CL-112 was" otherwise means
// reading a reflog, matching a Change-Id trailer by eye, and copying a SHA.

var (
	reCLNumber = regexp.MustCompile(`^(?i:CL-)?(\d{1,6})$`)
	reChangeID = regexp.MustCompile(`^I[0-9a-f]{40}$`)
)

var cmdRewind = &cobra.Command{
	Use:   "rewind [target]",
	Short: "Show recent HEAD positions, or hard-reset back to one",
	Long: `Show where HEAD has been, or move it back.

With no argument, lists recent HEAD positions (git reflog) newest first.

With a target, hard-resets the current branch to it. A target may be:

  HEAD@{2}                    a reflog selector, as listed by 'citric rewind'
  a1b2c3d                     any commit-ish git understands
  112  /  CL-112              a CL number — its current patchset commit
  I17d7b92b0326e7bc…          a Change-Id, matched against this branch's history

Resetting is destructive: commits above the target leave the branch and
uncommitted changes are discarded. citric refuses to run with a dirty working
tree unless --force, and always prints what is about to be dropped.

Nothing here touches the server. A CL that was already pushed still exists;
rewinding only moves your local branch.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		limit, _ := cmd.Flags().GetInt("limit")
		if len(args) == 0 {
			return showReflog(limit)
		}
		force, _ := cmd.Flags().GetBool("force")
		yes, _ := cmd.Flags().GetBool("yes")
		return rewindTo(args[0], force, yes)
	},
}

// showReflog prints recent HEAD positions with the commit subject alongside the
// reflog action, so a selector can be picked without a second git invocation.
func showReflog(limit int) error {
	out, err := gitOut("reflog", "show",
		"--format=%gd\x1f%h\x1f%gs\x1f%s", fmt.Sprintf("-n%d", limit))
	if err != nil {
		return fmt.Errorf("read reflog: %w", err)
	}
	if strings.TrimSpace(out) == "" {
		fmt.Println("No reflog entries — this branch has no recorded history yet.")
		return nil
	}

	head, _ := gitOut("rev-parse", "--short", "HEAD")
	fmt.Println("Recent HEAD positions (newest first):")
	fmt.Println()
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "\x1f")
		if len(f) < 4 {
			continue
		}
		selector, short, action, subject := f[0], f[1], f[2], f[3]
		marker := ""
		if short == head {
			marker = "  ← current"
		}
		fmt.Printf("  %-12s %-9s %-28s %s%s\n", selector, short, truncate(action, 28), truncate(subject, 48), marker)
	}
	fmt.Println()
	fmt.Println("Rewind with:  citric rewind HEAD@{2}      a position above")
	fmt.Println("              citric rewind 112          a CL number")
	fmt.Println("              citric rewind <sha>        any commit")
	return nil
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

// resolveRewindTarget turns a user-supplied target into a concrete SHA plus a
// human description of how it was resolved.
func resolveRewindTarget(target string) (sha, how string, err error) {
	switch {
	case reCLNumber.MatchString(target):
		id := reCLNumber.FindStringSubmatch(target)[1]
		sha, err = clHeadCommit(id)
		if err != nil {
			return "", "", err
		}
		return sha, "CL-" + id + " current patchset", nil

	case reChangeID.MatchString(target):
		// Change-Ids only exist in commit messages, so this is a local search —
		// it finds the commit on this branch, which is what you want to go back to.
		sha, err = gitOut("log", "--format=%H", "--grep=^Change-Id: "+target+"$", "-1", "HEAD")
		if err != nil || sha == "" {
			return "", "", fmt.Errorf("no commit on this branch carries %s", target)
		}
		return sha, "Change-Id " + target[:9] + "…", nil

	default:
		sha, err = gitOut("rev-parse", "--verify", "--quiet", target+"^{commit}")
		if err != nil || sha == "" {
			return "", "", fmt.Errorf("%q is not a commit, reflog selector, CL number, or Change-Id", target)
		}
		return sha, "commit-ish " + target, nil
	}
}

// clHeadCommit asks the server for a CL's current patchset commit.
func clHeadCommit(id string) (string, error) {
	client, err := newCritClient()
	if err != nil {
		return "", err
	}
	resp, err := client.get("/cl/" + id + "/patchsets")
	if err != nil {
		return "", err
	}
	var patchsets []*model.Patchset
	if err := decodeOK(resp, &patchsets); err != nil {
		return "", fmt.Errorf("look up CL-%s: %w", id, err)
	}
	if len(patchsets) == 0 {
		return "", fmt.Errorf("CL-%s has no patchsets", id)
	}
	latest := patchsets[0]
	for _, ps := range patchsets {
		if ps.Number > latest.Number {
			latest = ps
		}
	}
	if _, err := gitOut("rev-parse", "--verify", "--quiet", latest.CommitSHA+"^{commit}"); err != nil {
		return "", fmt.Errorf("CL-%s is at %s, which is not in this repo — fetch it first",
			id, shortSHA(latest.CommitSHA))
	}
	return latest.CommitSHA, nil
}

func rewindTo(target string, force, yes bool) error {
	sha, how, err := resolveRewindTarget(target)
	if err != nil {
		return err
	}

	head, err := gitOut("rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("read HEAD: %w", err)
	}
	if head == sha {
		fmt.Printf("Already at %s (%s) — nothing to do.\n", shortSHA(sha), how)
		return nil
	}

	dirty, _ := gitOut("status", "--porcelain")
	if dirty != "" && !force {
		fmt.Fprintln(os.Stderr, "Working tree has uncommitted changes:")
		fmt.Fprintln(os.Stderr)
		for _, l := range strings.Split(dirty, "\n") {
			fmt.Fprintln(os.Stderr, "  "+l)
		}
		fmt.Fprintln(os.Stderr)
		return fmt.Errorf("refusing to reset with a dirty working tree — commit, stash, or re-run with --force")
	}

	// Commits that will no longer be reachable from the branch. They survive in
	// the reflog (that is what makes this recoverable), but they leave the branch.
	dropped, _ := gitOut("log", "--oneline", sha+"..HEAD")

	subject, _ := gitOut("log", "-1", "--format=%s", sha)
	fmt.Printf("Rewinding to %s — %s\n", shortSHA(sha), how)
	fmt.Printf("  %s\n", subject)
	if dropped != "" {
		lines := strings.Split(dropped, "\n")
		fmt.Printf("\nLeaving the branch (%d commit%s):\n", len(lines), plural(len(lines)))
		for _, l := range lines {
			fmt.Printf("  %s\n", l)
		}
	}
	if dirty != "" {
		fmt.Printf("\nDiscarding uncommitted changes in %d file%s.\n",
			len(strings.Split(dirty, "\n")), plural(len(strings.Split(dirty, "\n"))))
	}

	if !yes {
		fmt.Print("\nProceed? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	if out, err := exec.Command("git", "reset", "--hard", sha).CombinedOutput(); err != nil {
		return fmt.Errorf("git reset --hard: %w\n%s", err, out)
	}
	fmt.Printf("✓ branch reset to %s\n", shortSHA(sha))
	if dropped != "" {
		fmt.Printf("  recover with: citric rewind %s\n", shortSHA(head))
	}
	return nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
