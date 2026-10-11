package api

import (
	"io"
	"io/fs"
	"net/http"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/blaxstar/citric/internal/config"
	"github.com/blaxstar/citric/internal/runner"
	"github.com/blaxstar/citric/internal/store"
	"github.com/blaxstar/citric/internal/web"
)

// Keycloak realm roles that gate specific citric capabilities.
const (
	PermGalleryRemove = "crit-gallery-remove"
	PermGalleryUpload = "crit-gallery-upload"
	PermRepoCreate    = "crit-repo-create"
	PermRepoModify    = "crit-repo-modify"
	PermSSOSync       = "crit-sso-sync"
)

type Server struct {
	cfg    *config.Config
	db     *store.DB
	runner *runner.Runner
	oidc   *oidcProvider // nil when Keycloak SSO is not configured
	mux    *chi.Mux
}

func NewServer(cfg *config.Config, db *store.DB) *Server {
	s := &Server{
		cfg:    cfg,
		db:     db,
		runner: runner.New(db, cfg),
		mux:    chi.NewRouter(),
	}
	// Keycloak SSO is enabled when a client is configured.
	if cfg.Keycloak.SSOEnabled() {
		s.oidc = newOIDCProvider(cfg.Keycloak, cfg.Auth.JWTSecret, cfg.Auth.TokenTTL)
	}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	r := s.mux
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", s.handleHealth)
	r.Get("/healthz", s.handleHealth)

	// public: install script + binary downloads (no auth — needed before a token exists)
	r.Get("/install.sh", s.handleInstallScript)
	r.Get("/install.ps1", s.handleInstallPS1)
	r.Get("/dl/{filename}", s.handleDownload)

	// session identity + CLI token minting (token comes from the web UI after SSO)
	r.With(s.requireAuth).Get("/auth/me", s.handleAuthMe)
	r.With(s.requireAuth).Post("/auth/cli-token", s.handleCLIToken)

	// auth config + Keycloak SSO (web). Registered only when SSO is configured.
	r.Get("/auth/config", s.handleAuthConfig)
	if s.oidc != nil {
		r.Get("/auth/sso/login", s.handleSSOLogin)
		r.Get("/auth/callback", s.handleSSOCallback)
		r.Get("/auth/logout", s.handleLogout)
	}

	// git smart HTTP — fetch is open; push auth is handled by the pre-receive hook
	r.Handle("/git/*", http.HandlerFunc(s.serveGitHTTP))

	r.Route("/cl", func(r chi.Router) {
		r.Use(s.requireAuth)
		r.Post("/new", s.handleCLCreate)
		r.Get("/", s.handleCLList)
		r.Route("/{id}", func(r chi.Router) {
			r.Use(s.requireCLView) // hide CLs in repos the caller can't view
			r.Get("/", s.handleCLGet)
			r.Get("/events", s.handleCLEvents)
			r.Get("/patchsets", s.handleCLPatchsets)
			r.Get("/patchsets/{ps}/summary", s.handlePatchsetSummary)
			r.Get("/diff", s.handleCLDiff)
			r.Get("/diff/{ps}", s.handleCLDiffPS)
			r.Get("/reviews", s.handleReviewList)
			r.Get("/comments", s.handleCommentList)
			r.Post("/review", s.handleCLReview)
			r.Post("/comment", s.handleCLComment)
			r.Post("/submit", s.handleCLSubmit)
			r.Get("/readiness", s.handleCLReadiness)
			r.Post("/abandon", s.handleCLAbandon)
			r.Post("/assign", s.handleCLAssign)
			r.Get("/builds", s.handleBuildList)
			r.Get("/builds/{build_id}", s.handleBuildGet)
			r.Get("/builds/{build_id}/stream", s.handleBuildStream)
			r.Post("/builds/{build_id}/cancel", s.handleBuildCancel)
			r.Post("/builds/trigger", s.handleBuildTrigger)
		})
	})

	r.With(s.requireAuth).Get("/tools", s.handleToolList)

	r.Route("/settings", func(r chi.Router) {
		r.Use(s.requireAuth)
		r.Get("/", s.handleGetSettings)
		r.With(s.requireAdmin).Put("/", s.handleUpdateSettings)
	})

	r.Route("/me/settings", func(r chi.Router) {
		r.Use(s.requireAuth)
		r.Get("/", s.handleGetViewSettings)
		r.Put("/", s.handleUpdateViewSettings)
	})

	r.Route("/repo", func(r chi.Router) {
		r.Use(s.requireAuth)
		r.Get("/", s.handleRepoList)
		r.With(s.requirePerm(PermRepoCreate)).Post("/", s.handleRepoCreate)
		r.Get("/{name}", s.handleRepoGet)
		r.With(s.requirePerm(PermRepoModify)).Patch("/{name}", s.handleRepoUpdate)
		r.With(s.requirePerm(PermRepoModify)).Delete("/{name}", s.handleRepoDelete)
		r.Get("/{name}/owners", s.handleOwnersGet)
		r.With(s.requirePerm(PermRepoModify)).Put("/{name}/owners", s.handleOwnersPut)
		r.With(s.requirePerm(PermRepoModify)).Get("/{name}/access", s.handleRepoAccessGet)
		r.With(s.requirePerm(PermRepoModify)).Put("/{name}/access", s.handleRepoAccessPut)
		// file browser
		r.Get("/{name}/branches", s.handleRepoBranches)
		r.Get("/{name}/commits", s.handleRepoCommits)
		r.Get("/{name}/tree", s.handleRepoTree)
		r.Get("/{name}/file", s.handleRepoFile)
		r.Get("/{name}/raw", s.handleRepoRaw)
		r.Post("/{name}/edit", s.handleRepoEdit)
		// repo metadata & downloads
		r.Get("/{name}/readme", s.handleRepoReadme)
		r.Get("/{name}/languages", s.handleRepoLanguages)
		r.Get("/{name}/contributors", s.handleRepoContributors)
		r.Get("/{name}/activity", s.handleRepoActivity)
		r.Get("/{name}/size", s.handleRepoSize)
		r.Get("/{name}/archive", s.handleRepoArchive)
		// stars
		r.Get("/{name}/star", s.handleIsStarred)
		r.Post("/{name}/star", s.handleStarRepo)
		r.Delete("/{name}/star", s.handleUnstarRepo)
		r.Get("/{name}/stars", s.handleStarCount)
	})

	r.Route("/user", func(r chi.Router) {
		r.Use(s.requireAuth)
		r.Get("/", s.handleUserList)
		r.Get("/{username}", s.handleUserGet)
		r.Get("/{username}/activity", s.handleUserActivity)
		r.Get("/{username}/contributions", s.handleContributions)
		r.Get("/{username}/following", s.handleFollowing)
		r.Get("/{username}/followers", s.handleFollowers)
		r.Get("/{username}/follow", s.handleIsFollowing)
		r.Post("/{username}/follow", s.handleFollow)
		r.Delete("/{username}/follow", s.handleUnfollow)
		r.Get("/{username}/starred", s.handleUserStarred)
		r.With(s.requireAdmin).Delete("/{username}", s.handleUserDelete)
	})

	r.With(s.requireAuth).Get("/activity", s.handleActivityFeed)

	// admin/role-gated maintenance + curated assets
	r.Route("/admin", func(r chi.Router) {
		r.Use(s.requireAuth)
		r.With(s.requirePerm(PermSSOSync)).Post("/sync-users", s.handleSyncUsers)
		r.With(s.requirePerm(PermRepoModify)).Get("/keycloak/roles", s.handleRealmRoles)
		r.With(s.requirePerm(PermGalleryUpload)).Post("/backgrounds", s.handleBackgroundUpload)
		r.With(s.requirePerm(PermGalleryRemove)).Delete("/backgrounds/{name}", s.handleBackgroundDelete)
		// Full admins only — identity mapping is server-wide and there is no
		// narrower role for it yet.
		r.With(s.requireAdmin).Get("/nodes", s.handleNodeList)
		r.With(s.requireAdmin).Post("/nodes/join-tokens", s.handleNodeJoinToken)
		r.With(s.requireAdmin).Post("/nodes/{name}/drain", s.handleNodeSetStatus(store.NodeDraining))
		r.With(s.requireAdmin).Post("/nodes/{name}/resume", s.handleNodeSetStatus(store.NodeActive))
		r.With(s.requireAdmin).Delete("/nodes/{name}", s.handleNodeDelete)
		r.With(s.requireAdmin).Get("/mailmap", s.handleMailmapGet)
		r.With(s.requireAdmin).Put("/mailmap", s.handleMailmapPut)
	})

	// build nodes
	r.Post("/node/join", s.handleNodeJoin)
	r.Route("/node", func(r chi.Router) {
		r.Use(s.requireNode)
		r.Post("/claim", s.handleNodeClaim)
		r.Post("/jobs/{build_id}/log", s.handleNodeLog)
		r.Post("/jobs/{build_id}/done", s.handleNodeDone)
	})

	// internal — called by the post-receive hook; nginx blocks external access
	r.Post("/internal/hook/post-receive", s.handleHook)

	// curated background gallery — listing is open to any authed client; upload/
	// delete are admin-only (registered under /admin above).
	r.With(s.requireAuth).Get("/backgrounds", s.handleBackgroundList)

	// uploaded files served directly from data_dir/uploads/
	uploadsDir := filepath.Join(s.cfg.Server.DataDir, "uploads")
	r.Get("/uploads/*", func(w http.ResponseWriter, req *http.Request) {
		http.StripPrefix("/uploads/", http.FileServer(http.Dir(uploadsDir))).ServeHTTP(w, req)
	})

	// frontend SPA — serve embedded dist for all other routes
	distFS, _ := fs.Sub(web.FS, "dist")
	fsrv := http.FileServer(http.FS(distFS))
	r.Get("/assets/*", func(w http.ResponseWriter, req *http.Request) { fsrv.ServeHTTP(w, req) })
	r.Get("/fonts/*", func(w http.ResponseWriter, req *http.Request) { fsrv.ServeHTTP(w, req) })
	r.Get("/favicon.ico", func(w http.ResponseWriter, req *http.Request) { fsrv.ServeHTTP(w, req) })
	r.Get("/favicon.svg", func(w http.ResponseWriter, req *http.Request) { fsrv.ServeHTTP(w, req) })
	r.Get("/icon.svg", func(w http.ResponseWriter, req *http.Request) { fsrv.ServeHTTP(w, req) })
	r.Get("/", s.serveIndex)
	r.NotFound(s.serveIndex)
}

// handleHealth answers /healthz (and the older /health): 200 when the server
// can reach its database, 503 when it can't. Public, for load balancers and
// container health checks.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.db.Ping(); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	distFS, err := fs.Sub(web.FS, "dist")
	if err != nil {
		http.Error(w, "internal error", 500)
		return
	}
	f, err := distFS.Open("index.html")
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	defer f.Close()
	stat, _ := f.Stat()
	http.ServeContent(w, r, "index.html", stat.ModTime(), f.(io.ReadSeeker))
}
