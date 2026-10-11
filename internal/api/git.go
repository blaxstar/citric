package api

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/blaxstar/citric/internal/auth"
	critgit "github.com/blaxstar/citric/internal/git"
)

// serveGitHTTP proxies git smart HTTP protocol requests to git-http-backend(1).
//
// Remote URL convention: https://<server>/git/<repo>.git
//
// git-http-backend is invoked as a CGI subprocess with GIT_PROJECT_ROOT set
// to the configured repos_dir. No authentication is applied here — the git
// layer relies on the JWT token checked by requireAuth on the parent route.
func (s *Server) serveGitHTTP(w http.ResponseWriter, r *http.Request) {
	// chi wildcard gives us everything after /git/
	repoAndPath := chi.URLParam(r, "*")

	// split into <repo.git> and /<rest> (e.g. "myrepo.git/info/refs")
	slash := strings.Index(repoAndPath, "/")
	var repoName, subPath string
	if slash < 0 {
		repoName = repoAndPath
		subPath = ""
	} else {
		repoName = repoAndPath[:slash]
		subPath = repoAndPath[slash:]
	}

	repoPath := filepath.Join(s.cfg.Server.ReposDir, repoName)
	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		http.NotFound(w, r)
		return
	}

	// Pushing always takes a signed-in user: a push opens a CL, and a CL runs
	// the pushed repo config's build commands on this server. Anonymous access
	// is fetch/clone of open repos only.
	if isGitPush(subPath, r.URL.Query().Get("service")) {
		if _, ok := s.gitTokenUser(r); !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="citric"`)
			http.Error(w, "sign in to push: citric auth login", http.StatusUnauthorized)
			return
		}
	}

	// Restricted repos require an authenticated, allow-listed user even for
	// fetch/clone. Open repos stay unauthenticated for reads.
	bareName := strings.TrimSuffix(repoName, ".git")
	// Build nodes fetch any repo they're asked to build, and only fetch.
	nodeFetch := !isGitPush(subPath, r.URL.Query().Get("service")) && s.isNodeCredential(r)
	if restricted, _ := s.db.IsRepoRestricted(bareName); restricted && !nodeFetch {
		username, ok := s.gitTokenUser(r)
		if !ok || !s.canView(username, s.userIsAdmin(username), bareName) {
			w.Header().Set("WWW-Authenticate", `Basic realm="citric"`)
			http.Error(w, "this repository is restricted", http.StatusUnauthorized)
			return
		}
	}

	// PATH_INFO must be absolute and relative to GIT_PROJECT_ROOT
	pathInfo := "/" + repoName + subPath

	// Identify the pusher from their token (the CLI always sends it) so the
	// post-receive hook can attribute the CL to the citric user, not the commit's
	// git author name. Falls back to "citric" for anonymous/raw git.
	remoteUser := "citric"
	if u, ok := s.gitTokenUser(r); ok {
		remoteUser = u
	}

	env := []string{
		"GIT_PROJECT_ROOT=" + s.cfg.Server.ReposDir,
		"GIT_HTTP_EXPORT_ALL=1",
		"PATH_INFO=" + pathInfo,
		"REQUEST_METHOD=" + r.Method,
		"QUERY_STRING=" + r.URL.RawQuery,
		"CONTENT_TYPE=" + r.Header.Get("Content-Type"),
		"REMOTE_ADDR=" + r.RemoteAddr,
		"SERVER_PROTOCOL=HTTP/1.1",
		// REMOTE_USER satisfies git-http-backend's push auth check and is read by
		// the post-receive hook to attribute the CL to the pushing citric user.
		"REMOTE_USER=" + remoteUser,
		// pass PATH through so git can find its sub-commands
		"PATH=" + os.Getenv("PATH"),
	}
	if cl := r.Header.Get("Content-Length"); cl != "" {
		env = append(env, "CONTENT_LENGTH="+cl)
	}
	if gp := r.Header.Get("Git-Protocol"); gp != "" {
		env = append(env, "GIT_PROTOCOL="+gp)
	}
	// Trust repos we don't own — see internal/git.Env. Without this,
	// git-http-backend emits "Status: 500" on dubious ownership.
	env = append(env, critgit.Env(s.cfg.Server.DataDir)...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd := exec.CommandContext(r.Context(), "git", "http-backend")
	cmd.Env = env
	cmd.Stdin = r.Body
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		http.Error(w, fmt.Sprintf("git http-backend: %v\n%s", err, stderr.String()), http.StatusInternalServerError)
		return
	}

	if err := writeCGIResponse(w, &stdout); err != nil {
		// headers may already be sent; log and bail
		fmt.Fprintf(os.Stderr, "citric: cgi response write error: %v\n", err)
	}
}

// isGitPush reports whether a smart-HTTP request is part of a push: the
// receive-pack ref advertisement or the receive-pack upload itself.
func isGitPush(subPath, service string) bool {
	return service == "git-receive-pack" || strings.HasSuffix(subPath, "/git-receive-pack")
}

// gitTokenUser extracts and verifies a citric token from a git HTTP request. The
// CLI sends it as a Bearer header (http.extraHeader); raw git sends it as the
// HTTP Basic password. Returns the username on success.
func (s *Server) gitTokenUser(r *http.Request) (string, bool) {
	token := ""
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		token = strings.TrimPrefix(h, "Bearer ")
	} else if _, pass, ok := r.BasicAuth(); ok {
		token = pass
	} else if c, err := r.Cookie(sessionCookie); err == nil {
		token = c.Value
	}
	if token == "" {
		return "", false
	}
	claims, err := auth.VerifyJWT(token, s.cfg.Auth.JWTSecret)
	if err != nil || claims.Subject == "" {
		return "", false
	}
	return claims.Subject, true
}

// writeCGIResponse parses the CGI header block and copies it + the body to w.
// CGI format: zero or more "Key: Value\r\n" lines, a blank line, then the body.
// An optional "Status: NNN text" header sets the HTTP status code.
func writeCGIResponse(w http.ResponseWriter, r io.Reader) error {
	br := bufio.NewReader(r)
	status := http.StatusOK

	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return fmt.Errorf("read cgi headers: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		parts := strings.SplitN(line, ": ", 2)
		if len(parts) != 2 {
			continue
		}
		key, val := parts[0], parts[1]
		if strings.EqualFold(key, "Status") {
			if code, err := strconv.Atoi(strings.Fields(val)[0]); err == nil {
				status = code
			}
			continue
		}
		w.Header().Add(key, val)
	}

	w.WriteHeader(status)
	_, err := io.Copy(w, br)
	return err
}
