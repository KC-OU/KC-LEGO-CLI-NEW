package uiapp

import (
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

// TestMissingExtraBadgeShowsAtTopOfBothViewsEvenAtZero is the direct test
// for the always-visible "MISSING N / EXTRA N" summary: a checker should
// never have to scan the whole table (or guess) to know where a check
// stands, in either the table view or the guided walk -- including the
// moment nothing has been marked yet, when both counts are genuinely zero
// rather than hidden.
func TestMissingExtraBadgeShowsAtTopOfBothViewsEvenAtZero(t *testing.T) {
	app, _ := flowApp(t)
	app.rebrick = &lego.Client{}
	seedOfflineCatalog(t, app)
	for _, q := range []string{
		`INSERT INTO cat_inventories (id, version, set_num) VALUES (1, 1, '75192-1')`,
		`INSERT INTO cat_inventory_parts (inventory_id, part_num, color_id, quantity) VALUES (1,'3001',4,10),(1,'3001',1,6)`,
	} {
		if _, err := app.legoDB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := app.legoDB.SetBagPolicy("75192-1", 2, "off"); err != nil {
		t.Fatal(err)
	}
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	app.cur, app.stack = scrLegoHub, nil
	startCheck(app, "75192-1", lego.CheckIntake)

	// Nothing marked yet: both counts are genuinely zero, shown as such.
	v := plain(app.View())
	if !strings.Contains(v, "MISSING 0") || !strings.Contains(v, "EXTRA 0") {
		t.Fatalf("table view should show the badge at zero, not hide it:\n%s", v)
	}

	typeKeys(app, "m")
	typeKeys(app, "2")
	key(app, tea.KeyEnter) // 2 red missing
	key(app, tea.KeyDown)
	typeKeys(app, "e")
	typeKeys(app, "3")
	key(app, tea.KeyEnter) // 3 blue extra

	v = plain(app.View())
	if !strings.Contains(v, "MISSING 2") || !strings.Contains(v, "EXTRA 3") {
		t.Fatalf("table view badge should reflect the running totals:\n%s", v)
	}

	// Switch to the guided walk: the same running totals at the top, plus
	// the current (blue, extra) line's own EXTRA 3 shown beside it.
	typeKeys(app, "w")
	v = plain(app.View())
	if !strings.Contains(v, "MISSING 2") || !strings.Contains(v, "EXTRA 3") {
		t.Fatalf("guided walk should show the same running totals:\n%s", v)
	}
	if !strings.Contains(v, "EXTRA 3") {
		t.Fatalf("guided walk should show the current line's own extra count too:\n%s", v)
	}
}

// TestTableRowsColourMissingRedAndExtraBlue is the per-row counterpart to the
// badge test above: a checker scanning down the list (not just the top
// summary) should see which specific lines are missing or extra at a
// glance, not just plain "M2"/"E3" text indistinguishable from any other
// column. lipgloss strips colour in this non-TTY test environment by
// default, so the colour profile is forced the same way theme_test.go does,
// and the RAW (unstripped) view is checked instead of plain(app.View()).
func TestTableRowsColourMissingRedAndExtraBlue(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	app, _ := flowApp(t)
	app.rebrick = &lego.Client{}
	seedOfflineCatalog(t, app)
	for _, q := range []string{
		`INSERT INTO cat_inventories (id, version, set_num) VALUES (1, 1, '75192-1')`,
		`INSERT INTO cat_inventory_parts (inventory_id, part_num, color_id, quantity) VALUES (1,'3001',4,10),(1,'3001',1,6)`,
	} {
		if _, err := app.legoDB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := app.legoDB.SetBagPolicy("75192-1", 2, "off"); err != nil {
		t.Fatal(err)
	}
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	app.cur, app.stack = scrLegoHub, nil
	startCheck(app, "75192-1", lego.CheckIntake)

	typeKeys(app, "m")
	typeKeys(app, "2")
	key(app, tea.KeyEnter) // line 0: 2 missing, rendered red
	key(app, tea.KeyDown)
	typeKeys(app, "e")
	typeKeys(app, "3")
	key(app, tea.KeyEnter) // line 1: 3 extra, rendered blue

	raw := app.View()
	wantMiss := app.theme.Danger.Render("M" + strconv.Itoa(2))
	wantExtra := app.theme.Info.Render("E" + strconv.Itoa(3))
	if !strings.Contains(raw, wantMiss) {
		t.Errorf("table view should colour the missing cell red (theme.Danger), got:\n%s", raw)
	}
	if !strings.Contains(raw, wantExtra) {
		t.Errorf("table view should colour the extra cell blue (theme.Info), got:\n%s", raw)
	}
}
