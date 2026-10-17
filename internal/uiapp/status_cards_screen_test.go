package uiapp

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

// TestStatusCardNoteScreenSetsListsAndClears drives the "put a card on
// someone" flow directly (the user/reason pickers themselves reuse
// startPick, already covered by the picker's own tests), then checks the
// list screen shows it and the C key clears it.
func TestStatusCardNoteScreenSetsListsAndClears(t *testing.T) {
	app, _ := flowApp(t)

	app.statusCard = &statusCardDraft{username: "alex", reason: lego.CardDelay}
	app.goTo(scrStatusCardNote)
	app.screens[scrStatusCardNote].(*formScreen).submit(app, []string{"waiting on parts"})

	if app.statusCard != nil {
		t.Error("the draft should be cleared after saving")
	}
	if !strings.Contains(app.message, "Delay") {
		t.Fatalf("expected a confirmation mentioning Delay, got %q", app.message)
	}

	_, rows, keys := statusCardRows(app)
	if len(rows) != 1 || keys[0] != "alex" || !strings.Contains(rows[0][1], "Delay") {
		t.Fatalf("statusCardRows = %+v, keys %+v", rows, keys)
	}

	statusCardKeys(app, "alex", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if c, err := app.legoDB.ActiveStatusCard("alex"); err != nil || c != nil {
		t.Fatalf("ActiveStatusCard after clearing = %+v, %v, want nil", c, err)
	}
	_, rows, _ = statusCardRows(app)
	if len(rows) != 0 {
		t.Errorf("statusCardRows after clearing = %+v, want none", rows)
	}
}

// TestJobQueueVKeyOpensStatusCardsAndShowsTheCardColumn confirms the Open
// Assignments queue surfaces an active card next to its assignee, and the
// V key opens the status-cards screen (see the Admin hub's terminal-fit
// budget -- this was deliberately NOT added as its own hub entry).
func TestJobQueueVKeyOpensStatusCardsAndShowsTheCardColumn(t *testing.T) {
	app, _ := flowApp(t)
	if _, err := app.legoDB.SetStatusCard("alex", lego.CardNoWork, "", "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.legoDB.AssignTicket(lego.TicketCheck, "75192-1", "Millennium Falcon", "alex", "", "", "admin"); err != nil {
		t.Fatal(err)
	}

	app.cur, app.stack = scrHub, nil
	app.goTo(scrJobQueue)
	rows, _, err := app.screens[scrJobQueue].(*tableScreen).fetch(app)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if r[2] == "alex" && strings.Contains(r[4], "No work available") {
			found = true
		}
	}
	if !found {
		t.Fatalf("Open Assignments rows = %+v, want alex's card shown", rows)
	}

	app.screens[scrJobQueue].(*tableScreen).extra(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	if app.cur != scrStatusCards {
		t.Fatalf("expected V to open scrStatusCards, got %q", app.cur)
	}
}
