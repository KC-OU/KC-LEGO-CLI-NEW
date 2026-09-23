// Package api implements the sync dashboard's REST endpoints and Prometheus
// metrics exporter over net/http — no framework, the route set is small and
// flat. Unlike the original Python service, every POST (mutating) endpoint
// requires HTTP Basic Auth; the original left POST routes unchecked, which
// was a real gap, not a feature to preserve since this is new code.
package api

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

type credentials struct {
	Username     string `json:"username"`
	PasswordHash string `json:"password_hash"`
	Salt         string `json:"salt"`
	UpdatedAt    string `json:"updated_at"`
}

type credentialStore struct {
	path string
	mu   sync.Mutex
}

func newCredentialStore(path string) *credentialStore {
	if path == "" {
		path = config.Get(config.CredentialsFile)
	}
	return &credentialStore{path: path}
}

// get bootstraps the credentials file from SYNC_ADMIN_USER/SYNC_ADMIN_PASS
// on first read, matching the original's get_admin_credentials.
func (s *credentialStore) get() (credentials, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		pass := config.Get(config.SyncAdminPass)
		if pass == "" {
			return credentials{}, fmt.Errorf("no credentials file at %s and SYNC_ADMIN_PASS is not set — refusing to bootstrap a default admin account; set SYNC_ADMIN_PASS and restart", s.path)
		}
		creds := credentials{Username: config.Get(config.SyncAdminUser)}
		keyHex, saltHex := auth.HashPBKDF2(pass, nil)
		creds.PasswordHash, creds.Salt = keyHex, saltHex
		creds.UpdatedAt = time.Now().Format(time.RFC3339)
		if writeErr := s.writeLocked(creds); writeErr != nil {
			return credentials{}, writeErr
		}
		return creds, nil
	}
	if err != nil {
		return credentials{}, err
	}
	var creds credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return credentials{}, err
	}
	return creds, nil
}

func (s *credentialStore) set(username, password string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	keyHex, saltHex := auth.HashPBKDF2(password, nil)
	return s.writeLocked(credentials{
		Username: username, PasswordHash: keyHex, Salt: saltHex,
		UpdatedAt: time.Now().Format(time.RFC3339),
	})
}

func (s *credentialStore) writeLocked(c credentials) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil { // holds the sync dashboard's password hash
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0600)
}

func (s *credentialStore) verify(username, password string) bool {
	creds, err := s.get()
	if err != nil {
		return false
	}
	return username == creds.Username && auth.VerifyPBKDF2(password, creds.PasswordHash, creds.Salt)
}

// SetAdminCredentials updates the sync dashboard's HTTP Basic Auth admin
// account. get() re-reads this file from disk on every auth check rather
// than caching it in memory, so a change made here (e.g. from the TUI's
// Settings screen) takes effect for an already-running sync server without
// a restart.
func SetAdminCredentials(username, password string) error {
	return newCredentialStore("").set(username, password)
}

// AdminUsername returns the currently configured sync dashboard admin
// username, for display on an admin settings screen — never the password
// or its hash.
func AdminUsername() (string, error) {
	c, err := newCredentialStore("").get()
	if err != nil {
		return "", err
	}
	return c.Username, nil
}
