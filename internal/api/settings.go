package api

import (
	"fmt"
	"net/http"
	"strings"
)

// settingKeys are the settings the admin UI may read and write.
var settingKeys = []string{"public_url", "site_title", "default_palette", settingRequireOtherLGTM}

// publicBaseURL returns the externally-visible base URL (scheme://host, no
// trailing slash). It prefers the admin-configured public_url setting, then the
// inbound request's host (correct behind nginx), then localhost:port.
func (s *Server) publicBaseURL(r *http.Request) string {
	if v, err := s.db.GetSetting("public_url"); err == nil {
		if v = strings.TrimRight(strings.TrimSpace(v), "/"); v != "" {
			return v
		}
	}
	if r != nil && r.Host != "" {
		return reqBaseURL(r)
	}
	host := s.cfg.Server.Host
	if host == "" || host == "0.0.0.0" {
		host = "localhost"
	}
	return fmt.Sprintf("http://%s:%d", host, s.cfg.Server.Port)
}

// handleGetSettings returns the editable settings. Requires auth so the SPA can
// surface site_title and clone hints without exposing them publicly.
func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	all, err := s.db.AllSettings()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := map[string]string{}
	for _, k := range settingKeys {
		out[k] = all[k]
	}
	writeJSON(w, out)
}

// handleUpdateSettings writes the editable settings. Admin only.
func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	if !decodeBody(w, r, &body, `{"<setting-key>": "<value>", ...} — a flat string map`) {
		return
	}
	allowed := map[string]bool{}
	for _, k := range settingKeys {
		allowed[k] = true
	}
	for k, v := range body {
		if !allowed[k] {
			continue
		}
		if err := s.db.SetSetting(k, strings.TrimSpace(v)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	s.db.WriteAudit(s.currentUser(r), "settings.update", "settings", "")
	w.WriteHeader(http.StatusNoContent)
}
