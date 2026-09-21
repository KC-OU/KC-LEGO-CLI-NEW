// Package twofa adds TOTP-based two-factor authentication to TUI logins.
// It deliberately does not touch ModernWMS's user_security table or
// Part-DB's users table — those are live third-party app schemas, and
// bolting 2FA columns onto them risks a future app upgrade wiping or
// conflicting with the extra columns. Instead this keeps its own small
// JSON store, the same pattern already used for credentials.json and
// link_overrides.json.
package twofa

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

var ErrNotEnrolled = errors.New("2FA is not enrolled for this account")
var ErrAlreadyEnabled = errors.New("2FA is already enabled for this account")
var ErrInvalidCode = errors.New("invalid code")

// ErrCodeReused is a correct code that was already accepted once (RFC 6238 §5.2:
// a code is single-use). It is not a guess, so it does not count toward lockout.
var ErrCodeReused = errors.New("that code was already used — wait for the next one")

// LockedError means too many wrong codes: the account refuses every code,
// right or wrong, until Until (or until an admin runs `wms users 2fa unlock`).
type LockedError struct{ Until time.Time }

func (e *LockedError) Error() string {
	mins := max(int(math.Ceil(time.Until(e.Until).Minutes())), 1)
	return fmt.Sprintf("too many wrong codes — locked for another %d min", mins)
}

const (
	maxFailures = 5               // wrong codes in a row before the account locks
	lockBase    = 5 * time.Minute // first lock; each further lock doubles it
	lockMax     = time.Hour
)

// bcryptCost hashes backup codes; tests lower it, since a whole set is 8 hashes.
var bcryptCost = bcrypt.DefaultCost

// now is the clock; tests move it to get a fresh TOTP step.
var now = time.Now

func lockFor(locks int) time.Duration {
	d := lockBase
	for i := 1; i < locks && d < lockMax; i++ {
		d *= 2
	}
	return min(d, lockMax)
}

type entry struct {
	Secret           string    `json:"secret"`
	Enabled          bool      `json:"enabled"`
	BackupCodeHashes []string  `json:"backup_code_hashes"`
	EnrolledAt       time.Time `json:"enrolled_at"`
	// VerifiedAt records, per origin (see MarkVerified), when a real code last
	// passed — the start of that origin's grace window. Only ever set by a
	// successful Verify; a grace login never refreshes it.
	VerifiedAt map[string]time.Time `json:"verified_at,omitempty"`
	// LastStep is the newest 30 s TOTP step already accepted; a code for that
	// step or an older one is a replay.
	LastStep    int64     `json:"last_step,omitempty"`
	Failures    int       `json:"failures,omitempty"`     // consecutive wrong codes
	Locks       int       `json:"locks,omitempty"`        // consecutive lockouts, for the doubling
	LockedUntil time.Time `json:"locked_until,omitempty"` // zero = not locked
}

// storeMu serializes every read-modify-write across all store instances.
// It must be package-level, not a per-store field: newStore() is called
// fresh on every exported function call, so a mutex embedded in the
// struct would never be shared and would guard against nothing.
var storeMu sync.Mutex

type store struct {
	path string
}

func newStore() *store {
	return &store{path: config.Get(config.TwoFAFile)}
}

func key(username, source string) string {
	return source + ":" + strings.ToLower(username)
}

func (s *store) load() (map[string]entry, error) {
	data, err := os.ReadFile(s.path)
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

// lock takes the in-process mutex and a flock on a sidecar file. Every telnet
// and web session is its own process, so the mutex alone would let two
// sessions read-modify-write the store at once and lose an attempt counter.
// It fails closed: if the lock cannot be taken, nothing is read or changed.
func (s *store) lock() (unlock func(), err error) {
	storeMu.Lock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		storeMu.Unlock()
		return nil, err
	}
	f, err := os.OpenFile(s.path+".lock", os.O_CREATE|os.O_RDWR, 0600)
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
func (s *store) save(entries map[string]entry) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".2fa-*.tmp")
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
	return os.Rename(tmp.Name(), s.path)
}

// matchStep returns the 30 s step a 6-digit code belongs to (this one, the
// previous, or the next, to tolerate clock drift), comparing in constant time.
func matchStep(secret, code string, t time.Time) (int64, bool) {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return 0, false
	}
	base := t.Unix() / 30
	opts := totp.ValidateOpts{Period: 30, Skew: 1, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1}
	found, step := 0, int64(0)
	for _, d := range []int64{-1, 0, 1} {
		want, err := totp.GenerateCodeCustom(secret, time.Unix((base+d)*30, 0), opts)
		if err == nil && subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			found, step = 1, base+d
		}
	}
	return step, found == 1
}

