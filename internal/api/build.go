package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/blaxstar/citric/internal/model"
	"github.com/go-chi/chi/v5"
)

func (s *Server) handleBuildList(w http.ResponseWriter, r *http.Request) {
	clID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	ps, err := s.db.LatestPatchset(clID)
	if err != nil || ps == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	builds, err := s.db.ListBuilds(clID, ps.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(builds)
}

func (s *Server) handleBuildGet(w http.ResponseWriter, r *http.Request) {
	buildID, _ := strconv.ParseInt(chi.URLParam(r, "build_id"), 10, 64)
	b, err := s.db.GetBuild(buildID)
	if err != nil || b == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(b)
}

func (s *Server) handleBuildStream(w http.ResponseWriter, r *http.Request) {
	buildID, _ := strconv.ParseInt(chi.URLParam(r, "build_id"), 10, 64)

	b, err := s.db.GetBuild(buildID)
	if err != nil || b == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	sendLine := func(line string) {
		fmt.Fprintf(w, "data: %s\n\n", strings.TrimRight(line, "\n"))
		flusher.Flush()
	}
	sendDone := func(status model.BuildStatus) {
		fmt.Fprintf(w, "event: done\ndata: %s\n\n", status)
		flusher.Flush()
	}
	streamFile := func(path string) {
		if path == "" {
			return
		}
		f, err := os.Open(path)
		if err != nil {
			return
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			sendLine(scanner.Text())
		}
	}

	// Build already done — serve the log file and exit
	if b.Status == model.BuildStatusPass || b.Status == model.BuildStatusFail {
		streamFile(b.LogPath)
		sendDone(b.Status)
		return
	}

	// Subscribe before re-checking status so we don't miss the completion signal
	ch, unsub := s.runner.Stream().Subscribe(buildID)
	defer unsub()

	// Re-check in case the build finished in the window between our first GetBuild and Subscribe
	if b2, err := s.db.GetBuild(buildID); err == nil && b2 != nil {
		if b2.Status == model.BuildStatusPass || b2.Status == model.BuildStatusFail {
			unsub()
			streamFile(b2.LogPath)
			sendDone(b2.Status)
			return
		}
	}

	// Catch up on any output already written to disk
	streamFile(b.LogPath)

	for {
		select {
		case line, ok := <-ch:
			if !ok {
				if final, err := s.db.GetBuild(buildID); err == nil && final != nil {
					sendDone(final.Status)
				}
				return
			}
			sendLine(line)
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) handleBuildCancel(w http.ResponseWriter, r *http.Request) {
	buildID, _ := strconv.ParseInt(chi.URLParam(r, "build_id"), 10, 64)
	b, err := s.db.GetBuild(buildID)
	if err != nil || b == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if b.Status != model.BuildStatusRunning && b.Status != model.BuildStatusQueued {
		http.Error(w, "build is not active", http.StatusConflict)
		return
	}
	if !s.runner.CancelBuild(buildID) {
		http.Error(w, "build not active in runner", http.StatusGone)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handleBuildTrigger(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	cl, err := s.db.GetCL(id)
	if err != nil || cl == nil {
		http.Error(w, "cl not found", http.StatusNotFound)
		return
	}
	ps, err := s.db.LatestPatchset(id)
	if err != nil || ps == nil {
		http.Error(w, "no patchsets", http.StatusNotFound)
		return
	}

	// Optional body: {"tools": ["lint", "test"]} — omit or empty to run all.
	var body struct {
		Tools []string `json:"tools"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	if err := s.runner.Run(cl, ps, body.Tools); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// handleToolList returns the server-configured tool names and their required flag.
// GET /tools
func (s *Server) handleToolList(w http.ResponseWriter, r *http.Request) {
	type toolInfo struct {
		Name      string   `json:"name"`
		Required  bool     `json:"required"`
		DependsOn []string `json:"depends_on"`
	}
	out := make([]toolInfo, 0, len(s.cfg.BuildTools))
	for _, t := range s.cfg.BuildTools {
		deps := t.DependsOn
		if deps == nil {
			deps = []string{}
		}
		out = append(out, toolInfo{Name: t.Name, Required: t.Required, DependsOn: deps})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}
