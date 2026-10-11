package api

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	critgit "github.com/blaxstar/citric/internal/git"
)

// The server-wide git mailmap maps a person's alternate addresses onto one
// canonical identity, so someone committing from both a work and a personal
// address is reported as a single contributor. It applies to every repo
// without needing a commit to any of them, which is what makes it usable for
// imported history.
//
// Format (git's own — see gitmailmap(5)); citric only supports the two forms
// that map an address, since a bare name rewrite cannot merge identities:
//
//	Canonical Name <canonical@example.com> <other@example.com>
//	<canonical@example.com> <other@example.com>

// maxMailmapBytes bounds what an admin can store. A mailmap is a handful of
// lines per person; anything approaching this is a mistake or an upload.
const maxMailmapBytes = 256 << 10 // 256 KiB

// handleMailmapGet returns the raw mailmap text. Admin-only.
// GET /admin/mailmap
func (s *Server) handleMailmapGet(w http.ResponseWriter, r *http.Request) {
	b, err := os.ReadFile(critgit.MailmapPath(s.cfg.Server.DataDir))
	if err != nil && !os.IsNotExist(err) {
		http.Error(w, "read mailmap: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// Not-yet-created reads as empty rather than 404 — the editor wants a
	// document to open either way.
	writeJSON(w, map[string]string{"content": string(b)})
}

// handleMailmapPut replaces the mailmap. Admin-only.
// PUT /admin/mailmap  {"content": "..."}
func (s *Server) handleMailmapPut(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Content string `json:"content"`
	}
	if !decodeBody(w, r, &body, `{"content": "<mailmap text>"}`) {
		return
	}
	if len(body.Content) > maxMailmapBytes {
		http.Error(w, fmt.Sprintf("mailmap too large (max %d bytes)", maxMailmapBytes), http.StatusRequestEntityTooLarge)
		return
	}
	if err := validateMailmap(body.Content); err != nil {
		// Reject rather than write: git silently ignores lines it cannot parse,
		// so a typo would otherwise look saved and simply never take effect.
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	content := body.Content
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if err := os.WriteFile(critgit.MailmapPath(s.cfg.Server.DataDir), []byte(content), 0o644); err != nil {
		http.Error(w, "write mailmap: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.db.WriteAudit(s.currentUser(r), "mailmap.update", "mailmap", "")
	w.WriteHeader(http.StatusNoContent)
}

// validateMailmap reports the first line git would not act on. Blank lines and
// '#' comments are fine; every other line needs at least one well-formed
// <address>, since that is what git matches commits against.
//
// One address is allowed on purpose: "Proper Name <addr>" is valid mailmap that
// corrects a display name without merging anything, which is useful for tidying
// up bot and import identities.
func validateMailmap(content string) error {
	for i, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if countAddresses(line) == 0 {
			return fmt.Errorf(
				"line %d: needs an <email> address in angle brackets — e.g. %q",
				i+1, "Your Name <canonical@example.com> <other@example.com>")
		}
	}
	return nil
}

// countAddresses counts well-formed <...> groups, requiring each to be closed
// and non-empty. Unbalanced angle brackets count as zero so they surface as an
// error rather than being silently dropped.
func countAddresses(line string) int {
	count, depth, start := 0, 0, 0
	for i, c := range line {
		switch c {
		case '<':
			if depth > 0 {
				return 0 // nested '<' — malformed
			}
			depth, start = 1, i
		case '>':
			if depth == 0 {
				return 0 // stray '>' — malformed
			}
			if i > start+1 { // non-empty between the brackets
				count++
			}
			depth = 0
		}
	}
	if depth != 0 {
		return 0 // unclosed '<'
	}
	return count
}
