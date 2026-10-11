package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/blaxstar/citric/internal/model"
)

type CLFilter struct {
	Status   string
	Author   string
	Reviewer string
}

func (d *DB) CreateCL(cl *model.CL) (int64, error) {
	now := time.Now().Unix()
	res, err := d.sql.Exec(`
		INSERT INTO cls (change_id, repo, target_branch, author, title, description, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		cl.ChangeID, cl.Repo, cl.TargetBranch, cl.Author,
		cl.Title, cl.Description, model.CLStatusOpen, now, now,
	)
	if err != nil {
		return 0, fmt.Errorf("create cl: %w", err)
	}
	return res.LastInsertId()
}

func (d *DB) GetCL(id int64) (*model.CL, error) {
	row := d.sql.QueryRow(`SELECT id, change_id, repo, target_branch, author, title, description, status, created_at, updated_at FROM cls WHERE id = ?`, id)
	return scanCL(row)
}

func (d *DB) GetCLByChangeID(changeID string) (*model.CL, error) {
	row := d.sql.QueryRow(`SELECT id, change_id, repo, target_branch, author, title, description, status, created_at, updated_at FROM cls WHERE change_id = ?`, changeID)
	return scanCL(row)
}

// GetCLByCommitSHA returns the CL that has a patchset at the given commit, or
// nil if no CL covers it. Used to name the blocking CL when a submit is refused
// for sitting on an unmerged commit — "CL-84 is not merged yet" beats printing
// a bare SHA the author then has to go look up.
func (d *DB) GetCLByCommitSHA(sha string) (*model.CL, error) {
	row := d.sql.QueryRow(`
		SELECT c.id, c.change_id, c.repo, c.target_branch, c.author, c.title,
		       c.description, c.status, c.created_at, c.updated_at
		FROM cls c
		JOIN patchsets p ON p.cl_id = c.id
		WHERE p.commit_sha = ?
		ORDER BY p.number DESC
		LIMIT 1`, sha)
	return scanCL(row)
}

func (d *DB) ListCLs(f CLFilter) ([]*model.CL, error) {
	// LEFT JOIN each CL's latest patchset (highest number) to expose its tip/base
	// commit — lets clients group CLs pushed together into a stack without an
	// extra request per CL. LEFT (not INNER) so a CL somehow missing a patchset
	// row still comes back, just with empty SHAs, rather than vanishing from the list.
	q := `
		SELECT c.id, c.change_id, c.repo, c.target_branch, c.author, c.title,
		       c.description, c.status, c.created_at, c.updated_at,
		       COALESCE(p.commit_sha, ''), COALESCE(p.base_sha, '')
		FROM cls c
		LEFT JOIN patchsets p ON p.cl_id = c.id
			AND p.number = (SELECT MAX(number) FROM patchsets WHERE cl_id = c.id)
		WHERE 1=1`
	args := []any{}
	if f.Status != "" {
		q += " AND c.status = ?"
		args = append(args, f.Status)
	}
	if f.Author != "" {
		q += " AND c.author = ?"
		args = append(args, f.Author)
	}
	if f.Reviewer != "" {
		// CLs the user is assigned to review (manual or auto-added).
		q += " AND c.id IN (SELECT cl_id FROM cl_reviewers WHERE username = ?)"
		args = append(args, f.Reviewer)
	}
	q += " ORDER BY c.updated_at DESC"

	rows, err := d.sql.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("list cls: %w", err)
	}
	defer rows.Close()

	cls := make([]*model.CL, 0)
	for rows.Next() {
		cl := &model.CL{}
		var createdAt, updatedAt int64
		if err := rows.Scan(&cl.ID, &cl.ChangeID, &cl.Repo, &cl.TargetBranch, &cl.Author,
			&cl.Title, &cl.Description, &cl.Status, &createdAt, &updatedAt,
			&cl.LatestCommitSHA, &cl.LatestBaseSHA); err != nil {
			return nil, fmt.Errorf("scan cl: %w", err)
		}
		cl.CreatedAt = time.Unix(createdAt, 0)
		cl.UpdatedAt = time.Unix(updatedAt, 0)
		cls = append(cls, cl)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(cls) > 0 {
		ids := make([]int64, len(cls))
		byID := make(map[int64]*model.CL, len(cls))
		for i, cl := range cls {
			ids[i] = cl.ID
			byID[cl.ID] = cl
		}
		reviewers, err := d.ReviewersForCLs(ids)
		if err != nil {
			return nil, err
		}
		for id, names := range reviewers {
			byID[id].Reviewers = names
		}
	}
	return cls, nil
}

func (d *DB) UpdateCLStatus(id int64, status model.CLStatus) error {
	_, err := d.sql.Exec(`UPDATE cls SET status = ?, updated_at = ? WHERE id = ?`, status, time.Now().Unix(), id)
	return err
}

func (d *DB) CreatePatchset(ps *model.Patchset) (int64, error) {
	res, err := d.sql.Exec(`
		INSERT INTO patchsets (cl_id, number, commit_sha, base_sha, ref, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		ps.CLID, ps.Number, ps.CommitSHA, ps.BaseSHA, ps.Ref, time.Now().Unix(),
	)
	if err != nil {
		return 0, fmt.Errorf("create patchset: %w", err)
	}
	return res.LastInsertId()
}

func (d *DB) ListPatchsets(clID int64) ([]*model.Patchset, error) {
	rows, err := d.sql.Query(`SELECT id, cl_id, number, commit_sha, base_sha, ref, created_at FROM patchsets WHERE cl_id = ? ORDER BY number`, clID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*model.Patchset, 0)
	for rows.Next() {
		ps := &model.Patchset{}
		var createdAt int64
		if err := rows.Scan(&ps.ID, &ps.CLID, &ps.Number, &ps.CommitSHA, &ps.BaseSHA, &ps.Ref, &createdAt); err != nil {
			return nil, err
		}
		ps.CreatedAt = time.Unix(createdAt, 0)
		out = append(out, ps)
	}
	return out, rows.Err()
}

func (d *DB) LatestPatchset(clID int64) (*model.Patchset, error) {
	row := d.sql.QueryRow(`SELECT id, cl_id, number, commit_sha, base_sha, ref, created_at FROM patchsets WHERE cl_id = ? ORDER BY number DESC LIMIT 1`, clID)
	ps := &model.Patchset{}
	var createdAt int64
	err := row.Scan(&ps.ID, &ps.CLID, &ps.Number, &ps.CommitSHA, &ps.BaseSHA, &ps.Ref, &createdAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ps.CreatedAt = time.Unix(createdAt, 0)
	return ps, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanCL(s scanner) (*model.CL, error) {
	cl := &model.CL{}
	var createdAt, updatedAt int64
	err := s.Scan(&cl.ID, &cl.ChangeID, &cl.Repo, &cl.TargetBranch, &cl.Author,
		&cl.Title, &cl.Description, &cl.Status, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan cl: %w", err)
	}
	cl.CreatedAt = time.Unix(createdAt, 0)
	cl.UpdatedAt = time.Unix(updatedAt, 0)
	return cl, nil
}
