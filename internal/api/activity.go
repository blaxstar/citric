package api

import (
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/blaxstar/citric/internal/model"
	"github.com/blaxstar/citric/internal/store"
	"github.com/go-chi/chi/v5"
)

// handleActivityFeed returns recent CLs from the current user and their followees.
// GET /activity
func (s *Server) handleActivityFeed(w http.ResponseWriter, r *http.Request) {
	me := s.currentUser(r)
	following := s.db.ListFollowing(me)

	authors := append([]string{me}, following...)
	seen := make(map[int64]struct{})
	all := make([]*model.CL, 0)

	for _, author := range authors {
		cls, _ := s.db.ListCLs(store.CLFilter{Author: author})
		for _, cl := range cls {
			if _, dup := seen[cl.ID]; !dup {
				seen[cl.ID] = struct{}{}
				all = append(all, cl)
			}
		}
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].UpdatedAt.After(all[j].UpdatedAt)
	})
	if len(all) > 60 {
		all = all[:60]
	}

	writeJSON(w, all)
}

// handleContributions returns per-day CL submission counts for the contribution graph.
// GET /user/:username/contributions?days=N (default 365, max 730)
func (s *Server) handleContributions(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	days := 365
	if d := r.URL.Query().Get("days"); d != "" {
		if v, err := strconv.Atoi(d); err == nil && v > 0 && v <= 730 {
			days = v
		}
	}

	cls, _ := s.db.ListCLs(store.CLFilter{Author: username})
	since := time.Now().AddDate(0, 0, -days)
	counts := make(map[string]int)

	for _, cl := range cls {
		if cl.CreatedAt.Before(since) {
			continue
		}
		counts[cl.CreatedAt.Format("2006-01-02")]++
	}

	type dayCount struct {
		Date  string `json:"date"`
		Count int    `json:"count"`
	}
	result := make([]dayCount, 0, len(counts))
	for d, c := range counts {
		result = append(result, dayCount{Date: d, Count: c})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Date < result[j].Date })

	writeJSON(w, result)
}
