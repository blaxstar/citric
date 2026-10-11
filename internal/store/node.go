package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Node statuses. A draining node finishes what it has but gets no new work.
const (
	NodeActive   = "active"
	NodeDraining = "draining"
)

// Node is a registered build node.
type Node struct {
	ID            int64      `json:"id"`
	Name          string     `json:"name"`
	Status        string     `json:"status"`
	OS            string     `json:"os"`
	Arch          string     `json:"arch"`
	Labels        []string   `json:"labels"`
	Version       string     `json:"version"`
	MaxConcurrent int        `json:"max_concurrent"`
	InFlight      int        `json:"in_flight"`
	LastSeen      *time.Time `json:"last_seen,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

// NodeReport is what a node tells the server each time it asks for work.
type NodeReport struct {
	OS            string
	Arch          string
	Labels        []string
	Version       string
	MaxConcurrent int
	InFlight      int
}

// HashSecret is how node credentials and join tokens are stored at rest.
func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// CreateJoinToken stores a one-time join token (by hash) valid until expires.
func (d *DB) CreateJoinToken(token, createdBy string, expires time.Time) error {
	_, err := d.sql.Exec(`INSERT INTO node_join_tokens (token_hash, created_by, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		HashSecret(token), createdBy, time.Now().Unix(), expires.Unix())
	return err
}

// ErrInvalidJoinToken is returned for unknown, expired or already-used tokens.
var ErrInvalidJoinToken = errors.New("invalid, expired or already-used join token")

// RegisterNode consumes a join token and creates the node with credential.
// Both happen in one transaction, so a token can only ever mint one node.
func (d *DB) RegisterNode(joinToken, name, credential string) (*Node, error) {
	tx, err := d.sql.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	res, err := tx.Exec(`UPDATE node_join_tokens SET used_at = ? WHERE token_hash = ? AND used_at IS NULL AND expires_at > ?`,
		now, HashSecret(joinToken), now)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, ErrInvalidJoinToken
	}
	if _, err := tx.Exec(`INSERT INTO nodes (name, credential_hash, created_at) VALUES (?, ?, ?)`,
		name, HashSecret(credential), now); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, fmt.Errorf("a node named %q already exists", name)
		}
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return d.GetNode(name)
}

const nodeColumns = `id, name, status, os, arch, labels, version, max_concurrent, in_flight, last_seen, created_at`

func scanNode(s scanner) (*Node, error) {
	n := &Node{}
	var labels string
	var lastSeen *int64
	var created int64
	if err := s.Scan(&n.ID, &n.Name, &n.Status, &n.OS, &n.Arch, &labels, &n.Version,
		&n.MaxConcurrent, &n.InFlight, &lastSeen, &created); err != nil {
		return nil, err
	}
	n.Labels = splitLabels(labels)
	if lastSeen != nil {
		t := time.Unix(*lastSeen, 0)
		n.LastSeen = &t
	}
	n.CreatedAt = time.Unix(created, 0)
	return n, nil
}

func splitLabels(s string) []string {
	out := []string{}
	for _, l := range strings.Split(s, ",") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// GetNode returns the named node, or nil when there is none.
func (d *DB) GetNode(name string) (*Node, error) {
	n, err := scanNode(d.sql.QueryRow(`SELECT `+nodeColumns+` FROM nodes WHERE name = ?`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return n, err
}

// NodeByCredential authenticates a node, returning nil for an unknown one.
func (d *DB) NodeByCredential(credential string) (*Node, error) {
	n, err := scanNode(d.sql.QueryRow(`SELECT `+nodeColumns+` FROM nodes WHERE credential_hash = ?`, HashSecret(credential)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return n, err
}

// ListNodes returns every registered node, by name.
func (d *DB) ListNodes() ([]*Node, error) {
	rows, err := d.sql.Query(`SELECT ` + nodeColumns + ` FROM nodes ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Node{}
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ReportNode records a node's latest self-description and marks it seen now.
func (d *DB) ReportNode(id int64, r NodeReport) error {
	_, err := d.sql.Exec(`UPDATE nodes SET os = ?, arch = ?, labels = ?, version = ?, max_concurrent = ?, in_flight = ?, last_seen = ? WHERE id = ?`,
		r.OS, r.Arch, strings.Join(r.Labels, ","), r.Version, r.MaxConcurrent, r.InFlight, time.Now().Unix(), id)
	return err
}

// SetNodeStatus sets a node active or draining.
func (d *DB) SetNodeStatus(name, status string) error {
	if status != NodeActive && status != NodeDraining {
		return fmt.Errorf("invalid node status %q", status)
	}
	res, err := d.sql.Exec(`UPDATE nodes SET status = ? WHERE name = ?`, status, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("no node named %q", name)
	}
	return nil
}

// DeleteNode removes a node; its credential stops working immediately.
func (d *DB) DeleteNode(name string) error {
	res, err := d.sql.Exec(`DELETE FROM nodes WHERE name = ?`, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("no node named %q", name)
	}
	return nil
}
