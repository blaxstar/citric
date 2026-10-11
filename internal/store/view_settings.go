package store

import (
	"database/sql"
	"time"
)

// GetViewSettings returns the raw JSON blob of view preferences saved for
// username, or "" if the user has never saved any.
func (d *DB) GetViewSettings(username string) (string, error) {
	var data string
	err := d.sql.QueryRow(`SELECT data FROM user_view_settings WHERE username = ?`, username).Scan(&data)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return data, nil
}

// SetViewSettings replaces the saved view-preferences blob for username.
// data is stored opaquely — the caller (the API handler) is responsible for
// validating it's well-formed JSON before it gets here.
func (d *DB) SetViewSettings(username, data string) error {
	_, err := d.sql.Exec(`
		INSERT INTO user_view_settings (username, data, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(username) DO UPDATE SET data = excluded.data, updated_at = excluded.updated_at`,
		username, data, time.Now().Unix(),
	)
	return err
}
