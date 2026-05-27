package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
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
	ChatType            string    `json:"chat_type"`
	ChatTitle           string    `json:"chat_title,omitempty"`
	RequestedByID       int64     `json:"requested_by_id"`
	RequestedByUsername string    `json:"requested_by_username,omitempty"`
	Reason              string    `json:"reason,omitempty"`
	RequestedAt         time.Time `json:"requested_at"`
}

type data struct {
	AuthorizedChats []AuthorizedChat `json:"authorized_chats"`
	PendingRequests []Request        `json:"pending_requests"`
}

type Store struct {
	path string
	mu   sync.RWMutex
	data data
}

func OpenStore(path string, bootstrapIDs []int64) (*Store, error) {
	s := &Store{path: path}
	if err := s.load(); err != nil {
		return nil, err
	}
	changed := false
	for _, id := range bootstrapIDs {
		if id == 0 || s.isAuthorizedLocked(id) {
			continue
		}
		s.data.AuthorizedChats = append(s.data.AuthorizedChats, AuthorizedChat{ChatID: id})
		changed = true
	}
	if changed {
		if err := s.save(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) Path() string { return s.path }

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read auth file: %w", err)
	}
	if len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, &s.data); err != nil {
		return fmt.Errorf("parse auth file %s: %w", s.path, err)
	}
	return nil
}

// save persists the current store atomically. Caller must hold the write lock
// unless the store is still private during initialization.
func (s *Store) save() error {
	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create auth dir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".auth-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

func (s *Store) IsAuthorized(chatID int64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isAuthorizedLocked(chatID)
}

func (s *Store) isAuthorizedLocked(chatID int64) bool {
	for _, chat := range s.data.AuthorizedChats {
		if chat.ChatID == chatID {
			return true
		}
	}
	return false
}

func (s *Store) AddAuthorized(chat AuthorizedChat) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.data.AuthorizedChats {
		if existing.ChatID == chat.ChatID {
			s.data.AuthorizedChats[i] = chat
			return s.save()
		}
	}
	s.data.AuthorizedChats = append(s.data.AuthorizedChats, chat)
	s.sortLocked()
	return s.save()
}

func (s *Store) RemoveAuthorized(chatID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, chat := range s.data.AuthorizedChats {
		if chat.ChatID == chatID {
			s.data.AuthorizedChats = append(s.data.AuthorizedChats[:i], s.data.AuthorizedChats[i+1:]...)
			return s.save()
		}
	}
	return nil
}

func (s *Store) UpsertPending(req Request) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.data.PendingRequests {
		if existing.ChatID == req.ChatID {
			s.data.PendingRequests[i] = req
			return s.save()
		}
	}
	s.data.PendingRequests = append(s.data.PendingRequests, req)
	s.sortLocked()
	return s.save()
}

func (s *Store) RemovePending(chatID int64) (Request, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, req := range s.data.PendingRequests {
		if req.ChatID == chatID {
			s.data.PendingRequests = append(s.data.PendingRequests[:i], s.data.PendingRequests[i+1:]...)
			return req, true, s.save()
		}
	}
	return Request{}, false, nil
}

func (s *Store) Pending(chatID int64) (Request, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, req := range s.data.PendingRequests {
		if req.ChatID == chatID {
			return req, true
		}
	}
	return Request{}, false
}

func (s *Store) ListAuthorized() []AuthorizedChat {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]AuthorizedChat, len(s.data.AuthorizedChats))
	copy(out, s.data.AuthorizedChats)
	return out
}

func (s *Store) ListPending() []Request {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Request, len(s.data.PendingRequests))
	copy(out, s.data.PendingRequests)
	return out
}

func (s *Store) sortLocked() {
	sort.Slice(s.data.AuthorizedChats, func(i, j int) bool {
		return s.data.AuthorizedChats[i].ChatID < s.data.AuthorizedChats[j].ChatID
	})
	sort.Slice(s.data.PendingRequests, func(i, j int) bool {
		return s.data.PendingRequests[i].ChatID < s.data.PendingRequests[j].ChatID
	})
}
