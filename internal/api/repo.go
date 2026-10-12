package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	critgit "github.com/blaxstar/citric/internal/git"
	"github.com/go-chi/chi/v5"
)

var repoNameRE = regexp.MustCompile(`^[a-zA-Z0-9_.\-]+$`)

type repoInfo struct {
	Name     string `json:"name"`
	CloneURL string `json:"clone_url"`
}

type repoDetail struct {
	Name          string `json:"name"`
	CloneURL      string `json:"clone_url"`
	Description   string `json:"description"`
	DefaultBranch string `json:"default_branch"`
	// Set on single-repo responses (get/update), not the list.
	RequireOtherLGTM       bool `json:"require_other_lgtm,omitempty"`        // this repo's own setting
	RequireOtherLGTMGlobal bool `json:"require_other_lgtm_global,omitempty"` // on server-wide
}

// gitDefaultPlaceholder is the description git writes into a fresh repo.
const gitDefaultPlaceholder = "Unnamed repository; edit this file 'description' to name the repository."

var branchNameRE = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

func (s *Server) repoPath(name string) (string, bool) {
	name = strings.TrimSuffix(name, ".git")
	if name == "" || !repoNameRE.MatchString(name) {
		return "", false
	}
	p := filepath.Join(s.cfg.Server.ReposDir, name+".git")
	if st, err := os.Stat(p); err != nil || !st.IsDir() {
		return "", false
	}
	return p, true
}

func repoDescription(repoPath string) string {
	b, err := os.ReadFile(filepath.Join(repoPath, "description"))
	if err != nil {
		return ""
	}
	d := strings.TrimSpace(string(b))
	if d == gitDefaultPlaceholder {
		return ""
	}
	return d
}

// gitC builds a `git -C <repoPath> ...` command carrying citric's safe.directory
// env, so it works on bare repos owned by a different user (no dubious-ownership).
func (s *Server) gitC(repoPath string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", append([]string{"-C", repoPath}, args...)...)
	cmd.Env = append(os.Environ(), critgit.Env(s.cfg.Server.DataDir)...)
	return cmd
}

func (s *Server) repoDefaultBranch(repoPath string) string {
	out, err := s.gitC(repoPath, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		return "main"
	}
	return strings.TrimSpace(string(out))
}

func (s *Server) repoDetailFor(r *http.Request, name, repoPath string) repoDetail {
	return repoDetail{
		Name:                   name,
		CloneURL:               s.cloneURL(r, name),
		Description:            repoDescription(repoPath),
		DefaultBranch:          s.repoDefaultBranch(repoPath),
		RequireOtherLGTM:       s.repoRequiresOtherLGTM(repoPath),
		RequireOtherLGTMGlobal: s.requireOtherLGTMGlobal(),
	}
}

// handleRepoGet returns details for one repo. Any authed user.
func (s *Server) handleRepoGet(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSuffix(chi.URLParam(r, "name"), ".git")
	repoPath, ok := s.repoPath(name)
	if !ok || !s.canViewRepo(r, name) {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}
	writeJSON(w, s.repoDetailFor(r, name, repoPath))
}

// handleRepoUpdate sets a repo's description and/or default branch. Admin only.
func (s *Server) handleRepoUpdate(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSuffix(chi.URLParam(r, "name"), ".git")
	repoPath, ok := s.repoPath(name)
	if !ok {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}
	var body struct {
		Description   *string `json:"description"`
		DefaultBranch *string `json:"default_branch"`
		// RequireOtherLGTM: the author's own LGTM doesn't count in this repo.
		RequireOtherLGTM *bool `json:"require_other_lgtm"`
	}
	if !decodeBody(w, r, &body, `{"description"?: string, "default_branch"?: string, "require_other_lgtm"?: bool}`) {
		return
	}
	if body.Description != nil {
		if err := os.WriteFile(filepath.Join(repoPath, "description"), []byte(strings.TrimSpace(*body.Description)+"\n"), 0o644); err != nil {
			http.Error(w, "could not write description", http.StatusInternalServerError)
			return
		}
	}
	if body.DefaultBranch != nil {
		branch := strings.TrimSpace(*body.DefaultBranch)
		if branch == "" || !branchNameRE.MatchString(branch) {
			http.Error(w, "invalid branch name", http.StatusBadRequest)
			return
		}
		if out, err := s.gitC(repoPath, "symbolic-ref", "HEAD", "refs/heads/"+branch).CombinedOutput(); err != nil {
			http.Error(w, fmt.Sprintf("set default branch: %v\n%s", err, out), http.StatusInternalServerError)
			return
		}
	}
	if body.RequireOtherLGTM != nil {
		if out, err := s.gitC(repoPath, "config", "--bool", gitConfigRequireOtherLGTM, strconv.FormatBool(*body.RequireOtherLGTM)).CombinedOutput(); err != nil {
			http.Error(w, fmt.Sprintf("set require_other_lgtm: %v\n%s", err, out), http.StatusInternalServerError)
			return
		}
	}
	s.db.WriteAudit(s.currentUser(r), "repo.update", name, "")
	writeJSON(w, s.repoDetailFor(r, name, repoPath))
}

