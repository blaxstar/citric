package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/blaxstar/citric/internal/auth"
)

type ctxKey string

const ctxUser ctxKey = "user"

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Two sources, same citric-issued JWT:
		//   - Authorization: Bearer  → CLI (SSH-key login)
		//   - crit_session cookie     → web (Keycloak SSO)
		token := ""
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			token = strings.TrimPrefix(h, "Bearer ")
		} else if c, err := r.Cookie(sessionCookie); err == nil {
			token = c.Value
		}
		if token == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		claims, err := auth.VerifyJWT(token, s.cfg.Auth.JWTSecret)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), ctxUser, claims.Subject)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) currentUser(r *http.Request) string {
	v, _ := r.Context().Value(ctxUser).(string)
	return v
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username := s.currentUser(r)
		if username == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		u, err := s.db.GetUser(username)
		if err != nil || u.Role != "admin" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// userHasPerm reports whether username is an admin OR holds the given Keycloak realm role.
func (s *Server) userHasPerm(username, role string) bool {
	return s.userIsAdmin(username) || s.db.HasUserRole(username, role)
}

// requirePerm gates a route: passes if the user is admin or has the named realm role.
func (s *Server) requirePerm(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			username := s.currentUser(r)
			if username == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if !s.userHasPerm(username, role) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
