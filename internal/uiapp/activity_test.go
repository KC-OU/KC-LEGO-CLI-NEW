package uiapp

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

func TestAdminEventsCKeyGoesToTheClearConfirmScreen(t *testing.T) {
	app := newTestApp(t)
	app.goTo(scrAdminEvents)

	scr := app.screens[scrAdminEvents].(*tableScreen)
	scr.extra(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if app.cur != scrAdminEventsClear {
		t.Fatalf("cur = %q, want scrAdminEventsClear", app.cur)
	}
}

func TestAdminEventsClearRequiresTypingYes(t *testing.T) {
	app := newTestApp(t)
	if err := app.legoDB.LogEvent(lego.EventMessage, "admin", "pat", "hi"); err != nil {
		t.Fatal(err)
	}

	app.goTo(scrAdminEventsClear)
	app.screens[scrAdminEventsClear].(*formScreen).submit(app, []string{"nope"})

	evs, err := app.legoDB.AdminEvents(10)
	if err != nil || len(evs) != 1 {
		t.Fatalf("a wrong confirmation must not clear anything: AdminEvents = %+v, %v", evs, err)
	}
}

func TestAdminEventsClearWipesTheFeed(t *testing.T) {
	app := newTestApp(t)
	if err := app.legoDB.LogEvent(lego.EventMessage, "admin", "pat", "hi"); err != nil {
		t.Fatal(err)
	}
	if err := app.legoDB.LogEvent(lego.EventMessage, "admin", "dave", "bye"); err != nil {
		t.Fatal(err)
	}

	app.goTo(scrAdminEventsClear)
	app.screens[scrAdminEventsClear].(*formScreen).submit(app, []string{"yes"})

	evs, err := app.legoDB.AdminEvents(10)
	if err != nil || len(evs) != 0 {
		t.Fatalf("AdminEvents = %+v, %v, want none left after confirming", evs, err)
	}
	if !auditHas(t, "ADMIN_EVENTS_CLEARED") {
		t.Error("expected an audit log entry")
	}
}
