package store

import "time"

func (d *DB) StarRepo(username, repo string) error {
	_, err := d.sql.Exec(
		`INSERT OR IGNORE INTO stars (username, repo, created_at) VALUES (?, ?, ?)`,
		username, repo, time.Now().Unix())
	return err
}

func (d *DB) UnstarRepo(username, repo string) error {
	_, err := d.sql.Exec(`DELETE FROM stars WHERE username = ? AND repo = ?`, username, repo)
	return err
}

func (d *DB) IsStarred(username, repo string) bool {
	var n int
	d.sql.QueryRow(`SELECT COUNT(*) FROM stars WHERE username = ? AND repo = ?`, username, repo).Scan(&n)
	return n > 0
}

func (d *DB) ListStarred(username string) []string {
	rows, err := d.sql.Query(`SELECT repo FROM stars WHERE username = ? ORDER BY created_at DESC`, username)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var repos []string
	for rows.Next() {
		var r string
		if rows.Scan(&r) == nil {
			repos = append(repos, r)
		}
	}
	return repos
}

func (d *DB) GetRepoStarCount(repo string) int {
	var n int
	d.sql.QueryRow(`SELECT COUNT(*) FROM stars WHERE repo = ?`, repo).Scan(&n)
	return n
}
