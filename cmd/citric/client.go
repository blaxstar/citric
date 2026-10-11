package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/blaxstar/citric/internal/brand"
	gitutil "github.com/blaxstar/citric/internal/git"
	"github.com/blaxstar/citric/internal/model"
)

// ---------------------------------------------------------------------------
// auth setup
// ---------------------------------------------------------------------------

var cmdAuth = &cobra.Command{
	Use:   "auth",
	Short: "Authentication commands",
}

var cmdAuthToken = &cobra.Command{
	Use:   "token",
	Short: "Print the stored auth token",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadClientConfig()
		if err != nil || cfg.Token == "" {
			return fmt.Errorf("not configured — run 'citric auth login' first")
		}
		fmt.Println(cfg.Token)
		return nil
	},
}

var cmdAuthLogin = &cobra.Command{
	Use:   "login",
	Short: "Authenticate the CLI with a token from the citric web UI",
	Long: `Sign in to the citric web UI, open the avatar/user menu and choose
"CLI access" to generate a token, then:

  citric auth login --server https://citric.example.com --token <paste>

Omit --token to be prompted to paste it. The server defaults to $CITRIC_SERVER,
then to the server you last logged in to.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		server, _ := cmd.Flags().GetString("server")
		token, _ := cmd.Flags().GetString("token")
		reader := bufio.NewReader(os.Stdin)

		if server == "" {
			existing, _ := loadClientConfig()
			def := brand.Getenv("SERVER")
			if def == "" {
				def = existing.Server
			}
			if def != "" {
				fmt.Printf("Server URL [%s]: ", def)
			} else {
				fmt.Print("Server URL (e.g. https://citric.example.com): ")
			}
			line, _ := reader.ReadString('\n')
			if line = strings.TrimSpace(line); line != "" {
				server = line
			} else {
				server = def
			}
			if server == "" {
				return fmt.Errorf("a server URL is required: pass --server or set CITRIC_SERVER")
			}
		}
		server = strings.TrimRight(server, "/")

		if token == "" {
			fmt.Print("Paste CLI token: ")
			line, _ := reader.ReadString('\n')
			token = strings.TrimSpace(line)
		}
		if token == "" {
			return fmt.Errorf("token is required")
		}

		// Validate the token by resolving the current user.
		client := &critClient{cfg: &ClientConfig{Server: server, Token: token}, http: &http.Client{}}
		resp, err := client.get("/auth/me")
		if err != nil {
			return fmt.Errorf("validate token: %w", err)
		}
		var me struct {
			Username string `json:"username"`
		}
		if err := decodeOK(resp, &me); err != nil {
			return fmt.Errorf("token rejected: %w", err)
		}
		if me.Username == "" {
			return fmt.Errorf("token rejected: empty user")
		}

		cfg := &ClientConfig{Server: server, Username: me.Username, Token: token}
		if err := saveClientConfig(cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		fmt.Printf("Authenticated as %s. Config saved to %s\n", me.Username, defaultClientConfigPath())
		return nil
	},
}

// ---------------------------------------------------------------------------
// push
// ---------------------------------------------------------------------------

var cmdPush = &cobra.Command{
	Use:   "push",
	Short: "Push current HEAD as a new CL or new patchset",
	RunE: func(cmd *cobra.Command, args []string) error {
		base, _ := cmd.Flags().GetString("base")
		remote, _ := cmd.Flags().GetString("remote")
		noVerify, _ := cmd.Flags().GetBool("no-verify")
		return gitPushForReview(remote, base, noVerify)
	},
}

// ---------------------------------------------------------------------------
// update
// ---------------------------------------------------------------------------

var cmdUpdate = &cobra.Command{
	Use:   "update",
	Short: "Download and install the latest citric binary over this one",
	Long: `Replaces the currently running citric binary in place with the latest
build from the server — the CLI equivalent of:

  curl -fsSL <server>/install.sh | sh -s -- --update

Uses the server already configured via 'citric auth login' and overwrites
wherever this binary is actually running from (resolved from the OS, not
guessed from PATH), so it works regardless of how citric was first installed.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadClientConfig()
		if err != nil {
			return err
		}
		if cfg.Server == "" {
			return fmt.Errorf("not configured — run 'citric auth login' first")
		}

		osName, arch := runtime.GOOS, runtime.GOARCH
		switch osName {
		case "linux", "darwin":
		default:
			return fmt.Errorf("unsupported OS: %s", osName)
		}
		switch arch {
		case "amd64", "arm64":
		default:
			return fmt.Errorf("unsupported architecture: %s", arch)
		}

		self, err := os.Executable()
		if err != nil {
			return fmt.Errorf("locate running binary: %w", err)
		}
		if resolved, err := filepath.EvalSymlinks(self); err == nil {
			self = resolved
		}

		url := fmt.Sprintf("%s/dl/%s-%s-%s", strings.TrimRight(cfg.Server, "/"), brand.Name, osName, arch)
		fmt.Printf("downloading %s\n", url)

		resp, err := http.Get(url)
		if err != nil {
			return fmt.Errorf("download: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("download failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
		}

		// Written into the same directory as the running binary, not /tmp, so the
		// rename below is an atomic same-filesystem move rather than a cross-device
		// copy that could leave a half-written binary in place if it's interrupted.
		tmp, err := os.CreateTemp(filepath.Dir(self), ".citric-update-*")
		if err != nil {
			return fmt.Errorf("create temp file: %w", err)
		}
		tmpPath := tmp.Name()
		defer os.Remove(tmpPath) // no-op once the rename below succeeds

		if _, err := io.Copy(tmp, resp.Body); err != nil {
			tmp.Close()
			return fmt.Errorf("write download: %w", err)
		}
		if err := tmp.Close(); err != nil {
			return fmt.Errorf("write download: %w", err)
		}
		if err := os.Chmod(tmpPath, 0o755); err != nil {
			return fmt.Errorf("chmod: %w", err)
		}
		if err := os.Rename(tmpPath, self); err != nil {
			return fmt.Errorf("install over %s: %w (try with sudo, or fall back to the curl installer with --update)", self, err)
		}

		fmt.Printf("✓ citric updated → %s\n", self)
		return nil
	},
}

// setupGitAuth exports the citric session token to git as an HTTP extra header,
// scoped to the citric server URL. This lets clone/fetch/pull/push of restricted
// repos authenticate. It's harmless for open repos (the server ignores it) and
// never leaks the token to other hosts (scoped via http.<server>.extraHeader).
// Uses git's env-based config so every git subprocess picks it up automatically,
// including the raw `git push` that 'citric import' shells out to.
//
// The env config is the ONE source of truth for the header. git concatenates
// extraHeader values across every config scope it reads, so a copy persisted in
// .git/config (as citric <= 2026-06 did) is additive, not overriding: the request
// goes out with two Authorization headers, nginx treats Authorization as a
// unique header line and rejects it with a bare 400 before citric ever sees it.
// Older citric tried to suppress that with an empty-value reset entry, but that
// leans on git's "empty extraHeader clears the list" semantics, which do not
// hold uniformly across git versions. Purging is deterministic; do that instead.
func setupGitAuth() {
	cfg, err := loadClientConfig()
	if err != nil || cfg.Token == "" || cfg.Server == "" {
		return
	}
	base := strings.TrimRight(cfg.Server, "/")
	key := "http." + base + ".extraHeader"

	purgePersistedAuthHeaders(key)

	n := 0
	if c := os.Getenv("GIT_CONFIG_COUNT"); c != "" {
		n, _ = strconv.Atoi(c)
	}
	os.Setenv("GIT_CONFIG_KEY_"+strconv.Itoa(n), key)
	os.Setenv("GIT_CONFIG_VALUE_"+strconv.Itoa(n), "Authorization: Bearer "+cfg.Token)
	os.Setenv("GIT_CONFIG_COUNT", strconv.Itoa(n+1))
}

