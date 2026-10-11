package store

import (
	"database/sql"
	"fmt"
)

// GetSetting returns the value for key, or "" if it is not set.
func (d *DB) GetSetting(key string) (string, error) {
	var v string
	err := d.sql.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get setting %q: %w", key, err)
	}
	return v, nil
}

// SetSetting upserts a single key/value pair.
func (d *DB) SetSetting(key, value string) error {
	_, err := d.sql.Exec(`
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	if err != nil {
		return fmt.Errorf("set setting %q: %w", key, err)
	}
	return nil
}

// AllSettings returns every key/value pair as a map.
func (d *DB) AllSettings() (map[string]string, error) {
	rows, err := d.sql.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return nil, fmt.Errorf("all settings: %w", err)
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}
