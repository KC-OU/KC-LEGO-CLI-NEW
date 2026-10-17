package uiapp

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/wmsdb"
)

// TestHeaderShowsFullNameAndAccuracy is the always-visible header: every
// screen's user band shows "<id> - <FullName> (<accuracy>%)" once both a
// display name (access.User.FullName) and a scored role are present, not
// just the kanban dashboard — confirmed on a plain, unrelated screen
// (scrOverview) to prove it's not special-cased to one place.
func TestHeaderShowsFullNameAndAccuracy(t *testing.T) {
	app := newTestApp(t)
	setPolicy(t, func(p *access.Policy) {
		p.Users["partdb:7354"] = &access.User{Groups: []string{"checker"}, TwoFA: access.TwoFAExempt, FullName: "Kieran Collins"}
	})
	signOn(app, "7354", "")
	app.cur = scrOverview

	out := plain(app.View())
	if !strings.Contains(out, "7354 - Kieran Collins (100%)") {
		t.Errorf("header missing \"7354 - Kieran Collins (100%%)\":\n%s", out)
	}
}

// TestHeaderFallsBackWithoutFullName confirms an unnamed numeric account
// still shows cleanly (just the ID and accuracy, no dangling " - ").
func TestHeaderFallsBackWithoutFullName(t *testing.T) {
	app := newTestApp(t)
	setPolicy(t, func(p *access.Policy) {
		p.Users["partdb:7355"] = &access.User{Groups: []string{"checker"}, TwoFA: access.TwoFAExempt}
	})
	signOn(app, "7355", "")
	app.cur = scrOverview

	out := plain(app.View())
	if !strings.Contains(out, "7355 (100%)") {
		t.Errorf("header missing \"7355 (100%%)\":\n%s", out)
	}
	if strings.Contains(out, "7355 - ") {
		t.Errorf("an unnamed account shouldn't show a dangling \" - \":\n%s", out)
	}
}

// TestShiftF1OpensKanbanFromHubOnly covers both halves of the binding: it
// opens the right board from each hub, and does nothing from a non-hub
// screen (same "scoped, not global" precedent as F10/F19's own test).
func TestShiftF1OpensKanbanFromHubOnly(t *testing.T) {
	app := newTestApp(t)
	app.session = &auth.Session{Source: "modernwms", Username: "admin", Role: "Admin", Permissions: &wmsdb.Permissions{IsAdmin: true}}
	app.authed = true

	app.cur = scrHub
	app.Update(tea.KeyMsg{Type: tea.KeyF13})
	if app.cur != scrKanbanAdmin {
		t.Errorf("from scrHub, F13 should open scrKanbanAdmin, got %q", app.cur)
	}

	setPolicy(t, func(p *access.Policy) {
		p.Users["partdb:checker3"] = &access.User{Groups: []string{"checker"}, TwoFA: access.TwoFAExempt}
	})
	signOn(app, "checker3", "")
	app.cur = scrPickerHub
	app.Update(tea.KeyMsg{Type: tea.KeyF13})
	if app.cur != scrKanbanBoard {
		t.Errorf("from scrPickerHub, F13 should open scrKanbanBoard, got %q", app.cur)
	}

	app.cur = scrLegoHub
	app.Update(tea.KeyMsg{Type: tea.KeyF13})
	if app.cur != scrLegoHub {
		t.Errorf("F13 should do nothing outside a hub screen, moved to %q", app.cur)
	}
}

// TestShiftF1IsNotAdvertisedInTheFooter matches F19's own precedent: a
// secondary/unlisted binding never appears in the universal footer legend,
// since (unlike every other DefaultFKeys entry) this one only works from the
// two hub screens, not globally.
func TestShiftF1IsNotAdvertisedInTheFooter(t *testing.T) {
	legend := plain(ui.ClassicLegend(ui.New(), ui.DefaultFKeys))
	if strings.Contains(legend, "F13") || strings.Contains(legend, "Shift+F1") {
		t.Errorf("the footer legend should not mention F13/Shift+F1: %q", legend)
	}
}

