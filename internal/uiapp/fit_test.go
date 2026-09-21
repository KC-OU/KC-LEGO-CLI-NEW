package uiapp

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// A screen taller than the terminal scrolls its header off the top, so every screen must fit.
func TestEveryScreenFitsAnEightyByTwentyFiveTerminal(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // screens that list containers must not reach the real docker
	for _, size := range [][2]int{{80, 25}, {80, 24}} {
		app := newTestApp(t)
		app.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		var ids []string
		for id := range app.screens {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("%s panicked: %v", id, r)
					}
				}()
				app.cur, app.stack, app.message = scrHub, nil, ""
				app.goTo(id)
				lines := strings.Split(strings.TrimRight(plain(app.View()), "\n"), "\n")
				if len(lines) > size[1] {
					t.Errorf("%dx%d: screen %q is %d rows tall", size[0], size[1], id, len(lines))
				}
				for i, l := range lines {
					if w := len([]rune(strings.TrimRight(l, " "))); w > size[0] {
						t.Errorf("%dx%d: screen %q row %d is %d columns wide", size[0], size[1], id, i, w)
					}
				}
			}()
		}
	}
}

func TestLongListsPageInsteadOfScrollingTheHeaderOff(t *testing.T) {
	app := newTestApp(t)
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 25})
	var rows [][]string
	for i := 1; i <= 300; i++ {
		rows = append(rows, []string{fmt.Sprintf("row-%03d", i), "name"})
	}
	app.screens["qa_long"] = &tableScreen{panelID: "QALONG", title: "Long", columns: []string{"No", "Name"},
		fetch: func(*App) ([][]string, string, error) { return rows, "300 rows", nil }}
	app.cur, app.stack = scrHub, nil
	app.goTo("qa_long")
	view := func() string { return plain(app.View()) }
	check := func(what, mustHave string) {
		t.Helper()
		out := view()
		if n := len(strings.Split(strings.TrimRight(out, "\n"), "\n")); n > 25 {
			t.Errorf("%s: %d rows on a 25-row screen", what, n)
		}
		if !strings.Contains(out, "KCPARTS") || !strings.Contains(out, mustHave) {
			t.Errorf("%s: header and %q must both be visible:\n%s", what, mustHave, out)
		}
	}
	check("first page", "row-001")
	if !strings.Contains(view(), "rows 1-") || !strings.Contains(view(), "of 300") {
		t.Errorf("the position must be shown:\n%s", view())
	}
	key(app, tea.KeyPgDown)
	check("after PgDn", "row-012")
	key(app, tea.KeyEnd)
	check("at the end", "row-300")
	key(app, tea.KeyDown)
	check("down past the end stays put", "row-300")
	key(app, tea.KeyHome)
	check("home", "row-001")
	key(app, tea.KeyUp)
	check("up past the start stays put", "row-001")
}

func TestAnEmptyValueShowsADashNotUnreachable(t *testing.T) {
	if orDash("") != "—" || orDash("x") != "x" || orUnreachable("") != "unreachable" {
		t.Fatalf("orDash(%q), orUnreachable(%q)", orDash(""), orUnreachable(""))
	}
}
