package camera

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Camera is a configured stream entry.
type Camera struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Store is a JSON-file-backed list of cameras with concurrent-safe access.
// Reads are served from memory under an RLock; writes mutate the slice under
// a write lock and persist atomically via a temp-file + os.Rename swap.
type Store struct {
	path string
	mu   sync.RWMutex
	cams []Camera
}

var (
	ErrAlreadyExists = errors.New("camera already exists")
	ErrNotFound      = errors.New("camera not found")
	ErrInvalid       = errors.New("camera name and url are required")
)

// OpenStore loads cameras from the given JSON file. A missing file is treated
// as an empty store; the file is created lazily on the first write.
func OpenStore(path string) (*Store, error) {
	s := &Store{path: path}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// Path returns the on-disk file path backing the store.
func (s *Store) Path() string { return s.path }

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.cams = nil
		return nil
	}
	if err != nil {
		return fmt.Errorf("read cameras file: %w", err)
	}
	if len(data) == 0 {
		s.cams = nil
		return nil
	}
	var cams []Camera
	if err := json.Unmarshal(data, &cams); err != nil {
		return fmt.Errorf("parse cameras file %s: %w", s.path, err)
	}
	s.cams = cams
	return nil
}

// save persists the current camera slice atomically.
// Caller must hold the write lock.
func (s *Store) save() error {
	data, err := json.MarshalIndent(s.cams, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create cameras dir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".cameras-*.json.tmp")
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

// List returns a copy of the current camera list.
func (s *Store) List() []Camera {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Camera, len(s.cams))
	copy(out, s.cams)
	return out
}

// Count returns the number of cameras currently stored.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.cams)
}

// Default returns the first camera (used by /mataelang) and whether one exists.
func (s *Store) Default() (Camera, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.cams) == 0 {
		return Camera{}, false
	}
	return s.cams[0], true
}

// Find looks up a camera by case-insensitive name match.
func (s *Store) Find(name string) (Camera, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.cams {
		if strings.EqualFold(c.Name, name) {
			return c, true
		}
	}
	return Camera{}, false
}

// Add appends a new camera and persists. Returns ErrAlreadyExists if the name
// (case-insensitive) is already taken.
func (s *Store) Add(cam Camera) error {
	cam.Name = strings.TrimSpace(cam.Name)
	cam.URL = strings.TrimSpace(cam.URL)
	if cam.Name == "" || cam.URL == "" {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.cams {
		if strings.EqualFold(c.Name, cam.Name) {
			return ErrAlreadyExists
		}
	}
	s.cams = append(s.cams, cam)
	return s.save()
}

// Remove deletes a camera by case-insensitive name. Returns ErrNotFound if absent.
func (s *Store) Remove(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.cams {
		if strings.EqualFold(c.Name, name) {
			s.cams = append(s.cams[:i], s.cams[i+1:]...)
			return s.save()
		}
	}
	return ErrNotFound
}

// Replace overwrites the entire camera list. Used for one-shot migration from
// legacy environment-variable configuration.
func (s *Store) Replace(cams []Camera) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cams = append([]Camera(nil), cams...)
	return s.save()
}