// purgePersistedAuthHeaders drops citric Authorization headers that older versions
// wrote into the repo's .git/config, leaving the env config as the only writer.
// The value regex means an extraHeader the user set for their own purposes is
// left alone. No-ops outside a work tree.
func purgePersistedAuthHeaders(scopedKey string) {
	if exec.Command("git", "rev-parse", "--git-dir").Run() != nil {
		return
	}
	for _, key := range []string{scopedKey, "http.extraHeader"} {
		exec.Command("git", "config", "--local", "--unset-all", key,
			"^Authorization: Bearer ").Run()
	}
}

// gitPushForReview pushes HEAD to the review namespace for the given base
// branch. Unless noVerify is set, it first runs the repo's own .citric.toml
// tools locally and refuses to push if any fail — see brokengate.md.
func gitPushForReview(remote, base string, noVerify bool) error {
	if noVerify {
		fmt.Fprintln(os.Stderr, "⚠ skipping local .citric.toml verification (--no-verify)")
	} else if err := verifyBeforePush(); err != nil {
		return err
	}

	refspec := "HEAD:refs/for/" + base
	// refs/for/* is a submission namespace, not a real branch — always force.
	pushCmd := exec.Command("git", "push", "--force", remote, refspec)
	pushCmd.Stdout = os.Stdout
	var stderr bytes.Buffer
	pushCmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
	if err := pushCmd.Run(); err != nil {
		if hint := diagnosePushFailure(stderr.String()); hint != "" {
			return fmt.Errorf("git push failed: %w\n\n%s", err, hint)
		}
		return fmt.Errorf("git push failed: %w", err)
	}
	return nil
}

// diagnosePushFailure maps git's opaque transport errors onto the cause and the
// fix. git reports what the wire said ("RPC failed; HTTP 400"), never why, and
// for the 400 case the response is generated by nginx and never reaches citric —
// so there is no server-side log to correlate against either.
func diagnosePushFailure(stderr string) string {
	switch {
	case strings.Contains(stderr, "HTTP 400"):
		return "The server rejected the request before it reached citric (HTTP 400).\n" +
			"Cause: git is sending the Authorization header twice — usually a stale copy\n" +
			"left in .git/config by an older citric, on top of the one citric injects.\n" +
			"Check:  git config --local --get-regexp 'extraHeader'\n" +
			"Fix:    git config --local --unset-all http.extraHeader\n" +
			"        (and the same for any http.<server>.extraHeader it lists)\n" +
			"An up-to-date citric purges these automatically — 'citric version' to check."
	case strings.Contains(stderr, "HTTP 401"), strings.Contains(stderr, "Authentication failed"):
		return "The server rejected your token (HTTP 401). Run 'citric auth setup' to re-authenticate."
	case strings.Contains(stderr, "HTTP 403"):
		return "You are authenticated but not allowed to push to this repo (HTTP 403).\n" +
			"Ask an admin to grant you access."
	case strings.Contains(stderr, "HTTP 413"):
		return "The push exceeds the server's request size limit (HTTP 413).\n" +
			"Split the import into smaller pushes, or ask an admin to raise\n" +
			"client_max_body_size on the citric vhost."
	case strings.Contains(stderr, "direct pushes to"):
		return "Direct pushes to a branch are blocked. Use 'citric push', which targets\n" +
			"refs/for/<branch> and opens a CL."
	case strings.Contains(stderr, "could not read Username"):
		return "No citric credentials on this machine. Run 'citric auth setup' first."
	}
	return ""
}

// headChangeID returns the Change-Id value ("I...") from the tip commit,
// or "" if there is no commit yet or no Change-Id present.
func headChangeID() string {
	out, err := exec.Command("git", "log", "-1", "--format=%B").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Change-Id:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Change-Id:"))
		}
	}
	return ""
}

// headSubject returns the subject (first line) of the tip commit's message,
// or "" if there is no commit yet.
func headSubject() string {
	out, err := exec.Command("git", "log", "-1", "--format=%s").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// buildCommitMessage assembles a commit message from a subject and any number
// of body paragraphs, separating each with exactly one blank line.
//
// This exists because composing a multi-paragraph message as a single -m string
// means embedding literal newlines in a shell argument, where a dropped or
// doubled "\n\n" silently produces a run-on paragraph or a lost one. Passing
// each paragraph as its own argument makes that unrepresentable.
//
// Paragraphs are trimmed and empty ones dropped, so a stray -p "" cannot emit a
// double blank line. Internal newlines within a paragraph are preserved as-is:
// a caller who wants hard-wrapped prose or a list inside one paragraph keeps it.
func buildCommitMessage(subject string, paragraphs []string, wrap int) string {
	parts := make([]string, 0, len(paragraphs)+1)
	if t := strings.TrimSpace(subject); t != "" {
		parts = append(parts, t) // never wrapped: the subject is one line by definition
	}
	for _, para := range paragraphs {
		if p := strings.TrimSpace(para); p != "" {
			parts = append(parts, wrapParagraph(p, wrap))
		}
	}
	return strings.Join(parts, "\n\n")
}

// wrapParagraph hard-wraps a paragraph at width columns, the git convention a
// hand-written body follows. A paragraph is left exactly as given when width is
// <= 0 (--wrap=0) or when it already contains a newline: internal newlines mean
// the caller formatted it deliberately — a list, a code block, an aligned table
// — and reflowing that would destroy it.
//
// Words longer than the width (a URL, a long path) are never broken; they get a
// line to themselves and overflow, which is what every other wrapper does and
// what keeps them copy-pasteable.
//
// Width is counted in runes, not bytes. These messages are full of em dashes and
// curly quotes, and counting their UTF-8 bytes would wrap several columns early
// on exactly the lines that contain them.
func wrapParagraph(para string, width int) string {
	if width <= 0 || strings.Contains(para, "\n") {
		return para
	}
	words := strings.Fields(para)
	if len(words) == 0 {
		return para
	}
	var b strings.Builder
	lineLen := 0
	for i, w := range words {
		n := utf8.RuneCountInString(w)
		switch {
		case i == 0:
			b.WriteString(w)
			lineLen = n
		case lineLen+1+n > width:
			b.WriteString("\n")
			b.WriteString(w)
			lineLen = n
		default:
			b.WriteString(" ")
			b.WriteString(w)
			lineLen += 1 + n
		}
	}
	return b.String()
}

// withChangeID appends a Change-Id trailer unless the message already carries
// one. Appending unconditionally is how a reworded amend ends up with the
// trailer twice: the message being passed back in is often the previous message,
// which already ends in one.
func withChangeID(msg, cid string) string {
	if cid == "" {
		return msg
	}
	for _, line := range strings.Split(msg, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "Change-Id:") {
			return msg
		}
	}
	return strings.TrimRight(msg, "\n") + "\n\nChange-Id: " + cid
}

// ---------------------------------------------------------------------------
// commit
// ---------------------------------------------------------------------------

