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

	if store.IsAuthorized(0, 0) {
		t.Fatalf("zero chat should not be authorized")
	}
	if !store.IsAuthorized(10, 0) || !store.IsAuthorized(20, 0) {
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
	if !store.IsAuthorized(10, 0) {
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

	if err := store.RemoveAuthorized(10, 0); err != nil {
		t.Fatalf("RemoveAuthorized() error = %v", err)
	}
	if store.IsAuthorized(10, 0) {
		t.Fatalf("chat should not be authorized after removal")
	}
}

func TestAuthorizedChatsAreScopedByTopic(t *testing.T) {
	store, _ := newAuthStore(t, nil)

	if err := store.AddAuthorized(AuthorizedChat{ChatID: -100, MessageThreadID: 1, ChatTitle: "Ops"}); err != nil {
		t.Fatalf("AddAuthorized() topic 1 error = %v", err)
	}
	if err := store.AddAuthorized(AuthorizedChat{ChatID: -100, MessageThreadID: 2, ChatTitle: "Ops"}); err != nil {
		t.Fatalf("AddAuthorized() topic 2 error = %v", err)
	}
	if !store.IsAuthorized(-100, 1) || !store.IsAuthorized(-100, 2) {
		t.Fatalf("topic authorizations were not stored")
	}
	if store.IsAuthorized(-100, 0) {
		t.Fatalf("general chat should not be authorized when only topics are authorized")
	}
	if err := store.RemoveAuthorized(-100, 1); err != nil {
		t.Fatalf("RemoveAuthorized() topic 1 error = %v", err)
	}
	if store.IsAuthorized(-100, 1) {
		t.Fatalf("topic 1 should not be authorized after removal")
	}
	if !store.IsAuthorized(-100, 2) {
		t.Fatalf("topic 2 should remain authorized after removing topic 1")
	}
}

func TestPendingRequestLifecycle(t *testing.T) {
	store, _ := newAuthStore(t, nil)
	requestedAt := time.Date(2026, 5, 29, 11, 0, 0, 0, time.UTC)
	req := Request{ChatID: -100, MessageThreadID: 42, ChatType: "supergroup", ChatTitle: "NOC", RequestedByID: 7, RequestedByUsername: "operator", Reason: "Need access", RequestedAt: requestedAt}

	if err := store.UpsertPending(req); err != nil {
		t.Fatalf("UpsertPending() error = %v", err)
	}
	got, ok := store.Pending(-100, 42)
	if !ok {
		t.Fatalf("Pending() ok = false, want true")
	}
	if got.ChatID != req.ChatID || got.MessageThreadID != req.MessageThreadID || got.ChatType != req.ChatType || got.ChatTitle != req.ChatTitle || got.RequestedByID != req.RequestedByID || got.RequestedByUsername != req.RequestedByUsername || got.Reason != req.Reason || !got.RequestedAt.Equal(req.RequestedAt) {
		t.Fatalf("Pending() = %#v, want %#v", got, req)
	}

	req.Reason = "Updated reason"
	if err := store.UpsertPending(req); err != nil {
		t.Fatalf("UpsertPending() update error = %v", err)
	}
	pending := store.ListPending()
	if len(pending) != 1 || pending[0].Reason != "Updated reason" || pending[0].MessageThreadID != 42 {
		t.Fatalf("ListPending() = %#v, want updated request", pending)
	}

	removed, ok, err := store.RemovePending(-100, 42)
	if err != nil || !ok {
		t.Fatalf("RemovePending() = %#v, %v, %v; want request, true, nil", removed, ok, err)
	}
	if removed.Reason != "Updated reason" {
		t.Fatalf("removed request = %#v, want updated request", removed)
	}
	if _, ok := store.Pending(-100, 42); ok {
		t.Fatalf("Pending() ok = true after removal")
	}
	_, ok, err = store.RemovePending(-100, 42)
	if err != nil || ok {
		t.Fatalf("RemovePending() missing = ok %v err %v, want false nil", ok, err)
	}
}

func TestPendingRequestsAreScopedByTopic(t *testing.T) {
	store, _ := newAuthStore(t, nil)
	requestedAt := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	for _, threadID := range []int{1, 2} {
		req := Request{ChatID: -100, MessageThreadID: threadID, ChatType: "supergroup", ChatTitle: "NOC", RequestedByID: int64(threadID), RequestedAt: requestedAt}
		if err := store.UpsertPending(req); err != nil {
			t.Fatalf("UpsertPending(%d) error = %v", threadID, err)
		}
	}

	if _, ok := store.Pending(-100, 0); ok {
		t.Fatalf("general chat should not have pending request when only topics do")
	}
	if _, ok := store.Pending(-100, 1); !ok {
		t.Fatalf("topic 1 pending request not found")
	}
	if _, ok := store.Pending(-100, 2); !ok {
		t.Fatalf("topic 2 pending request not found")
	}
	if _, ok, err := store.RemovePending(-100, 1); err != nil || !ok {
		t.Fatalf("RemovePending(topic 1) ok=%v err=%v, want true nil", ok, err)
	}
	if _, ok := store.Pending(-100, 1); ok {
		t.Fatalf("topic 1 should be removed")
	}
	if _, ok := store.Pending(-100, 2); !ok {
		t.Fatalf("topic 2 should remain pending")
	}
}
