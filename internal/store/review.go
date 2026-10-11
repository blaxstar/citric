package store

import (
	"fmt"
	"strings"
	"time"

	"github.com/blaxstar/citric/internal/model"
)

func (d *DB) UpsertReview(r *model.Review) error {
	_, err := d.sql.Exec(`
		INSERT INTO reviews (cl_id, ps_id, reviewer, verdict, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(cl_id, ps_id, reviewer) DO UPDATE SET verdict = excluded.verdict`,
		r.CLID, r.PSID, r.Reviewer, r.Verdict, time.Now().Unix(),
	)
	return err
}

func (d *DB) ListReviews(clID, psID int64) ([]*model.Review, error) {
	rows, err := d.sql.Query(`SELECT id, cl_id, ps_id, reviewer, verdict, created_at FROM reviews WHERE cl_id = ? AND ps_id = ?`, clID, psID)
	if err != nil {
		return nil, fmt.Errorf("list reviews: %w", err)
	}
	defer rows.Close()

	out := make([]*model.Review, 0)
	for rows.Next() {
		r := &model.Review{}
		var createdAt int64
		if err := rows.Scan(&r.ID, &r.CLID, &r.PSID, &r.Reviewer, &r.Verdict, &createdAt); err != nil {
			return nil, err
		}
		r.CreatedAt = time.Unix(createdAt, 0)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) CreateComment(c *model.Comment) (int64, error) {
	res, err := d.sql.Exec(`
		INSERT INTO comments (cl_id, ps_id, author, file_path, line, body, parent_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		c.CLID, c.PSID, c.Author, c.FilePath, c.Line, c.Body, c.ParentID, time.Now().Unix(),
	)
	if err != nil {
		return 0, fmt.Errorf("create comment: %w", err)
	}
	return res.LastInsertId()
}

func (d *DB) ListComments(clID, psID int64) ([]*model.Comment, error) {
	rows, err := d.sql.Query(`SELECT id, cl_id, ps_id, author, file_path, line, body, parent_id, created_at FROM comments WHERE cl_id = ? AND ps_id = ? ORDER BY created_at`, clID, psID)
	if err != nil {
		return nil, fmt.Errorf("list comments: %w", err)
	}
	defer rows.Close()

	out := make([]*model.Comment, 0)
	for rows.Next() {
		c := &model.Comment{}
		var createdAt int64
		if err := rows.Scan(&c.ID, &c.CLID, &c.PSID, &c.Author, &c.FilePath, &c.Line, &c.Body, &c.ParentID, &createdAt); err != nil {
			return nil, err
		}
		c.CreatedAt = time.Unix(createdAt, 0)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (d *DB) AddReviewer(clID int64, username string, source model.ReviewerSource) error {
	_, err := d.sql.Exec(`
		INSERT OR IGNORE INTO cl_reviewers (cl_id, username, source) VALUES (?, ?, ?)`,
		clID, username, source,
	)
	return err
}

// ReviewersForCLs batches the cl_reviewers lookup for a set of CLs into one
// query instead of one query per CL — used by ListCLs to populate CL.Reviewers
// for the list view (previously never populated at all: the frontend's
// reviewer-avatars row has been dead code since cl_reviewers was write-only).
func (d *DB) ReviewersForCLs(clIDs []int64) (map[int64][]string, error) {
	if len(clIDs) == 0 {
		return map[int64][]string{}, nil
	}
	placeholders := make([]string, len(clIDs))
	args := make([]any, len(clIDs))
	for i, id := range clIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	q := `SELECT cl_id, username FROM cl_reviewers WHERE cl_id IN (` +
		strings.Join(placeholders, ",") + `) ORDER BY cl_id, username`

	rows, err := d.sql.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("list reviewers for cls: %w", err)
	}
	defer rows.Close()

	out := make(map[int64][]string)
	for rows.Next() {
		var clID int64
		var username string
		if err := rows.Scan(&clID, &username); err != nil {
			return nil, fmt.Errorf("scan reviewer: %w", err)
		}
		out[clID] = append(out[clID], username)
	}
	return out, rows.Err()
}
