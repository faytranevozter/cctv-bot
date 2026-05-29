package auth

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/faytranevozter/cctv-bot/database"
)

func newAuthStore(t *testing.T, bootstrap []int64) (*Store, *sql.DB) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })
	store, err := NewStore(db, bootstrap)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	return store, db
}

func TestNewStoreBootstrapsAuthorizedChats(t *testing.T) {
	store, _ := newAuthStore(t, []int64{0, 20, 10, 20})

	if store.IsAuthorized(0) {
		t.Fatalf("zero chat should not be authorized")
	}
	if !store.IsAuthorized(10) || !store.IsAuthorized(20) {
		t.Fatalf("bootstrap chats were not authorized")
	}
	chats := store.ListAuthorized()
	if len(chats) != 2 || chats[0].ChatID != 10 || chats[1].ChatID != 20 {
		t.Fatalf("ListAuthorized() = %#v, want sorted unique chats 10,20", chats)
	}
}

func TestAuthorizedChatLifecycle(t *testing.T) {
	store, _ := newAuthStore(t, nil)
	approvedAt := time.Date(2026, 5, 29, 10, 0, 0, 0, time.UTC)

	err := store.AddAuthorized(AuthorizedChat{ChatID: 10, ChatType: "group", ChatTitle: "Ops", ApprovedByID: 99, ApprovedByUsername: "admin", ApprovedAt: approvedAt})
	if err != nil {
		t.Fatalf("AddAuthorized() error = %v", err)
	}
	if !store.IsAuthorized(10) {
		t.Fatalf("chat should be authorized")
	}

	err = store.AddAuthorized(AuthorizedChat{ChatID: 10, ChatType: "supergroup", ChatTitle: "Ops 2", ApprovedByID: 100, ApprovedByUsername: "root", ApprovedAt: approvedAt.Add(time.Hour)})
	if err != nil {
		t.Fatalf("AddAuthorized() upsert error = %v", err)
	}
	chats := store.ListAuthorized()
	if len(chats) != 1 || chats[0].ChatTitle != "Ops 2" || chats[0].ApprovedByID != 100 || chats[0].ApprovedByUsername != "root" {
		t.Fatalf("ListAuthorized() after upsert = %#v", chats)
	}
	if !chats[0].ApprovedAt.Equal(approvedAt.Add(time.Hour)) {
		t.Fatalf("ApprovedAt = %v, want %v", chats[0].ApprovedAt, approvedAt.Add(time.Hour))
	}

	if err := store.RemoveAuthorized(10); err != nil {
		t.Fatalf("RemoveAuthorized() error = %v", err)
	}
	if store.IsAuthorized(10) {
		t.Fatalf("chat should not be authorized after removal")
	}
}

func TestPendingRequestLifecycle(t *testing.T) {
	store, _ := newAuthStore(t, nil)
	requestedAt := time.Date(2026, 5, 29, 11, 0, 0, 0, time.UTC)
	req := Request{ChatID: -100, MessageThreadID: 42, ChatType: "supergroup", ChatTitle: "NOC", RequestedByID: 7, RequestedByUsername: "operator", Reason: "Need access", RequestedAt: requestedAt}

	if err := store.UpsertPending(req); err != nil {
		t.Fatalf("UpsertPending() error = %v", err)
	}
	got, ok := store.Pending(-100)
	if !ok {
		t.Fatalf("Pending() ok = false, want true")
	}
	if got.ChatID != req.ChatID || got.MessageThreadID != req.MessageThreadID || got.ChatType != req.ChatType || got.ChatTitle != req.ChatTitle || got.RequestedByID != req.RequestedByID || got.RequestedByUsername != req.RequestedByUsername || got.Reason != req.Reason || !got.RequestedAt.Equal(req.RequestedAt) {
		t.Fatalf("Pending() = %#v, want %#v", got, req)
	}

	req.Reason = "Updated reason"
	req.MessageThreadID = 77
	if err := store.UpsertPending(req); err != nil {
		t.Fatalf("UpsertPending() update error = %v", err)
	}
	pending := store.ListPending()
	if len(pending) != 1 || pending[0].Reason != "Updated reason" || pending[0].MessageThreadID != 77 {
		t.Fatalf("ListPending() = %#v, want updated request", pending)
	}

	removed, ok, err := store.RemovePending(-100)
	if err != nil || !ok {
		t.Fatalf("RemovePending() = %#v, %v, %v; want request, true, nil", removed, ok, err)
	}
	if removed.Reason != "Updated reason" {
		t.Fatalf("removed request = %#v, want updated request", removed)
	}
	if _, ok := store.Pending(-100); ok {
		t.Fatalf("Pending() ok = true after removal")
	}
	_, ok, err = store.RemovePending(-100)
	if err != nil || ok {
		t.Fatalf("RemovePending() missing = ok %v err %v, want false nil", ok, err)
	}
}