// TestKanbanBoardReplacesCurrentJobsOnPickerHub confirms the picker hub's
// old "My Current Jobs" entry now opens the kanban board instead (decision:
// replace in place, keep the same catalog key so any saved customization
// referencing it still resolves).
func TestKanbanBoardReplacesCurrentJobsOnPickerHub(t *testing.T) {
	app := newTestApp(t)
	setPolicy(t, func(p *access.Policy) {
		p.Users["partdb:checker3"] = &access.User{Groups: []string{"checker"}, TwoFA: access.TwoFAExempt}
	})
	signOn(app, "checker3", "")

	scr := app.screens[scrPickerHub]
	app.cur = scrPickerHub
	scr.OnEnter(app)
	var found *menuOption
	for _, o := range scr.(*menuScreen).shown(app) {
		if o.Key == "3" {
			opt := o
			found = &opt
		}
	}
	if found == nil || found.Label != "Kanban" {
		t.Fatalf("hotkey 3 = %+v, want label Kanban", found)
	}
	found.Go(app)
	if app.cur != scrKanbanBoard {
		t.Fatalf("cur = %q, want scrKanbanBoard", app.cur)
	}
}

// TestKanbanAdminBoardShowsBothLanesForPickedUser confirms the admin
// per-user filter shows that user's check AND pick cards together (one
// board, swimlanes by kind).
func TestKanbanAdminBoardShowsBothLanesForPickedUser(t *testing.T) {
	app := newTestApp(t)
	app.session = &auth.Session{Source: "modernwms", Username: "admin", Role: "Admin", Permissions: &wmsdb.Permissions{IsAdmin: true}}

	chk, err := app.legoDB.AssignTicket(lego.TicketCheck, "75192-1", "Falcon check", "kc", "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.legoDB.ClaimTicket(chk.ID, "kc"); err != nil {
		t.Fatal(err)
	}
	pick, err := app.legoDB.AssignTicket(lego.TicketOrder, "9", "Order #9 pick", "someone-else", "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.legoDB.ClaimTicket(pick.ID, "someone-else"); err != nil {
		t.Fatal(err)
	}

	app.kanbanFilterUser = "someone-else"
	scr := app.screens[scrKanbanBoardAll].(*menuScreen)
	out := plain(scr.intro(app))
	if !strings.Contains(out, "Order #9 pick") {
		t.Errorf("filtered board missing the picked user's order card:\n%s", out)
	}
	if strings.Contains(out, "Falcon check") {
		t.Errorf("filtered board should not show kc's card when viewing someone-else:\n%s", out)
	}
}

// TestKanbanNoteAddRespectsVisibilityToggle exercises the note form end to
// end: a private note stays private, a shared one logs the admin-activity
// event too.
func TestKanbanNoteAddRespectsVisibilityToggle(t *testing.T) {
	app := newTestApp(t)
	setPolicy(t, func(p *access.Policy) {
		p.Users["partdb:checker3"] = &access.User{Groups: []string{"checker"}, TwoFA: access.TwoFAExempt}
	})
	signOn(app, "checker3", "")

	tk, err := app.legoDB.AssignTicket(lego.TicketCheck, "75192-1", "Falcon", "checker3", "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.legoDB.ClaimTicket(tk.ID, "checker3"); err != nil {
		t.Fatal(err)
	}

	startKanbanNote(app, tk.ID, tk.Label)
	app.screens[scrKanbanNoteAdd].(*formScreen).submit(app, []string{tk.Label, "keeping an eye on this", "no"})
	if app.messageErr {
		t.Fatalf("private note submit failed: %q", app.message)
	}

	startKanbanNote(app, tk.ID, tk.Label)
	app.screens[scrKanbanNoteAdd].(*formScreen).submit(app, []string{tk.Label, "box was already opened", "yes"})
	if app.messageErr {
		t.Fatalf("shared note submit failed: %q", app.message)
	}

	notes, err := app.legoDB.KanbanNotes(tk.ID, "checker3", true)
	if err != nil || len(notes) != 2 {
		t.Fatalf("notes = %+v, %v, want 2", notes, err)
	}
	var sawPrivate, sawShared bool
	for _, n := range notes {
		if n.Visibility == "private" && n.Body == "keeping an eye on this" {
			sawPrivate = true
		}
		if n.Visibility == "shared" && n.Body == "box was already opened" {
			sawShared = true
		}
	}
	if !sawPrivate || !sawShared {
		t.Fatalf("notes = %+v, missing expected private/shared entries", notes)
	}

	events, err := app.legoDB.AdminEvents(10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		if e.Kind == lego.EventKanbanNoteShared {
			found = true
		}
	}
	if !found {
		t.Error("a shared note should log an admin-activity event; a private one should not add an unrelated extra one")
	}
}
