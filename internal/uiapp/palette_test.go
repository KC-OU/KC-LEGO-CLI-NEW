package uiapp

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func key(app *App, t tea.KeyType) { app.Update(tea.KeyMsg{Type: t}) }

func TestCtrlKOpensThePaletteAndTypingJumpsToAScreen(t *testing.T) {
	app := detailApp(t)
	app.enterHub()
	key(app, tea.KeyCtrlK)
	if !app.paletteOpen {
		t.Fatal("Ctrl-K opens the palette")
	}
	out := plain(app.View())
	for _, want := range []string{"Command Palette", "Go to:", "Overview", "more: keep typing to narrow", "Type to filter"} {
		if !strings.Contains(out, want) {
			t.Errorf("palette lacks %q:\n%s", want, out)
		}
	}
	typeInto(app, "what can") // letters like l, u, g must be typed, not fire shortcuts
	if !app.authed || !app.paletteOpen {
		t.Fatalf("typing must not trigger shortcuts (authed=%v open=%v)", app.authed, app.paletteOpen)
	}
	if out := plain(app.View()); !strings.Contains(out, "> What can I build?") {
		t.Errorf("the best match is selected:\n%s", out)
	}
	key(app, tea.KeyEnter)
	if app.paletteOpen || app.cur != scrLegoBuild {
		t.Errorf("Enter goes there: open=%v cur=%q", app.paletteOpen, app.cur)
	}
	app.onBack()
	if app.cur != scrHub {
		t.Errorf("Back from a palette jump returns to the main menu, got %q", app.cur)
	}
}

func TestF2IsAFallbackAndEscCloses(t *testing.T) {
	app := detailApp(t)
	app.enterHub()
	key(app, tea.KeyF2)
	if !app.paletteOpen {
		t.Fatal("F2 opens it too (some terminals swallow Ctrl-K)")
	}
	typeInto(app, "zzz")
	if out := plain(app.View()); !strings.Contains(out, "nothing matches") {
		t.Errorf("no match:\n%s", out)
	}
	key(app, tea.KeyEnter) // nothing selected: must not crash or close into nothing
	key(app, tea.KeyEsc)
	if app.paletteOpen || app.cur != scrHub {
		t.Errorf("Esc closes and leaves you where you were: open=%v cur=%q", app.paletteOpen, app.cur)
	}
}

func TestPaletteFindsPartsAndSetsFromTheOfflineCatalog(t *testing.T) {
	app := detailApp(t)
	app.enterHub()
	key(app, tea.KeyCtrlK)
	typeInto(app, "falcon")
	out := plain(app.View())
	if !strings.Contains(out, "Set 75192-1 — Millennium Falcon (2017)") {
		t.Fatalf("a set from the catalog:\n%s", out)
	}
	key(app, tea.KeyEnter)
	if app.cur != scrLegoDetail || app.detail == nil || app.detail.Kind != "set" || !strings.Contains(plain(app.View()), "Millennium Falcon") {
		t.Errorf("Enter opens that set's page: cur=%q detail=%+v", app.cur, app.detail)
	}
	app.cur, app.stack = scrHub, nil
	key(app, tea.KeyCtrlK)
	typeInto(app, "brick 2")
	if out := plain(app.View()); !strings.Contains(out, "Part 3001 — Brick 2 x 4") {
		t.Errorf("a part from the catalog:\n%s", out)
	}
	key(app, tea.KeyEnter)
	if app.cur != scrLegoDetail || app.detail.Kind != "part" || app.detail.Num != "3001" {
		t.Errorf("part page: cur=%q detail=%+v", app.cur, app.detail)
	}
}

func TestPaletteListsRecentPartsWhenEmptyAndMovesWithArrows(t *testing.T) {
	app := detailApp(t)
	app.enterHub()
	if err := app.legoDB.AddRecent("3001"); err != nil {
		t.Fatal(err)
	}
	key(app, tea.KeyCtrlK)
	if out := plain(app.View()); !strings.Contains(out, "Recent part 3001") {
		t.Errorf("recent parts come first:\n%s", out)
	}
	key(app, tea.KeyDown)
	if app.paletteSel != 1 {
		t.Errorf("Down moves the selection: %d", app.paletteSel)
	}
	key(app, tea.KeyUp)
	key(app, tea.KeyUp) // stays at the top
	if app.paletteSel != 0 {
		t.Errorf("selection is clamped: %d", app.paletteSel)
	}
	key(app, tea.KeyEnter)
	if app.cur != scrLegoDetail || app.detail.Num != "3001" {
		t.Errorf("the recent part's page: cur=%q", app.cur)
	}
}