// Enroll generates a new secret and stores it as not-yet-enabled — the
// account only gains a 2FA requirement once Confirm verifies the user
// actually scanned it and can produce a valid code, so a half-finished
// enrollment never locks anyone out.
func Enroll(username, source string) (secret, otpauthURI string, err error) {
	s := newStore()
	unlock, lerr := s.lock()
	if lerr != nil {
		return "", "", lerr
	}
	defer unlock()

	k, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "ModernWMS-PartDB",
		AccountName: username,
	})
	if err != nil {
		return "", "", err
	}

	entries, err := s.load()
	if err != nil {
		return "", "", err
	}
	entries[key(username, source)] = entry{Secret: k.Secret(), Enabled: false, EnrolledAt: time.Now()}
	if err := s.save(entries); err != nil {
		return "", "", err
	}
	return k.Secret(), k.URL(), nil
}

// Confirm verifies the first code from the authenticator app, flips the
// account to enabled, and issues backup codes — plaintext returned exactly
// once, only their bcrypt hashes are ever persisted.
func Confirm(username, source, code string) ([]string, error) {
	s := newStore()
	unlock, lerr := s.lock()
	if lerr != nil {
		return nil, lerr
	}
	defer unlock()

	entries, err := s.load()
	if err != nil {
		return nil, err
	}
	e, ok := entries[key(username, source)]
	if !ok {
		return nil, ErrNotEnrolled
	}
	step, ok := matchStep(e.Secret, code, now())
	if !ok {
		return nil, ErrInvalidCode
	}

	codes, hashes, err := generateBackupCodes()
	if err != nil {
		return nil, err
	}
	e.Enabled = true
	e.BackupCodeHashes = hashes
	e.LastStep = step // the enrolment code must not double as the first login code
	entries[key(username, source)] = e
	if err := s.save(entries); err != nil {
		return nil, err
	}
	return codes, nil
}

// Verify checks a live TOTP code first, falling back to consuming a
// matching single-use backup code. Returns whether a backup code was used
// so the caller can warn the user how many remain.
//
// A code accepted once is refused if presented again (ErrCodeReused). Five
// wrong codes in a row lock the account (*LockedError) for 5 minutes, doubling
// with each further lockout up to an hour; while locked, even a correct code
// is refused, so guessing cannot continue. A success clears the counters.
func Verify(username, source, code string) (usedBackupCode bool, err error) {
	s := newStore()
	unlock, lerr := s.lock()
	if lerr != nil {
		return false, lerr
	}
	defer unlock()

	entries, err := s.load()
	if err != nil {
		return false, err
	}
	k := key(username, source)
	e, ok := entries[k]
	if !ok || !e.Enabled {
		return false, ErrNotEnrolled
	}
	t := now()
	if e.LockedUntil.After(t) {
		return false, &LockedError{Until: e.LockedUntil}
	}
	succeed := func() {
		e.Failures, e.Locks, e.LockedUntil = 0, 0, time.Time{}
	}

	if step, ok := matchStep(e.Secret, code, t); ok {
		if step <= e.LastStep {
			return false, ErrCodeReused
		}
		e.LastStep = step
		succeed()
		entries[k] = e
		return false, s.save(entries)
	}

	normalized := strings.ToUpper(strings.TrimSpace(code))
	for i, h := range e.BackupCodeHashes {
		if bcrypt.CompareHashAndPassword([]byte(h), []byte(normalized)) == nil {
			e.BackupCodeHashes = append(e.BackupCodeHashes[:i], e.BackupCodeHashes[i+1:]...)
			succeed()
			entries[k] = e
			if err := s.save(entries); err != nil {
				return false, err
			}
			return true, nil
		}
	}

	e.Failures++
	var result error = ErrInvalidCode
	if e.Failures >= maxFailures {
		e.Locks++
		e.LockedUntil = t.Add(lockFor(e.Locks))
		e.Failures = 0
		result = &LockedError{Until: e.LockedUntil}
	}
	entries[k] = e
	if err := s.save(entries); err != nil {
		return false, err
	}
	return false, result
}

// Locked reports whether the account is currently locked out, and until when.
func Locked(username, source string) (until time.Time, locked bool) {
	s := newStore()
	unlock, lerr := s.lock()
	if lerr != nil {
		return time.Time{}, false
	}
	defer unlock()
	entries, err := s.load()
	if err != nil {
		return time.Time{}, false
	}
	e, ok := entries[key(username, source)]
	return e.LockedUntil, ok && e.LockedUntil.After(now())
}