var cmdCommit = &cobra.Command{
	Use:   "commit",
	Short: "Create or update a change (keeps a stable Change-Id)",
	Long: `Commit changes for review without juggling git flags.

  citric commit -m "message"        start a new change (new commit + Change-Id)
  citric commit                     update the current change (amend, keep message)
  citric commit --amend -m "msg"    update the current change AND reword it
  citric commit -a ...              stage ALL changes first (incl. new files)
  citric commit --push ...          commit, then push for review

Multi-paragraph messages are easier to get right with -t/-p than with one -m
string, where a miscounted newline silently merges two paragraphs or drops one:

  citric commit -t "subject line" \
      -p "First body paragraph." \
      -p "Second body paragraph."

Each -p becomes its own paragraph, separated by exactly one blank line, in the
order given, hard-wrapped at 72 columns (--wrap=0 to keep long lines). A -p that
already contains newlines is never rewrapped — a list or an aligned block is
left exactly as written. -t alone is just a subject. On an amend, -p without -t
rewords the body and keeps the current subject. -m takes a whole message and
cannot be combined with -t/-p.

Updating amends the tip commit so its Change-Id is preserved — a re-push lands
as a new patchset on the same CL instead of creating a new one.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		msg, _ := cmd.Flags().GetString("message")
		title, _ := cmd.Flags().GetString("title")
		paragraphs, _ := cmd.Flags().GetStringArray("paragraph")
		wrap, _ := cmd.Flags().GetInt("wrap")
		all, _ := cmd.Flags().GetBool("all")
		amend, _ := cmd.Flags().GetBool("amend")
		push, _ := cmd.Flags().GetBool("push")
		base, _ := cmd.Flags().GetString("base")
		noVerify, _ := cmd.Flags().GetBool("no-verify")

		// -t/-p compose the same message -m takes whole; accepting both would
		// leave which one wins up to argument order.
		if msg != "" && (title != "" || len(paragraphs) > 0) {
			return fmt.Errorf("-m cannot be combined with -t/-p — use one or the other")
		}
		if title != "" || len(paragraphs) > 0 {
			subject := title
			if subject == "" {
				// -p alone rewords the body and keeps the existing subject.
				// Only meaningful on an amend: without a commit to take the
				// subject from there is no message at all.
				if !amend {
					return fmt.Errorf("-p needs a subject: pass -t \"title\", or --amend to keep the current one")
				}
				if subject = headSubject(); subject == "" {
					return fmt.Errorf("no commit to take a subject from — pass -t \"title\"")
				}
			}
			msg = buildCommitMessage(subject, paragraphs, wrap)
		}

		// -a stages everything, including untracked files (unlike `git commit -a`,
		// which only catches modified/deleted tracked files). This is the common
		// "include the new files I just created" case.
		if all {
			add := exec.Command("git", "add", "-A")
			add.Stdout, add.Stderr = os.Stdout, os.Stderr
			if err := add.Run(); err != nil {
				return fmt.Errorf("git add -A: %w", err)
			}
		}

		// -m without --amend = new change; otherwise update the current one.
		newChange := msg != "" && !amend

		gitArgs := []string{"commit"}

		if newChange {
			gitArgs = append(gitArgs, "-m", msg)
		} else {
			cid := headChangeID()
			if cid == "" {
				return fmt.Errorf("no current change to update — start one with: citric commit -m \"message\"")
			}
			gitArgs = append(gitArgs, "--amend")
			if msg != "" {
				// Reword but keep the existing Change-Id so it stays the same CL.
				// The commit-msg hook sees the trailer already present and leaves it.
				gitArgs = append(gitArgs, "-m", withChangeID(msg, cid))
			} else {
				gitArgs = append(gitArgs, "--no-edit")
			}
		}

		c := exec.Command("git", gitArgs...)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		c.Stdin = os.Stdin
		if err := c.Run(); err != nil {
			return fmt.Errorf("git commit failed: %w", err)
		}

		if push {
			return gitPushForReview("origin", base, noVerify)
		}
		return nil
	},
}

// ---------------------------------------------------------------------------
// reparent
// ---------------------------------------------------------------------------

var cmdReparent = &cobra.Command{
	Use:   "reparent <cl-id>",
	Short: "Attach HEAD commit to an existing CL (fixes a forgotten --amend)",
	Long: `Rewrite the Change-Id trailer of the HEAD commit to match an existing CL,
so the next 'citric push' lands as a new patchset on that CL instead of
creating a new one.

Use this when you ran 'citric commit -m "..."' but meant to run
'citric commit --amend -m "..."'.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		push, _ := cmd.Flags().GetBool("push")
		base, _ := cmd.Flags().GetString("base")
		noVerify, _ := cmd.Flags().GetBool("no-verify")

		client, err := newCritClient()
		if err != nil {
			return err
		}
		if err := client.checkSetup(); err != nil {
			return err
		}

		resp, err := client.get("/cl/" + args[0])
		if err != nil {
			return fmt.Errorf("fetching CL: %w", err)
		}
		var cl model.CL
		if err := decodeOK(resp, &cl); err != nil {
			return err
		}
		if cl.ChangeID == "" {
			return fmt.Errorf("CL-%s has no Change-Id recorded", args[0])
		}
		if cl.Status != model.CLStatusOpen {
			return fmt.Errorf("CL-%d is %s — can only reparent onto an open CL", cl.ID, cl.Status)
		}

		// Read current HEAD commit message.
		msgBytes, err := exec.Command("git", "log", "-1", "--format=%B").Output()
		if err != nil {
			return fmt.Errorf("reading HEAD commit: %w", err)
		}

		// Strip any existing Change-Id trailer, then append the target one.
		var lines []string
		for _, line := range strings.Split(strings.TrimRight(string(msgBytes), "\n"), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "Change-Id:") {
				lines = append(lines, line)
			}
		}
		// Trim trailing blank lines so the trailer sits cleanly at the end.
		for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
			lines = lines[:len(lines)-1]
		}
		newMsg := strings.Join(lines, "\n") + "\n\nChange-Id: " + cl.ChangeID

		c := exec.Command("git", "commit", "--amend", "-m", newMsg)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			return fmt.Errorf("git commit --amend: %w", err)
		}

		fmt.Printf("HEAD commit reparented to CL-%d (%s)\n", cl.ID, cl.ChangeID)

		if push {
			return gitPushForReview("origin", base, noVerify)
		}
		return nil
	},
}

// ---------------------------------------------------------------------------
// rebase
// ---------------------------------------------------------------------------

var clRefPattern = regexp.MustCompile(`(?i)^(cl-)?\d+$`)

// parseCLID reports whether s looks like a CL reference ("282" or "CL-282")
// and, if so, returns the numeric id.
func parseCLID(s string) (int64, bool) {
	if !clRefPattern.MatchString(s) {
		return 0, false
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(strings.ToUpper(s), "CL-"), 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}

// currentRepoName derives the repo name from the "origin" remote URL,
// matching the <server>/git/<repo>.git convention citric init/serve use —
// so a CL's Repo field can be checked against it before fetching a ref that
// only ever exists on that CL's own repo, not whatever's checked out here.
func currentRepoName() (string, error) {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return "", fmt.Errorf("no 'origin' remote configured")
	}
	return strings.TrimSuffix(path.Base(strings.TrimSpace(string(out))), ".git"), nil
}

// fetchLatestPatchset fetches a CL's latest patchset ref from origin and
// returns its commit SHA and patchset number, so a caller can rebase or
// check out onto exactly what's currently on the server for that CL.
func fetchLatestPatchset(clID int64) (sha string, psNumber int, err error) {
	client, err := newCritClient()
	if err != nil {
		return "", 0, err
	}
	if err := client.checkSetup(); err != nil {
		return "", 0, err
	}

	// The ref we're about to fetch only exists on the CL's own repo — fetching
	// it from whatever's checked out here fails with a bare git error
	// ("couldn't find remote ref") that gives no hint why. Check first.
	clResp, err := client.get(fmt.Sprintf("/cl/%d", clID))
	if err != nil {
		return "", 0, fmt.Errorf("fetching CL-%d: %w", clID, err)
	}
	var cl model.CL
	if err := decodeOK(clResp, &cl); err != nil {
		return "", 0, err
	}
	if repo, err := currentRepoName(); err == nil && cl.Repo != "" && !strings.EqualFold(repo, cl.Repo) {
		return "", 0, fmt.Errorf("CL-%d belongs to repo %q, but this checkout's origin is %q — run this from a checkout of %s instead (e.g. citric clone %s)", clID, cl.Repo, repo, cl.Repo, cl.Repo)
	}

	resp, err := client.get(fmt.Sprintf("/cl/%d/patchsets", clID))
	if err != nil {
		return "", 0, fmt.Errorf("fetching CL-%d patchsets: %w", clID, err)
	}
	var pss []model.Patchset
	if err := decodeOK(resp, &pss); err != nil {
		return "", 0, err
	}
	if len(pss) == 0 {
		return "", 0, fmt.Errorf("CL-%d has no patchsets", clID)
	}
	latest := pss[0]
	for _, ps := range pss {
		if ps.Number > latest.Number {
			latest = ps
		}
	}

	ref := gitutil.PatchsetRef(clID, latest.Number)
	fmt.Printf("--> fetching CL-%d patchset %d (%s)\n", clID, latest.Number, ref)
	if out, err := exec.Command("git", "fetch", "origin", ref).CombinedOutput(); err != nil {
		return "", 0, fmt.Errorf("git fetch: %w\n%s", err, out)
	}
	return latest.CommitSHA, latest.Number, nil
}

