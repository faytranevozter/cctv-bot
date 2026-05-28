package camera

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Camera is a configured stream entry.
type Camera struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Shortcut string `json:"shortcut,omitempty"`
	URL      string `json:"url"`
}

// Store is a SQLite-backed camera store.
type Store struct {
	db *sql.DB
}

var (
	ErrAlreadyExists   = errors.New("camera already exists")
	ErrShortcutTaken   = errors.New("camera shortcut already exists")
	ErrNotFound        = errors.New("camera not found")
	ErrInvalid         = errors.New("camera name and url are required")
	ErrInvalidShortcut = errors.New("camera shortcut is invalid")
)

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// List returns all cameras ordered by insertion order.
func (s *Store) List() []Camera {
	rows, err := s.db.Query(`SELECT id, name, COALESCE(shortcut, ''), url FROM cameras ORDER BY id`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var cams []Camera
	for rows.Next() {
		var cam Camera
		if err := rows.Scan(&cam.ID, &cam.Name, &cam.Shortcut, &cam.URL); err == nil {
			cams = append(cams, cam)
		}
	}
	return cams
}

// Count returns the number of cameras currently stored.
func (s *Store) Count() int {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM cameras`).Scan(&count); err != nil {
		return 0
	}
	return count
}

// Find looks up a camera by case-insensitive name match.
func (s *Store) Find(name string) (Camera, bool) {
	return s.find(`SELECT id, name, COALESCE(shortcut, ''), url FROM cameras WHERE name = ? COLLATE NOCASE`, strings.TrimSpace(name))
}

// FindByShortcut looks up a camera by case-insensitive shortcut match.
func (s *Store) FindByShortcut(shortcut string) (Camera, bool) {
	shortcut = strings.TrimPrefix(strings.TrimSpace(shortcut), "/")
	if shortcut == "" {
		return Camera{}, false
	}
	return s.find(`SELECT id, name, COALESCE(shortcut, ''), url FROM cameras WHERE shortcut = ? COLLATE NOCASE`, shortcut)
}

// FindByID looks up a camera by database ID.
func (s *Store) FindByID(id int64) (Camera, bool) {
	return s.find(`SELECT id, name, COALESCE(shortcut, ''), url FROM cameras WHERE id = ?`, id)
}

func (s *Store) find(query string, arg any) (Camera, bool) {
	var cam Camera
	err := s.db.QueryRow(query, arg).Scan(&cam.ID, &cam.Name, &cam.Shortcut, &cam.URL)
	if errors.Is(err, sql.ErrNoRows) {
		return Camera{}, false
	}
	return cam, err == nil
}

// Add appends a new camera and persists. Returns ErrAlreadyExists if the name
// (case-insensitive) is already taken.
func (s *Store) Add(cam Camera) error {
	cam.Name = strings.TrimSpace(cam.Name)
	cam.URL = strings.TrimSpace(cam.URL)
	cam.Shortcut = strings.TrimPrefix(strings.TrimSpace(cam.Shortcut), "/")
	if cam.Name == "" || cam.URL == "" {
		return ErrInvalid
	}
	if _, ok := s.Find(cam.Name); ok {
		return ErrAlreadyExists
	}
	if cam.Shortcut != "" {
		if _, ok := s.FindByShortcut(cam.Shortcut); ok {
			return ErrShortcutTaken
		}
	}

	shortcut := sql.NullString{String: cam.Shortcut, Valid: cam.Shortcut != ""}
	_, err := s.db.Exec(`INSERT INTO cameras (name, shortcut, url) VALUES (?, ?, ?)`, cam.Name, shortcut, cam.URL)
	if err != nil {
		return fmt.Errorf("insert camera: %w", err)
	}
	return nil
}

// RenameByID renames a camera by database ID.
func (s *Store) RenameByID(id int64, name string) error {
	name = strings.TrimSpace(name)
	if id == 0 || name == "" {
		return ErrInvalid
	}

	cam, ok := s.FindByID(id)
	if !ok {
		return ErrNotFound
	}
	if existing, ok := s.Find(name); ok && existing.ID != cam.ID {
		return ErrAlreadyExists
	}

	res, err := s.db.Exec(`UPDATE cameras SET name = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, name, id)
	if err != nil {
		return fmt.Errorf("rename camera: %w", err)
	}
	if changed, _ := res.RowsAffected(); changed == 0 {
		return ErrNotFound
	}
	return nil
}

