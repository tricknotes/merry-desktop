// Package settings stores the app's user settings as JSON. Nothing secret goes
// here: registry passwords live in the OS credential store (F-12).
package settings

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// Settings is what the user configured.
type Settings struct {
	// Registries are the registries offered for login (F-12), as host[:port].
	Registries []string `json:"registries"`
	// Integrations are the environments (WSL distros) whose docker CLI may use
	// the engine. nil means the backend's default; an empty list means none.
	Integrations *[]string `json:"integrations,omitempty"`
}

// Store reads and writes Settings at Path.
type Store struct {
	Path string
	mu   sync.Mutex
}

// Load returns the settings; a missing file gives the zero Settings.
func (s *Store) Load() (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out Settings
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	return out, json.Unmarshal(data, &out)
}

// Update loads the settings, applies change and writes them back atomically.
func (s *Store) Update(change func(*Settings)) (Settings, error) {
	cur, err := s.Load()
	if err != nil {
		return cur, err
	}
	change(&cur)
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.MarshalIndent(cur, "", "  ")
	if err != nil {
		return cur, err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return cur, err
	}
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return cur, err
	}
	return cur, os.Rename(tmp, s.Path)
}
