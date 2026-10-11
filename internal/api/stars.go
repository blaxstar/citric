package api

import (
	"net/http"
	"path/filepath"

	"github.com/go-chi/chi/v5"
)

func (s *Server) handleStarRepo(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if _, ok := s.repoPath(name); !ok || !s.canViewRepo(r, name) {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}
	username := s.currentUser(r)
	if err := s.db.StarRepo(username, name); err != nil {
		http.Error(w, "failed to star", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUnstarRepo(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	username := s.currentUser(r)
	if err := s.db.UnstarRepo(username, name); err != nil {
		http.Error(w, "failed to unstar", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleIsStarred(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	username := s.currentUser(r)
	writeJSON(w, map[string]bool{"starred": s.db.IsStarred(username, name)})
}

func (s *Server) handleStarCount(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	writeJSON(w, map[string]int{"count": s.db.GetRepoStarCount(name)})
}

func (s *Server) handleUserStarred(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	starred := s.db.ListStarred(username)

	username2 := s.currentUser(r)
	isAdmin := s.userIsAdmin(username2)

	type starredRepo struct {
		Name        string `json:"name"`
		CloneURL    string `json:"clone_url"`
		Description string `json:"description"`
	}

	repos := make([]starredRepo, 0, len(starred))
	for _, repoName := range starred {
		if !s.canView(username2, isAdmin, repoName) {
			continue
		}
		repoPath := filepath.Join(s.cfg.Server.ReposDir, repoName+".git")
		repos = append(repos, starredRepo{
			Name:        repoName,
			CloneURL:    s.cloneURL(r, repoName),
			Description: repoDescription(repoPath),
		})
	}
	writeJSON(w, repos)
}
