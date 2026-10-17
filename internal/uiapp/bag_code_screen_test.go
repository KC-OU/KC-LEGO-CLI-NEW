package uiapp

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

func seedOneCheckablePart(t *testing.T, app *App) {
	t.Helper()
	seedOfflineCatalog(t, app)
	if _, err := app.legoDB.Exec(`INSERT INTO cat_inventories (id, version, set_num) VALUES (1, 1, '75192-1');
		INSERT INTO cat_inventory_parts (inventory_id, part_num, color_id, quantity) VALUES (1,'3001',4,10)`); err != nil {
		t.Fatal(err)
	}
}

// TestFinishHoldsForTheBagsThisCheckNeedsThenFinishesOnceAllScanned is the
// TUI's half of the live bag-scan gate: pressing F (finishCheck) on a check
// whose parts need a bag (every part needs at least its main/small placement
// — gating is on by default for every set) holds at the prompt until every
// required kind has been scanned in, then finishes.
func TestFinishHoldsForTheBagsThisCheckNeedsThenFinishesOnceAllScanned(t *testing.T) {
	app, _ := flowApp(t)
	app.rebrick = &lego.Client{}
	seedOneCheckablePart(t, app)
	if err := app.legoDB.SetBagSize("3001", lego.BagSmall); err != nil {
		t.Fatal(err)
	}
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	app.cur, app.stack = scrLegoHub, nil
	startCheck(app, "75192-1", lego.CheckIntake)
	st := app.checking
	if st == nil {
		t.Fatal("expected startCheck to populate app.checking")
	}
	for i := range st.check.Lines {
		st.check.Lines[i].Have = st.check.Lines[i].Need
	}

	finishCheck(app)
	if app.cur != scrBagsNeeded {
		t.Fatalf("expected an unscanned small bag to hold at the bags-needed prompt, got screen %q", app.cur)
	}
	if st.check.Status == lego.StatusDone {
		t.Fatal("the check must not be finished yet")
	}

	app.screens[scrBagsNeeded].(*formScreen).submit(app, []string{lego.BagCode("75192-1", lego.BagSmall)})

	if st.check.Status != lego.StatusDone {
		t.Fatalf("expected the check to finish after scanning the one bag it needed, status=%q, screen=%q", st.check.Status, app.cur)
	}
	bags, err := app.legoDB.CheckBags(st.check.ID)
	if err != nil || len(bags) != 1 || bags[0].ScannedAt.IsZero() {
		t.Errorf("CheckBags = %+v, err = %v, want one scanned instance", bags, err)
	}
}

// TestFinishSkipsTheBagPromptWhenGatingIsOffForTheSet confirms the per-set
// admin override (SetBagPolicy ... "off") exempts a set from the live gate
// entirely — same immediate finish as before this feature existed.
func TestFinishSkipsTheBagPromptWhenGatingIsOffForTheSet(t *testing.T) {
	app, _ := flowApp(t)
	app.rebrick = &lego.Client{}
	seedOneCheckablePart(t, app)
	if err := app.legoDB.SetBagPolicy("75192-1", 2, "off"); err != nil {
		t.Fatal(err)
	}
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	app.cur, app.stack = scrLegoHub, nil
	startCheck(app, "75192-1", lego.CheckIntake)
	st := app.checking
	for i := range st.check.Lines {
		st.check.Lines[i].Have = st.check.Lines[i].Need
	}

	finishCheck(app)
	if st.check.Status != lego.StatusDone {
		t.Fatalf("expected a gating-off set to finish immediately, status=%q, screen=%q", st.check.Status, app.cur)
	}
}

// TestWrongBagScanWarnsButStillMarksTheLine is the "warning, not a hard
// block" decision: marking a part while the wrong bag is active still
// applies the mark, just with a flash warning telling the checker to scan a
// different bag.
func TestWrongBagScanWarnsButStillMarksTheLine(t *testing.T) {
	app, _ := flowApp(t)
	app.rebrick = &lego.Client{}
	seedOneCheckablePart(t, app)
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	app.cur, app.stack = scrLegoHub, nil
	startCheck(app, "75192-1", lego.CheckIntake)
	st := app.checking
	if !st.bagGatingOn {
		t.Fatal("expected gating on by default")
	}

	scr := app.screens[scrSetCheck].(*setCheckScreen)
	scr.HandleKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	if scr.flash == "" {
		t.Error("expected a warning flash for marking with no bag scanned yet")
	}
	if st.check.Lines[0].Have != st.check.Lines[0].Need {
		t.Error("the mark must still apply despite the warning — this is a warning, not a block")
	}
}
