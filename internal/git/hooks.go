package git

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PreReceiveHook rejects direct pushes to refs/heads/*; only refs/for/* allowed.
const PreReceiveHook = `#!/bin/sh
while read old new ref; do
  case "$ref" in
    refs/for/*) ;;
    refs/heads/*)
      if [ "${CRIT_INTERNAL_PUSH:-0}" != "1" ]; then
        echo "[citric] direct pushes to $ref are not allowed. Use: git push origin HEAD:refs/for/<branch>" >&2
        exit 1
      fi
      ;;
  esac
done
exit 0
`

const postReceiveHookTemplate = `#!/bin/sh
CITRIC_SERVER="%s"
CITRIC_HOOK_TOKEN="%s"
REPO_NAME=$(basename "$(pwd)" .git)
while read old new ref; do
  printf '%%s %%s %%s\n' "$old" "$new" "$ref"
done | curl -sf \
  -X POST \
  -H "Content-Type: text/plain" \
  -H "X-Citric-Hook-Token: ${CITRIC_HOOK_TOKEN}" \
  --data-binary @- \
  "${CITRIC_SERVER}/internal/hook/post-receive?repo=${REPO_NAME}&pusher=${REMOTE_USER:-}" \
  || true
`

// PostReceiveHook returns the post-receive hook script with serverURL and the
// server's hook token embedded. The token is what lets the hook endpoint tell
// a real post-receive from anyone else on the network calling it.
func PostReceiveHook(serverURL, token string) string {
	return fmt.Sprintf(postReceiveHookTemplate, serverURL, token)
}

// HookFileMode keeps hooks (which embed the hook token) out of reach of
// accounts outside the service user's group.
const HookFileMode = 0o750

// HookToken returns the server's post-receive hook token, generating it into
// <dataDir>/hook_token (0600) on first use.
func HookToken(dataDir string) (string, error) {
	path := filepath.Join(dataDir, "hook_token")
	if b, err := os.ReadFile(path); err == nil {
		if t := strings.TrimSpace(string(b)); t != "" {
			return t, nil
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", err
	}
	return token, nil
}

// CommitMsgHook is a client-side hook installed into a developer's local repo
// (.git/hooks/commit-msg). It inserts a stable Change-Id trailer the first time
// a commit is created, so later `git commit --amend` + re-push lands as a new
// patchset on the SAME CL instead of spawning a new one. Idempotent: existing
// Change-Id trailers are left untouched (so amends keep the same ID).
const CommitMsgHook = `#!/bin/sh
# citric commit-msg hook — keeps one stable Change-Id per change.
# Managed by 'citric template --scaffold'; safe to re-run.
set -e
MSG="$1"

# Already has a Change-Id? Leave it (amends must keep the same ID).
if grep -qiE '^Change-Id: I[0-9a-f]{40}$' "$MSG"; then
  exit 0
fi

# Derive a stable, unique id from commit metadata + entropy.
seed=$(
  { git var GIT_AUTHOR_IDENT 2>/dev/null || true
    cat "$MSG"
    date +%s%N 2>/dev/null || date +%s
    echo "$$" ; } | git hash-object --stdin
)

# Insert as a proper trailer (handles blank-line/footer placement correctly).
git interpret-trailers \
  --if-exists doNothing \
  --trailer "Change-Id: I${seed}" \
  --in-place "$MSG"
`