// SetShortcut assigns a shortcut to a camera by case-insensitive camera name.
func (s *Store) SetShortcut(name, shortcut string) error {
	name = strings.TrimSpace(name)
	shortcut = strings.TrimPrefix(strings.TrimSpace(shortcut), "/")
	if name == "" || shortcut == "" {
		return ErrInvalidShortcut
	}

	cam, ok := s.Find(name)
	if !ok {
		return ErrNotFound
	}
	if existing, ok := s.FindByShortcut(shortcut); ok && !strings.EqualFold(existing.Name, cam.Name) {
		return ErrShortcutTaken
	}

	res, err := s.db.Exec(`UPDATE cameras SET shortcut = ?, updated_at = CURRENT_TIMESTAMP WHERE name = ? COLLATE NOCASE`, shortcut, name)
	if err != nil {
		return fmt.Errorf("set shortcut: %w", err)
	}
	if changed, _ := res.RowsAffected(); changed == 0 {
		return ErrNotFound
	}
	return nil
}

// SetShortcutByID assigns a shortcut to a camera by database ID.
func (s *Store) SetShortcutByID(id int64, shortcut string) error {
	shortcut = strings.TrimPrefix(strings.TrimSpace(shortcut), "/")
	if id == 0 || shortcut == "" {
		return ErrInvalidShortcut
	}

	cam, ok := s.FindByID(id)
	if !ok {
		return ErrNotFound
	}
	if existing, ok := s.FindByShortcut(shortcut); ok && existing.ID != cam.ID {
		return ErrShortcutTaken
	}

	res, err := s.db.Exec(`UPDATE cameras SET shortcut = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, shortcut, id)
	if err != nil {
		return fmt.Errorf("set shortcut: %w", err)
	}
	if changed, _ := res.RowsAffected(); changed == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteShortcut removes a shortcut from a camera by case-insensitive camera name.
func (s *Store) DeleteShortcut(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrNotFound
	}
	res, err := s.db.Exec(`UPDATE cameras SET shortcut = NULL, updated_at = CURRENT_TIMESTAMP WHERE name = ? COLLATE NOCASE`, name)
	if err != nil {
		return fmt.Errorf("delete shortcut: %w", err)
	}
	if changed, _ := res.RowsAffected(); changed == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteShortcutByID removes a shortcut from a camera by database ID.
func (s *Store) DeleteShortcutByID(id int64) error {
	if id == 0 {
		return ErrNotFound
	}
	res, err := s.db.Exec(`UPDATE cameras SET shortcut = NULL, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete shortcut: %w", err)
	}
	if changed, _ := res.RowsAffected(); changed == 0 {
		return ErrNotFound
	}
	return nil
}

// Remove deletes a camera by case-insensitive name. Returns ErrNotFound if absent.
func (s *Store) Remove(name string) error {
	res, err := s.db.Exec(`DELETE FROM cameras WHERE name = ? COLLATE NOCASE`, strings.TrimSpace(name))
	if err != nil {
		return fmt.Errorf("delete camera: %w", err)
	}
	if changed, _ := res.RowsAffected(); changed == 0 {
		return ErrNotFound
	}
	return nil
}

// RemoveByID deletes a camera by database ID.
func (s *Store) RemoveByID(id int64) error {
	res, err := s.db.Exec(`DELETE FROM cameras WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete camera: %w", err)
	}
	if changed, _ := res.RowsAffected(); changed == 0 {
		return ErrNotFound
	}
	return nil
}