var cmdRebase = &cobra.Command{
	Use:   "rebase [<cl-id-or-commit>]",
	Short: "Rebase HEAD onto the latest base branch, another CL, or a commit",
	Long: `Rebase the current branch onto a new base:

  citric rebase          rebase onto the latest origin/<base> (default: main)
  citric rebase 281      rebase onto CL-281's latest patchset
  citric rebase a1b2c3d  rebase onto that commit — any git revision works
                        (SHA, branch, tag, origin/main, HEAD~3, ...)

Fetches the target first so it's current, then runs 'git rebase' onto it.
Conflicts are yours to resolve as usual — 'git rebase --continue' or
'--abort'. The Change-Id trailer isn't touched by a rebase, so 'citric push'
afterward still lands on the same CL; this only moves the commit, it
never pushes for you.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		base, _ := cmd.Flags().GetString("base")

		var target string
		switch {
		case len(args) == 0:
			fmt.Printf("--> fetching origin/%s\n", base)
			if out, err := exec.Command("git", "fetch", "origin", base).CombinedOutput(); err != nil {
				return fmt.Errorf("git fetch: %w\n%s", err, out)
			}
			target = "origin/" + base

		case clRefPattern.MatchString(args[0]):
			clID, ok := parseCLID(args[0])
			if !ok {
				return fmt.Errorf("invalid CL id %q", args[0])
			}
			sha, _, err := fetchLatestPatchset(clID)
			if err != nil {
				return err
			}
			target = sha

		default:
			// A raw git revision — SHA, branch, tag, origin/main, HEAD~3, etc.
			// git rebase resolves it directly; nothing to fetch on our side
			// since we don't know what kind of ref this is.
			target = args[0]
		}

		fmt.Printf("--> rebasing onto %s\n", target)
		c := exec.Command("git", "rebase", target)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			return fmt.Errorf("rebase stopped — resolve conflicts and run 'git rebase --continue', or 'git rebase --abort' to back out")
		}

		fmt.Println("done — run 'citric push' to submit the rebased commit(s)")
		return nil
	},
}

// ---------------------------------------------------------------------------
// init (client onboarding)
// ---------------------------------------------------------------------------

var cmdInit = &cobra.Command{
	Use:   "init [repo-name]",
	Short: "Set up the current directory to work with citric",
	Long: `Bootstrap a repository for citric in one step:
  - initialize git if needed
  - point the remote at the citric server (<server>/git/<name>.git)
  - scaffold .citric.toml, supporting config files, and the commit-msg hook

repo-name defaults to the current directory name. The server URL comes from your
citric auth config — run 'citric auth setup' first if you haven't.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		force, _ := cmd.Flags().GetBool("force")
		typeOverride, _ := cmd.Flags().GetString("type")
		remoteName, _ := cmd.Flags().GetString("remote")

		dir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get cwd: %w", err)
		}

		repoName := filepath.Base(dir)
		if len(args) == 1 && args[0] != "" {
			repoName = strings.TrimSuffix(args[0], ".git")
		}

		// 1. ensure a git repo
		if _, err := os.Stat(filepath.Join(dir, ".git")); os.IsNotExist(err) {
			if out, err := exec.Command("git", "init").CombinedOutput(); err != nil {
				return fmt.Errorf("git init: %w\n%s", err, out)
			}
			fmt.Println("✓ initialized git repository")
		}

		// 2. wire up the remote from the auth config
		cfg, err := loadClientConfig()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		if cfg.Server == "" {
			fmt.Fprintln(os.Stderr, "  skip remote (no server configured — run 'citric auth setup' first)")
		} else {
			cloneURL := strings.TrimRight(cfg.Server, "/") + "/git/" + repoName + ".git"
			if exec.Command("git", "remote", "get-url", remoteName).Run() == nil {
				exec.Command("git", "remote", "set-url", remoteName, cloneURL).Run()
				fmt.Printf("✓ remote %q → %s\n", remoteName, cloneURL)
			} else if out, err := exec.Command("git", "remote", "add", remoteName, cloneURL).CombinedOutput(); err != nil {
				return fmt.Errorf("git remote add: %w\n%s", err, out)
			} else {
				fmt.Printf("✓ remote %q → %s\n", remoteName, cloneURL)
			}

			// Auth is injected per-invocation by setupGitAuth, which every git
			// subprocess citric spawns inherits — including the raw `git push` behind
			// 'citric import'. Deliberately NOT persisted to .git/config: git would
			// then send the header twice (see setupGitAuth), and it keeps the token
			// off disk.
			if cfg.Token != "" {
				fmt.Println("✓ configured citric auth for git (author attribution + restricted repos)")
			}
		}

		// 3. scaffold .citric.toml + config files + commit-msg hook
		if err := scaffoldProject(dir, typeOverride, force, true); err != nil {
			return err
		}

		// 4. auth hint
		if cfg.Token == "" {
			fmt.Println()
			fmt.Println("  note: not authenticated yet — run 'citric auth setup'")
		}

		fmt.Println()
		fmt.Println("ready. iterate with:")
		fmt.Println("  citric commit -m \"your message\"")
		fmt.Println("  citric push")
		return nil
	},
}

// ---------------------------------------------------------------------------
// clone
// ---------------------------------------------------------------------------

var cmdClone = &cobra.Command{
	Use:   "clone <repo> [dir]",
	Short: "Clone a repo from the citric server (installs the commit-msg hook)",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadClientConfig()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		if cfg.Server == "" {
			return fmt.Errorf("no server configured — run 'citric auth setup' first")
		}
		repo := strings.TrimSuffix(args[0], ".git")
		cloneURL := strings.TrimRight(cfg.Server, "/") + "/git/" + repo + ".git"
		dir := repo
		if len(args) == 2 && args[1] != "" {
			dir = args[1]
		}

		clone := exec.Command("git", "clone", cloneURL, dir)
		clone.Stdout, clone.Stderr = os.Stdout, os.Stderr
		if err := clone.Run(); err != nil {
			return fmt.Errorf("git clone failed: %w", err)
		}

		// Install the commit-msg hook so commits get a stable Change-Id.
		fmt.Println(installCommitMsgHook(dir, false))
		fmt.Printf("\ncloned. iterate with:\n  cd %s\n  citric commit -m \"...\"\n  citric push\n", dir)
		return nil
	},
}

// ---------------------------------------------------------------------------
// pull
// ---------------------------------------------------------------------------

