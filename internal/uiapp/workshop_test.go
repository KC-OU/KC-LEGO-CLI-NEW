package uiapp

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

func typeKeys(app *App, s string) {
	for _, r := range s {
		app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func TestSetCheckMissingOrderReceiveAndLabel(t *testing.T) {
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
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	app.cur, app.stack = scrLegoHub, nil
	startCheck(app, "75192-1", lego.CheckIntake)
	if app.cur != scrSetCheck {
		t.Fatalf("check did not open: %q", app.message)
	}
	v := plain(app.View())
	if !strings.Contains(v, "Parts check: 75192-1") || !strings.Contains(v, "16 pieces") || !strings.Contains(v, "COMPLETE") {
		t.Fatalf("check screen:\n%s", v)
	}
	// Lines are colour ordered (red, blue, then alphabetical): Red 3001 x10, then Blue 3001 x6.
	typeKeys(app, "m")
	typeKeys(app, "2")
	key(app, tea.KeyEnter) // 2 red missing
	key(app, tea.KeyDown)
	typeKeys(app, "e")
	typeKeys(app, "3")
	key(app, tea.KeyEnter) // 3 blue extra
	if v = plain(app.View()); !strings.Contains(v, "INCOMPLETE — 2 missing") || !strings.Contains(v, "3 extra") {
		t.Fatalf("after marking:\n%s", v)
	}
	// Scanner mode: a scan of a line that is already complete is refused.
	typeKeys(app, "z")
	typeKeys(app, "3001")
	key(app, tea.KeyEnter)
	if v = plain(app.View()); !strings.Contains(v, "3001 Red — 9 of 10") {
		t.Fatalf("scanner should add one to the first short line:\n%s", v)
	}
	key(app, tea.KeyEsc)
	typeKeys(app, "u") // undo the scan
	pressAndDrain(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	if app.cur == scrSetCheck || !strings.Contains(app.message, "INCOMPLETE, 2 missing") || !strings.Contains(app.message, "Part-DB: 2 line(s)") {
		t.Fatalf("finish: cur=%q msg=%q", app.cur, app.message)
	}
	st := app.legoDB.GetSetState("75192-1")
	if st.MissingQty != 2 || st.LastCheck.CheckedBy != "admin" || st.PDBLocationID == 0 {
		t.Fatalf("state = %+v", st)
	}
	if p, _ := app.legoDB.GetOwnedPart("3001", 1, ""); p == nil || p.Qty != 3 {
		t.Fatalf("extras should be loose parts: %+v", p)
	}
	var lots int
	_ = app.pdb.QueryRow(`SELECT COUNT(*) FROM part_lots WHERE id_store_location = ?`, st.PDBLocationID).Scan(&lots)
	if lots != 2 {
		t.Fatalf("Part-DB lots in the set's location = %d", lots)
	}
	if len(app.popups) != 1 || !strings.Contains(app.popups[0].Body, "75192-1 is missing 2 part(s)") {
		t.Fatalf("finishing short should queue an on-screen popup: %+v", app.popups)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}) // any key but Q: must not dismiss it
	if len(app.popups) != 1 {
		t.Fatal("only Q should dismiss the popup")
	}
	typeKeys(app, "q") // dismiss it
	if len(app.popups) != 0 {
		t.Fatal("Q should dismiss the popup")
	}

	// Missing parts → order → received → complete.
	app.ws().set = "75192-1"
	app.goTo(scrSetMissing)
	if v = plain(app.View()); !strings.Contains(v, "3001") || !strings.Contains(v, "Red") {
		t.Fatalf("missing list:\n%s", v)
	}
	typeKeys(app, "o")
	if app.cur != scrOrderEdit {
		t.Fatalf("O should open a new order, on %q (%s)", app.cur, app.message)
	}
	app.screens[scrOrderEdit].(*formScreen).submit(app, []string{"brickowl", "Bricks R Us", "BO-1", "INV-9", "TRK1", "Royal Mail", "GBP", "2.50", ""})
	if app.cur != scrOrderLines {
		t.Fatalf("after saving: %q (%s)", app.cur, app.message)
	}
	id := app.ws().orderID
	setOrderStatus(app, id, "shipped")
	setOrderStatus(app, id, "received")
	drainPending(app)
	if !strings.Contains(app.message, "Set 75192-1 is now COMPLETE") {
		t.Fatalf("receive: %q", app.message)
	}
	if st = app.legoDB.GetSetState("75192-1"); st.MissingQty != 0 {
		t.Fatalf("set still short: %+v", st)
	}

	// A label for it.
	startLabels(app, []string{"75192-1"})
	typeKeys(app, "1")
	if app.exportRes == nil || !strings.HasSuffix(app.exportRes.Path, ".pdf") {
		t.Fatalf("labels: %+v %q", app.exportRes, app.message)
	}
	b, _ := os.ReadFile(app.exportRes.Path)
	if !strings.HasPrefix(string(b), "%PDF") || !strings.Contains(string(b), "COMPLETE") || !strings.Contains(string(b), "admin") {
		t.Fatal("label PDF lacks the status or checker")
	}
}

func TestSetCheckOToggleKeepsStickersOffMissingByDefault(t *testing.T) {
	app, _ := flowApp(t)
	app.rebrick = &lego.Client{}
	seedOfflineCatalog(t, app)
	for _, q := range []string{
		`INSERT INTO cat_categories (id, name) VALUES (58, 'Stickers')`,
		`INSERT INTO cat_parts (part_num, name, part_cat_id) VALUES ('STK1', 'Sticker Sheet', 58)`,
		`INSERT INTO cat_inventories (id, version, set_num) VALUES (1, 1, '75192-1')`,
		`INSERT INTO cat_inventory_parts (inventory_id, part_num, color_id, quantity) VALUES (1,'3001',4,10),(1,'STK1',0,1)`,
	} {
		if _, err := app.legoDB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	app.cur, app.stack = scrLegoHub, nil
	startCheck(app, "75192-1", lego.CheckIntake)
	if v := plain(app.View()); !strings.Contains(v, "optional") {
		t.Fatalf("the sticker line should read optional by default:\n%s", v)
	}
	stkIdx := -1
	for i, l := range app.checking.check.Lines {
		if l.PartNum == "STK1" {
			stkIdx = i
			app.checking.check.Lines[i].Have = 0 // never counted: shouldn't affect completeness
		}
	}
	if stkIdx < 0 {
		t.Fatal("the sticker line is missing from the check")
	}
	if v := plain(app.View()); !strings.Contains(v, "COMPLETE") {
		t.Fatalf("a set short only an optional line must still read complete:\n%s", v)
	}
	scr := app.screens[scrSetCheck].(*setCheckScreen)
	for vi, idx := range scr.visible(app) {
		if idx == stkIdx {
			scr.sel = vi
		}
	}
	typeKeys(app, "o")
	if !strings.Contains(app.message, "required again") {
		t.Fatalf("toggling off should say so: %q", app.message)
	}
	if v := plain(app.View()); !strings.Contains(v, "INCOMPLETE") {
		t.Fatalf("once required again, the short sticker should count:\n%s", v)
	}
	if optional, _ := app.legoDB.IsOptional("STK1", "Stickers"); optional {
		t.Error("the O toggle should persist SetOptional(false)")
	}
}