// Unlock clears a lockout and the failure counters (an admin's recovery path).
func Unlock(username, source string) error {
	s := newStore()
	unlock, lerr := s.lock()
	if lerr != nil {
		return lerr
	}
	defer unlock()
	entries, err := s.load()
	if err != nil {
		return err
	}
	k := key(username, source)
	e, ok := entries[k]
	if !ok {
		return ErrNotEnrolled
	}
	e.Failures, e.Locks, e.LockedUntil = 0, 0, time.Time{}
	entries[k] = e
	return s.save(entries)
}

// MarkVerified starts (or restarts) the grace window for origin: a caller
// that just saw Verify succeed records where it came from ("local", or
// "telnet:<address>"), so a later sign-in from the same place within window
// can skip the code prompt. An empty origin (the web terminal, whose client
// address the TUI can't see) or a non-positive window records nothing, and
// entries older than window are dropped so the store doesn't grow.
func MarkVerified(username, source, origin string, window time.Duration) error {
	if origin == "" || window <= 0 {
		return nil
	}
	s := newStore()
	unlock, lerr := s.lock()
	if lerr != nil {
		return lerr
	}
	defer unlock()

	entries, err := s.load()
	if err != nil {
		return err
	}
	k := key(username, source)
	e, ok := entries[k]
	if !ok || !e.Enabled {
		return ErrNotEnrolled
	}
	now := time.Now()
	fresh := map[string]time.Time{origin: now}
	for o, t := range e.VerifiedAt {
		if o != origin && now.Sub(t) < window {
			fresh[o] = t
		}
	}
	e.VerifiedAt = fresh
	entries[k] = e
	return s.save(entries)
}

// WithinGrace reports whether origin passed a real 2FA check less than
// window ago, and how long ago that was. It never extends the window.
func WithinGrace(username, source, origin string, window time.Duration) (ago time.Duration, ok bool) {
	if origin == "" || window <= 0 {
		return 0, false
	}
	s := newStore()
	unlock, lerr := s.lock()
	if lerr != nil {
		return 0, false
	}
	defer unlock()
	entries, err := s.load()
	if err != nil {
		return 0, false
	}
	e, found := entries[key(username, source)]
	if !found || !e.Enabled {
		return 0, false
	}
	if e.LockedUntil.After(now()) { // a locked account gets no shortcut
		return 0, false
	}
	t, seen := e.VerifiedAt[origin]
	if !seen {
		return 0, false
	}
	ago = time.Since(t)
	return ago, ago >= 0 && ago < window
}

func Disable(username, source string) error {
	s := newStore()
	unlock, lerr := s.lock()
	if lerr != nil {
		return lerr
	}
	defer unlock()

	entries, err := s.load()
	if err != nil {
		return err
	}
	k := key(username, source)
	if _, ok := entries[k]; !ok {
		return ErrNotEnrolled
	}
	delete(entries, k)
	return s.save(entries)
}

func IsEnabled(username, source string) bool {
	s := newStore()
	unlock, lerr := s.lock()
	if lerr != nil {
		return false
	}
	defer unlock()
	entries, err := s.load()
	if err != nil {
		return false
	}
	e, ok := entries[key(username, source)]
	return ok && e.Enabled
}

// RemainingBackupCodes reports how many unused backup codes an enrolled
// account has left, for the "N remaining" warning after a backup-code login.
func RemainingBackupCodes(username, source string) int {
	s := newStore()
	unlock, lerr := s.lock()
	if lerr != nil {
		return 0
	}
	defer unlock()
	entries, err := s.load()
	if err != nil {
		return 0
	}
	return len(entries[key(username, source)].BackupCodeHashes)
}

const backupCodeCount = 8
const backupCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0/O/1/I ambiguity

func generateBackupCodes() (plain []string, hashes []string, err error) {
	for i := 0; i < backupCodeCount; i++ {
		code, err := randomBackupCode()
		if err != nil {
			return nil, nil, err
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(code), bcryptCost)
		if err != nil {
			return nil, nil, err
		}
		plain = append(plain, code)
		hashes = append(hashes, string(hash))
	}
	return plain, hashes, nil
}

func randomBackupCode() (string, error) {
	var half [2]string
	for h := 0; h < 2; h++ {
		b := make([]byte, 4)
		for i := range b {
			n, err := rand.Int(rand.Reader, big.NewInt(int64(len(backupCodeAlphabet))))
			if err != nil {
				return "", err
			}
			b[i] = backupCodeAlphabet[n.Int64()]
		}
		half[h] = string(b)
	}
	return fmt.Sprintf("%s-%s", half[0], half[1]), nil
}
