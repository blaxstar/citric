package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/blaxstar/citric/internal/model"
	"github.com/blaxstar/citric/internal/store"
)

// Build node endpoints. Admins mint one-time join tokens; a node trades one
// for a long-lived credential (POST /node/join), then long-polls for work with
// it (POST /node/claim) and reports output and results per job. Nodes only
// ever call out to the server, so they work from behind NAT.

const (
	joinTokenTTL = 24 * time.Hour
	claimWait    = 25 * time.Second
	maxLogChunk  = 1 << 20
)

var nodeNameRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,62}$`)

const ctxNode ctxKey = "node"

func randomSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func bearer(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return ""
}

// requireNode authenticates a build node by its credential.
func (s *Server) requireNode(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cred := bearer(r)
		if cred == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		n, err := s.db.NodeByCredential(cred)
		if err != nil || n == nil {
			http.Error(w, "unknown node credential", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxNode, n)))
	})
}

func currentNode(r *http.Request) *store.Node {
	n, _ := r.Context().Value(ctxNode).(*store.Node)
	return n
}

// isNodeCredential reports whether a git request carries a build node's
// credential, which grants read (fetch) access to every repo.
func (s *Server) isNodeCredential(r *http.Request) bool {
	cred := bearer(r)
	if cred == "" {
		if _, pass, ok := r.BasicAuth(); ok {
			cred = pass
		}
	}
	if cred == "" {
		return false
	}
	n, err := s.db.NodeByCredential(cred)
	return err == nil && n != nil
}

// POST /admin/nodes/join-tokens → {"token": "...", "expires_at": "..."}
func (s *Server) handleNodeJoinToken(w http.ResponseWriter, r *http.Request) {
	token, err := randomSecret()
	if err != nil {
		http.Error(w, "could not generate token", http.StatusInternalServerError)
		return
	}
	expires := time.Now().Add(joinTokenTTL)
	if err := s.db.CreateJoinToken(token, s.currentUser(r), expires); err != nil {
		http.Error(w, "could not store token", http.StatusInternalServerError)
		return
	}
	s.db.WriteAudit(s.currentUser(r), "node.join_token", "", "")
	writeJSON(w, map[string]any{"token": token, "expires_at": expires.UTC()})
}

// GET /admin/nodes
func (s *Server) handleNodeList(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.db.ListNodes()
	if err != nil {
		http.Error(w, "could not list nodes", http.StatusInternalServerError)
		return
	}
	writeJSON(w, nodes)
}

// POST /admin/nodes/{name}/drain and /resume
func (s *Server) handleNodeSetStatus(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		if err := s.db.SetNodeStatus(name, status); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		s.db.WriteAudit(s.currentUser(r), "node."+status, name, "")
		w.WriteHeader(http.StatusNoContent)
	}
}

// DELETE /admin/nodes/{name}
func (s *Server) handleNodeDelete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := s.db.DeleteNode(name); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	s.db.WriteAudit(s.currentUser(r), "node.remove", name, "")
	w.WriteHeader(http.StatusNoContent)
}

// POST /node/join {"token": "...", "name": "..."} → {"name": "...", "credential": "..."}
func (s *Server) handleNodeJoin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !nodeNameRE.MatchString(body.Name) {
		http.Error(w, "node name must be 1-63 letters, digits, '.', '_' or '-'", http.StatusBadRequest)
		return
	}
	cred, err := randomSecret()
	if err != nil {
		http.Error(w, "could not generate credential", http.StatusInternalServerError)
		return
	}
	n, err := s.db.RegisterNode(body.Token, body.Name, cred)
	if err != nil {
		code := http.StatusConflict
		if errors.Is(err, store.ErrInvalidJoinToken) {
			code = http.StatusUnauthorized
		}
		http.Error(w, err.Error(), code)
		return
	}
	s.db.WriteAudit("node:"+n.Name, "node.join", n.Name, "")
	writeJSON(w, map[string]string{"name": n.Name, "credential": cred})
}

// claimRequest is a node's self-report, sent with every claim.
type claimRequest struct {
	OS            string   `json:"os"`
	Arch          string   `json:"arch"`
	Labels        []string `json:"labels"`
	Version       string   `json:"version"`
	MaxConcurrent int      `json:"max_concurrent"`
	InFlight      int      `json:"in_flight"`
}

// POST /node/claim → 200 with a runner.RemoteJob, or 204 when no work came up
// within the long-poll window. Doubles as the node's heartbeat.
func (s *Server) handleNodeClaim(w http.ResponseWriter, r *http.Request) {
	n := currentNode(r)
	var req claimRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 16384)).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	labels := append([]string{req.OS, req.Arch}, req.Labels...)
	if err := s.db.ReportNode(n.ID, store.NodeReport{
		OS: req.OS, Arch: req.Arch, Labels: req.Labels, Version: req.Version,
		MaxConcurrent: req.MaxConcurrent, InFlight: req.InFlight,
	}); err != nil {
		http.Error(w, "could not record node", http.StatusInternalServerError)
		return
	}
	if n.Status != store.NodeActive || (req.MaxConcurrent > 0 && req.InFlight >= req.MaxConcurrent) {
		w.WriteHeader(http.StatusNoContent) // draining, or already full: heartbeat only
		return
	}
	job := s.runner.ClaimRemote(n.Name, labels, claimWait, r.Context().Done())
	if job == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, job)
}

func buildIDParam(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "build_id"), 10, 64)
	return id, err == nil
}

// POST /node/jobs/{build_id}/log  (raw output) → {"cancel": bool}
func (s *Server) handleNodeLog(w http.ResponseWriter, r *http.Request) {
	id, ok := buildIDParam(r)
	if !ok {
		http.Error(w, "bad build id", http.StatusBadRequest)
		return
	}
	chunk, err := io.ReadAll(io.LimitReader(r.Body, maxLogChunk))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	cancel, err := s.runner.RemoteLog(currentNode(r).Name, id, chunk)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, map[string]bool{"cancel": cancel})
}

// POST /node/jobs/{build_id}/done {"status": "pass"|"fail", "error": "..."}
func (s *Server) handleNodeDone(w http.ResponseWriter, r *http.Request) {
	id, ok := buildIDParam(r)
	if !ok {
		http.Error(w, "bad build id", http.StatusBadRequest)
		return
	}
	var body struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 65536)).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := s.runner.RemoteDone(currentNode(r).Name, id, model.BuildStatus(body.Status), body.Error); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
