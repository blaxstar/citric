package api

import (
	"encoding/json"
	"net/http"

	"github.com/blaxstar/citric/internal/keycloak"
	"github.com/blaxstar/citric/internal/model"
	"github.com/blaxstar/citric/internal/store"
	"github.com/go-chi/chi/v5"
)

func (s *Server) handleUserList(w http.ResponseWriter, r *http.Request) {
	users, err := s.db.ListUsers()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(users)
}

func (s *Server) handleUserGet(w http.ResponseWriter, r *http.Request) {
	u, err := s.db.GetUser(chi.URLParam(r, "username"))
	if err != nil || u == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(u)
}

// handleUserActivity returns a user's profile: their CLs, comments, and reviews
// (grouped by verdict) for the profile page.
func (s *Server) handleUserActivity(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	u, err := s.db.GetUser(username)
	if err != nil || u == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	cls, _ := s.db.ListCLs(store.CLFilter{Author: username})
	comments, _ := s.db.ListCommentsByAuthor(username, 200)
	reviews, _ := s.db.ListReviewsByAuthor(username, 200)

	byVerdict := map[model.Verdict][]*store.ProfileReview{
		model.VerdictLGTM:      {},
		model.VerdictNeedsWork: {},
		model.VerdictComment:   {},
	}
	for _, rv := range reviews {
		byVerdict[rv.Verdict] = append(byVerdict[rv.Verdict], rv)
	}

	writeJSON(w, map[string]any{
		"user":     u,
		"cls":      cls,
		"comments": comments,
		"reviews": map[string]any{
			"lgtm":       byVerdict[model.VerdictLGTM],
			"needs_work": byVerdict[model.VerdictNeedsWork],
			"comment":    byVerdict[model.VerdictComment],
		},
	})
}

func (s *Server) handleUserDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.db.DeleteUser(chi.URLParam(r, "username")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleFollow(w http.ResponseWriter, r *http.Request) {
	me := s.currentUser(r)
	target := chi.URLParam(r, "username")
	if me == target {
		http.Error(w, "cannot follow yourself", http.StatusBadRequest)
		return
	}
	if err := s.db.Follow(me, target); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUnfollow(w http.ResponseWriter, r *http.Request) {
	me := s.currentUser(r)
	target := chi.URLParam(r, "username")
	if err := s.db.Unfollow(me, target); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleIsFollowing(w http.ResponseWriter, r *http.Request) {
	me := s.currentUser(r)
	target := chi.URLParam(r, "username")
	writeJSON(w, map[string]bool{"following": s.db.IsFollowing(me, target)})
}

func (s *Server) handleFollowing(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	list := s.db.ListFollowing(username)
	if list == nil {
		list = []string{}
	}
	writeJSON(w, list)
}

func (s *Server) handleFollowers(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	list := s.db.ListFollowers(username)
	if list == nil {
		list = []string{}
	}
	writeJSON(w, list)
}

// handleSyncUsers triggers an immediate Keycloak user + role sync. Admin only.
func (s *Server) handleSyncUsers(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Keycloak.SyncEnabled() {
		http.Error(w, "keycloak not configured", http.StatusBadRequest)
		return
	}
	if err := keycloak.Sync(s.db, s.cfg.Keycloak); err != nil {
		http.Error(w, "sync failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	s.db.WriteAudit(s.currentUser(r), "users.sync", "keycloak", "")
	w.WriteHeader(http.StatusNoContent)
}
