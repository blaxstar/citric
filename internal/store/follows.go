package store

import "time"

func (d *DB) Follow(follower, followee string) error {
	_, err := d.sql.Exec(
		`INSERT OR IGNORE INTO follows (follower, followee, created_at) VALUES (?, ?, ?)`,
		follower, followee, time.Now().Unix())
	return err
}

func (d *DB) Unfollow(follower, followee string) error {
	_, err := d.sql.Exec(
		`DELETE FROM follows WHERE follower = ? AND followee = ?`,
		follower, followee)
	return err
}

func (d *DB) IsFollowing(follower, followee string) bool {
	var n int
	d.sql.QueryRow(`SELECT COUNT(*) FROM follows WHERE follower = ? AND followee = ?`, follower, followee).Scan(&n)
	return n > 0
}

func (d *DB) ListFollowing(username string) []string {
	rows, err := d.sql.Query(`SELECT followee FROM follows WHERE follower = ? ORDER BY created_at DESC`, username)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

func (d *DB) ListFollowers(username string) []string {
	rows, err := d.sql.Query(`SELECT follower FROM follows WHERE followee = ? ORDER BY created_at DESC`, username)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		rows.Scan(&s)
		out = append(out, s)
	}
	return out
}
