package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS cameras (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE COLLATE NOCASE,
			shortcut TEXT UNIQUE COLLATE NOCASE,
			url TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS authorized_chats (
			chat_id INTEGER NOT NULL,
			message_thread_id INTEGER NOT NULL DEFAULT 0,
			chat_type TEXT,
			chat_title TEXT,
			approved_by_id INTEGER,
			approved_by_username TEXT,
			approved_at TEXT,
			PRIMARY KEY (chat_id, message_thread_id)
		)`,
		`CREATE TABLE IF NOT EXISTS pending_access_requests (
			chat_id INTEGER NOT NULL,
			message_thread_id INTEGER NOT NULL DEFAULT 0,
			chat_type TEXT NOT NULL,
			chat_title TEXT,
			requested_by_id INTEGER NOT NULL,
			requested_by_username TEXT,
			reason TEXT,
			requested_at TEXT NOT NULL,
			PRIMARY KEY (chat_id, message_thread_id)
		)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return runMigrations(db)
}

type migration struct {
	version int
	name    string
	up      []string
}

var migrations = []migration{{
	version: 2,
	name:    "topic scoped authorization",
	up: []string{
		`CREATE TABLE authorized_chats_new (
			chat_id INTEGER NOT NULL,
			message_thread_id INTEGER NOT NULL DEFAULT 0,
			chat_type TEXT,
			chat_title TEXT,
			approved_by_id INTEGER,
			approved_by_username TEXT,
			approved_at TEXT,
			PRIMARY KEY (chat_id, message_thread_id)
		)`,
		`INSERT OR REPLACE INTO authorized_chats_new (chat_id, message_thread_id, chat_type, chat_title, approved_by_id, approved_by_username, approved_at)
			SELECT chat_id, COALESCE(message_thread_id, 0), chat_type, chat_title, approved_by_id, approved_by_username, approved_at
			FROM authorized_chats`,
		`DROP TABLE authorized_chats`,
		`ALTER TABLE authorized_chats_new RENAME TO authorized_chats`,
		`CREATE TABLE pending_access_requests_new (
			chat_id INTEGER NOT NULL,
			message_thread_id INTEGER NOT NULL DEFAULT 0,
			chat_type TEXT NOT NULL,
			chat_title TEXT,
			requested_by_id INTEGER NOT NULL,
			requested_by_username TEXT,
			reason TEXT,
			requested_at TEXT NOT NULL,
			PRIMARY KEY (chat_id, message_thread_id)
		)`,
		`INSERT OR REPLACE INTO pending_access_requests_new (chat_id, message_thread_id, chat_type, chat_title, requested_by_id, requested_by_username, reason, requested_at)
			SELECT chat_id, COALESCE(message_thread_id, 0), chat_type, chat_title, requested_by_id, requested_by_username, reason, requested_at
			FROM pending_access_requests`,
		`DROP TABLE pending_access_requests`,
		`ALTER TABLE pending_access_requests_new RENAME TO pending_access_requests`,
	},
}}

func runMigrations(db *sql.DB) error {
	if err := baselineSchema(db); err != nil {
		return err
	}
	for _, m := range migrations {
		applied, err := migrationApplied(db, m.version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		if err := applyMigration(db, m); err != nil {
			return err
		}
	}
	return nil
}

func baselineSchema(db *sql.DB) error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if _, err := db.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (1, CURRENT_TIMESTAMP)`); err != nil {
		return err
	}
	if tableHasCompositePrimaryKey(db, "authorized_chats", "chat_id", "message_thread_id") && tableHasCompositePrimaryKey(db, "pending_access_requests", "chat_id", "message_thread_id") {
		_, err := db.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (2, CURRENT_TIMESTAMP)`)
		return err
	}
	return nil
}

func migrationApplied(db *sql.DB, version int) (bool, error) {
	var exists int
	err := db.QueryRow(`SELECT 1 FROM schema_migrations WHERE version = ?`, version).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func applyMigration(db *sql.DB, m migration) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if m.version == 2 {
		if err := ensureMigrationColumn(tx, "authorized_chats", "message_thread_id", `ALTER TABLE authorized_chats ADD COLUMN message_thread_id INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
		if err := ensureMigrationColumn(tx, "pending_access_requests", "message_thread_id", `ALTER TABLE pending_access_requests ADD COLUMN message_thread_id INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}

	for _, stmt := range m.up {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("apply migration %d %s: %w", m.version, m.name, err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, CURRENT_TIMESTAMP)`, m.version); err != nil {
		return err
	}
	return tx.Commit()
}

func ensureMigrationColumn(tx *sql.Tx, table, column, alter string) error {
	found, err := txTableHasColumn(tx, table, column)
	if err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = tx.Exec(alter)
	return err
}

func txTableHasColumn(tx *sql.Tx, table, column string) (bool, error) {
	rows, err := tx.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return false, nil
}

func tableHasCompositePrimaryKey(db *sql.DB, table string, columns ...string) bool {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false
	}
	defer rows.Close()

	pkColumns := make([]string, len(columns))
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return false
		}
		if pk > 0 && pk <= len(pkColumns) {
			pkColumns[pk-1] = name
		}
	}
	if rows.Err() != nil {
		return false
	}
	return strings.Join(pkColumns, ",") == strings.Join(columns, ",")
}
