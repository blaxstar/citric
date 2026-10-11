package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/blaxstar/citric/internal/model"
)

func (d *DB) CreateUser(u *model.User) error {
	_, err := d.sql.Exec(`
		INSERT INTO users (username, email, ssh_key, role, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		u.Username, u.Email, u.SSHKey, u.Role, time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

func (d *DB) GetUser(username string) (*model.User, error) {
	row := d.sql.QueryRow(`SELECT username, email, ssh_key, role, created_at FROM users WHERE username = ?`, username)
	return scanUser(row)
}

func (d *DB) GetUserByEmail(email string) (*model.User, error) {
	row := d.sql.QueryRow(`SELECT username, email, ssh_key, role, created_at FROM users WHERE email = ?`, email)
	return scanUser(row)
}

func (d *DB) ListUsers() ([]*model.User, error) {
	rows, err := d.sql.Query(`SELECT username, email, ssh_key, role, created_at FROM users ORDER BY username`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	out := make([]*model.User, 0)
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (d *DB) UpdateUser(username string, role model.Role, sshKey string) error {
	_, err := d.sql.Exec(`UPDATE users SET role = ?, ssh_key = ? WHERE username = ?`, role, sshKey, username)
	return err
}

// SetUserRole updates only a user's role. Used to reflect Keycloak realm-role
// membership (admin determination is centralized in Keycloak).
func (d *DB) SetUserRole(username string, role model.Role) error {
	_, err := d.sql.Exec(`UPDATE users SET role = ? WHERE username = ?`, role, username)
	return err
}

func (d *DB) DeleteUser(username string) error {
	_, err := d.sql.Exec(`DELETE FROM users WHERE username = ?`, username)
	return err
}

// UpsertKeycloakUser inserts a user synced from Keycloak if they don't exist,
// or updates their email if it changed. Role is set to 'reviewer' on creation
// and never downgraded — existing users keep whatever role they have.
func (d *DB) UpsertKeycloakUser(username, email string) error {
	_, err := d.sql.Exec(`
		INSERT INTO users (username, email, ssh_key, role, created_at)
		VALUES (?, ?, '', 'reviewer', ?)
		ON CONFLICT(username) DO UPDATE SET email = excluded.email`,
		username, email, time.Now().Unix(),
	)
	return err
}

func (d *DB) WriteAudit(actor, action, target, detail string) error {
	_, err := d.sql.Exec(`INSERT INTO audit (actor, action, target, detail, created_at) VALUES (?, ?, ?, ?, ?)`,
		actor, action, target, detail, time.Now().Unix())
	return err
}

func scanUser(s scanner) (*model.User, error) {
	u := &model.User{}
	var createdAt int64
	err := s.Scan(&u.Username, &u.Email, &u.SSHKey, &u.Role, &createdAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan user: %w", err)
	}
	u.CreatedAt = time.Unix(createdAt, 0)
	return u, nil
}

// CountAdmins returns how many users hold the admin role.
func (d *DB) CountAdmins() (int, error) {
	var n int
	err := d.sql.QueryRow(`SELECT COUNT(*) FROM users WHERE role = ?`, model.RoleAdmin).Scan(&n)
	return n, err
}
