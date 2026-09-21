package uiapp

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

var sampleRows = [][]string{
	{"3001", "Brick 2 x 4", "Red", "30"},
	{"3023", "Plate 1 x 2", "Blue", "12"},
	{"3024", "Plate 1 x 1", "Red", "8"},
	{"3622", "Brick 1 x 3", "Dark Red", "2"},
}

func TestFilterRowsIsFuzzyAndRanksExactMatchesFirst(t *testing.T) {
	names := func(rows [][]string) string {
		var n []string
		for _, r := range rows {
			n = append(n, r[0])
		}
		return strings.Join(n, ",")
	}
	if got := names(filterRows(sampleRows, "")); got != "3001,3023,3024,3622" {
		t.Errorf("an empty filter keeps everything in order: %s", got)
	}
	if got := names(filterRows(sampleRows, "red")); got != "3001,3024,3622" {
		t.Errorf("red = %s", got)
	}
	if got := names(filterRows(sampleRows, "brick red")); got != "3001,3622" {
		t.Errorf("every word must match: %s", got)
	}
	if got := names(filterRows(sampleRows, "bk 2x4")); got != "3001" {
		t.Errorf("subsequence matching finds Brick 2 x 4 from 'bk 2x4': %s", got)
	}
	if got := names(filterRows(sampleRows, "plt")); got != "3023,3024" {
		t.Errorf("plt = %s", got)
	}
	// "blue" matches 3023 contiguously; nothing else contains those letters in order.
	if got := names(filterRows(sampleRows, "BLUE")); got != "3023" {
		t.Errorf("case-insensitive: %s", got)
	}
	if got := filterRows(sampleRows, "zzzz"); len(got) != 0 {
		t.Errorf("no match = %v", got)
	}
	// A contiguous hit outranks a scattered one.
	rows := [][]string{{"x", "b r i c k"}, {"y", "brick"}}
	if got := filterRows(rows, "brick"); got[0][0] != "y" {
		t.Errorf("contiguous should rank first: %v", got)
	}
}

func typeInto(app *App, s string) {
	for _, r := range s {
		if r == ' ' {
			app.Update(tea.KeyMsg{Type: tea.KeySpace})
		} else {
			app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
	}
}

func TestSlashFiltersAListWithoutTriggeringShortcuts(t *testing.T) {
	app := newTestApp(t)
	app.enterHub()
	for _, p := range []struct{ num, name, cat string }{{"3001", "Brick 2 x 4", "Bricks"}, {"3023", "Plate 1 x 2", "Plates"}, {"3024", "Plate 1 x 1", "Plates"}} {
		if err := app.legoDB.AddOwnedPart(ownedFor(p.num, p.name, p.cat)); err != nil {
			t.Fatal(err)
		}
	}
	app.cur, app.stack = scrLegoHub, nil
	app.goTo(scrLegoPartOwned)
	tbl := app.screens[scrLegoPartOwned].(*tableScreen)
	if len(tbl.rows) != 3 {
		t.Fatalf("rows = %d", len(tbl.rows))
	}
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if !tbl.filtering {
		t.Fatal("/ starts the filter")
	}
	typeInto(app, "plate ") // the letters l, u and a space must be typed, not treated as Lock / Undo
	if !app.authed || app.cur != scrLegoPartOwned {
		t.Fatalf("typing a filter must not trigger shortcuts (authed=%v cur=%q)", app.authed, app.cur)
	}
	if len(tbl.rows) != 2 || !strings.Contains(plain(app.View()), "[/ plate _ : 2 of 3]") {
		t.Fatalf("filter view:\n%s", plain(app.View()))
	}
	app.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	app.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if tbl.filter != "plat" {
		t.Errorf("backspace: %q", tbl.filter)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEnter}) // keep the filter, stop typing
	if tbl.filtering || len(tbl.rows) != 2 {
		t.Fatalf("Enter keeps the filter: filtering=%v rows=%d", tbl.filtering, len(tbl.rows))
	}
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}}) // refresh keeps it too
	if len(tbl.rows) != 2 {
		t.Errorf("refresh must keep the filter, rows = %d", len(tbl.rows))
	}
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	app.Update(tea.KeyMsg{Type: tea.KeyEsc}) // Esc while typing clears it and does NOT go back
	if app.cur != scrLegoPartOwned || tbl.filter != "" || len(tbl.rows) != 3 || tbl.filtering {
		t.Errorf("Esc clears the filter: cur=%q filter=%q rows=%d", app.cur, tbl.filter, len(tbl.rows))
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEsc}) // and with nothing to clear, Esc is Back again
	if app.cur == scrLegoPartOwned {
		t.Error("Esc outside the filter goes back")
	}
}
