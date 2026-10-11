package store

import (
	"fmt"
	"time"

	"github.com/blaxstar/citric/internal/model"
)

func (d *DB) CreateBuild(b *model.Build) (int64, error) {
	res, err := d.sql.Exec(`
		INSERT INTO builds (cl_id, ps_id, tool_name, status) VALUES (?, ?, ?, ?)`,
		b.CLID, b.PSID, b.ToolName, model.BuildStatusQueued,
	)
	if err != nil {
		return 0, fmt.Errorf("create build: %w", err)
	}
	return res.LastInsertId()
}

func (d *DB) GetBuild(id int64) (*model.Build, error) {
	row := d.sql.QueryRow(`SELECT id, cl_id, ps_id, tool_name, status, log_path, node, started_at, ended_at FROM builds WHERE id = ?`, id)
	return scanBuild(row)
}

func (d *DB) ListBuilds(clID, psID int64) ([]*model.Build, error) {
	rows, err := d.sql.Query(`SELECT id, cl_id, ps_id, tool_name, status, log_path, node, started_at, ended_at FROM builds WHERE cl_id = ? AND ps_id = ?`, clID, psID)
	if err != nil {
		return nil, fmt.Errorf("list builds: %w", err)
	}
	defer rows.Close()

	out := make([]*model.Build, 0)
	for rows.Next() {
		b, err := scanBuild(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (d *DB) UpdateBuildStatus(id int64, status model.BuildStatus) error {
	now := time.Now().Unix()
	switch status {
	case model.BuildStatusRunning:
		_, err := d.sql.Exec(`UPDATE builds SET status = ?, started_at = ? WHERE id = ?`, status, now, id)
		return err
	case model.BuildStatusPass, model.BuildStatusFail:
		_, err := d.sql.Exec(`UPDATE builds SET status = ?, ended_at = ? WHERE id = ?`, status, now, id)
		return err
	default:
		_, err := d.sql.Exec(`UPDATE builds SET status = ? WHERE id = ?`, status, id)
		return err
	}
}

func (d *DB) SetBuildLogPath(id int64, logPath string) error {
	_, err := d.sql.Exec(`UPDATE builds SET log_path = ? WHERE id = ?`, logPath, id)
	return err
}

// SetBuildNode records which node a build was dispatched to.
func (d *DB) SetBuildNode(id int64, node string) error {
	_, err := d.sql.Exec(`UPDATE builds SET node = ? WHERE id = ?`, node, id)
	return err
}

func scanBuild(s scanner) (*model.Build, error) {
	b := &model.Build{}
	var logPath, node *string
	var startedAt, endedAt *int64
	err := s.Scan(&b.ID, &b.CLID, &b.PSID, &b.ToolName, &b.Status, &logPath, &node, &startedAt, &endedAt)
	if err != nil {
		return nil, fmt.Errorf("scan build: %w", err)
	}
	if logPath != nil {
		b.LogPath = *logPath
	}
	if node != nil {
		b.Node = *node
	}
	if startedAt != nil {
		t := time.Unix(*startedAt, 0)
		b.StartedAt = &t
	}
	if endedAt != nil {
		t := time.Unix(*endedAt, 0)
		b.EndedAt = &t
	}
	return b, nil
}