func TestPaletteRespectsPermissionsAndActions(t *testing.T) {
	app := detailApp(t)
	app.enterHub()
	app.session.Permissions.IsAdmin = false
	key(app, tea.KeyCtrlK)
	typeInto(app, "settings")
	key(app, tea.KeyEnter)
	if app.cur == scrSettingsHub || app.cur == scrSettingsTheme {
		t.Errorf("a non-admin must not reach settings through the palette (it hides them) (cur=%q msg=%q)", app.cur, app.message)
	}
	for _, e := range app.paletteEntries("settings") {
		if strings.HasPrefix(e.Label, "Settings") {
			t.Errorf("palette lists %q to a non-admin", e.Label)
		}
	}
	app.session.Permissions.CanWrite = false
	app.cur, app.stack = scrHub, nil
	key(app, tea.KeyCtrlK)
	typeInto(app, "create")
	key(app, tea.KeyEnter)
	if app.cur == scrPartDBCreate {
		t.Errorf("a read-only role cannot open a write screen through the palette (cur=%q)", app.cur)
	}
	app.cur, app.stack = scrHub, nil
	key(app, tea.KeyCtrlK)
	typeInto(app, "lock the")
	key(app, tea.KeyEnter)
	if app.authed || app.cur != scrLogin {
		t.Errorf("the Lock action locks: authed=%v cur=%q", app.authed, app.cur)
	}
}

func TestPaletteIsUnavailableBeforeSignOnAndHelpMentionsIt(t *testing.T) {
	app := newTestApp(t)
	app.authed, app.session, app.cur = false, nil, scrLogin
	key(app, tea.KeyCtrlK)
	key(app, tea.KeyF2)
	if app.paletteOpen {
		t.Fatal("no palette at sign-on")
	}
	app2 := detailApp(t)
	app2.enterHub()
	key(app2, tea.KeyF1)
	if out := plain(app2.View()); !strings.Contains(out, "Ctrl-K  or  F2") {
		t.Errorf("the key help lists the palette:\n%s", out)
	}
}

func TestPaletteRanksScreensForWordsAndPartsForNumbersAndKeepsRowsToOneLine(t *testing.T) {
	app := newTestApp(t)
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 25})
	for _, q := range []string{"stat", "history"} {
		e := app.paletteEntries(q)
		if len(e) == 0 || strings.HasPrefix(e[0].Label, "Part ") || strings.HasPrefix(e[0].Label, "Set ") {
			t.Errorf("%q names a screen, which must come before catalog hits: %+v", q, e)
		}
	}
	if _, err := app.legoDB.Exec(`INSERT INTO cat_parts (part_num, name, part_cat_id) VALUES ('3001', 'Brick 2 x 4 with a very very very long name that cannot possibly fit on one palette row of a terminal', 11)`); err != nil {
		t.Fatal(err)
	}
	if _, err := app.legoDB.Exec(`INSERT INTO fts_parts(fts_parts) VALUES ('rebuild')`); err != nil {
		t.Fatal(err)
	}
	if e := app.paletteEntries("3001"); len(e) == 0 || !strings.HasPrefix(e[0].Label, "Part 3001") {
		t.Errorf("a number names a part, which comes first: %+v", e)
	}
	app.paletteOpen, app.paletteInput = true, "3001"
	for _, l := range strings.Split(plain(app.paletteView()), "\n") {
		if strings.Contains(l, "Part 3001") && !strings.Contains(l, "…") {
			t.Errorf("a long entry must be cut with an ellipsis, not wrapped: %q", l)
		}
		if n := len([]rune(l)); n > 80 {
			t.Errorf("row of %d columns: %q", n, l)
		}
	}
}
