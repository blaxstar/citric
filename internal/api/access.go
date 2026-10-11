package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/blaxstar/citric/internal/keycloak"
	"github.com/blaxstar/citric/internal/model"
	"github.com/go-chi/chi/v5"
)

// userIsAdmin reports whether username has the citric admin role.
func (s *Server) userIsAdmin(username string) bool {
	u, _ := s.db.GetUser(username)
	return u != nil && u.Role == model.RoleAdmin
}

// canView reports whether the user may view repo. Errors are treated as "no".
func (s *Server) canView(username string, isAdmin bool, repo string) bool {
	ok, err := s.db.CanViewRepo(username, isAdmin, repo)
	return err == nil && ok
}

// canViewRepo is the per-request convenience wrapper.
func (s *Server) canViewRepo(r *http.Request, repo string) bool {
	username := s.currentUser(r)
	return s.canView(username, s.userIsAdmin(username), repo)
}

// requireCLView gates the /cl/{id} subtree: the caller must be able to view the
// CL's repository. Hidden CLs return 404 so restriction doesn't leak existence.
func (s *Server) requireCLView(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		cl, err := s.db.GetCL(id)
		if err != nil || cl == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if !s.canViewRepo(r, cl.Repo) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// repoAccess is the GET/PUT body for a repo's visibility settings.
type repoAccess struct {
	Restricted bool     `json:"restricted"`
	Users      []string `json:"users"`
	Roles      []string `json:"roles"`
}

// handleRepoAccessGet returns a repo's visibility settings. Admin only.
func (s *Server) handleRepoAccessGet(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSuffix(chi.URLParam(r, "name"), ".git")
	if _, ok := s.repoPath(name); !ok {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}
	restricted, _ := s.db.IsRepoRestricted(name)
	users, roles, err := s.db.GetRepoACL(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, repoAccess{Restricted: restricted, Users: users, Roles: roles})
}

// handleRepoAccessPut writes a repo's visibility settings. Admin only.
func (s *Server) handleRepoAccessPut(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSuffix(chi.URLParam(r, "name"), ".git")
	if _, ok := s.repoPath(name); !ok {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}
	var body repoAccess
	if !decodeBody(w, r, &body, `{"restricted": bool, "users": [username, ...]}`) {
		return
	}
	if err := s.db.SetRepoAccess(name, body.Restricted, body.Users, body.Roles); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.db.WriteAudit(s.currentUser(r), "repo.access", name, "")
	w.WriteHeader(http.StatusNoContent)
}

// handleRealmRoles lists role names for the access UI's team picker. With
// Keycloak sync configured it asks Keycloak for every realm role; with any
// other identity provider it returns the roles and groups seen in people's
// sign-in tokens so far. Names can always be typed in by hand too.
func (s *Server) handleRealmRoles(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Keycloak.SyncEnabled() {
		roles, err := s.db.ListKnownRoles()
		if err != nil {
			http.Error(w, "could not list roles", http.StatusInternalServerError)
			return
		}
		writeJSON(w, roles)
		return
	}
	roles, err := keycloak.ListRealmRoles(s.cfg.Keycloak)
	if err != nil {
		http.Error(w, "could not list roles: "+err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, roles)
}
