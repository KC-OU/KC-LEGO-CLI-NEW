package uiapp

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestAlertPopupOnlyQDismissesAndOtherMessagesPassThrough(t *testing.T) {
	app := newTestApp(t)
	app.authed = true
	app.queueAlert("Missing parts", "Set 1-1 is missing 3 part(s).")
	app.queueAlert("Missing parts", "Set 2-1 is missing 1 part(s).")

	if v := plain(app.View()); !strings.Contains(v, "Missing parts") || !strings.Contains(v, "Set 1-1 is missing 3 part(s)") {
		t.Fatalf("the front of the queue should show:\n%s", v)
	}

	// A non-input message (a background job finishing, say) must never be swallowed
	// by a queued popup — this is exactly the bug caught while building this.
	if app.closeAlertOn(checkFinishedMsg{}) {
		t.Error("closeAlertOn must return false for a non-input message so it still reaches its own handler")
	}

	// Any key but Q is swallowed, without dismissing.
	if !app.closeAlertOn(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}) {
		t.Fatal("a key other than Q must still be swallowed while a popup is showing")
	}
	if len(app.popups) != 2 {
		t.Fatalf("a non-Q key must not dismiss anything: %d left", len(app.popups))
	}

	// Q dismisses the front one only, revealing the next.
	if !app.closeAlertOn(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}) {
		t.Fatal("Q must be swallowed too (it's what dismisses)")
	}
	if len(app.popups) != 1 {
		t.Fatalf("Q should dismiss exactly the front alert: %d left", len(app.popups))
	}
	if v := plain(app.View()); !strings.Contains(v, "Set 2-1 is missing 1 part(s)") {
		t.Fatalf("the second alert should now show:\n%s", v)
	}

	app.closeAlertOn(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if len(app.popups) != 0 {
		t.Fatal("the queue should be empty after dismissing both")
	}
	if app.closeAlertOn(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}) {
		t.Error("closeAlertOn must return false once nothing is queued")
	}
}

func TestHelpOverlayTakesPriorityOverAQueuedAlert(t *testing.T) {
	app := newTestApp(t)
	app.authed = true
	app.helpOpen = true
	app.queueAlert("Missing parts", "Set 1-1 is missing 1 part(s).")

	if app.closeAlertOn(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}) {
		t.Fatal("while help is open, the alert must wait — closeAlertOn should not react")
	}
	if v := plain(app.View()); strings.Contains(v, "Missing parts") {
		t.Fatalf("help should render instead of the queued alert:\n%s", v)
	}
}
