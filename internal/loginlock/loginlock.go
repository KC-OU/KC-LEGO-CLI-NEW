// Package loginlock persists a failed-login-attempt counter per username, so
// a lockout actually survives a reconnect. A telnet (or web terminal) login
// is a fresh TUI process per connection — an in-memory counter on *App
// resets to zero every time, which in practice means no lockout at all for
// anyone willing to just reconnect. This keeps its own small JSON store, the
// same pattern internal/twofa already uses for 2FA code attempts, rather
// than folding into internal/access's per-(source,username) Policy.Users:
// at the point a password attempt fails, the source (ModernWMS vs Part-DB)
// isn't known yet (auth.AuthenticateUser tries one then the other), so this
// tracks the bare username only — the goal is "stop hammering this account",
// regardless of which backend would have accepted it.
package loginlock

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

const (
	// MaxFailures is how many wrong passwords in a row lock the account.
	MaxFailures = 5
	// LockFor is how long a lockout lasts once triggered.
	LockFor = 15 * time.Minute
)

type entry struct {
	Fails       int       `json:"fails,omitempty"`
	LockedUntil time.Time `json:"locked_until,omitempty"` // zero = not locked
}

// storeMu serializes every read-modify-write across all store instances —
// package-level, not per-instance, since a fresh store is opened on every
// call (see internal/twofa's own identical comment on this).
var storeMu sync.Mutex

func storePath() string { return config.Get(config.LoginLockFile) }

func load(path string) (map[string]entry, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]entry{}, nil
	}
	if err != nil {
		return nil, err
	}
	entries := map[string]entry{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &entries); err != nil {
			return nil, err
		}
	}
	return entries, nil
}

// lock takes the in-process mutex and a flock on a sidecar file, so two
// processes (every telnet/web session is its own) never lose an attempt
// counter to a lost update. Fails closed: nothing is read or changed if the
// lock can't be taken.
func lock(path string) (unlock func(), err error) {
	storeMu.Lock()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		storeMu.Unlock()
		return nil, err
	}
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		storeMu.Unlock()
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		storeMu.Unlock()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
		storeMu.Unlock()
	}, nil
}

// save writes to a temp file and renames it over the store, so a crash or a
// reader in another process never sees a half-written file.
func save(path string, entries map[string]entry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".login-lock-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func key(username string) string { return strings.ToLower(strings.TrimSpace(username)) }

// update is the shared locked read-modify-write every exported function
// here goes through, same shape as internal/access.Update.
func update(username string, change func(e *entry)) (entry, error) {
	path := storePath()
	unlock, err := lock(path)
	if err != nil {
		return entry{}, err
	}
	defer unlock()
	entries, err := load(path)
	if err != nil {
		return entry{}, err
	}
	k := key(username)
	e := entries[k]
	change(&e)
	entries[k] = e
	if err := save(path, entries); err != nil {
		return entry{}, err
	}
	return e, nil
}

// Locked reports whether username is currently locked out, and until when.
// An expired lock is cleared as a side effect (best-effort: even if the
// clear fails to persist, the time comparison on the next read still
// correctly reports "not locked" from the stale data).
func Locked(username string) (until time.Time, locked bool, err error) {
	e, err := update(username, func(e *entry) {
		if !e.LockedUntil.IsZero() && !time.Now().Before(e.LockedUntil) {
			e.LockedUntil, e.Fails = time.Time{}, 0 // the lock has expired
		}
	})
	if err != nil {
		return time.Time{}, false, err
	}
	return e.LockedUntil, !e.LockedUntil.IsZero(), nil
}

// RecordFailure counts one failed login attempt, locking the account for
// LockFor once it reaches MaxFailures in a row — the same shape as the
// existing bot-PIN lockout in internal/access (BotPINFails/BotPINLockUntil).
// Reports whether this failure just triggered a fresh lock.
func RecordFailure(username string) (justLocked bool, until time.Time, err error) {
	e, err := update(username, func(e *entry) {
		e.Fails++
		if e.Fails >= MaxFailures {
			e.LockedUntil = time.Now().Add(LockFor)
			e.Fails = 0
			justLocked = true
		}
	})
	if err != nil {
		return false, time.Time{}, err
	}
	return justLocked, e.LockedUntil, nil
}

// ClearFailures resets the failure count and any lock — called on a
// successful login, and by an admin resetting someone's password (treated
// the same as "let them try again").
func ClearFailures(username string) error {
	_, err := update(username, func(e *entry) { *e = entry{} })
	return err
}

// Rename moves a failed-attempt/lockout entry (if any) from oldUsername to
// newUsername, for an admin renaming a numeric login ID. A no-op if the old
// username has no entry; an error if the new username already has one.
func Rename(oldUsername, newUsername string) error {
	path := storePath()
	unlock, err := lock(path)
	if err != nil {
		return err
	}
	defer unlock()
	entries, err := load(path)
	if err != nil {
		return err
	}
	oldKey, newKey := key(oldUsername), key(newUsername)
	e, ok := entries[oldKey]
	if !ok {
		return nil
	}
	if _, taken := entries[newKey]; taken {
		return errors.New(newUsername + " already has a login-lock entry")
	}
	delete(entries, oldKey)
	entries[newKey] = e
	return save(path, entries)
}
