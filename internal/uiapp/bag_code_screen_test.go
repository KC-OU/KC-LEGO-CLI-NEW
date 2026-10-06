package uiapp

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

// TestFinishHoldsForABagCodeThenFinishesOnceGiven is the TUI's half of the
// bag-barcode-verification gate: pressing F (finishCheck) on a check with a
// small-bag line and no code yet opens the prompt instead of finishing;
// submitting a code there both records it and completes the finish.
func TestFinishHoldsForABagCodeThenFinishesOnceGiven(t *testing.T) {
	app, _ := flowApp(t)
	app.rebrick = &lego.Client{}
	seedOfflineCatalog(t, app)
	if _, err := app.legoDB.Exec(`INSERT INTO cat_inventories (id, version, set_num) VALUES (1, 1, '75192-1');
		INSERT INTO cat_inventory_parts (inventory_id, part_num, color_id, quantity) VALUES (1,'3001',4,10)`); err != nil {
		t.Fatal(err)
	}
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
	if app.cur != scrBagCodePrompt {
		t.Fatalf("expected a small-bag line with no code to hold at the bag-code prompt, got screen %q", app.cur)
	}
	if st.check.Status == lego.StatusDone {
		t.Fatal("the check must not be finished yet")
	}

	app.screens[scrBagCodePrompt].(*formScreen).submit(app, []string{"BAG-7"})

	if st.check.Status != lego.StatusDone {
		t.Fatalf("expected the check to be finished after supplying a bag code, status=%q", st.check.Status)
	}
	code, err := app.legoDB.CheckBagCode(st.check.ID)
	if err != nil || code != "BAG-7" {
		t.Errorf("CheckBagCode = %q, %v, want BAG-7", code, err)
	}
}

// TestFinishSkipsTheBagPromptWithNoSmallBagLines confirms an ordinary check
// (nothing marked small-bag) finishes straight away, same as before this
// feature existed.
func TestFinishSkipsTheBagPromptWithNoSmallBagLines(t *testing.T) {
	app, _ := flowApp(t)
	app.rebrick = &lego.Client{}
	seedOfflineCatalog(t, app)
	if _, err := app.legoDB.Exec(`INSERT INTO cat_inventories (id, version, set_num) VALUES (1, 1, '75192-1');
		INSERT INTO cat_inventory_parts (inventory_id, part_num, color_id, quantity) VALUES (1,'3001',4,10)`); err != nil {
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
		t.Fatalf("expected an ordinary check to finish immediately, status=%q, screen=%q", st.check.Status, app.cur)
	}
}