var cmdPull = &cobra.Command{
	Use:   "pull",
	Short: "Update the current branch from the citric server (fetch + rebase)",
	Long: `Fetch the server's base branch and rebase the current branch onto it.

  citric pull            fetch + rebase (fails if the rebase hits conflicts or
                       there are uncommitted changes)
  citric pull --force    hard-reset the branch to the server's base, DISCARDING
                       any local commits and uncommitted changes — the escape
                       hatch when the local branch is diverged or wedged.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		remote, _ := cmd.Flags().GetString("remote")
		base, _ := cmd.Flags().GetString("base")
		force, _ := cmd.Flags().GetBool("force")

		if force {
			// Make the branch exactly match the server, throwing away local
			// commits AND working-tree changes. Fetch first so FETCH_HEAD names
			// the server's current base regardless of remote-tracking config.
			fetch := exec.Command("git", "fetch", remote, base)
			fetch.Stdout, fetch.Stderr = os.Stdout, os.Stderr
			if err := fetch.Run(); err != nil {
				return fmt.Errorf("git fetch failed: %w", err)
			}
			fmt.Fprintf(os.Stderr, "force: resetting to %s/%s — local commits and uncommitted changes are discarded\n", remote, base)
			reset := exec.Command("git", "reset", "--hard", "FETCH_HEAD")
			reset.Stdout, reset.Stderr = os.Stdout, os.Stderr
			if err := reset.Run(); err != nil {
				return fmt.Errorf("git reset --hard failed: %w", err)
			}
			return nil
		}

		c := exec.Command("git", "pull", "--rebase", remote, base)
		c.Stdout, c.Stderr, c.Stdin = os.Stdout, os.Stderr, os.Stdin
		if err := c.Run(); err != nil {
			return fmt.Errorf("git pull failed: %w", err)
		}
		return nil
	},
}

// ---------------------------------------------------------------------------
// import
// ---------------------------------------------------------------------------

var cmdImport = &cobra.Command{
	Use:   "import",
	Short: "Import this branch's commits as patchsets of a single CL",
	Long: `Replay each commit in a range as a successive patchset of ONE CL,
preserving the history as the change's revision trail.

  citric import                  import the whole branch history (root..HEAD)
  citric import --onto main       import only main..HEAD
  citric import --onto <sha>      import only <sha>..HEAD

It stamps one shared Change-Id across the range and pushes each commit (oldest
first) to refs/for/<base>, so each becomes the next patchset. Your branch and
working tree are NOT modified — the pushed commits are fabricated on the fly.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		onto, _ := cmd.Flags().GetString("onto")
		base, _ := cmd.Flags().GetString("base")
		remote, _ := cmd.Flags().GetString("remote")

		rng := "HEAD"
		var parent string
		if onto != "" {
			p, err := gitOut("rev-parse", "--verify", onto+"^{commit}")
			if err != nil {
				return fmt.Errorf("bad --onto %q: %w", onto, err)
			}
			parent = p
			rng = onto + "..HEAD"
		}

		list, err := gitOut("rev-list", "--reverse", "--no-merges", rng)
		if err != nil {
			return fmt.Errorf("list commits: %w", err)
		}
		shas := strings.Fields(list)
		if len(shas) == 0 {
			return fmt.Errorf("no commits to import in range %q", rng)
		}

		cid := "I" + randHex(20)
		fmt.Printf("importing %d commit(s) as patchsets of one CL (Change-Id %s)\n", len(shas), cid)

		for i, sha := range shas {
			tree, err := gitOut("rev-parse", sha+"^{tree}")
			if err != nil {
				return err
			}
			rawMsg, err := gitOut("log", "-1", "--format=%B", sha)
			if err != nil {
				return err
			}
			msg := strings.TrimRight(stripChangeID(rawMsg), "\n") + "\n\nChange-Id: " + cid + "\n"

			meta, err := gitOut("log", "-1", "--format=%an%n%ae%n%aI%n%cn%n%ce%n%cI", sha)
			if err != nil {
				return err
			}
			m := strings.Split(meta, "\n")
			env := append(os.Environ(),
				"GIT_AUTHOR_NAME="+m[0], "GIT_AUTHOR_EMAIL="+m[1], "GIT_AUTHOR_DATE="+m[2],
				"GIT_COMMITTER_NAME="+m[3], "GIT_COMMITTER_EMAIL="+m[4], "GIT_COMMITTER_DATE="+m[5],
			)

			ctArgs := []string{"commit-tree", tree}
			if parent != "" {
				ctArgs = append(ctArgs, "-p", parent)
			}
			newSha, err := gitOutEnvStdin(env, msg, ctArgs...)
			if err != nil {
				return fmt.Errorf("commit-tree for %s: %w", sha[:8], err)
			}

			push := exec.Command("git", "push", remote, newSha+":refs/for/"+base)
			push.Stdout, push.Stderr = os.Stdout, os.Stderr
			if err := push.Run(); err != nil {
				return fmt.Errorf("push patchset %d/%d: %w", i+1, len(shas), err)
			}
			fmt.Printf("  ✓ patchset %d/%d  (%s)\n", i+1, len(shas), sha[:8])
			parent = newSha
		}

		fmt.Printf("\ndone — %d patchsets on a single CL.\n", len(shas))
		return nil
	},
}

// gitOut runs git and returns trimmed stdout.
func gitOut(args ...string) (string, error) {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// gitOutEnvStdin runs git with a custom env and stdin, returning trimmed stdout.
func gitOutEnvStdin(env []string, stdin string, args ...string) (string, error) {
	c := exec.Command("git", args...)
	c.Env = env
	c.Stdin = strings.NewReader(stdin)
	out, err := c.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// stripChangeID removes any existing Change-Id trailer lines from a message.
func stripChangeID(msg string) string {
	var keep []string
	for _, l := range strings.Split(msg, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "Change-Id:") {
			continue
		}
		keep = append(keep, l)
	}
	return strings.Join(keep, "\n")
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ---------------------------------------------------------------------------
// list
// ---------------------------------------------------------------------------

var cmdList = &cobra.Command{
	Use:   "list",
	Short: "List CLs",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCritClient()
		if err != nil {
			return err
		}

		status, _ := cmd.Flags().GetString("status")
		mine, _ := cmd.Flags().GetBool("mine")

		q := url.Values{"status": {status}}
		if mine {
			q.Set("author", client.cfg.Username)
		}

		resp, err := client.get("/cl?" + q.Encode())
		if err != nil {
			return err
		}
		var cls []*model.CL
		if err := decodeOK(resp, &cls); err != nil {
			return err
		}

		if len(cls) == 0 {
			fmt.Println("No CLs found.")
			return nil
		}

		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tTITLE\tAUTHOR\tREPO\tSTATUS\tAGE\t")
		for _, cl := range cls {
			title := cl.Title
			if len(title) > 50 {
				title = title[:47] + "..."
			}
			rebase := ""
			if cl.NeedsRebase {
				rebase = "⚠ needs rebase"
			}
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s→%s\t%s\t%s\t%s\n",
				cl.ID, title, cl.Author, cl.Repo, cl.TargetBranch,
				string(cl.Status), humanTime(cl.CreatedAt), rebase)
		}
		return tw.Flush()
	},
}

// ---------------------------------------------------------------------------
// status / add
// ---------------------------------------------------------------------------

var cmdStatus = &cobra.Command{
	Use:   "status",
	Short: "Show the current CL's status and what isn't in the CL yet",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := exec.Command("git", "rev-parse", "--git-dir").Run(); err != nil {
			return fmt.Errorf("not a git repository")
		}
		branch, _ := gitOut("rev-parse", "--abbrev-ref", "HEAD")
		cid := headChangeID()

		fmt.Printf("branch:  %s\n", branch)
		if cid == "" {
			fmt.Println("change:  (HEAD has no Change-Id — make a commit to start a CL)")
		} else {
			fmt.Printf("change:  %s\n", cid)
			if client, err := newCritClient(); err == nil && client.checkSetup() == nil {
				if cl := findCLByChangeID(client, cid); cl != nil {
					printCLStatus(client, cl)
				} else {
					fmt.Println("CL:      not on the server yet — run `citric push`")
				}
			}
		}
		printWorkingTree()
		return nil
	},
}

