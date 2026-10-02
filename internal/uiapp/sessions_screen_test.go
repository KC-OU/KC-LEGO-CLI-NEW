package uiapp

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

// TestFinishKickFlagsSessionLogsAndNotifies covers the admin side of "kick a
// live session": finishKick itself (the picker steps only gather app.kick's
// fields, same as forceOff's pattern) writes the pending ForceLogoff row,
// logs it to the activity feed, and audits it.
func TestFinishKickFlagsSessionLogsAndNotifies(t *testing.T) {
	app := newTestApp(t)
	app.kick = &kickDraft{sessionID: "deadbeef", username: "checker3"}

	finishKick(app, forceLogoffMessage)

	if app.kick != nil {
		t.Error("the draft should be cleared once the action completes")
	}
	msg, found, err := app.legoDB.ConsumeForceLogoff("deadbeef")
	if err != nil || !found || msg != forceLogoffMessage {
		t.Fatalf("ConsumeForceLogoff = %q, %v, %v, want %q, true, nil", msg, found, err, forceLogoffMessage)
	}

	evs, err := app.legoDB.AdminEvents(10)
	if err != nil || len(evs) == 0 || evs[0].Kind != lego.EventForcedOff {
		t.Fatalf("AdminEvents = %+v, %v, want the newest to be forced_off", evs, err)
	}
	if !strings.Contains(app.message, "checker3") {
		t.Fatalf("confirmation message = %q, want it to mention checker3", app.message)
	}
}

// TestCheckForceLogoffEndsTheKickedSessionAtItsNextIdleTick is the other
// side: the kicked session's own process discovers the flag on its next idle
// tick (checkForceLogoff), same poll-and-clear shape as checkTicketStillMine,
// and ends up back at sign-on showing the admin's message, not the generic
// "Session locked."
func TestCheckForceLogoffEndsTheKickedSessionAtItsNextIdleTick(t *testing.T) {
	app := newTestApp(t)
	setPolicy(t, func(p *access.Policy) {
		p.Users["partdb:checker3"] = &access.User{Groups: []string{"checker"}, TwoFA: access.TwoFAExempt}
	})
	signOn(app, "checker3", "")
	app.sessionID = "feedface"

	if err := app.legoDB.ForceLogoff(app.sessionID, forceLogoffMessage); err != nil {
		t.Fatal(err)
	}

	app.checkForceLogoff()

	if app.authed || app.session != nil {
		t.Fatalf("authed = %v, session = %v, want the kicked session fully signed out", app.authed, app.session)
	}
	if app.message != forceLogoffMessage || !app.messageErr {
		t.Fatalf("message = %q (err=%v), want the admin's message shown as an error-styled notice", app.message, app.messageErr)
	}

	// A second tick finds nothing pending — it must not keep firing.
	app.message = ""
	app.checkForceLogoff()
	if app.message != "" {
		t.Errorf("checkForceLogoff fired again after being consumed, message = %q", app.message)
	}
}

func TestLiveSessionKeysRefusesToKickYourOwnSession(t *testing.T) {
	app := newTestApp(t)
	app.sessionID = "self"
	if err := app.legoDB.Heartbeat("self", "admin", "admin", "tui", "", scrLiveSessions); err != nil {
		t.Fatal(err)
	}

	liveSessionKeys(app, "self", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})

	if app.kick != nil {
		t.Error("kicking your own session must be refused, not start the picker")
	}
	if !strings.Contains(app.message, "own session") {
		t.Errorf("message = %q, want a refusal mentioning your own session", app.message)
	}
}
