package api

import (
	"encoding/json"
	"net/http"

	"github.com/blaxstar/citric/internal/auth"
)

// handleAuthMe returns the current user's username if the session is valid.
// GET /auth/me  (crit_session cookie, or Authorization: Bearer for the CLI)
func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	username := s.currentUser(r)
	role := ""
	email := ""
	if u, _ := s.db.GetUser(username); u != nil {
		role = string(u.Role)
		email = u.Email
	}
	perms, _ := s.db.GetUserRoles(username)
	if perms == nil {
		perms = []string{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"username":    username,
		"role":        role,
		"email":       email,
		"permissions": perms,
	})
}

// handleCLIToken mints a citric session token for the authenticated user, for use
// with the CLI (`citric auth login --token <token>`). The caller is already
// authenticated via the web SSO session cookie, so this just re-issues that
// identity as a bearer token the CLI can store. POST /auth/cli-token
func (s *Server) handleCLIToken(w http.ResponseWriter, r *http.Request) {
	username := s.currentUser(r)
	token, err := auth.IssueJWT(username, s.cfg.Auth.JWTSecret, s.cfg.Auth.TokenTTL)
	if err != nil {
		http.Error(w, "token issue failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"token": token, "username": username})
}
