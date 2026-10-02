package uiapp

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
)

// TestMessagesShowRightAtSignOn is the "pop up like the new-user guide" fix: a
// message sent while someone was away must be waiting for them the moment they
// reach the hub, shown full-screen (scrMessagesFull), not up to 15 seconds later
// on the next idle tick and not as a corner popup.
func TestMessagesShowRightAtSignOn(t *testing.T) {
	app := newTestApp(t)
	setPolicy(t, func(p *access.Policy) {
		p.Users["partdb:checker3"] = &access.User{Groups: []string{"checker"}, TwoFA: access.TwoFAExempt}
	})
	if err := app.legoDB.SendMessage("admin", "checker3", "welcome back, please recheck 75192-1"); err != nil {
		t.Fatal(err)
	}

	signOn(app, "checker3", "")

	if app.cur != scrMessagesFull {
		t.Fatalf("cur after sign-on with a pending message = %q, want %q", app.cur, scrMessagesFull)
	}
	if len(app.toasts) != 1 {
		t.Fatalf("queued messages = %d, want 1", len(app.toasts))
	}
	if app.toasts[0].Body != "welcome back, please recheck 75192-1" {
		t.Errorf("message body = %q", app.toasts[0].Body)
	}
	out := plain(app.View())
	if !strings.Contains(out, "welcome back, please recheck 75192-1") {
		t.Errorf("full message body should be on screen, not truncated:\n%s", out)
	}
}

// TestMessagesFullScreenAdvancesThenReturns exercises the tour-style paging:
// Enter reads through each message, the last Enter (or Q at any point) returns
// to wherever the screen was reached from.
func TestMessagesFullScreenAdvancesThenReturns(t *testing.T) {
	app := newTestApp(t)
	app.queueMessage(1, "admin", "first message")
	app.queueMessage(2, "admin", "second message")
	app.cur, app.stack = scrOverview, []string{scrHub}
	app.goTo(scrMessagesFull)

	scr := app.screens[scrMessagesFull]
	scr.HandleKey(app, tea.KeyMsg{Type: tea.KeyEnter})
	if len(app.toasts) != 1 || app.toasts[0].Body != "second message" {
		t.Fatalf("after one Enter, toasts = %+v, want just the second message", app.toasts)
	}
	if app.cur != scrMessagesFull {
		t.Fatalf("cur after reading one of two = %q, want to stay on %q", app.cur, scrMessagesFull)
	}

	scr.HandleKey(app, tea.KeyMsg{Type: tea.KeyEnter})
	if len(app.toasts) != 0 {
		t.Errorf("toasts after reading both = %d, want 0", len(app.toasts))
	}
	if app.cur != scrOverview {
		t.Errorf("cur after the last message = %q, want back to where it was reached from (%q)", app.cur, scrOverview)
	}
}

// TestMessageComposeOffersADozenTemplatesAndFreeText covers the picker-based
// compose flow (messageComposeScreen/menuScreen could only dispatch one
// keystroke per option, so it was retired once the template list grew past
// nine entries) — both a numbered quick-message pick and free text must reach
// SendMessage with the right body.
func TestMessageComposeOffersADozenTemplatesAndFreeText(t *testing.T) {
	if len(quickMessages) < 10 {
		t.Fatalf("quickMessages = %d entries, want at least ~12", len(quickMessages))
	}

	app := newTestApp(t)
	app.message2 = &messageDraft{to: "checker3"}
	startMessageCompose(app)
	if app.cur != scrPick {
		t.Fatalf("cur after startMessageCompose = %q, want %q", app.cur, scrPick)
	}

	pickSubmit(app, "3") // "Nice work — keep it up"
	msgs, err := app.legoDB.UndeliveredMessages("checker3")
	if err != nil || len(msgs) != 1 || msgs[0].Body != quickMessages[2].Label {
		t.Fatalf("UndeliveredMessages = %+v, %v, want one message with body %q", msgs, err, quickMessages[2].Label)
	}
	_ = app.legoDB.MarkDelivered(msgs[0].ID)

	app.message2 = &messageDraft{to: "checker3"}
	startMessageCompose(app)
	pickSubmit(app, "running a bit behind, back in ten")
	msgs, err = app.legoDB.UndeliveredMessages("checker3")
	if err != nil || len(msgs) != 1 || msgs[0].Body != "running a bit behind, back in ten" {
		t.Fatalf("UndeliveredMessages after free text = %+v, %v", msgs, err)
	}
}

func TestMessagesFullScreenQDismissesAllAtOnce(t *testing.T) {
	app := newTestApp(t)
	app.queueMessage(1, "admin", "first message")
	app.queueMessage(2, "admin", "second message")
	app.cur, app.stack = scrOverview, []string{scrHub}
	app.goTo(scrMessagesFull)

	app.screens[scrMessagesFull].HandleKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})

	if len(app.toasts) != 0 {
		t.Errorf("Q should dismiss every queued message at once, got %d left", len(app.toasts))
	}
	if app.cur != scrOverview {
		t.Errorf("cur after Q = %q, want back to where it was reached from (%q)", app.cur, scrOverview)
	}
}
