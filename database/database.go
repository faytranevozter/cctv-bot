package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

func Open(path string) (*sql.DB, error) {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
	}

	dsn := path
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		dsn = fmt.Sprintf("file:%s", path)
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := initDB(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func initDB(db *sql.DB) error {
	stmts := []string{
		`PRAGMA journal_mode = WAL`,
		`PRAGMA busy_timeout = 5000`,
		`PRAGMA foreign_keys = ON`,
		`CREATE TABLE IF NOT EXISTS cameras (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE COLLATE NOCASE,
			shortcut TEXT UNIQUE COLLATE NOCASE,
			url TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS authorized_chats (
			chat_id INTEGER PRIMARY KEY,
			chat_type TEXT,
			chat_title TEXT,
			approved_by_id INTEGER,
			approved_by_username TEXT,
			approved_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS pending_access_requests (
			chat_id INTEGER PRIMARY KEY,
			chat_type TEXT NOT NULL,
			chat_title TEXT,
			requested_by_id INTEGER NOT NULL,
			requested_by_username TEXT,
			reason TEXT,
			requested_at TEXT NOT NULL
		)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
