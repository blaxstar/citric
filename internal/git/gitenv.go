package git

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

var (
	gitConfigOnce sync.Once
	gitConfigPath string
	gitConfigErr  error
)

// ensureGitConfig writes a citric-managed global gitconfig under dataDir that
// whitelists every repository via safe.directory, and returns its path.
// Written once per process.
func ensureGitConfig(dataDir string) (string, error) {
	gitConfigOnce.Do(func() {
		p := filepath.Join(dataDir, "gitconfig")
		// safe.directory=* trusts repos regardless of filesystem owner.
		// safe.bareRepository=all (the historical default) keeps bare-repo
		// access working through GIT_DIR/GIT_PROJECT_ROOT.
		content := []byte("[safe]\n\tdirectory = *\n\tbareRepository = all\n")

		// If a usable config is already present, reuse it. This matters when
		// the file was created by a different user (e.g. a dev running the
		// binary directly) and the service user lacks write permission on it —
		// the content is static, so an existing copy is just as good.
		if existing, err := os.ReadFile(p); err == nil && bytes.Equal(existing, content) {
			gitConfigPath = p
			return
		}

		if err := os.WriteFile(p, content, 0o644); err != nil {
			// Couldn't write — but if a readable file exists we can still use it.
			if _, statErr := os.Stat(p); statErr == nil {
				gitConfigPath = p
				return
			}
			gitConfigErr = err
			return
		}
		gitConfigPath = p
	})
	return gitConfigPath, gitConfigErr
}

// Env returns environment additions that let the current process run git
// against repositories it does not own without tripping git's "dubious
// ownership" check (which otherwise aborts with a Status: 500 from
// git-http-backend, or a non-zero exit from plumbing commands).
//
// The citric service runs as one user while bare repos may be owned
// by another (whoever created them), so this is required for both
// the smart-HTTP transport and the build runner's worktree commands.
//
// Append the result to a base environment (e.g. os.Environ()) before exec.
// GIT_CONFIG_SYSTEM is pinned to /dev/null so a stray /etc/gitconfig can't
// reintroduce safe.bareRepository=explicit or other surprises.
//
// core.quotePath=false goes in GIT_CONFIG_* rather than the gitconfig file
// above because ensureGitConfig falls back to an existing file when it cannot
// write one (a config created by another user), so a file-based setting can be
// silently defeated by a stale copy on an already-deployed box. Without it git
// C-quotes any path holding non-ASCII bytes, and a name with emoji reaches the
// UI as "\342\235\244\357\270\217 CORE VALUES \342\235\244\357\270\217.md".
func Env(dataDir string) []string {
	overrides := []string{
		"GIT_CONFIG_COUNT=2",
		"GIT_CONFIG_KEY_0=core.quotePath",
		"GIT_CONFIG_VALUE_0=false",
		// A missing mailmap file is ignored by git (verified: exit 0, no
		// warning), so this can be set unconditionally — the admin UI creates
		// the file lazily on first save.
		"GIT_CONFIG_KEY_1=mailmap.file",
		"GIT_CONFIG_VALUE_1=" + MailmapPath(dataDir),
	}
	p, err := ensureGitConfig(dataDir)
	if err != nil || p == "" {
		return overrides
	}
	return append([]string{
		"GIT_CONFIG_GLOBAL=" + p,
		"GIT_CONFIG_SYSTEM=/dev/null",
	}, overrides...)
}

// MailmapPath is the server-wide git mailmap: one canonical identity per
// person, applied to every repo. Commands that report authorship must pass
// --use-mailmap and read %aN/%aE (capitalised) for it to take effect.
//
// A repo may also carry its own .mailmap; git picks that up automatically even
// in a bare repo, where mailmap.blob defaults to HEAD:.mailmap. The two compose,
// so per-repo entries keep working alongside this file.
func MailmapPath(dataDir string) string {
	return filepath.Join(dataDir, "mailmap")
}

// UnquotePath undoes git's C-style path quoting. core.quotePath=false stops git
// quoting non-ASCII, but it still quotes paths containing a double quote,
// backslash or control character, so parsers of porcelain output need this.
// Returns s unchanged when it is not a quoted string or cannot be decoded.
func UnquotePath(s string) string {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return s
	}
	// git's escapes (\", \\, \a \b \f \n \r \t \v and \nnn octal) are all valid
	// Go string-literal escapes, so Unquote decodes them byte for byte.
	if unquoted, err := strconv.Unquote(s); err == nil {
		return unquoted
	}
	return s
}