// findCLByChangeID returns the caller's CL matching a Change-Id, or nil.
func findCLByChangeID(c *critClient, cid string) *model.CL {
	resp, err := c.get("/cl?author=" + url.QueryEscape(c.cfg.Username))
	if err != nil {
		return nil
	}
	var cls []*model.CL
	if decodeOK(resp, &cls) != nil {
		return nil
	}
	for _, cl := range cls {
		if cl.ChangeID == cid {
			return cl
		}
	}
	return nil
}

// printCLStatus prints a one-glance summary of a CL: patchsets, reviews, builds.
func printCLStatus(c *critClient, cl *model.CL) {
	fmt.Printf("\nCL #%d  %q  [%s]\n", cl.ID, cl.Title, cl.Status)
	id := strconv.FormatInt(cl.ID, 10)

	var ps []*model.Patchset
	if r, e := c.get("/cl/" + id + "/patchsets"); e == nil {
		decodeOK(r, &ps)
	}
	var reviews []*model.Review
	if r, e := c.get("/cl/" + id + "/reviews"); e == nil {
		decodeOK(r, &reviews)
	}
	lgtm, needs := 0, 0
	for _, rv := range reviews {
		switch rv.Verdict {
		case model.VerdictLGTM:
			lgtm++
		case model.VerdictNeedsWork:
			needs++
		}
	}
	var builds []*model.Build
	if r, e := c.get("/cl/" + id + "/builds"); e == nil {
		decodeOK(r, &builds)
	}
	latest := map[string]*model.Build{}
	for _, b := range builds {
		if cur, ok := latest[b.ToolName]; !ok || b.ID > cur.ID {
			latest[b.ToolName] = b
		}
	}

	fmt.Printf("  patchsets: %d\n", len(ps))
	fmt.Printf("  reviews:   %d LGTM, %d needs-work\n", lgtm, needs)
	if cl.NeedsRebase {
		fmt.Printf("  ⚠ needs rebase — %s has moved past this patchset's base (citric rebase)\n", cl.TargetBranch)
	}
	if len(latest) > 0 {
		parts := make([]string, 0, len(latest))
		for tool, b := range latest {
			parts = append(parts, fmt.Sprintf("%s:%s", tool, b.Status))
		}
		sort.Strings(parts)
		fmt.Printf("  builds:    %s\n", strings.Join(parts, "  "))
	}
	fmt.Printf("  %s/#/cl/%d\n", strings.TrimRight(c.cfg.Server, "/"), cl.ID)
}

// printWorkingTree categorizes `git status --porcelain` into staged / modified /
// untracked — i.e. everything that isn't in the CL's HEAD commit yet. Untracked
// already excludes .gitignore'd paths (git's default).
func printWorkingTree() {
	raw, err := exec.Command("git", "status", "--porcelain", "--untracked-files=all").Output()
	if err != nil {
		return
	}
	body := strings.TrimRight(string(raw), "\n")
	if body == "" {
		fmt.Println("\nworking tree clean — everything is committed into the CL.")
		return
	}
	var staged, unstaged, untracked []string
	for _, line := range strings.Split(body, "\n") {
		if len(line) < 3 {
			continue
		}
		x, y, path := line[0], line[1], line[3:]
		if x == '?' && y == '?' {
			untracked = append(untracked, path)
			continue
		}
		if x != ' ' && x != '?' {
			staged = append(staged, fmt.Sprintf("%c %s", x, path))
		}
		if y != ' ' && y != '?' {
			unstaged = append(unstaged, fmt.Sprintf("%c %s", y, path))
		}
	}
	fmt.Println("\nnot in the CL yet:")
	printGroup("  staged (will land in the next patchset):", staged)
	printGroup("  modified, not staged:", unstaged)
	printGroup("  untracked (new files, not git-ignored):", untracked)
	fmt.Println("\nstage with:  citric add <paths>   (or citric add -A)")
	fmt.Println("then:        citric commit --amend   (update this CL)   |   citric commit -m \"…\"   (new CL)")
}

func printGroup(title string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Println(title)
	for _, it := range items {
		fmt.Printf("    %s\n", it)
	}
}

var cmdAdd = &cobra.Command{
	Use:     "add [paths...]",
	Aliases: []string{"stage"},
	Short:   "Stage files for the next patchset (wraps git add)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := exec.Command("git", "rev-parse", "--git-dir").Run(); err != nil {
			return fmt.Errorf("not a git repository")
		}
		all, _ := cmd.Flags().GetBool("all")
		var gitArgs []string
		switch {
		case all:
			gitArgs = []string{"add", "-A"}
		case len(args) > 0:
			gitArgs = append([]string{"add"}, args...)
		default:
			fmt.Println("Nothing specified. New files not yet in the CL (and not git-ignored):")
			raw, _ := exec.Command("git", "status", "--porcelain", "--untracked-files=all").Output()
			any := false
			for _, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
				if strings.HasPrefix(line, "??") {
					fmt.Printf("    %s\n", line[3:])
					any = true
				}
			}
			if !any {
				fmt.Println("    (none)")
			}
			fmt.Println("\nstage with:  citric add <paths...>   or   citric add -A   (all changes)")
			return nil
		}
		c := exec.Command("git", gitArgs...)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			return fmt.Errorf("git add: %w", err)
		}
		fmt.Println("✓ staged. next: citric commit --amend  (update current CL)  |  citric commit -m \"…\"  (new CL)")
		return nil
	},
}

// ---------------------------------------------------------------------------
// show
// ---------------------------------------------------------------------------

var cmdShow = &cobra.Command{
	Use:   "show <CL-ID>",
	Short: "Show CL detail: patchsets, reviewers, builds",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCritClient()
		if err != nil {
			return err
		}
		clID := args[0]

		// fan out three requests concurrently
		type result struct {
			patchsets []*model.Patchset
			reviews   []*model.Review
			builds    []*model.Build
			err       error
		}
		ch := make(chan result, 1)

		var cl model.CL
		resp, err := client.get("/cl/" + clID)
		if err != nil {
			return err
		}
		if err := decodeOK(resp, &cl); err != nil {
			return err
		}

		go func() {
			var r result
			if rr, e := client.get("/cl/" + clID + "/patchsets"); e == nil {
				decodeOK(rr, &r.patchsets)
			}
			if rr, e := client.get("/cl/" + clID + "/reviews"); e == nil {
				decodeOK(rr, &r.reviews)
			}
			if rr, e := client.get("/cl/" + clID + "/builds"); e == nil {
				decodeOK(rr, &r.builds)
			}
			ch <- r
		}()
		res := <-ch

		// header
		fmt.Printf("\nCL-%d: %s\n", cl.ID, cl.Title)
		fmt.Printf("  %-12s %s\n", "Author:", cl.Author)
		fmt.Printf("  %-12s %s → %s\n", "Repo:", cl.Repo, cl.TargetBranch)
		fmt.Printf("  %-12s %s\n", "Status:", statusBadge(string(cl.Status)))
		fmt.Printf("  %-12s %s\n", "Created:", humanTime(cl.CreatedAt))
		if cl.Description != "" {
			fmt.Println()
			for _, line := range strings.Split(cl.Description, "\n") {
				fmt.Println("  " + line)
			}
		}

		// patchsets
		if len(res.patchsets) > 0 {
			fmt.Println("\nPatchsets:")
			for _, ps := range res.patchsets {
				marker := ""
				if ps.Number == len(res.patchsets) {
					marker = " ← current"
				}
				fmt.Printf("  PS-%-3d  %s%s\n", ps.Number, shortSHA(ps.CommitSHA), marker)
			}
		}

		// reviews
		if len(res.reviews) > 0 {
			fmt.Println("\nReviews:")
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			for _, rv := range res.reviews {
				fmt.Fprintf(tw, "  %s\t%s\n", rv.Reviewer, verdictBadge(string(rv.Verdict)))
			}
			tw.Flush()
		}

		// builds
		if len(res.builds) > 0 {
			fmt.Println("\nBuilds:")
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			for _, b := range res.builds {
				dur := ""
				if b.StartedAt != nil && b.EndedAt != nil {
					dur = b.EndedAt.Sub(*b.StartedAt).Round(time.Second).String()
				}
				fmt.Fprintf(tw, "  %s\t%s\t%s\n", b.ToolName, buildBadge(string(b.Status)), dur)
			}
			tw.Flush()
		}
		fmt.Println()
		return nil
	},
}

