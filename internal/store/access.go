package store

import "fmt"

// IsRepoRestricted reports whether a repo limits view access.
func (d *DB) IsRepoRestricted(repo string) (bool, error) {
	var restricted int
	err := d.sql.QueryRow(`SELECT restricted FROM repo_meta WHERE repo = ?`, repo).Scan(&restricted)
	if err != nil {
		// no row → open
		return false, nil
	}
	return restricted == 1, nil
}

// GetRepoACL returns the allowed usernames and roles for a repo.
func (d *DB) GetRepoACL(repo string) (users, roles []string, err error) {
	rows, err := d.sql.Query(`SELECT principal, kind FROM repo_acl WHERE repo = ? ORDER BY principal`, repo)
	if err != nil {
		return nil, nil, fmt.Errorf("get repo acl: %w", err)
	}
	defer rows.Close()
	users, roles = []string{}, []string{}
	for rows.Next() {
		var principal, kind string
		if err := rows.Scan(&principal, &kind); err != nil {
			return nil, nil, err
		}
		if kind == "role" {
			roles = append(roles, principal)
		} else {
			users = append(users, principal)
		}
	}
	return users, roles, rows.Err()
}

// SetRepoAccess replaces a repo's restricted flag and full allow-list in one txn.
func (d *DB) SetRepoAccess(repo string, restricted bool, users, roles []string) error {
	tx, err := d.sql.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	r := 0
	if restricted {
		r = 1
	}
	if _, err := tx.Exec(`
		INSERT INTO repo_meta (repo, restricted) VALUES (?, ?)
		ON CONFLICT(repo) DO UPDATE SET restricted = excluded.restricted`, repo, r); err != nil {
		return fmt.Errorf("set repo_meta: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM repo_acl WHERE repo = ?`, repo); err != nil {
		return fmt.Errorf("clear repo_acl: %w", err)
	}
	for _, u := range users {
		if u == "" {
			continue
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO repo_acl (repo, principal, kind) VALUES (?, ?, 'user')`, repo, u); err != nil {
			return err
		}
	}
	for _, role := range roles {
		if role == "" {
			continue
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO repo_acl (repo, principal, kind) VALUES (?, ?, 'role')`, repo, role); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CanViewRepo reports whether a user may view a repo. Open repos and admins
// always pass; otherwise the user must be an allow-listed username, or hold an
// allow-listed role (per user_roles).
func (d *DB) CanViewRepo(username string, isAdmin bool, repo string) (bool, error) {
	restricted, err := d.IsRepoRestricted(repo)
	if err != nil {
		return false, err
	}
	if !restricted || isAdmin {
		return true, nil
	}
	// user match
	var n int
	if err := d.sql.QueryRow(
		`SELECT COUNT(1) FROM repo_acl WHERE repo = ? AND kind = 'user' AND principal = ?`,
		repo, username).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}
	// role match: any of the user's roles is allow-listed
	if err := d.sql.QueryRow(`
		SELECT COUNT(1) FROM repo_acl a
		JOIN user_roles ur ON ur.role = a.principal
		WHERE a.repo = ? AND a.kind = 'role' AND ur.username = ?`,
		repo, username).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetUserRoles returns the cached effective realm roles for a user.
func (d *DB) GetUserRoles(username string) ([]string, error) {
	rows, err := d.sql.Query(`SELECT role FROM user_roles WHERE username = ?`, username)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// HasUserRole reports whether the user has the given realm role cached.
func (d *DB) HasUserRole(username, role string) bool {
	var n int
	d.sql.QueryRow(`SELECT COUNT(1) FROM user_roles WHERE username = ? AND role = ?`, username, role).Scan(&n)
	return n > 0
}

// ReplaceUserRoles overwrites the cached effective roles for a user.
func (d *DB) ReplaceUserRoles(username string, roles []string) error {
	tx, err := d.sql.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM user_roles WHERE username = ?`, username); err != nil {
		return err
	}
	for _, role := range roles {
		if role == "" {
			continue
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO user_roles (username, role) VALUES (?, ?)`, username, role); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListKnownRoles returns every role or group name recorded from people's
// sign-in tokens, sorted. With an identity provider that has no admin API
// for listing roles, this is the set the access UI can suggest.
func (d *DB) ListKnownRoles() ([]string, error) {
	rows, err := d.sql.Query(`SELECT DISTINCT role FROM user_roles ORDER BY role`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
