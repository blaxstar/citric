package store

import (
	"database/sql"
	_ "embed"
	"fmt"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

type DB struct {
	sql *sql.DB
}

func Open(path string) (*DB, error) {
	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite is single-writer; serialize via connection pool
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	d := &DB{sql: db}
	if err := d.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return d, nil
}

func (d *DB) Close() error {
	return d.sql.Close()
}

// Ping checks the database answers a trivial query.
func (d *DB) Ping() error {
	var one int
	return d.sql.QueryRow(`SELECT 1`).Scan(&one)
}

// Snapshot writes a consistent copy of the live database to dest (which must
// not exist yet) without stopping the server, via SQLite's VACUUM INTO.
func (d *DB) Snapshot(dest string) error {
	if _, err := d.sql.Exec(`VACUUM INTO ?`, dest); err != nil {
		return fmt.Errorf("snapshot database: %w", err)
	}
	return nil
}

// migrate applies changes CREATE TABLE IF NOT EXISTS can't express: columns
// added to existing tables. Each step checks before acting, so it is safe on
// every start.
func (d *DB) migrate() error {
	if err := d.addColumn("builds", "node", "TEXT"); err != nil {
		return err
	}
	return nil
}

func (d *DB) addColumn(table, column, decl string) error {
	rows, err := d.sql.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()
	if _, err := d.sql.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, decl)); err != nil {
		return fmt.Errorf("add %s.%s: %w", table, column, err)
	}
	return nil
}
