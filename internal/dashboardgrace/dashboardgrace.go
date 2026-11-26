// Package dashboardgrace backs the web dashboard's own 2FA grace window:
// after a genuine full Clerk login (password + a real second factor, or
// OAuth), the dashboard re-checks the password on every sign-in for the next
// few minutes but skips asking for a 2FA code again. This never touches
// Clerk's own session store or secret key — it is a wholly separate,
// short-lived credential this service mints and checks itself, the same
// "never extends the window" grace semantics internal/twofa already applies
// to the CLI/TUI (see that package's WithinGrace/MarkVerified).
//
// A record here never holds a password or anything Clerk-issued — only a
// SHA-256 hash of a random lookup token, the Clerk user id it was minted
// for, and when it expires. The raw token itself is handed to the caller
// exactly once, at Mint, and never written to disk; only its hash is
// (same reason a password is hashed, not stored plain — a leaked store file
// should not itself be a usable credential).
package dashboardgrace

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

type record struct {
	ClerkUserID string    `json:"clerk_user_id"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// storeMu serializes every read-modify-write across all store instances --
// package-level, not per-instance, since a fresh store is opened on every
// call (see internal/twofa's own identical comment on this).
var storeMu sync.Mutex

func storePath() string { return config.Get(config.DashboardGraceFile) }

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func load(path string) (map[string]record, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]record{}, nil
	}
	if err != nil {
		return nil, err
	}
	records := map[string]record{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &records); err != nil {
			return nil, err
		}
	}
	return records, nil
}

// lock takes the in-process mutex and a flock on a sidecar file -- every
// telnet/web/gateway process is separate, so the mutex alone would let two
// of them read-modify-write the store at once. Fails closed: if the lock
// can't be taken, nothing is read or changed. Mirrors internal/twofa's own
// lock() exactly.
func lock(path string) (unlock func(), err error) {
	storeMu.Lock()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil { // same 0700 as 2fa.json -- no listing by other local accounts
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
// reader in another process never sees a half-written file. Also drops
// anything already expired -- the only cleanup this store needs, since every
// write already has the lock and the whole file in hand.
func save(path string, records map[string]record) error {
	now := time.Now()
	pruned := make(map[string]record, len(records))
	for hash, r := range records {
		if r.ExpiresAt.After(now) {
			pruned[hash] = r
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(pruned, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".dashboard-grace-*.tmp")
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

// Mint records a fresh grace window for clerkUserID and returns the raw
// lookup token (never stored -- only its hash is) and when it expires. Only
// ever call this right after a genuine full Clerk login completes (password
// + a real second factor, or OAuth) -- see the caller's own doc comment for
// how that's enforced (a freshly, independently verified Clerk session JWT
// is required to reach the endpoint backing this).
func Mint(clerkUserID string, window time.Duration) (token string, expiresAt time.Time, err error) {
	raw := make([]byte, 32) // 256 bits -- well beyond brute-force range for a token this short-lived
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, fmt.Errorf("generating a grace token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	expiresAt = time.Now().Add(window)

	path := storePath()
	unlock, err := lock(path)
	if err != nil {
		return "", time.Time{}, err
	}
	defer unlock()
	records, err := load(path)
	if err != nil {
		return "", time.Time{}, err
	}
	records[hashToken(token)] = record{ClerkUserID: clerkUserID, ExpiresAt: expiresAt}
	if err := save(path, records); err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

// Check reports whether token is a currently-valid grace token minted for
// exactly clerkUserID, and if so, exactly when it expires -- the caller
// (the dashboard's grace-login handler) needs that to mint a follow-up
// session that expires at the SAME moment, never later: a grace-login must
// never extend the window a real login originally opened, only ride it out.
// expiresAt is the zero time when ok is false (indistinguishable on purpose
// for an unknown, expired, or mismatched-user token -- a grace-login
// failure is reported identically regardless of which of those it was,
// same as a plain wrong password). Does not consume the token -- it stays
// valid for repeated sign-in/sign-out within the window, matching the
// CLI's own grace semantics ("logs back in within that timeframe").
func Check(token, clerkUserID string) (ok bool, expiresAt time.Time) {
	if token == "" || clerkUserID == "" {
		return false, time.Time{}
	}
	path := storePath()
	unlock, err := lock(path)
	if err != nil {
		return false, time.Time{}
	}
	defer unlock()
	records, err := load(path)
	if err != nil {
		return false, time.Time{}
	}
	r, found := records[hashToken(token)]
	if !found {
		return false, time.Time{}
	}
	if time.Now().After(r.ExpiresAt) {
		return false, time.Time{}
	}
	// Constant-time: this is the one place a mismatched clerk_user_id would
	// otherwise be distinguishable from "token not found" by timing alone.
	if subtle.ConstantTimeCompare([]byte(r.ClerkUserID), []byte(clerkUserID)) != 1 {
		return false, time.Time{}
	}
	return true, r.ExpiresAt
}

// WindowMinutes is the configured grace window length (DASHBOARD_GRACE_MINUTES,
// default 10) -- a plain helper so callers don't each re-parse the config
// string themselves.
func WindowMinutes() int {
	raw := config.Get(config.DashboardGraceMinutes)
	n := 0
	if _, err := fmt.Sscanf(raw, "%d", &n); err != nil || n <= 0 {
		return 10
	}
	return n
}
