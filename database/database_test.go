package database

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestOpenCreatesSchemaAndParentDirectory(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "nested", "cctv.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })

	for _, table := range []string{"cameras", "authorized_chats", "pending_access_requests"} {
		if !tableExists(t, db, table) {
			t.Fatalf("table %s does not exist", table)
		}
	}
	if !columnExists(t, db, "pending_access_requests", "message_thread_id") {
		t.Fatalf("pending_access_requests.message_thread_id does not exist")
	}
}

func TestOpenMigratesLegacyPendingRequestTable(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	_, err = db.Exec(`CREATE TABLE pending_access_requests (
		chat_id INTEGER PRIMARY KEY,
		chat_type TEXT NOT NULL,
		chat_title TEXT,
		requested_by_id INTEGER NOT NULL,
		requested_by_username TEXT,
		reason TEXT,
		requested_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatalf("create legacy table: %v", err)
	}
	db.Close()

	db, err = Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if !columnExists(t, db, "pending_access_requests", "message_thread_id") {
		t.Fatalf("legacy table was not migrated")
	}
}

func tableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var name string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
	return err == nil
}

func columnExists(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info(%s): %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan table info: %v", err)
		}
		if name == column {
			return true
		}
	}
	return false
}
