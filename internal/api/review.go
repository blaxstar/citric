package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

func (s *Server) handleReviewList(w http.ResponseWriter, r *http.Request) {
	clID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	ps, err := s.db.LatestPatchset(clID)
	if err != nil || ps == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	reviews, err := s.db.ListReviews(clID, ps.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(reviews)
}

func (s *Server) handleCommentList(w http.ResponseWriter, r *http.Request) {
	clID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	ps, err := s.db.LatestPatchset(clID)
	if err != nil || ps == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	comments, err := s.db.ListComments(clID, ps.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(comments)
}