// ---------------------------------------------------------------------------
// diff
// ---------------------------------------------------------------------------

var cmdDiff = &cobra.Command{
	Use:   "diff <CL-ID>",
	Short: "Show unified diff for a CL (pipes through delta if available)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCritClient()
		if err != nil {
			return err
		}
		ps, _ := cmd.Flags().GetInt("ps")
		path := "/cl/" + args[0] + "/diff"
		if ps > 0 {
			path = fmt.Sprintf("/cl/%s/diff/%d", args[0], ps)
		}
		resp, err := client.getAccept(path, "text/plain")
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			msg, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("server error %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
		}
		return pipeToPager(resp.Body)
	},
}

// ---------------------------------------------------------------------------
// review
// ---------------------------------------------------------------------------

var cmdReview = &cobra.Command{
	Use:   "review <CL-ID>",
	Short: "Submit a verdict on a CL (--lgtm or --needs-work)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCritClient()
		if err != nil {
			return err
		}
		lgtm, _ := cmd.Flags().GetBool("lgtm")
		nw, _ := cmd.Flags().GetBool("needs-work")
		if !lgtm && !nw {
			return fmt.Errorf("specify --lgtm or --needs-work")
		}
		if lgtm && nw {
			return fmt.Errorf("--lgtm and --needs-work are mutually exclusive")
		}
		verdict := "lgtm"
		if nw {
			verdict = "needs_work"
		}
		resp, err := client.post("/cl/"+args[0]+"/review", map[string]string{"verdict": verdict})
		if err != nil {
			return err
		}
		if err := expectOK(resp); err != nil {
			return err
		}
		fmt.Printf("Posted %s on CL-%s.\n", verdictBadge(verdict), args[0])
		return nil
	},
}

// ---------------------------------------------------------------------------
// comment
// ---------------------------------------------------------------------------

var cmdComment = &cobra.Command{
	Use:   "comment <CL-ID>",
	Short: "Post a comment on a CL",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCritClient()
		if err != nil {
			return err
		}
		file, _ := cmd.Flags().GetString("file")
		lineNum, _ := cmd.Flags().GetInt("line")
		body, _ := cmd.Flags().GetString("body")

		if body == "" {
			return fmt.Errorf("--body is required")
		}

		payload := map[string]interface{}{
			"file_path": file,
			"body":      body,
		}
		if lineNum > 0 {
			payload["line"] = lineNum
		}

		resp, err := client.post("/cl/"+args[0]+"/comment", payload)
		if err != nil {
			return err
		}
		var out struct {
			ID int64 `json:"id"`
		}
		if err := decodeOK(resp, &out); err != nil {
			return err
		}
		loc := ""
		if file != "" {
			loc = file
			if lineNum > 0 {
				loc = fmt.Sprintf("%s:%d", file, lineNum)
			}
			loc = " on " + loc
		}
		fmt.Printf("Comment #%d posted on CL-%s%s.\n", out.ID, args[0], loc)
		return nil
	},
}

// ---------------------------------------------------------------------------
// submit
// ---------------------------------------------------------------------------

var cmdSubmit = &cobra.Command{
	Use:   "submit <CL-ID>",
	Short: "Submit (merge) a CL",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCritClient()
		if err != nil {
			return err
		}
		force, _ := cmd.Flags().GetBool("force")
		strategy, _ := cmd.Flags().GetString("strategy")

		fmt.Printf("Checking submit gate for CL-%s...\n", args[0])
		resp, err := client.post("/cl/"+args[0]+"/submit", map[string]interface{}{
			"strategy":     strategy,
			"force_submit": force,
		})
		if err != nil {
			return err
		}
		var out struct {
			MergedSHA string `json:"merged_sha"`
		}
		if err := decodeOK(resp, &out); err != nil {
			return err
		}
		fmt.Printf("Merged: %s\n", shortSHA(out.MergedSHA))
		return nil
	},
}

// ---------------------------------------------------------------------------
// abandon
// ---------------------------------------------------------------------------

var cmdAbandon = &cobra.Command{
	Use:   "abandon <CL-ID> [CL-ID...]",
	Short: "Abandon one or more CLs",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCritClient()
		if err != nil {
			return err
		}
		var failed int
		for _, id := range args {
			resp, err := client.post("/cl/"+id+"/abandon", nil)
			if err != nil {
				fmt.Fprintf(os.Stderr, "CL-%s: %v\n", id, err)
				failed++
				continue
			}
			if err := expectOK(resp); err != nil {
				fmt.Fprintf(os.Stderr, "CL-%s: %v\n", id, err)
				failed++
				continue
			}
			fmt.Printf("CL-%s abandoned.\n", id)
		}
		if failed > 0 {
			return fmt.Errorf("%d of %d failed", failed, len(args))
		}
		return nil
	},
}

// ---------------------------------------------------------------------------
// build
// ---------------------------------------------------------------------------

var cmdBuild = &cobra.Command{
	Use:   "build <CL-ID>",
	Short: "Trigger builds or stream logs for a CL",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCritClient()
		if err != nil {
			return err
		}
		logs, _ := cmd.Flags().GetBool("logs")
		buildID, _ := cmd.Flags().GetInt64("build-id")

		if logs {
			if buildID == 0 {
				// find the latest build for this CL
				resp, err := client.get("/cl/" + args[0] + "/builds")
				if err != nil {
					return err
				}
				var builds []*model.Build
				if err := decodeOK(resp, &builds); err != nil {
					return err
				}
				if len(builds) == 0 {
					return fmt.Errorf("no builds found for CL-%s — trigger one first", args[0])
				}
				buildID = builds[len(builds)-1].ID
			}
			return streamBuildLogs(client, args[0], buildID)
		}

		// trigger
		resp, err := client.post("/cl/"+args[0]+"/builds/trigger", nil)
		if err != nil {
			return err
		}
		if err := expectOK(resp); err != nil {
			return err
		}
		fmt.Printf("Builds triggered for CL-%s.\n", args[0])
		return nil
	},
}

func streamBuildLogs(client *critClient, clID string, buildID int64) error {
	if err := client.checkSetup(); err != nil {
		return err
	}
	path := fmt.Sprintf("/cl/%s/builds/%d/stream", clID, buildID)
	req, err := http.NewRequest("GET", client.cfg.Server+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+client.cfg.Token)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := client.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server error %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}

	scanner := bufio.NewScanner(resp.Body)
	var eventType string
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			eventType = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data := strings.TrimPrefix(line, "data: ")
			if eventType == "done" {
				fmt.Printf("\n[%s]\n", buildBadge(data))
				return nil
			}
			fmt.Println(data)
		case line == "":
			eventType = ""
		}
	}
	return scanner.Err()
}

// ---------------------------------------------------------------------------
// sync
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// switch
// ---------------------------------------------------------------------------

