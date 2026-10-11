package store

import (
	"fmt"
	"time"

	"github.com/blaxstar/citric/internal/model"
)

// ProfileComment is a comment enriched with its CL's repo and title, for a
// user's profile activity feed.
type ProfileComment struct {
	ID        int64     `json:"id"`
	CLID      int64     `json:"cl_id"`
	Repo      string    `json:"repo"`
	CLTitle   string    `json:"cl_title"`
	FilePath  string    `json:"file_path"`
	Line      *int      `json:"line"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

// ProfileReview is a review enriched with its CL's repo and title.
type ProfileReview struct {
	CLID      int64         `json:"cl_id"`
	Repo      string        `json:"repo"`
	CLTitle   string        `json:"cl_title"`
	Verdict   model.Verdict `json:"verdict"`
	CreatedAt time.Time     `json:"created_at"`
}

// ListCommentsByAuthor returns a user's most recent comments, newest first.
func (d *DB) ListCommentsByAuthor(username string, limit int) ([]*ProfileComment, error) {
	rows, err := d.sql.Query(`
		SELECT c.id, c.cl_id, cl.repo, cl.title, c.file_path, c.line, c.body, c.created_at
		FROM comments c JOIN cls cl ON cl.id = c.cl_id
		WHERE c.author = ?
		ORDER BY c.created_at DESC
		LIMIT ?`, username, limit)
	if err != nil {
		return nil, fmt.Errorf("list comments by author: %w", err)
	}
	defer rows.Close()

	out := make([]*ProfileComment, 0)
	for rows.Next() {
		c := &ProfileComment{}
		var createdAt int64
		if err := rows.Scan(&c.ID, &c.CLID, &c.Repo, &c.CLTitle, &c.FilePath, &c.Line, &c.Body, &createdAt); err != nil {
			return nil, err
		}
		c.CreatedAt = time.Unix(createdAt, 0)
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListReviewsByAuthor returns a user's most recent reviews, newest first.
func (d *DB) ListReviewsByAuthor(username string, limit int) ([]*ProfileReview, error) {
	rows, err := d.sql.Query(`
		SELECT r.cl_id, cl.repo, cl.title, r.verdict, r.created_at
		FROM reviews r JOIN cls cl ON cl.id = r.cl_id
		WHERE r.reviewer = ?
		ORDER BY r.created_at DESC
		LIMIT ?`, username, limit)
	if err != nil {
		return nil, fmt.Errorf("list reviews by author: %w", err)
	}
	defer rows.Close()

	out := make([]*ProfileReview, 0)
	for rows.Next() {
		rv := &ProfileReview{}
		var createdAt int64
		if err := rows.Scan(&rv.CLID, &rv.Repo, &rv.CLTitle, &rv.Verdict, &createdAt); err != nil {
			return nil, err
		}
		rv.CreatedAt = time.Unix(createdAt, 0)
		out = append(out, rv)
	}
	return out, rows.Err()
}
