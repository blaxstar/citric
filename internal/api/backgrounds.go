package api

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
)

// allowedImageExts are the image types accepted for background uploads.
var allowedImageExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".gif": true, ".avif": true,
}

var bgNameRE = regexp.MustCompile(`[^a-zA-Z0-9_.-]+`)

func (s *Server) backgroundsDir() string {
	return filepath.Join(s.cfg.Server.DataDir, "uploads", "backgrounds")
}

// handleBackgroundList returns the URLs of all curated background images. Any
// authed user (the appearance drawer lets each client pick one).
func (s *Server) handleBackgroundList(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(s.backgroundsDir())
	if err != nil {
		writeJSON(w, []string{}) // no dir yet → empty gallery
		return
	}
	urls := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if allowedImageExts[strings.ToLower(filepath.Ext(e.Name()))] {
			urls = append(urls, "/uploads/backgrounds/"+e.Name())
		}
	}
	writeJSON(w, urls)
}

// handleBackgroundUpload stores an image in the curated gallery. Admin only.
func (s *Server) handleBackgroundUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "file too large (max 32 MB)", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "field 'file' required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if !allowedImageExts[ext] {
		http.Error(w, "unsupported image type", http.StatusBadRequest)
		return
	}

	dir := s.backgroundsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}

	stem := strings.TrimSuffix(filepath.Base(header.Filename), ext)
	stem = bgNameRE.ReplaceAllString(stem, "-")
	stem = strings.Trim(stem, "-")
	if stem == "" {
		stem = "bg"
	}
	name := stem + ext
	// avoid clobbering an existing gallery image
	for i := 1; ; i++ {
		if _, err := os.Stat(filepath.Join(dir, name)); os.IsNotExist(err) {
			break
		}
		name = fmt.Sprintf("%s-%d%s", stem, i, ext)
	}

	dest, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}
	defer dest.Close()
	if _, err := io.Copy(dest, file); err != nil {
		http.Error(w, "write error", http.StatusInternalServerError)
		return
	}
	s.db.WriteAudit(s.currentUser(r), "background.upload", name, "")
	writeJSON(w, map[string]string{"url": "/uploads/backgrounds/" + name})
}

// handleBackgroundDelete removes a curated background image. Admin only.
func (s *Server) handleBackgroundDelete(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(chi.URLParam(r, "name"))
	if name == "" || name == "." || name == "/" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	if err := os.Remove(filepath.Join(s.backgroundsDir(), name)); err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}
	s.db.WriteAudit(s.currentUser(r), "background.delete", name, "")
	w.WriteHeader(http.StatusNoContent)
}
