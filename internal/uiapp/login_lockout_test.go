package uiapp

import (
	"strings"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/loginlock"
)

// TestLoginLocksOutAfterRepeatedFailuresAndPersistsAcrossAFreshProcess is the
// direct test for the real, persisted lockout: today's app.loginAttempts
// alone resets to zero on every reconnect (a fresh TUI process per telnet
// connection), so it never actually stops repeated guessing. This confirms
// the new loginlock-backed check both locks after MaxFailures AND survives
// a brand-new App pointed at the same lock file — standing in for the real
// "reconnect and try again" scenario, which a single in-memory counter on
// one *App could never demonstrate.
func TestLoginLocksOutAfterRepeatedFailuresAndPersistsAcrossAFreshProcess(t *testing.T) {
	app := newTestApp(t)
	app.authed, app.session, app.cur = false, nil, scrLogin

	for i := 0; i < loginlock.MaxFailures-1; i++ {
		doLoginSubmit(app, []string{"77001", "wrong"})
		if strings.Contains(app.message, "locked") {
			t.Fatalf("locked out too early, on attempt %d: %q", i+1, app.message)
		}
	}
	doLoginSubmit(app, []string{"77001", "wrong"})
	if !strings.Contains(app.message, "locked") {
		t.Fatalf("expected a lockout message after %d failures, got %q", loginlock.MaxFailures, app.message)
	}

	// A fresh process (a new App), but pointed at the SAME persisted lock
	// file — the real shared state a reconnecting telnet session would see.
	lockFile := config.Get(config.LoginLockFile)
	app2 := newTestApp(t)
	t.Setenv(config.LoginLockFile, lockFile)

	app2.authed, app2.session, app2.cur = false, nil, scrLogin
	doLoginSubmit(app2, []string{"77001", "doesn't matter, still locked"})
	if !strings.Contains(app2.message, "locked") {
		t.Fatalf("a fresh process should still see the persisted lockout, got %q", app2.message)
	}
}

// TestLoginLockoutIsPerUsername confirms one account's lockout doesn't
// block anyone else from even trying.
func TestLoginLockoutIsPerUsername(t *testing.T) {
	app := newTestApp(t)
	app.authed, app.session, app.cur = false, nil, scrLogin
	for i := 0; i < loginlock.MaxFailures; i++ {
		doLoginSubmit(app, []string{"77002", "wrong"})
	}
	if !strings.Contains(app.message, "locked") {
		t.Fatalf("expected 77002 to be locked, got %q", app.message)
	}

	doLoginSubmit(app, []string{"77003", "wrong"})
	if strings.Contains(app.message, "locked") {
		t.Errorf("a different username should not be affected, got %q", app.message)
	}
}
