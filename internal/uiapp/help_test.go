package uiapp

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func keyRune(app *App, r rune) { app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}) }

func TestQuestionMarkOpensThePerScreenKeySheetAndAnyKeyClosesIt(t *testing.T) {
	app := newTestApp(t)
	app.enterHub()
	keyRune(app, '?')
	out := plain(app.View())
	for _, want := range []string{"Keys", "On this screen", "choose the numbered option", "F10  or  L", "Press any key to close"} {
		if !strings.Contains(out, want) {
			t.Errorf("help sheet missing %q:\n%s", want, out)
		}
	}
	cur := app.cur
	keyRune(app, 'l') // would lock if it reached the app: it must only close the sheet
	if app.helpOpen || !app.authed || app.cur != cur {
		t.Fatalf("the closing key must be swallowed (help=%v authed=%v cur=%q)", app.helpOpen, app.authed, app.cur)
	}
}

func TestF1WorksOnFormsWhereAQuestionMarkIsText(t *testing.T) {
	app := newTestApp(t)
	app.enterHub()
	app.goTo(scrLegoPartAdd)
	keyRune(app, '?')
	if app.helpOpen {
		t.Fatal("a ? typed into a form is text, not a help request")
	}
	if got := app.screens[scrLegoPartAdd].ActiveForm().Fields[0].Value; got != "?" {
		t.Errorf("field = %q", got)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyF1})
	if !app.helpOpen || !strings.Contains(plain(app.View()), "accept the field and go to the next") {
		t.Fatalf("F1 must open the form's help:\n%s", plain(app.View()))
	}
}

func TestHelpIsNotAvailableBeforeSignOn(t *testing.T) {
	app := newTestApp(t)
	app.authed, app.session, app.cur = false, nil, scrLogin
	app.Update(tea.KeyMsg{Type: tea.KeyF1})
	if app.helpOpen {
		t.Fatal("no help sheet on the sign-on screen")
	}
}

func TestEveryScreenHasAHelpSheetThatRenders(t *testing.T) {
	app := newTestApp(t)
	for _, id := range smokeTestScreens {
		app.cur, app.stack = scrHub, nil
		app.goTo(id)
		app.helpOpen = true
		if out := plain(app.View()); !strings.Contains(out, "Everywhere") {
			t.Errorf("%s: help sheet did not render", id)
		}
		app.helpOpen = false
	}
}