var cmdSwitch = &cobra.Command{
	Use:   "switch <cl-id>",
	Short: "Get a CL onto your machine so you can look at it, build it, or keep working on it",
	Long: `Fetches a CL's latest patchset and puts your working copy on it, on a
local branch named cl-<id>:

  citric switch 282       switch to CL-282's latest patchset

This is the command for "let me try out someone else's CL" or "let me get
back to a CL I started on another machine" — 'citric commit --amend' and
'citric push' from here continue it exactly like normal.

Safe to run again later to pick up a newer patchset. If you made local
commits on cl-282 since the last switch that the server doesn't have yet,
this stops rather than discard them — pass --force to discard them on
purpose. It also refuses to run over uncommitted changes for the same
reason, --force included.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		force, _ := cmd.Flags().GetBool("force")

		clID, ok := parseCLID(args[0])
		if !ok {
			return fmt.Errorf("expected a CL id (e.g. 282 or CL-282), got %q", args[0])
		}

		if !force {
			out, err := exec.Command("git", "status", "--porcelain").Output()
			if err == nil && strings.TrimSpace(string(out)) != "" {
				return fmt.Errorf("uncommitted changes in the working tree — commit, stash, or re-run with --force")
			}
		}

		sha, psNumber, err := fetchLatestPatchset(clID)
		if err != nil {
			return err
		}

		branch := fmt.Sprintf("cl-%d", clID)

		// If cl-<id> already exists locally with commits beyond what the
		// server has, a plain reset would throw them away silently — only
		// allow that with --force.
		if !force && exec.Command("git", "rev-parse", "--verify", "--quiet", branch).Run() == nil {
			if err := exec.Command("git", "merge-base", "--is-ancestor", branch, sha).Run(); err != nil {
				return fmt.Errorf("local branch %q has commits CL-%d's server copy doesn't — switch to it and push first, or re-run with --force to discard them", branch, clID)
			}
		}

		c := exec.Command("git", "checkout", "-B", branch, sha)
		c.Stdout, c.Stderr = os.Stdout, os.Stderr
		if err := c.Run(); err != nil {
			return fmt.Errorf("git checkout: %w", err)
		}

		fmt.Printf("switched to CL-%d, patchset %d, on local branch %q\n", clID, psNumber, branch)
		return nil
	},
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func humanTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("2006-01-02")
	}
}

func shortSHA(sha string) string {
	if len(sha) >= 8 {
		return sha[:8]
	}
	return sha
}

func statusBadge(status string) string {
	switch status {
	case "open":
		return "OPEN"
	case "merged":
		return "MERGED"
	case "abandoned":
		return "ABANDONED"
	default:
		return strings.ToUpper(status)
	}
}

func verdictBadge(verdict string) string {
	switch verdict {
	case "lgtm":
		return "LGTM"
	case "needs_work":
		return "NEEDS WORK"
	case "comment":
		return "COMMENT"
	default:
		return strings.ToUpper(verdict)
	}
}

func buildBadge(status string) string {
	switch status {
	case "pass":
		return "PASS"
	case "fail":
		return "FAIL"
	case "running":
		return "RUNNING"
	case "queued":
		return "QUEUED"
	default:
		return strings.ToUpper(status)
	}
}

// pipeToPager writes r to delta (if installed) or less, falling back to stdout.
func pipeToPager(r io.Reader) error {
	pager := ""
	if _, err := exec.LookPath("delta"); err == nil {
		pager = "delta"
	} else if _, err := exec.LookPath("less"); err == nil {
		pager = "less"
	}

	if pager == "" {
		_, err := io.Copy(os.Stdout, r)
		return err
	}

	cmd := exec.Command(pager)
	cmd.Stdin = r
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ---------------------------------------------------------------------------
// flag wiring (called from init in main.go)
// ---------------------------------------------------------------------------

func initClientFlags() {
	cmdAuth.AddCommand(cmdAuthLogin)
	cmdAuth.AddCommand(cmdAuthToken)
	cmdAuthLogin.Flags().String("server", "", "citric server URL")
	cmdAuthLogin.Flags().String("token", "", "CLI token from the web UI")

	cmdPush.Flags().String("base", "main", "target branch")
	cmdPush.Flags().String("remote", "origin", "git remote to push to")
	cmdPush.Flags().Bool("no-verify", false, "skip running .citric.toml tools locally before pushing")

	cmdPublish.Flags().String("remote", "public", "git remote name for the external mirror")
	cmdPublish.Flags().String("branch", "", "destination branch (default: public_branch in .citric.toml, else main)")
	cmdPublish.Flags().Bool("force", false, "force-update the external branch (needed after a history rewrite)")
	cmdPublish.Flags().Bool("dry-run", false, "rewrite and show what would be pushed, without pushing")

	cmdCommit.Flags().StringP("message", "m", "", "commit message (start a new change)")
	cmdCommit.Flags().StringP("title", "t", "", "subject line; body comes from -p (cannot be used with -m)")
	cmdCommit.Flags().StringArrayP("paragraph", "p", nil, "body paragraph, repeatable — each becomes its own paragraph")
	cmdCommit.Flags().Int("wrap", 72, "wrap -p paragraphs at this column; 0 disables (paragraphs containing newlines are never rewrapped)")
	cmdCommit.Flags().BoolP("all", "a", false, "stage modified/deleted tracked files first")
	cmdCommit.Flags().Bool("amend", false, "update the current change (reword with -m)")
	cmdCommit.Flags().Bool("push", false, "push for review after committing")
	cmdCommit.Flags().String("base", "main", "target branch (with --push)")
	cmdCommit.Flags().Bool("no-verify", false, "with --push, skip running .citric.toml tools locally first")

	cmdReparent.Flags().Bool("push", false, "push for review after reparenting")
	cmdReparent.Flags().String("base", "main", "target branch (with --push)")
	cmdReparent.Flags().Bool("no-verify", false, "with --push, skip running .citric.toml tools locally first")

	cmdRebase.Flags().String("base", "main", "base branch to rebase onto when no argument is given")

	cmdSwitch.Flags().Bool("force", false, "discard uncommitted changes / local-only commits to switch anyway")

	cmdInit.Flags().Bool("force", false, "overwrite existing config files")
	cmdInit.Flags().String("type", "", "override project type detection")
	cmdInit.Flags().String("remote", "origin", "git remote name to configure")

	cmdAdd.Flags().BoolP("all", "A", false, "stage all changes including untracked (git add -A)")

	cmdImport.Flags().String("onto", "", "import only <ref>..HEAD (default: whole history)")
	cmdImport.Flags().String("base", "main", "target branch for the CL")
	cmdImport.Flags().String("remote", "origin", "git remote to push to")

	cmdPull.Flags().String("remote", "origin", "git remote to pull from")
	cmdPull.Flags().String("base", "main", "branch to pull")
	cmdPull.Flags().Bool("force", false, "hard-reset to the server's base (discards local commits and changes)")

	cmdList.Flags().Bool("mine", false, "show only your CLs")
	cmdList.Flags().String("status", "open", "filter by status (open|merged|abandoned)")

	cmdDiff.Flags().Int("ps", 0, "patchset number (default: latest)")

	cmdReview.Flags().Bool("lgtm", false, "approve the CL")
	cmdReview.Flags().Bool("needs-work", false, "request changes")

	cmdComment.Flags().String("file", "", "file path for inline comment")
	cmdComment.Flags().Int("line", 0, "line number for inline comment")
	cmdComment.Flags().String("body", "", "comment text (required)")

	cmdSubmit.Flags().Bool("force", false, "force submit (admin only — bypasses build gate)")
	cmdSubmit.Flags().String("strategy", "fast-forward", "merge strategy (fast-forward|rebase|cherry-pick)")

	cmdBuild.Flags().Bool("logs", false, "stream live build logs instead of triggering")
	cmdBuild.Flags().Int64("build-id", 0, "specific build ID to stream (default: latest)")

	// unused stubs removed — citric assign is a direct server operation
	// wire up the unused strconv import via clID parsing in show
	_ = strconv.Itoa
}
