// Package dashboardlink tracks which Go-side login usernames also have a
// provisioned kc-parts-auth-node (web dashboard) login.
//
// This is deliberately its OWN store, not a field on internal/access.User:
// internal/uiapp's own governed()/legacyCan() treat the mere *existence* of
// an access.Policy entry as the signal that an account is "governed" (only
// what its groups/perms explicitly allow) rather than "ungoverned" (keeps
// its full legacy ModernWMS/Part-DB role access). Storing the dashboard
// flag there would silently flip an otherwise-untouched Picker/Checker
// account from full legacy telnet access to zero permissions the instant
// an admin turned dashboard access on for them -- a real regression, not a
// style choice. Keeping this list separate means linking a dashboard login
// never touches access.Policy at all.
package dashboardlink

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

type Store struct {
	path string
	mu   sync.Mutex
}

func New(path string) *Store {
	if path == "" {
		path = defaultPath()
	}
	return &Store{path: path}
}

func defaultPath() string {
	if p := config.Get(config.DashboardLinksFile); p != "" {
		return p
	}
	return filepath.Join(filepath.Dir(config.Get(config.LegoDBPath)), "dashboard_links.json")
}

func (s *Store) load() (map[string]bool, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out map[string]bool
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) save(m map[string]bool) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0644)
}

// Has reports whether username currently has a linked dashboard login.
func (s *Store) Has(username string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return false, err
	}
	return m[strings.ToLower(username)], nil
}

// Set links or unlinks username. Unlinking never deprovisions the
// BetterAuth side itself -- it only stops the dashboard from treating this
// username as a dashboard-linked identity for permission-mirroring
// purposes (see internal/dashboardapi's /dashboard/me/permissions).
func (s *Store) Set(username string, on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return err
	}
	key := strings.ToLower(username)
	if on {
		m[key] = true
	} else {
		delete(m, key)
	}
	return s.save(m)
}
