package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

type linkOverride struct {
	PartDBLink    string `json:"partdb_link"`
	ModernWMSLink string `json:"modernwms_link"`
}

// linkStore persists manual per-part link overrides to one JSON file — a
// handful of operator-entered URLs doesn't warrant a database.
type linkStore struct {
	path string
	mu   sync.Mutex
}

func newLinkStore(path string) *linkStore {
	if path == "" {
		path = config.Get(config.LinkOverridesFile)
	}
	return &linkStore{path: path}
}

func (s *linkStore) all() (map[int]linkOverride, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readLocked()
}

func (s *linkStore) readLocked() (map[int]linkOverride, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return map[int]linkOverride{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out map[int]linkOverride
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *linkStore) set(partID int, o linkOverride) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	all, err := s.readLocked()
	if err != nil {
		return err
	}
	all[partID] = o

	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0644)
}
