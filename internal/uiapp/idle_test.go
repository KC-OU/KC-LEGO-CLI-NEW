package uiapp

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

func clocked(t *testing.T, app *App) *time.Time {
	t.Helper()
	clock := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return clock }
	app.lastInput = clock
	return &clock
}

func TestIdleSessionLocksAfterTheConfiguredMinutes(t *testing.T) {
	app := newTestApp(t)
	clock := clocked(t, app)
	app.enterHub()

	*clock = clock.Add(14 * time.Minute)
	app.checkIdle()
	if !app.authed {
		t.Fatal("14 idle minutes must not lock (default limit is 15)")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}) // any key resets the timer
	*clock = clock.Add(14 * time.Minute)
	app.checkIdle()
	if !app.authed {
		t.Fatal("a keypress must restart the idle timer")
	}

	*clock = clock.Add(2 * time.Minute)
	app.checkIdle()
	if app.authed || app.session != nil || app.cur != scrLogin {
		t.Fatalf("15 idle minutes must lock: authed=%v cur=%q", app.authed, app.cur)
	}
	if !strings.Contains(app.message, "locked after 15 minutes idle") {
		t.Errorf("message = %q", app.message)
	}
	if !strings.Contains(auditText(t, app), "SESSION_IDLE_LOCK") {
		t.Error("the idle lock must be audited")
	}
}

func TestIdleLockCanBeTurnedOffOrChanged(t *testing.T) {
	app := newTestApp(t)
	clock := clocked(t, app)
	app.enterHub()
	t.Setenv(config.IdleLockMinutes, "0")
	*clock = clock.Add(10 * time.Hour)
	app.checkIdle()
	if !app.authed {
		t.Fatal("0 minutes turns the idle lock off")
	}
	t.Setenv(config.IdleLockMinutes, "1")
	app.checkIdle()
	if app.authed {
		t.Fatal("a 1 minute limit must lock an hours-idle session")
	}
}

func TestGatewaySignOnClosesWhenLeftIdleButLocalDoesNot(t *testing.T) {
	gw := newTestApp(t)
	gw.authed, gw.session, gw.requireTwoFA = false, nil, true
	clock := clocked(t, gw)
	*clock = clock.Add(signOnTimeout + time.Second)
	if _, cmd := gw.Update(idleTickMsg{}); cmd == nil || !gw.quitting {
		t.Fatal("an idle gateway sign-on must close the connection")
	}

	local := newTestApp(t)
	local.authed, local.session = false, nil
	clock = clocked(t, local)
	*clock = clock.Add(time.Hour)
	local.checkIdle()
	if local.quitting {
		t.Fatal("a local terminal is never closed for idling at sign-on")
	}
}

func TestFifthFailedSignInMarksTheSessionLockedOut(t *testing.T) {
	app := newTestApp(t)
	app.authed, app.session = false, nil
	app.cur = scrLogin
	for i := 0; i < 5; i++ {
		doLoginSubmit(app, []string{"nobody", "wrong"})
	}
	if !app.LockedOut() || !app.quitting {
		t.Fatalf("five failures must end the session as locked out (lockedOut=%v quitting=%v)", app.LockedOut(), app.quitting)
	}
}

func TestSignOnWarnsAboutPlaintextTelnet(t *testing.T) {
	app := newTestApp(t)
	app.authed, app.session, app.cur = false, nil, scrLogin
	app.screens[scrLogin].OnEnter(app)
	if out := plain(app.View()); strings.Contains(out, "telnet") {
		t.Errorf("no note for a local session:\n%s", out)
	}
	app.SetTransport("telnet", "192.168.1.20")
	if out := plain(app.View()); !strings.Contains(out, "Note: telnet is not encrypted") || strings.Contains(out, "WARNING") {
		t.Errorf("a LAN telnet client gets the quiet note:\n%s", out)
	}
	app.SetTransport("telnet", "203.0.113.5")
	if out := plain(app.View()); !strings.Contains(out, "WARNING: telnet is not encrypted") {
		t.Errorf("a public telnet client gets the warning:\n%s", out)
	}
}
