package api

import (
	"encoding/json"
	"net/http"
)

// handleGetViewSettings returns the caller's saved view preferences (sort
// order, filters, ...) as an opaque JSON object — {} if they've never saved
// any. Scoped to currentUser(r), never a path param, so there's no way to
// read another user's preferences.
func (s *Server) handleGetViewSettings(w http.ResponseWriter, r *http.Request) {
	data, err := s.db.GetViewSettings(s.currentUser(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if data == "" {
		w.Write([]byte("{}"))
		return
	}
	w.Write([]byte(data))
}

// handleUpdateViewSettings replaces the caller's saved view preferences
// wholesale — the frontend sends its full current preferences object, not a
// partial patch. The server doesn't interpret the shape, only validates it's
// well-formed JSON, so the frontend can add new preference keys over time
// without a server change.
func (s *Server) handleUpdateViewSettings(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	if !decodeBody(w, r, &raw, `an arbitrary JSON object of view preferences, e.g. {"cl_list": {"sort": "id_asc"}}`) {
		return
	}
	if err := s.db.SetViewSettings(s.currentUser(r), string(raw)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