// handleRepoList returns all bare repos in repos_dir.
func (s *Server) handleRepoList(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(s.cfg.Server.ReposDir)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, []repoInfo{})
			return
		}
		http.Error(w, "failed to list repos", http.StatusInternalServerError)
		return
	}

	username := s.currentUser(r)
	isAdmin := s.userIsAdmin(username)

	repos := make([]repoDetail, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() || !strings.HasSuffix(e.Name(), ".git") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".git")
		if !s.canView(username, isAdmin, name) {
			continue
		}
		repoPath := filepath.Join(s.cfg.Server.ReposDir, e.Name())
		repos = append(repos, repoDetail{
			Name:          name,
			CloneURL:      s.cloneURL(r, name),
			Description:   repoDescription(repoPath),
			DefaultBranch: s.repoDefaultBranch(repoPath),
		})
	}
	writeJSON(w, repos)
}

// handleRepoCreate creates a new bare repo and installs citric hooks. Admin only.
func (s *Server) handleRepoCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	name := strings.TrimSuffix(body.Name, ".git")
	if !repoNameRE.MatchString(name) {
		http.Error(w, "name may only contain letters, digits, hyphens, underscores, and dots", http.StatusBadRequest)
		return
	}

	repoPath := filepath.Join(s.cfg.Server.ReposDir, name+".git")
	if _, err := os.Stat(repoPath); err == nil {
		http.Error(w, "repo already exists", http.StatusConflict)
		return
	}

	if err := os.MkdirAll(s.cfg.Server.ReposDir, 0o755); err != nil {
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}

	// --shared=group: group-writable + setgid so both the service user and the
	// dev group can write objects regardless of who created the repo.
	if out, err := exec.CommandContext(r.Context(), "git", "init", "--bare", "--shared=group", repoPath).CombinedOutput(); err != nil {
		http.Error(w, fmt.Sprintf("git init: %v\n%s", err, out), http.StatusInternalServerError)
		return
	}

	// Allow pushes via git smart HTTP
	if out, err := exec.CommandContext(r.Context(), "git", "config",
		"-f", filepath.Join(repoPath, "config"),
		"http.receivepack", "true",
	).CombinedOutput(); err != nil {
		os.RemoveAll(repoPath)
		http.Error(w, fmt.Sprintf("set http.receivepack: %v\n%s", err, out), http.StatusInternalServerError)
		return
	}

	serverURL := fmt.Sprintf("http://localhost:%d", s.cfg.Server.Port)
	hookToken, err := critgit.HookToken(s.cfg.Server.DataDir)
	if err != nil {
		os.RemoveAll(repoPath)
		http.Error(w, "hook token unavailable", http.StatusInternalServerError)
		return
	}
	hooksDir := filepath.Join(repoPath, "hooks")
	hooks := map[string]string{
		"pre-receive":  critgit.PreReceiveHook,
		"post-receive": critgit.PostReceiveHook(serverURL, hookToken),
	}
	for hookName, content := range hooks {
		if err := os.WriteFile(filepath.Join(hooksDir, hookName), []byte(content), critgit.HookFileMode); err != nil {
			os.RemoveAll(repoPath)
			http.Error(w, "hook install failed", http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, repoInfo{Name: name, CloneURL: s.cloneURL(r, name)})
}

// handleRepoDelete removes a bare repo. Admin only.
func (s *Server) handleRepoDelete(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSuffix(chi.URLParam(r, "name"), ".git")
	if name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	repoPath := filepath.Join(s.cfg.Server.ReposDir, name+".git")
	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}
	if err := os.RemoveAll(repoPath); err != nil {
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// cloneURL builds the git clone URL using the configured public base URL
// (falling back to the request host, then localhost:port).
func (s *Server) cloneURL(r *http.Request, repoName string) string {
	return fmt.Sprintf("%s/git/%s.git", s.publicBaseURL(r), repoName)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// decodeBody decodes a JSON request body into dst, and on failure answers with a
// 400 that says which endpoint rejected it, what shape it wanted, and what the
// decoder actually choked on. Returns false if it has already written a response.
//
// `expected` describes the payload, e.g. `{"verdict": "lgtm|needs_work|comment"}`.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any, expected string) bool {
	err := json.NewDecoder(r.Body).Decode(dst)
	if err == nil {
		return true
	}
	msg := fmt.Sprintf("%s %s: could not parse JSON body: %v\nexpected: %s",
		r.Method, r.URL.Path, err, expected)
	if errors.Is(err, io.EOF) {
		msg = fmt.Sprintf("%s %s: request body is empty\nexpected: %s",
			r.Method, r.URL.Path, expected)
	}
	http.Error(w, msg, http.StatusBadRequest)
	return false
}
