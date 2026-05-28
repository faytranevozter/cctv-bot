package auth

import (
	"database/sql"
	"fmt"
	"time"
)

type AuthorizedChat struct {
	ChatID             int64     `json:"chat_id"`
	ChatType           string    `json:"chat_type,omitempty"`
	ChatTitle          string    `json:"chat_title,omitempty"`
	ApprovedByID       int64     `json:"approved_by_id,omitempty"`
	ApprovedByUsername string    `json:"approved_by_username,omitempty"`
	ApprovedAt         time.Time `json:"approved_at,omitempty"`
}

type Request struct {
	ChatID              int64     `json:"chat_id"`
	MessageThreadID     int       `json:"message_thread_id,omitempty"`
	ChatType            string    `json:"chat_type"`
	ChatTitle           string    `json:"chat_title,omitempty"`
	RequestedByID       int64     `json:"requested_by_id"`
	RequestedByUsername string    `json:"requested_by_username,omitempty"`
	Reason              string    `json:"reason,omitempty"`
	RequestedAt         time.Time `json:"requested_at"`
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB, bootstrapIDs []int64) (*Store, error) {
	s := &Store{db: db}
	for _, id := range bootstrapIDs {
		if id == 0 || s.IsAuthorized(id) {
			continue
		}
		if err := s.AddAuthorized(AuthorizedChat{ChatID: id}); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) IsAuthorized(chatID int64) bool {
	var exists int
	err := s.db.QueryRow(`SELECT 1 FROM authorized_chats WHERE chat_id = ?`, chatID).Scan(&exists)
	return err == nil
}

func (s *Store) AddAuthorized(chat AuthorizedChat) error {
	approvedAt := timeString(chat.ApprovedAt)
	_, err := s.db.Exec(`INSERT INTO authorized_chats (chat_id, chat_type, chat_title, approved_by_id, approved_by_username, approved_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(chat_id) DO UPDATE SET
			chat_type = excluded.chat_type,
			chat_title = excluded.chat_title,
			approved_by_id = excluded.approved_by_id,
			approved_by_username = excluded.approved_by_username,
			approved_at = excluded.approved_at`,
		chat.ChatID, chat.ChatType, chat.ChatTitle, nullInt(chat.ApprovedByID), chat.ApprovedByUsername, approvedAt)
	if err != nil {
		return fmt.Errorf("add authorized chat: %w", err)
	}
	return nil
}

func (s *Store) RemoveAuthorized(chatID int64) error {
	_, err := s.db.Exec(`DELETE FROM authorized_chats WHERE chat_id = ?`, chatID)
	if err != nil {
		return fmt.Errorf("remove authorized chat: %w", err)
	}
	return nil
}

func (s *Store) UpsertPending(req Request) error {
	_, err := s.db.Exec(`INSERT INTO pending_access_requests (chat_id, message_thread_id, chat_type, chat_title, requested_by_id, requested_by_username, reason, requested_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(chat_id) DO UPDATE SET
			message_thread_id = excluded.message_thread_id,
			chat_type = excluded.chat_type,
			chat_title = excluded.chat_title,
			requested_by_id = excluded.requested_by_id,
			requested_by_username = excluded.requested_by_username,
			reason = excluded.reason,
			requested_at = excluded.requested_at`,
		req.ChatID, req.MessageThreadID, req.ChatType, req.ChatTitle, req.RequestedByID, req.RequestedByUsername, req.Reason, req.RequestedAt.Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("upsert pending request: %w", err)
	}
	return nil
}

func (s *Store) RemovePending(chatID int64) (Request, bool, error) {
	req, ok := s.Pending(chatID)
	if !ok {
		return Request{}, false, nil
	}
	if _, err := s.db.Exec(`DELETE FROM pending_access_requests WHERE chat_id = ?`, chatID); err != nil {
		return Request{}, false, fmt.Errorf("remove pending request: %w", err)
	}
	return req, true, nil
}

func (s *Store) Pending(chatID int64) (Request, bool) {
	rows, err := s.db.Query(`SELECT chat_id, COALESCE(message_thread_id, 0), chat_type, COALESCE(chat_title, ''), requested_by_id, COALESCE(requested_by_username, ''), COALESCE(reason, ''), requested_at FROM pending_access_requests WHERE chat_id = ?`, chatID)
	if err != nil {
		return Request{}, false
	}
	defer rows.Close()
	reqs := scanRequests(rows)
	if len(reqs) == 0 {
		return Request{}, false
	}
	return reqs[0], true
}

func (s *Store) ListAuthorized() []AuthorizedChat {
	rows, err := s.db.Query(`SELECT chat_id, COALESCE(chat_type, ''), COALESCE(chat_title, ''), COALESCE(approved_by_id, 0), COALESCE(approved_by_username, ''), COALESCE(approved_at, '') FROM authorized_chats ORDER BY chat_id`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []AuthorizedChat
	for rows.Next() {
		var chat AuthorizedChat
		var approvedAt string
		if err := rows.Scan(&chat.ChatID, &chat.ChatType, &chat.ChatTitle, &chat.ApprovedByID, &chat.ApprovedByUsername, &approvedAt); err != nil {
			continue
		}
		chat.ApprovedAt = parseTime(approvedAt)
		out = append(out, chat)
	}
	return out
}

func (s *Store) ListPending() []Request {
	rows, err := s.db.Query(`SELECT chat_id, COALESCE(message_thread_id, 0), chat_type, COALESCE(chat_title, ''), requested_by_id, COALESCE(requested_by_username, ''), COALESCE(reason, ''), requested_at FROM pending_access_requests ORDER BY chat_id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	return scanRequests(rows)
}

func scanRequests(rows *sql.Rows) []Request {
	var out []Request
	for rows.Next() {
		var req Request
		var requestedAt string
		if err := rows.Scan(&req.ChatID, &req.MessageThreadID, &req.ChatType, &req.ChatTitle, &req.RequestedByID, &req.RequestedByUsername, &req.Reason, &requestedAt); err != nil {
			continue
		}
		req.RequestedAt = parseTime(requestedAt)
		out = append(out, req)
	}
	return out
}

func nullInt(v int64) sql.NullInt64 {
	return sql.NullInt64{Int64: v, Valid: v != 0}
}

func timeString(t time.Time) sql.NullString {
	if t.IsZero() {
		return sql.NullString{}
	}
	return sql.NullString{String: t.Format(time.RFC3339), Valid: true}
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
