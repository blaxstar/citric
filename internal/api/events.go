package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

// handleCLEvents is a Server-Sent Events stream that pings whenever the CL
// changes (builds created/started/finished, new patchset). The client re-fetches
// authoritative state on each ping — no payload is sent. GET /cl/{id}/events
//
// Auth is the standard /cl middleware: the browser's EventSource sends the
// crit_session cookie (same-origin), which requireAuth accepts.
func (s *Server) handleCLEvents(w http.ResponseWriter, r *http.Request) {
	clID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
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

	ch, unsub := s.runner.Events().Subscribe(clID)
	defer unsub()

	// Tell the client it's connected (also flushes headers immediately).
	fmt.Fprint(w, "event: update\ndata: 1\n\n")
	flusher.Flush()

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ch:
			fmt.Fprint(w, "event: update\ndata: 1\n\n")
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprint(w, ": ping\n\n") // comment heartbeat keeps proxies from closing the stream
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
