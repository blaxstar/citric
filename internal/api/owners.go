package api

import (
	"net/http"
	"strings"

	critgit "github.com/blaxstar/citric/internal/git"
	"github.com/blaxstar/citric/internal/owners"
	"github.com/go-chi/chi/v5"
)

// handleOwnersGet returns the repo-root OWNERS file at the default branch tip,
// both as raw text and parsed structure. Any authed user.
func (s *Server) handleOwnersGet(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSuffix(chi.URLParam(r, "name"), ".git")
	repoPath, ok := s.repoPath(name)
	if !ok || !s.canViewRepo(r, name) {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}
	branch := s.repoDefaultBranch(repoPath)
	raw, err := critgit.ShowFile(s.cfg.Server.DataDir, repoPath, branch, "OWNERS")
	if err != nil {
		http.Error(w, "could not read OWNERS", http.StatusInternalServerError)
		return
	}
	parsed, _ := owners.Parse(strings.NewReader(raw))
	writeJSON(w, map[string]any{
		"branch": branch,
		"raw":    raw,
		"parsed": parsed,
	})
}

// handleOwnersPut validates and commits a new repo-root OWNERS file to the
// default branch as the calling admin. Admin only.
func (s *Server) handleOwnersPut(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSuffix(chi.URLParam(r, "name"), ".git")
	repoPath, ok := s.repoPath(name)
	if !ok {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}
	var body struct {
		Raw string `json:"raw"`
	}
	if !decodeBody(w, r, &body, `{"raw": string — the OWNERS file contents}`) {
		return
	}
	// Validate by parsing — surfaces obvious mistakes before committing.
	if _, err := owners.Parse(strings.NewReader(body.Raw)); err != nil {
		http.Error(w, "invalid OWNERS: "+err.Error(), http.StatusBadRequest)
		return
	}

	content := body.Raw
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}

	username := s.currentUser(r)
	email := username + "@citric"
	if u, _ := s.db.GetUser(username); u != nil && u.Email != "" {
		email = u.Email
	}

	branch := s.repoDefaultBranch(repoPath)
	if err := critgit.CommitFile(
		s.cfg.Server.DataDir, repoPath, branch, "OWNERS", content,
		username, email, "Update OWNERS via citric",
	); err != nil {
		http.Error(w, "commit failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.db.WriteAudit(username, "owners.update", name, "")
	w.WriteHeader(http.StatusNoContent)
}
