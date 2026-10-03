package lego

import (
	"crypto/rand"
	"encoding/base32"
	"errors"
	"strings"
	"time"
)

// ErrNoMobileSession is any reason a token doesn't resolve to a usable
// session (unknown, expired, still waiting on a 2FA step) — collapsed into
// one error the same way exports.Claim and the bot API's PIN check do,
// rather than leaking which part failed.
var ErrNoMobileSession = errors.New("no such mobile session")

// pendingTTL is how long a password-verified-but-not-yet-2FA'd login stays
// valid — short, since the phone's own screen should already have the 2FA
// field up; sessionTTL is a full shift.
const (
	pendingTTL = 5 * time.Minute
	sessionTTL = 14 * time.Hour
)

func newMobileToken() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)), nil
}

// StartMobileLogin records a password-verified login — immediately usable if
// needs2FA is false, otherwise a pending token that ConfirmMobile2FA must
// clear before anything else accepts it.
func (d *DB) StartMobileLogin(username, source, role string, needs2FA bool) (token string, err error) {
	token, err = newMobileToken()
	if err != nil {
		return "", err
	}
	ttl := sessionTTL
	need := 0
	if needs2FA {
		ttl, need = pendingTTL, 1
	}
	now := time.Now()
	_, err = d.Exec(`INSERT INTO mobile_sessions (token, username, source, role, needs_2fa, created_at, expires_at) VALUES (?,?,?,?,?,?,?)`,
		token, username, source, role, need, now.Format(time.RFC3339), now.Add(ttl).Format(time.RFC3339))
	return token, err
}

// ConfirmMobile2FA clears a pending session's needs_2fa flag and extends it
// to a full session, once the caller has separately verified the 2FA code.
func (d *DB) ConfirmMobile2FA(token string) error {
	res, err := d.Exec(`UPDATE mobile_sessions SET needs_2fa = 0, expires_at = ? WHERE token = ? AND needs_2fa = 1 AND expires_at > ?`,
		time.Now().Add(sessionTTL).Format(time.RFC3339), token, time.Now().Format(time.RFC3339))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoMobileSession
	}
	return nil
}

// MobileSession is one signed-in mobile session, resolved from its token.
type MobileSession struct {
	Username, Source, Role string
}

// PendingMobile2FA resolves a still-pending (needs_2fa) token to who it's
// for — the one lookup that's allowed to see a pending row, since the 2FA
// step itself needs the username/source to verify a code against before the
// session becomes otherwise usable (MobileSessionFor refuses it until then).
func (d *DB) PendingMobile2FA(token string) (MobileSession, error) {
	var s MobileSession
	var expiresAt string
	var needs2FA int
	err := d.QueryRow(`SELECT username, source, role, needs_2fa, expires_at FROM mobile_sessions WHERE token = ?`, token).
		Scan(&s.Username, &s.Source, &s.Role, &needs2FA, &expiresAt)
	if err != nil || needs2FA == 0 {
		return MobileSession{}, ErrNoMobileSession
	}
	exp, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil || time.Now().After(exp) {
		return MobileSession{}, ErrNoMobileSession
	}
	return s, nil
}

// MobileSessionFor resolves token to who it belongs to — refuses an unknown,
// expired, or still-pending (needs_2fa) token, the same collapsed-error
// shape as exports.Claim.
func (d *DB) MobileSessionFor(token string) (MobileSession, error) {
	var s MobileSession
	var expiresAt string
	var needs2FA int
	err := d.QueryRow(`SELECT username, source, role, needs_2fa, expires_at FROM mobile_sessions WHERE token = ?`, token).
		Scan(&s.Username, &s.Source, &s.Role, &needs2FA, &expiresAt)
	if err != nil {
		return MobileSession{}, ErrNoMobileSession
	}
	if needs2FA != 0 {
		return MobileSession{}, ErrNoMobileSession
	}
	exp, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil || time.Now().After(exp) {
		return MobileSession{}, ErrNoMobileSession
	}
	return s, nil
}

// EndMobileSession signs a token out immediately (an explicit logout, not
// just letting it expire).
func (d *DB) EndMobileSession(token string) error {
	_, err := d.Exec(`DELETE FROM mobile_sessions WHERE token = ?`, token)
	return err
}

// CleanupExpiredMobileSessions removes anything past its expiry — called
// the same opportunistic way exports.Cleanup is, not on a dedicated timer.
func (d *DB) CleanupExpiredMobileSessions() error {
	_, err := d.Exec(`DELETE FROM mobile_sessions WHERE expires_at <= ?`, time.Now().Format(time.RFC3339))
	return err
}
